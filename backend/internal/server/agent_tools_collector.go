// Package server — token-gated agent-tool endpoints for the collector
// agent. These are the read-only data lookups the Go agent runner's tool
// registry fans out to.
package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"memento/backend/internal/msgvaultapi"
)

// ---- POST /api/internal/agent-tools/fts-search ----

type ftsSearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type messageHit struct {
	MessageID int64  `json:"message_id"`
	Subject   string `json:"subject"`
	Snippet   string `json:"snippet"`
	Date      string `json:"date"`
	FromEmail string `json:"from_email,omitempty"`
	FromName  string `json:"from_name,omitempty"`
	ThreadID  int64  `json:"thread_id,omitempty"`
}

func (s *Server) handleAgentFTSSearch(w http.ResponseWriter, r *http.Request) {
	var req ftsSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("query is required"))
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	ids, err := runMsgvaultSearchCLI(r.Context(), req.Query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, []messageHit{})
		return
	}

	hits, err := enrichMessageIDs(r.Context(), s.reader.DB(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

// ---- POST /api/internal/agent-tools/vector-search ----

type vectorSearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (s *Server) handleAgentVectorSearch(w http.ResponseWriter, r *http.Request) {
	var req vectorSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("query is required"))
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	ids, err := runMsgvaultVectorSearchCLI(r.Context(), req.Query, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, []messageHit{})
		return
	}

	hits, err := enrichMessageIDs(r.Context(), s.reader.DB(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

// runMsgvaultVectorSearchCLI uses the msgvault HTTP API when configured, then
// falls back to `msgvault search` with mode vector.
// Returns the matched message ids.
func runMsgvaultVectorSearchCLI(ctx context.Context, query string, limit int) ([]int64, error) {
	if client, ok := msgvaultapi.FromEnv(); ok {
		if ids, err := client.SearchIDs(ctx, query, "vector", limit); err == nil {
			return ids, nil
		}
	}

	limitStr := fmt.Sprintf("%d", limit)
	cmd := exec.CommandContext(ctx, "msgvault", "search", query,
		"--mode", "vector", "--json", "--limit", limitStr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("msgvault vector search failed: %w (stderr: %s)", err, stderr.String())
	}
	ids := parseMsgvaultSearchOutput(stdout.Bytes())
	return ids, nil
}

// runMsgvaultSearchCLI uses the msgvault HTTP API when configured, then falls
// back to `msgvault search` with hybrid->FTS fallback. Returns the matched
// message ids in score order.
func runMsgvaultSearchCLI(ctx context.Context, query string, limit int) ([]int64, error) {
	if client, ok := msgvaultapi.FromEnv(); ok {
		modes := []string{"hybrid", "fts"}
		if msgvaultapi.RequiresFTSMode(query) {
			modes = []string{"fts"}
		}
		for _, mode := range modes {
			if ids, err := client.SearchIDs(ctx, query, mode, limit); err == nil {
				return ids, nil
			}
		}
	}

	limitStr := fmt.Sprintf("%d", limit)
	modes := []string{"hybrid", "fts"}
	if msgvaultapi.RequiresFTSMode(query) {
		modes = []string{"fts"}
	}
	for _, mode := range modes {
		cmd := exec.CommandContext(ctx, "msgvault", "search", query,
			"--mode", mode, "--json", "--limit", limitStr)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			if mode == "fts" {
				return nil, fmt.Errorf("msgvault search failed: %w (stderr: %s)", err, stderr.String())
			}
			continue
		}
		ids := parseMsgvaultSearchOutput(stdout.Bytes())
		return ids, nil
	}
	return nil, fmt.Errorf("msgvault search exhausted both modes")
}

func parseMsgvaultSearchOutput(raw []byte) []int64 {
	idx := bytes.IndexAny(raw, "{[")
	if idx >= 0 {
		raw = raw[idx:]
	}
	// Hybrid: {"results":[{"id":...}, ...]}
	var hybrid struct {
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &hybrid); err == nil && len(hybrid.Results) > 0 {
		out := make([]int64, 0, len(hybrid.Results))
		for _, r := range hybrid.Results {
			out = append(out, r.ID)
		}
		return out
	}
	// FTS: [{"id":...}, ...]
	var fts []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &fts); err == nil {
		out := make([]int64, 0, len(fts))
		for _, r := range fts {
			out = append(out, r.ID)
		}
		return out
	}
	return nil
}

// enrichMessageIDs takes a set of message ids and joins back to messages +
// participants for human-readable hit details. Preserves the input order.
func enrichMessageIDs(ctx context.Context, db *sql.DB, ids []int64) ([]messageHit, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders, args := buildInClause(ids)
	q := fmt.Sprintf(`
		SELECT m.id,
		       COALESCE(m.subject, ''),
		       COALESCE(m.snippet, ''),
		       COALESCE(m.sent_at, ''),
		       COALESCE(p.email_address, ''),
		       COALESCE(p.display_name, ''),
		       COALESCE(m.conversation_id, 0)
		FROM messages m
		LEFT JOIN participants p ON p.id = m.sender_id
		WHERE m.id IN (%s)`, placeholders)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[int64]messageHit, len(ids))
	for rows.Next() {
		var h messageHit
		if err := rows.Scan(&h.MessageID, &h.Subject, &h.Snippet, &h.Date, &h.FromEmail, &h.FromName, &h.ThreadID); err != nil {
			return nil, err
		}
		byID[h.MessageID] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]messageHit, 0, len(ids))
	for _, id := range ids {
		if h, ok := byID[id]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}

