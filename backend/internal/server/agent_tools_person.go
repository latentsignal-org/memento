// Package server — token-gated agent-tool endpoints for the person agent
// (Phase 5). These tools expose scoped relationship context, user notes, and
// citation-preserving writes for facets and narrative sections.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"memento/backend/internal/people"
	"memento/backend/internal/refresh"
)

type personToolRequest struct {
	PersonID int64 `json:"person_id"`
}

func loadPersonForAgent(ctx context.Context, db *sql.DB, personID int64) (map[string]any, error) {
	notes, errNotes := loadNotes(ctx, db, "person", personID)
	if errNotes != nil && !isNotSetUp(errNotes) {
		return nil, errNotes
	}
	if notes == nil {
		notes = []noteRow{}
	}

	var p people.PagePerson
	var first, last, aliasesJSON, timelineJSON, correspondentsJSON string
	err := db.QueryRowContext(ctx, `
		SELECT person_id, canonical_name, primary_email, domain, email_count,
		       total_messages, from_contact_count, to_contact_count,
		       bidirectional_score, classification,
		       COALESCE(first_message_at, ''), COALESCE(last_message_at, ''),
		       aliases_json, timeline_json, top_correspondents_json
		FROM memento_people_report
		WHERE person_id = ?
	`, personID).Scan(
		&p.PersonID, &p.CanonicalName, &p.PrimaryEmail, &p.Domain, &p.EmailCount,
		&p.TotalMessages, &p.FromContactCount, &p.ToContactCount,
		&p.BidirectionalScore, &p.Classification, &first, &last,
		&aliasesJSON, &timelineJSON, &correspondentsJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			var name, email string
			fallbackErr := db.QueryRowContext(ctx, `
				SELECT canonical_name, primary_email FROM memento_person WHERE id = ?
			`, personID).Scan(&name, &email)
			if fallbackErr != nil {
				return nil, err // Return original ErrNoRows
			}
			var aliases []string
			rows, _ := db.QueryContext(ctx, `SELECT email_address FROM memento_person_email WHERE person_id = ?`, personID)
			if rows != nil {
				defer rows.Close()
				for rows.Next() {
					var addr string
					if err := rows.Scan(&addr); err == nil {
						aliases = append(aliases, addr)
					}
				}
			}
			return map[string]any{
				"person_id":              personID,
				"canonical_name":         name,
				"primary_email":          email,
				"aliases":                aliases,
				"total_messages":         0,
				"email_count":            0,
				"classification":         "candidate",
				"top_correspondents":     []any{},
				"recent_timeline_sample": []any{},
				"notes":                  notes,
			}, nil
		}
		return nil, err
	}
	p.FirstMessageAt = first
	p.LastMessageAt = last
	_ = json.Unmarshal([]byte(aliasesJSON), &p.Aliases)
	_ = json.Unmarshal([]byte(timelineJSON), &p.Timeline)
	_ = json.Unmarshal([]byte(correspondentsJSON), &p.TopCorrespondents)
	return map[string]any{
		"person_id":              p.PersonID,
		"canonical_name":         p.CanonicalName,
		"primary_email":          p.PrimaryEmail,
		"domain":                 p.Domain,
		"email_count":            p.EmailCount,
		"aliases":                p.Aliases,
		"total_messages":         p.TotalMessages,
		"from_contact_count":     p.FromContactCount,
		"to_contact_count":       p.ToContactCount,
		"bidirectional_score":    p.BidirectionalScore,
		"classification":         p.Classification,
		"first_message_at":       p.FirstMessageAt,
		"last_message_at":        p.LastMessageAt,
		"top_correspondents":     p.TopCorrespondents,
		"recent_timeline_sample": p.Timeline,
		"notes":                  notes,
	}, nil
}

type listPersonMessagesRequest struct {
	PersonID int64  `json:"person_id"`
	Limit    int    `json:"limit"`
	Fields   string `json:"fields"` // "full" | "compact"
}

