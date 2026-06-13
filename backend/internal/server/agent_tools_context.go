package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type bundleIndexRequest struct {
	Kind      string `json:"kind"`
	ProjectID int64  `json:"project_id"`
	ConceptID int64  `json:"concept_id"`
}

type bundleIndexResponse struct {
	Kind            string            `json:"kind"`
	EntityID        int64             `json:"entity_id"`
	MessageCount    int               `json:"message_count"`
	EstimatedTokens int64             `json:"estimated_tokens"`
	Messages        []bundleIndexItem `json:"messages"`
	Instructions    string            `json:"instructions"`
}

type bundleIndexItem struct {
	MessageID           int64  `json:"message_id"`
	Date                string `json:"date"`
	SenderCanonicalName string `json:"sender_canonical_name"`
	SenderPrimaryEmail  string `json:"sender_primary_email"`
	Subject             string `json:"subject"`
	Snippet             string `json:"snippet"`
	Direction           string `json:"direction,omitempty"`
	IsNewsletter        bool   `json:"is_newsletter,omitempty"`
	NewsletterSlug      string `json:"newsletter_slug,omitempty"`
	QueryTerm           string `json:"query_term,omitempty"`
	ThreadID            int64  `json:"thread_id,omitempty"`
}

func (s *Server) handleAgentGetBundleIndex(w http.ResponseWriter, r *http.Request) {
	var req bundleIndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resp, err := s.getBundleIndex(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getBundleIndex(ctx context.Context, req bundleIndexRequest) (bundleIndexResponse, error) {
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		switch {
		case req.ProjectID > 0:
			kind = "project"
		case req.ConceptID > 0:
			kind = "concept"
		}
	}
	switch kind {
	case "project":
		if req.ProjectID <= 0 {
			return bundleIndexResponse{}, fmt.Errorf("project_id is required")
		}
		return s.getProjectBundleIndex(ctx, req.ProjectID)
	case "concept":
		if req.ConceptID <= 0 {
			return bundleIndexResponse{}, fmt.Errorf("concept_id is required")
		}
		return s.getConceptBundleIndex(ctx, req.ConceptID)
	default:
		return bundleIndexResponse{}, fmt.Errorf("kind must be project or concept")
	}
}

func (s *Server) getProjectBundleIndex(ctx context.Context, projectID int64) (bundleIndexResponse, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH account_emails AS (
			SELECT lower(identifier) AS email
			FROM sources
			WHERE identifier LIKE '%@%'
		),
		account_participants AS (
			SELECT id FROM participants WHERE lower(email_address) IN (SELECT email FROM account_emails)
		)
		SELECT
			m.id,
			COALESCE(m.sent_at, ''),
			COALESCE(mp.canonical_name, p.display_name, p.email_address, ''),
			COALESCE(p.email_address, ''),
			COALESCE(m.subject, ''),
			COALESCE(m.snippet, ''),
			CASE
				WHEN m.sender_id IN (SELECT id FROM account_participants) THEN 'from_account'
				WHEN EXISTS (
					SELECT 1 FROM message_recipients mr
					WHERE mr.message_id = m.id
					  AND mr.participant_id IN (SELECT id FROM account_participants)
				) THEN 'to_account'
				ELSE 'other'
			END,
			COALESCE(m.conversation_id, 0)
		FROM memento_project_message mpm
		JOIN messages m ON m.id = mpm.message_id
		LEFT JOIN participants p ON p.id = m.sender_id
		LEFT JOIN memento_person_email mpe ON mpe.email_address = lower(p.email_address)
		LEFT JOIN memento_person mp ON mp.id = mpe.person_id
		WHERE mpm.project_id = ?
		ORDER BY m.sent_at ASC, m.id ASC`, projectID)
	if err != nil {
		return bundleIndexResponse{}, err
	}
	defer rows.Close()

	var items []bundleIndexItem
	for rows.Next() {
		var item bundleIndexItem
		if err := rows.Scan(
			&item.MessageID, &item.Date, &item.SenderCanonicalName, &item.SenderPrimaryEmail,
			&item.Subject, &item.Snippet, &item.Direction, &item.ThreadID,
		); err != nil {
			return bundleIndexResponse{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return bundleIndexResponse{}, err
	}
	return bundleIndexResponse{
		Kind:            "project",
		EntityID:        projectID,
		MessageCount:    len(items),
		EstimatedTokens: estimateJSONTokens(items),
		Messages:        items,
		Instructions:    "Use this index to choose relevant message_ids, then call get_message_batch for only the bodies needed by each section.",
	}, nil
}

func (s *Server) getConceptBundleIndex(ctx context.Context, conceptID int64) (bundleIndexResponse, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			m.id,
			COALESCE(m.sent_at, ''),
			COALESCE(mp.canonical_name, p.display_name, p.email_address, ''),
			COALESCE(p.email_address, ''),
			COALESCE(m.subject, ''),
			COALESCE(m.snippet, ''),
			CASE WHEN mns.id IS NOT NULL THEN 1 ELSE 0 END,
			COALESCE(mns.slug, ''),
			COALESCE(mcm.query_term, ''),
			COALESCE(m.conversation_id, 0)
		FROM memento_concept_message mcm
		JOIN messages m ON m.id = mcm.message_id
		LEFT JOIN participants p ON p.id = m.sender_id
		LEFT JOIN memento_person_email mpe ON mpe.email_address = lower(p.email_address)
		LEFT JOIN memento_person mp ON mp.id = mpe.person_id
		LEFT JOIN memento_newsletter_source mns ON lower(mns.sender_email) = lower(p.email_address)
		WHERE mcm.concept_id = ?
		ORDER BY m.sent_at ASC, m.id ASC`, conceptID)
	if err != nil {
		return bundleIndexResponse{}, err
	}
	defer rows.Close()

	var items []bundleIndexItem
	for rows.Next() {
		var item bundleIndexItem
		var isNewsletter int
		if err := rows.Scan(
			&item.MessageID, &item.Date, &item.SenderCanonicalName, &item.SenderPrimaryEmail,
			&item.Subject, &item.Snippet, &isNewsletter, &item.NewsletterSlug, &item.QueryTerm,
			&item.ThreadID,
		); err != nil {
			return bundleIndexResponse{}, err
		}
		item.IsNewsletter = isNewsletter != 0
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return bundleIndexResponse{}, err
	}
	return bundleIndexResponse{
		Kind:            "concept",
		EntityID:        conceptID,
		MessageCount:    len(items),
		EstimatedTokens: estimateJSONTokens(items),
		Messages:        items,
		Instructions:    "Cluster or scan this index first, then call get_message_batch for only the bodies needed by each theme.",
	}, nil
}