// ---- POST /api/internal/agent-tools/get-message ----

type getMessageRequest struct {
	MessageID int64 `json:"message_id"`
}

type messageDetail struct {
	MessageID  int64    `json:"message_id"`
	Subject    string   `json:"subject"`
	Snippet    string   `json:"snippet"`
	Date       string   `json:"date"`
	FromEmail  string   `json:"from_email"`
	FromName   string   `json:"from_name"`
	Recipients []string `json:"recipients"`
	ThreadID   int64    `json:"thread_id"`
}

func (s *Server) handleAgentGetMessage(w http.ResponseWriter, r *http.Request) {
	var req getMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.MessageID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("message_id is required"))
		return
	}

	d, err := s.getMessageDetailForAgent(r.Context(), req.MessageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("message %d not found", req.MessageID))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) getMessageDetailForAgent(ctx context.Context, messageID int64) (messageDetail, error) {
	if messageID <= 0 {
		return messageDetail{}, fmt.Errorf("message_id is required")
	}
	db := s.reader.DB()
	var d messageDetail
	err := db.QueryRowContext(ctx, `
		SELECT m.id,
		       COALESCE(m.subject, ''),
		       COALESCE(m.snippet, ''),
		       COALESCE(m.sent_at, ''),
		       COALESCE(p.email_address, ''),
		       COALESCE(p.display_name, ''),
		       COALESCE(m.conversation_id, 0)
		FROM messages m
		LEFT JOIN participants p ON p.id = m.sender_id
		WHERE m.id = ?`, messageID,
	).Scan(&d.MessageID, &d.Subject, &d.Snippet, &d.Date, &d.FromEmail, &d.FromName, &d.ThreadID)
	if err != nil {
		return messageDetail{}, err
	}

	// Recipients (to/cc/bcc emails). Best-effort; missing rows = empty list.
	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(p.email_address, '')
		FROM message_recipients mr
		LEFT JOIN participants p ON p.id = mr.participant_id
		WHERE mr.message_id = ? AND mr.recipient_type IN ('to','cc','bcc')`, messageID,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var e string
			if err := rows.Scan(&e); err == nil && e != "" {
				d.Recipients = append(d.Recipients, e)
			}
		}
	}
	return d, nil
}

// ---- POST /api/internal/agent-tools/find-people ----

type findPeopleRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type personHit struct {
	PersonID     int64  `json:"person_id"`
	DisplayName  string `json:"display_name"`
	PrimaryEmail string `json:"primary_email"`
	EmailCount   int    `json:"email_count"`
}

func (s *Server) handleAgentFindPeople(w http.ResponseWriter, r *http.Request) {
	var req findPeopleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("query is required"))
		return
	}
	hits, err := s.findPeopleForAgent(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (s *Server) findPeopleForAgent(ctx context.Context, req findPeopleRequest) ([]personHit, error) {
	if strings.TrimSpace(req.Query) == "" {
		return []personHit{}, nil
	}
	limit := req.Limit
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	like := "%" + strings.ToLower(req.Query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id,
		       p.canonical_name,
		       p.primary_email,
		       (SELECT COUNT(*) FROM memento_person_email pe WHERE pe.person_id = p.id) AS email_count
		FROM memento_person p
		WHERE lower(p.canonical_name) LIKE ?
		   OR lower(p.primary_email) LIKE ?
		   OR p.id IN (
		     SELECT person_id FROM memento_person_email
		     WHERE lower(email_address) LIKE ? OR lower(display_name) LIKE ?
		   )
		ORDER BY email_count DESC, p.canonical_name
		LIMIT ?`, like, like, like, like, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []personHit
	for rows.Next() {
		var h personHit
		if err := rows.Scan(&h.PersonID, &h.DisplayName, &h.PrimaryEmail, &h.EmailCount); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	if hits == nil {
		hits = []personHit{}
	}
	return hits, rows.Err()
}

// ---- POST /api/internal/agent-tools/get-thread ----

type getThreadRequest struct {
	ThreadID int64 `json:"thread_id"`
}

type threadSummary struct {
	ThreadID     int64    `json:"thread_id"`
	Subject      string   `json:"subject"`
	MessageCount int      `json:"message_count"`
	FirstDate    string   `json:"first_date,omitempty"`
	LastDate     string   `json:"last_date,omitempty"`
	MessageIDs   []int64  `json:"message_ids"`
	Participants []string `json:"participants"`
}

func (s *Server) handleAgentGetThread(w http.ResponseWriter, r *http.Request) {
	var req getThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ThreadID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("thread_id is required"))
		return
	}
	summary, err := s.getThreadForAgent(r.Context(), req.ThreadID)
	if err != nil {
		if strings.Contains(err.Error(), "has no messages") {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) getThreadForAgent(ctx context.Context, threadID int64) (threadSummary, error) {
	if threadID <= 0 {
		return threadSummary{}, fmt.Errorf("thread_id is required")
	}
	db := s.reader.DB()
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(subject, ''), COALESCE(sent_at, '')
		FROM messages
		WHERE conversation_id = ?
		ORDER BY sent_at`, threadID,
	)
	if err != nil {
		return threadSummary{}, err
	}
	defer rows.Close()

	summary := threadSummary{ThreadID: threadID}
	for rows.Next() {
		var id int64
		var subj, sent string
		if err := rows.Scan(&id, &subj, &sent); err != nil {
			return threadSummary{}, err
		}
		summary.MessageIDs = append(summary.MessageIDs, id)
		if summary.Subject == "" {
			summary.Subject = subj
		}
		if summary.FirstDate == "" || (sent != "" && sent < summary.FirstDate) {
			summary.FirstDate = sent
		}
		if sent > summary.LastDate {
			summary.LastDate = sent
		}
	}
	if err := rows.Err(); err != nil {
		return threadSummary{}, err
	}
	summary.MessageCount = len(summary.MessageIDs)
	if summary.MessageCount == 0 {
		return threadSummary{}, fmt.Errorf("thread %d has no messages", threadID)
	}

	// Participants — distinct emails of any sender or recipient on these messages.
	if len(summary.MessageIDs) > 0 {
		placeholders, args := buildInClause(summary.MessageIDs)
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
		prows, err := db.QueryContext(ctx, q, args2...)
		if err == nil {
			defer prows.Close()
			for prows.Next() {
				var e string
				if err := prows.Scan(&e); err == nil {
					summary.Participants = append(summary.Participants, e)
				}
			}
		}
	}

	if summary.Participants == nil {
		summary.Participants = []string{}
	}
	return summary, nil
}

// buildInClause produces "?,?,?" placeholders and the matching []any args.
func buildInClause(ids []int64) (string, []any) {
	parts := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		parts[i] = "?"
		args[i] = id
	}
	return strings.Join(parts, ","), args
}