type personMessageSummary struct {
	MessageID int64  `json:"message_id"`
	Date      string `json:"date"`
	Direction string `json:"direction"`
	ViaEmail  string `json:"via_email,omitempty"`
	Subject   string `json:"subject"`
	Snippet   string `json:"snippet,omitempty"`
	ThreadID  int64  `json:"thread_id"`
	FromEmail string `json:"from_email,omitempty"`
	FromName  string `json:"from_name,omitempty"`
}

func (s *Server) handleAgentListPersonMessages(w http.ResponseWriter, r *http.Request) {
	var req listPersonMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PersonID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_id is required"))
		return
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > 200 {
		req.Limit = 200
	}
	if req.Fields == "" {
		req.Fields = "full"
	}
	msgs, err := listPersonMessages(r.Context(), s.db, req.PersonID, req.Limit, req.Fields)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

func listPersonMessages(ctx context.Context, db *sql.DB, personID int64, limit int, fields string) ([]personMessageSummary, error) {
	viaEmailSel := "i.via_email"
	snippetSel := "COALESCE(m.snippet, '')"
	fromEmailSel := "COALESCE(p.email_address, '')"
	fromNameSel := "COALESCE(p.display_name, '')"

	if fields == "compact" {
		viaEmailSel = "''"
		snippetSel = "''"
		fromEmailSel = "''"
		fromNameSel = "''"
	}

	query := fmt.Sprintf(`
		WITH account_emails AS (
			SELECT lower(identifier) AS email
			FROM sources
			WHERE identifier LIKE '%%@%%'
		),
		account_participants AS (
			SELECT p.id
			FROM participants p
			JOIN account_emails ae ON ae.email = lower(p.email_address)
		),
		person_emails AS (
			SELECT lower(email_address) AS email
			FROM memento_person_email
			WHERE person_id = ?
		),
		person_participants AS (
			SELECT p.id, p.email_address
			FROM participants p
			JOIN person_emails pe ON pe.email = lower(p.email_address)
		),
		involvement AS (
			SELECT m.id AS message_id, m.sent_at, 'from_contact' AS direction, pp.email_address AS via_email
			FROM messages m
			JOIN person_participants pp ON pp.id = m.sender_id
			WHERE m.sender_id IS NOT NULL
			  AND m.sender_id NOT IN (SELECT id FROM account_participants)

			UNION ALL

			SELECT m.id AS message_id, m.sent_at, 'to_contact' AS direction, pp.email_address AS via_email
			FROM message_recipients mr
			JOIN messages m ON m.id = mr.message_id
			JOIN person_participants pp ON pp.id = mr.participant_id
			WHERE m.sender_id IN (SELECT id FROM account_participants)
			  AND mr.recipient_type IN ('to', 'cc', 'bcc', 'mention')
		)
		SELECT DISTINCT
			i.message_id,
			COALESCE(i.sent_at, ''),
			i.direction,
			%s,
			COALESCE(m.subject, ''),
			%s,
			COALESCE(m.conversation_id, 0),
			%s,
			%s
		FROM involvement i
		JOIN messages m ON m.id = i.message_id
		LEFT JOIN participants p ON p.id = m.sender_id
		ORDER BY i.sent_at DESC, i.message_id DESC
		LIMIT ?`, viaEmailSel, snippetSel, fromEmailSel, fromNameSel)

	rows, err := db.QueryContext(ctx, query, personID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []personMessageSummary
	for rows.Next() {
		var m personMessageSummary
		if err := rows.Scan(
			&m.MessageID, &m.Date, &m.Direction, &m.ViaEmail, &m.Subject,
			&m.Snippet, &m.ThreadID, &m.FromEmail, &m.FromName,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []personMessageSummary{}
	}
	return out, nil
}

// ---- POST /api/internal/agent-tools/refresh-people-rollup ----

type refreshPeopleRequest struct {
	PersonID int64 `json:"person_id"`
}

// handleAgentRefreshPeople refreshes the people rollup table so person detail
// pages read freshly-written facets and narrative sections immediately after an
// enrich run completes.
func (s *Server) handleAgentRefreshPeople(w http.ResponseWriter, r *http.Request) {
	var req refreshPeopleRequest
	// Decode body but ignore error if body is empty for backward compatibility
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.PersonID > 0 {
		if err := refresh.RefreshPeopleReportForPerson(r.Context(), s.db, req.PersonID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		if _, err := refresh.RefreshPeopleReport(r.Context(), s.db); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type getNotesRequest struct {
	Dimension string `json:"dimension"`
	EntityID  int64  `json:"entity_id"`
}

func (s *Server) handleAgentGetNotes(w http.ResponseWriter, r *http.Request) {
	var req getNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Dimension) == "" || req.EntityID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("dimension and entity_id are required"))
		return
	}
	notes, err := loadNotes(r.Context(), s.db, req.Dimension, req.EntityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, notes)
}

type scopedSearchRequest struct {
	PersonID int64  `json:"person_id"`
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
}

func (s *Server) handleAgentFTSSearchScoped(w http.ResponseWriter, r *http.Request) {
	var req scopedSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PersonID <= 0 || strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_id and query are required"))
		return
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 50 {
		req.Limit = 50
	}
	hits, err := s.ftsSearchScoped(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, hits)
}

func (s *Server) ftsSearchScoped(ctx context.Context, req scopedSearchRequest) ([]messageHit, error) {
	if req.PersonID <= 0 || strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("person_id and query are required")
	}
	limit := clampLimit(req.Limit, 20, 50)
	ids, err := runMsgvaultSearchCLI(ctx, req.Query, limit*4)
	if err != nil {
		return nil, err
	}
	ids, err = filterMessageIDsForPerson(ctx, s.db, req.PersonID, ids)
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return enrichMessageIDs(ctx, s.db, ids)
}

func filterMessageIDsForPerson(ctx context.Context, db *sql.DB, personID int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	placeholders, args := buildInClause(ids)
	args = append([]any{personID}, args...)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		WITH person_emails AS (
			SELECT lower(email_address) AS email
			FROM memento_person_email
			WHERE person_id = ?
		),
		person_participants AS (
			SELECT id
			FROM participants
			WHERE lower(email_address) IN (SELECT email FROM person_emails)
		)
		SELECT DISTINCT m.id
		FROM messages m
		LEFT JOIN message_recipients mr ON mr.message_id = m.id
		WHERE m.id IN (%s)
		  AND (m.sender_id IN (SELECT id FROM person_participants)
		       OR mr.participant_id IN (SELECT id FROM person_participants))
	`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allowed := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		allowed[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if allowed[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *Server) handleAgentResetPersonOutput(w http.ResponseWriter, r *http.Request) {
	var req personToolRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PersonID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_id is required"))
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		DELETE FROM memento_person_facet
		WHERE person_id = ? AND edited_by = 'llm'
	`, req.PersonID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		DELETE FROM memento_person_narrative
		WHERE person_id = ? AND edited_by = 'llm'
	`, req.PersonID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type writeFacetRequest struct {
	PersonID         int64   `json:"person_id"`
	FacetType        string  `json:"facet_type"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
	Confidence       float64 `json:"confidence"`
}

type writePersonAttributeRequest struct {
	PersonID         int64   `json:"person_id"`
	Category         string  `json:"category"`
	Label            string  `json:"label"`
	Value            string  `json:"value"`
	DateValue        string  `json:"date_value"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
	Confidence       float64 `json:"confidence"`
}

var allowedFacetTypes = map[string]bool{
	"interest":            true,
	"life_event":          true,
	"recurring_topic":     true,
	"relationship_signal": true,
	"fact":                true,
}

var allowedPersonAttributeCategories = map[string]bool{
	"vital_date":          true,
	"preference":          true,
	"interest":            true,
	"relationship_marker": true,
	"household":           true,
	"work":                true,
	"location":            true,
	"routine":             true,
	"identifier":          true,
}

func (s *Server) handleAgentWriteFacet(w http.ResponseWriter, r *http.Request) {
	var req writeFacetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PersonID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_id is required"))
		return
	}
	if !allowedFacetTypes[req.FacetType] {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported facet_type %q", req.FacetType))
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("content is required"))
		return
	}
	if len(req.SourceMessageIDs) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("source_message_ids must contain at least one message id"))
		return
	}
	result, err := s.writeFacet(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeFacet(ctx context.Context, req writeFacetRequest) (map[string]any, error) {
	if req.PersonID <= 0 {
		return nil, fmt.Errorf("person_id is required")
	}
	if !allowedFacetTypes[req.FacetType] {
		return nil, fmt.Errorf("unsupported facet_type %q", req.FacetType)
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}
	if len(req.SourceMessageIDs) == 0 {
		return nil, fmt.Errorf("source_message_ids must contain at least one message id")
	}
	if req.Confidence <= 0 {
		req.Confidence = 1.0
	}
	ids, _ := json.Marshal(req.SourceMessageIDs)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO memento_person_facet (
			person_id, facet_type, content, source_message_ids, confidence, generated_at, edited_by
		)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')
	`, req.PersonID, req.FacetType, req.Content, string(ids), req.Confidence)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return map[string]any{"ok": true, "facet_id": id}, nil
}

func (s *Server) writePersonAttribute(ctx context.Context, req writePersonAttributeRequest) (map[string]any, error) {
	if req.PersonID <= 0 {
		return nil, fmt.Errorf("person_id is required")
	}
	if !allowedPersonAttributeCategories[req.Category] {
		return nil, fmt.Errorf("unsupported attribute category %q", req.Category)
	}
	req.Label = strings.TrimSpace(req.Label)
	req.Value = strings.TrimSpace(req.Value)
	req.DateValue = strings.TrimSpace(req.DateValue)
	if req.Label == "" || req.Value == "" {
		return nil, fmt.Errorf("label and value are required")
	}
	if strings.ContainsAny(req.Label, "\r\n") || strings.ContainsAny(req.Value, "\r\n") {
		return nil, fmt.Errorf("label and value must be single-line")
	}
	if len([]rune(req.Label)) > 40 {
		return nil, fmt.Errorf("label must be 40 characters or fewer")
	}
	if len([]rune(req.Value)) > 160 {
		return nil, fmt.Errorf("value must be 160 characters or fewer")
	}
	if len(req.SourceMessageIDs) == 0 {
		return nil, fmt.Errorf("source_message_ids must contain at least one message id")
	}
	if req.Confidence <= 0 {
		req.Confidence = 1.0
	}
	ids, _ := json.Marshal(req.SourceMessageIDs)
	var dateValue any
	if req.DateValue != "" {
		dateValue = req.DateValue
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO memento_person_attribute (
			person_id, category, label, value, date_value, source_message_ids,
			confidence, generated_at, edited_by
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')
	`, req.PersonID, req.Category, req.Label, req.Value, dateValue, string(ids), req.Confidence)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return map[string]any{"ok": true, "attribute_id": id}, nil
}

type writePersonSectionRequest struct {
	PersonID         int64   `json:"person_id"`
	Section          string  `json:"section"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

var allowedPersonSections = map[string]bool{
	"summary":          true,
	"relationship_arc": true,
	"current_status":   true,
}

func (s *Server) handleAgentWritePersonSection(w http.ResponseWriter, r *http.Request) {
	var req writePersonSectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PersonID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_id is required"))
		return
	}
	if !allowedPersonSections[req.Section] {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported section %q", req.Section))
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("content is required"))
		return
	}
	if len(req.SourceMessageIDs) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("source_message_ids must contain at least one message id"))
		return
	}
	result, err := savePersonSection(r.Context(), s.db, req.PersonID, req.Section, req.Content, req.SourceMessageIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func savePersonSection(
	ctx context.Context, db *sql.DB,
	personID int64, section, content string, msgIDs []int64,
) (saveSectionResult, error) {
	var editedBy string
	err := db.QueryRowContext(ctx,
		`SELECT edited_by FROM memento_person_narrative WHERE person_id = ? AND section = ?`,
		personID, section,
	).Scan(&editedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return saveSectionResult{}, err
	}
	if editedBy == "user" {
		return saveSectionResult{
			OK:         true,
			Skipped:    true,
			SkipReason: "section is user-edited; agent writes preserved as draft only",
		}, nil
	}
	ids, _ := json.Marshal(msgIDs)
	_, err = db.ExecContext(ctx, `
		INSERT INTO memento_person_narrative (person_id, section, content, source_message_ids, generated_at, edited_by)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')
		ON CONFLICT(person_id, section) DO UPDATE SET
		    content = excluded.content,
		    source_message_ids = excluded.source_message_ids,
		    generated_at = CURRENT_TIMESTAMP,
		    edited_by = 'llm'`,
		personID, section, content, string(ids),
	)
	if err != nil {
		return saveSectionResult{}, err
	}
	return saveSectionResult{OK: true, CitationCnt: len(msgIDs)}, nil
}

func loadPersonFacets(ctx context.Context, db *sql.DB, personID int64) ([]people.PersonFacet, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, facet_type, content, source_message_ids, confidence, edited_by, generated_at
		FROM memento_person_facet
		WHERE person_id = ?
		ORDER BY
			CASE facet_type
				WHEN 'life_event' THEN 1
				WHEN 'relationship_signal' THEN 2
				WHEN 'interest' THEN 3
				WHEN 'recurring_topic' THEN 4
				ELSE 5
			END,
			confidence DESC,
			id ASC
	`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var facets []people.PersonFacet
	for rows.Next() {
		var f people.PersonFacet
		var idsJSON string
		if err := rows.Scan(&f.ID, &f.FacetType, &f.Content, &idsJSON, &f.Confidence, &f.EditedBy, &f.GeneratedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(idsJSON), &f.SourceMessageIDs)
		if f.SourceMessageIDs == nil {
			f.SourceMessageIDs = []int64{}
		}
		facets = append(facets, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if facets == nil {
		facets = []people.PersonFacet{}
	}
	return facets, nil
}

func loadPersonAttributes(ctx context.Context, db *sql.DB, personID int64) ([]people.PersonAttribute, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, category, label, value, COALESCE(date_value, ''),
		       source_message_ids, confidence, edited_by, generated_at
		FROM memento_person_attribute
		WHERE person_id = ?
		ORDER BY
			CASE category
				WHEN 'vital_date' THEN 1
				WHEN 'relationship_marker' THEN 2
				WHEN 'preference' THEN 3
				WHEN 'interest' THEN 4
				ELSE 5
			END,
			label ASC,
			id ASC
	`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attrs []people.PersonAttribute
	for rows.Next() {
		var attr people.PersonAttribute
		var idsJSON string
		if err := rows.Scan(
			&attr.ID, &attr.Category, &attr.Label, &attr.Value, &attr.DateValue,
			&idsJSON, &attr.Confidence, &attr.EditedBy, &attr.GeneratedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(idsJSON), &attr.SourceMessageIDs)
		if attr.SourceMessageIDs == nil {
			attr.SourceMessageIDs = []int64{}
		}
		attrs = append(attrs, attr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if attrs == nil {
		attrs = []people.PersonAttribute{}
	}
	return attrs, nil
}

func loadPersonNarrative(ctx context.Context, db *sql.DB, personID int64) (people.PersonNarrative, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT section, content, source_message_ids, edited_by, COALESCE(generated_at, '')
		FROM memento_person_narrative
		WHERE person_id = ?
	`, personID)
	if err != nil {
		return people.PersonNarrative{}, err
	}
	defer rows.Close()
	var narrative people.PersonNarrative
	for rows.Next() {
		var section string
		var ns people.NarrativeSection
		var idsJSON string
		if err := rows.Scan(&section, &ns.Content, &idsJSON, &ns.EditedBy, &ns.GeneratedAt); err != nil {
			return people.PersonNarrative{}, err
		}
		_ = json.Unmarshal([]byte(idsJSON), &ns.SourceMessageIDs)
		if ns.SourceMessageIDs == nil {
			ns.SourceMessageIDs = []int64{}
		}
		switch section {
		case "summary":
			narrative.Summary = ns
		case "relationship_arc":
			narrative.RelationshipArc = ns
		case "current_status":
			narrative.CurrentStatus = ns
		}
	}
	return narrative, rows.Err()
}