type messageBatchRequest struct {
	MessageIDs     []int64 `json:"message_ids"`
	IncludeBody    bool    `json:"include_body"`
	BodyCharLimit  int     `json:"body_char_limit"`
	IncludeHeaders bool    `json:"include_headers"`
}

type messageBatchItem struct {
	MessageID  int64    `json:"message_id"`
	Subject    string   `json:"subject"`
	Snippet    string   `json:"snippet"`
	Date       string   `json:"date"`
	FromEmail  string   `json:"from_email"`
	FromName   string   `json:"from_name"`
	Recipients []string `json:"recipients,omitempty"`
	ThreadID   int64    `json:"thread_id"`
	BodyText   string   `json:"body_text,omitempty"`
}

func (s *Server) handleAgentGetMessageBatch(w http.ResponseWriter, r *http.Request) {
	var req messageBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.getMessageBatch(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getMessageBatch(ctx context.Context, req messageBatchRequest) ([]messageBatchItem, error) {
	ids := uniquePositiveIDs(req.MessageIDs, 25)
	if len(ids) == 0 {
		return []messageBatchItem{}, nil
	}
	bodyLimit := req.BodyCharLimit
	if bodyLimit <= 0 {
		bodyLimit = 1200
	}
	if bodyLimit > 4000 {
		bodyLimit = 4000
	}
	placeholders, args := buildInClause(ids)
	bodyExpr := "''"
	if req.IncludeBody {
		bodyExpr = "COALESCE(mb.body_text, '')"
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id,
		       COALESCE(m.subject, ''),
		       COALESCE(m.snippet, ''),
		       COALESCE(m.sent_at, ''),
		       COALESCE(p.email_address, ''),
		       COALESCE(p.display_name, ''),
		       COALESCE(m.conversation_id, 0),
		       %s
		FROM messages m
		LEFT JOIN participants p ON p.id = m.sender_id
		LEFT JOIN message_bodies mb ON mb.message_id = m.id
		WHERE m.id IN (%s)`, bodyExpr, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[int64]messageBatchItem, len(ids))
	for rows.Next() {
		var item messageBatchItem
		if err := rows.Scan(
			&item.MessageID, &item.Subject, &item.Snippet, &item.Date,
			&item.FromEmail, &item.FromName, &item.ThreadID, &item.BodyText,
		); err != nil {
			return nil, err
		}
		if len(item.BodyText) > bodyLimit {
			item.BodyText = item.BodyText[:bodyLimit] + " [...]"
		}
		byID[item.MessageID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if req.IncludeHeaders {
		if err := loadRecipients(ctx, s.db, byID); err != nil {
			return nil, err
		}
	}
	out := make([]messageBatchItem, 0, len(ids))
	for _, id := range ids {
		if item, ok := byID[id]; ok {
			out = append(out, item)
		}
	}
	return out, nil
}

type summarizeThreadRequest struct {
	ThreadID    int64 `json:"thread_id"`
	MaxMessages int   `json:"max_messages"`
}

type threadDigestRow struct {
	id      int64
	subject string
	date    string
	snippet string
}

type threadDigest struct {
	ThreadID         int64    `json:"thread_id"`
	Subject          string   `json:"subject"`
	MessageCount     int      `json:"message_count"`
	FirstDate        string   `json:"first_date,omitempty"`
	LastDate         string   `json:"last_date,omitempty"`
	MessageIDs       []int64  `json:"message_ids"`
	Participants     []string `json:"participants"`
	Representative   []string `json:"representative_snippets"`
	EstimatedTokens  int64    `json:"estimated_tokens"`
	NextStepGuidance string   `json:"next_step_guidance"`
}

func (s *Server) handleAgentSummarizeThread(w http.ResponseWriter, r *http.Request) {
	var req summarizeThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	digest, err := s.summarizeThread(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, digest)
}

func (s *Server) summarizeThread(ctx context.Context, req summarizeThreadRequest) (threadDigest, error) {
	if req.ThreadID <= 0 {
		return threadDigest{}, fmt.Errorf("thread_id is required")
	}
	maxMessages := req.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 12
	}
	if maxMessages > 30 {
		maxMessages = 30
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(subject, ''), COALESCE(sent_at, ''), COALESCE(snippet, '')
		FROM messages
		WHERE conversation_id = ?
		ORDER BY sent_at ASC, id ASC`, req.ThreadID)
	if err != nil {
		return threadDigest{}, err
	}
	defer rows.Close()

	var all []threadDigestRow
	for rows.Next() {
		var r threadDigestRow
		if err := rows.Scan(&r.id, &r.subject, &r.date, &r.snippet); err != nil {
			return threadDigest{}, err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return threadDigest{}, err
	}
	if len(all) == 0 {
		return threadDigest{}, fmt.Errorf("thread %d has no messages", req.ThreadID)
	}
	digest := threadDigest{
		ThreadID:         req.ThreadID,
		Subject:          all[0].subject,
		MessageCount:     len(all),
		FirstDate:        all[0].date,
		LastDate:         all[len(all)-1].date,
		NextStepGuidance: "Call get_message_batch with selected message_ids if this thread is relevant enough to cite.",
	}
	for _, r := range all {
		digest.MessageIDs = append(digest.MessageIDs, r.id)
	}
	for _, r := range representativeRows(all, maxMessages) {
		s := strings.TrimSpace(fmt.Sprintf("%s [msg:%d] %s", r.date, r.id, r.snippet))
		if s != "" {
			digest.Representative = append(digest.Representative, s)
		}
	}
	participants, err := threadParticipants(ctx, s.db, digest.MessageIDs)
	if err == nil {
		digest.Participants = participants
	}
	digest.EstimatedTokens = estimateJSONTokens(digest)
	return digest, nil
}

func representativeRows(rows []threadDigestRow, max int) []threadDigestRow {
	if len(rows) <= max {
		return rows
	}
	out := make([]threadDigestRow, 0, max)
	head := max / 3
	tail := max / 3
	mid := max - head - tail
	out = append(out, rows[:head]...)
	step := len(rows) / (mid + 1)
	for i := 1; i <= mid; i++ {
		out = append(out, rows[i*step])
	}
	out = append(out, rows[len(rows)-tail:]...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].date < out[j].date || (out[i].date == out[j].date && out[i].id < out[j].id)
	})
	return out
}

func uniquePositiveIDs(ids []int64, max int) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func loadRecipients(ctx context.Context, db *sql.DB, items map[int64]messageBatchItem) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	placeholders, args := buildInClause(ids)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT mr.message_id, COALESCE(p.email_address, '')
		FROM message_recipients mr
		LEFT JOIN participants p ON p.id = mr.participant_id
		WHERE mr.message_id IN (%s)
		  AND mr.recipient_type IN ('to','cc','bcc')
		  AND COALESCE(p.email_address, '') <> ''
		ORDER BY mr.message_id, mr.recipient_type`, placeholders), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return err
		}
		item := items[id]
		item.Recipients = append(item.Recipients, email)
		items[id] = item
	}
	return rows.Err()
}

func threadParticipants(ctx context.Context, db *sql.DB, messageIDs []int64) ([]string, error) {
	if len(messageIDs) == 0 {
		return []string{}, nil
	}
	placeholders, args := buildInClause(messageIDs)
	q := fmt.Sprintf(`
		SELECT DISTINCT COALESCE(p.email_address, '')
		FROM (
		  SELECT sender_id AS pid FROM messages WHERE id IN (%s)
		  UNION
		  SELECT participant_id AS pid FROM message_recipients WHERE message_id IN (%s)
		) u
		LEFT JOIN participants p ON p.id = u.pid
		WHERE COALESCE(p.email_address, '') <> ''`, placeholders, placeholders)
	args2 := append(append([]any{}, args...), args...)
	rows, err := db.QueryContext(ctx, q, args2...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		out = append(out, email)
	}
	if out == nil {
		out = []string{}
	}
	return out, rows.Err()
}

func estimateJSONTokens(v any) int64 {
	raw, _ := json.Marshal(v)
	return int64((len(raw) + 3) / 4)
}
