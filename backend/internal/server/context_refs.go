package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/store"
)

type contextSearchResult struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Label    string `json:"label"`
	Subtitle string `json:"subtitle,omitempty"`
}

type contextRefInput struct {
	Kind      string `json:"kind"`
	ID        string `json:"id,omitempty"`
	PersonID  int64  `json:"person_id,omitempty"`
	SessionID int64  `json:"session_id,omitempty"`
	Slug      string `json:"slug,omitempty"`
	Label     string `json:"label,omitempty"`
}

type validatedAskContext struct {
	Refs        []store.AskContextRef
	Bootstrap   []map[string]any
	Warnings    []string
	DisplayRefs []map[string]any
}

func (s *Server) handleContextSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	trigger := strings.TrimSpace(r.URL.Query().Get("trigger"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if trigger == "@" {
		kind = "person"
	}
	if trigger == "#" {
		kind = "artifact"
	}
	if kind == "" {
		kind = "artifact"
	}

	results := []contextSearchResult{}
	var err error
	switch kind {
	case "person":
		results, err = s.searchContextPeople(r.Context(), q)
	case "artifact", "#":
		results, err = s.searchContextArtifacts(r.Context(), q)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported context search kind %q", kind))
		return
	}
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, map[string]any{"results": []contextSearchResult{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func likePattern(q string) string {
	if q == "" {
		return "%"
	}
	return "%" + q + "%"
}

func (s *Server) searchContextPeople(ctx context.Context, q string) ([]contextSearchResult, error) {
	pat := likePattern(q)
	rows, err := s.db.QueryContext(ctx, `
		SELECT person_id, slug, canonical_name, primary_email, total_messages
		FROM memento_people_report
		WHERE classification != 'excluded'
		  AND (? = '%' OR canonical_name LIKE ? OR primary_email LIKE ? OR EXISTS (
		    SELECT 1 FROM memento_person_email pe
		    WHERE pe.person_id = memento_people_report.person_id
		      AND (pe.email_address LIKE ? OR pe.display_name LIKE ?)
		  ))
		ORDER BY total_messages DESC, last_message_at DESC
		LIMIT 8`, pat, pat, pat, pat, pat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contextSearchResult{}
	for rows.Next() {
		var id, total int64
		var slug, name, email string
		if err := rows.Scan(&id, &slug, &name, &email, &total); err != nil {
			return nil, err
		}
		label := name
		if strings.TrimSpace(label) == "" {
			label = email
		}
		out = append(out, contextSearchResult{
			Kind:     "person",
			ID:       strconv.FormatInt(id, 10),
			Slug:     slug,
			Label:    label,
			Subtitle: fmt.Sprintf("%s · %d messages", email, total),
		})
	}
	return out, rows.Err()
}

func (s *Server) searchContextArtifacts(ctx context.Context, q string) ([]contextSearchResult, error) {
	pat := likePattern(q)
	out := []contextSearchResult{}
	sessionRows, err := s.db.QueryContext(ctx, `
		SELECT id, slug, title, summary
		FROM memento_ask_session
		WHERE archived_at IS NULL
		  AND (? = '%' OR title LIKE ? OR summary LIKE ? OR slug LIKE ?)
		ORDER BY pinned DESC, updated_at DESC
		LIMIT 6`, pat, pat, pat, pat)
	if err != nil && !isNotSetUp(err) {
		return nil, err
	}
	if err == nil {
		defer sessionRows.Close()
		for sessionRows.Next() {
			var id int64
			var slug, title, summary string
			if err := sessionRows.Scan(&id, &slug, &title, &summary); err != nil {
				return nil, err
			}
			out = append(out, contextSearchResult{
				Kind:     "ask_session",
				ID:       strconv.FormatInt(id, 10),
				Slug:     slug,
				Label:    title,
				Subtitle: compactSearchSubtitle("Session", summary),
			})
		}
		if err := sessionRows.Err(); err != nil {
			return nil, err
		}
	}

	projectRows, err := s.db.QueryContext(ctx, `
		SELECT project_id, slug, name, status
		FROM memento_projects_report
		WHERE ? = '%' OR name LIKE ? OR slug LIKE ?
		ORDER BY name ASC
		LIMIT 6`, pat, pat, pat)
	if err != nil && !isNotSetUp(err) {
		return nil, err
	}
	if err == nil {
		defer projectRows.Close()
		for projectRows.Next() {
			var id int64
			var slug, name, status string
			if err := projectRows.Scan(&id, &slug, &name, &status); err != nil {
				return nil, err
			}
			out = append(out, contextSearchResult{
				Kind:     "project",
				ID:       strconv.FormatInt(id, 10),
				Slug:     slug,
				Label:    name,
				Subtitle: "Project · " + status,
			})
		}
		if err := projectRows.Err(); err != nil {
			return nil, err
		}
	}

	conceptRows, err := s.db.QueryContext(ctx, `
		SELECT concept_id, slug, name, scope_description
		FROM memento_concepts_report
		WHERE ? = '%' OR name LIKE ? OR slug LIKE ? OR scope_description LIKE ?
		ORDER BY name ASC
		LIMIT 6`, pat, pat, pat, pat)
	if err != nil && !isNotSetUp(err) {
		return nil, err
	}
	if err == nil {
		defer conceptRows.Close()
		for conceptRows.Next() {
			var id int64
			var slug, name, scope string
			if err := conceptRows.Scan(&id, &slug, &name, &scope); err != nil {
				return nil, err
			}
			out = append(out, contextSearchResult{
				Kind:     "concept",
				ID:       strconv.FormatInt(id, 10),
				Slug:     slug,
				Label:    name,
				Subtitle: compactSearchSubtitle("Concept", scope),
			})
		}
		if err := conceptRows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func compactSearchSubtitle(prefix, text string) string {
	clean := strings.Join(strings.Fields(text), " ")
	if clean == "" {
		return prefix
	}
	if len(clean) > 96 {
		clean = strings.TrimSpace(clean[:96]) + "..."
	}
	return prefix + " · " + clean
}

func (s *Server) validateAskContextRefs(ctx context.Context, raw any) (validatedAskContext, error) {
	var inputs []contextRefInput
	if raw == nil {
		return validatedAskContext{Refs: []store.AskContextRef{}, Bootstrap: []map[string]any{}, DisplayRefs: []map[string]any{}}, nil
	}
	rawJSON, err := json.Marshal(raw)
	if err != nil {
		return validatedAskContext{}, err
	}
	if err := json.Unmarshal(rawJSON, &inputs); err != nil {
		return validatedAskContext{}, fmt.Errorf("invalid context_refs: %w", err)
	}
	out := validatedAskContext{
		Refs:        []store.AskContextRef{},
		Bootstrap:   []map[string]any{},
		Warnings:    []string{},
		DisplayRefs: []map[string]any{},
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		kind := strings.TrimSpace(input.Kind)
		if kind == "" {
			out.Warnings = append(out.Warnings, "dropped context ref with empty kind")
			continue
		}
		ref, bootstrap, display, err := s.validateOneAskContextRef(ctx, input)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				out.Warnings = append(out.Warnings, fmt.Sprintf("dropped missing %s context %q", kind, input.Label))
				continue
			}
			return out, err
		}
		key := ref.RefKind + ":" + ref.RefID
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Refs = append(out.Refs, ref)
		out.Bootstrap = append(out.Bootstrap, bootstrap)
		out.DisplayRefs = append(out.DisplayRefs, display)
	}
	return out, nil
}

func (s *Server) validateOneAskContextRef(ctx context.Context, input contextRefInput) (store.AskContextRef, map[string]any, map[string]any, error) {
	switch input.Kind {
	case "person":
		return s.validatePersonAskContext(ctx, input)
	case "project":
		return s.validateProjectAskContext(ctx, input)
	case "concept":
		return s.validateConceptAskContext(ctx, input)
	case "ask_session":
		return s.validateSessionAskContext(ctx, input)
	default:
		return store.AskContextRef{}, nil, nil, fmt.Errorf("unsupported context ref kind %q", input.Kind)
	}
}

func (s *Server) validatePersonAskContext(ctx context.Context, input contextRefInput) (store.AskContextRef, map[string]any, map[string]any, error) {
	personID := input.PersonID
	if personID <= 0 && input.ID != "" {
		personID, _ = strconv.ParseInt(input.ID, 10, 64)
	}
	var row struct {
		ID       int64
		Slug     string
		Name     string
		Email    string
		Messages int64
		Last     sql.NullString
	}
	var err error
	if personID > 0 {
		err = s.db.QueryRowContext(ctx, `
			SELECT person_id, slug, canonical_name, primary_email, total_messages, last_message_at
			FROM memento_people_report WHERE person_id = ?`, personID,
		).Scan(&row.ID, &row.Slug, &row.Name, &row.Email, &row.Messages, &row.Last)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT person_id, slug, canonical_name, primary_email, total_messages, last_message_at
			FROM memento_people_report WHERE slug = ?`, input.Slug,
		).Scan(&row.ID, &row.Slug, &row.Name, &row.Email, &row.Messages, &row.Last)
	}
	if err != nil {
		return store.AskContextRef{}, nil, nil, err
	}
	label := row.Name
	if label == "" {
		label = row.Email
	}
	payload := map[string]any{
		"person_id":      row.ID,
		"slug":           row.Slug,
		"canonical_name": row.Name,
		"primary_email":  row.Email,
		"total_messages": row.Messages,
	}
	if row.Last.Valid {
		payload["last_message_at"] = row.Last.String
	}
	raw, _ := json.Marshal(payload)
	ref := store.AskContextRef{RefKind: "person", RefID: strconv.FormatInt(row.ID, 10), Label: label, PayloadJSON: string(raw)}
	display := map[string]any{"kind": "person", "person_id": row.ID, "slug": row.Slug, "label": label}
	return ref, payload, display, nil
}

func (s *Server) validateProjectAskContext(ctx context.Context, input contextRefInput) (store.AskContextRef, map[string]any, map[string]any, error) {
	var row struct {
		ID      int64
		Slug    string
		Name    string
		Status  string
		Summary string
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT project_id, slug, name, status, summary_json
		FROM memento_projects_report WHERE slug = ?`, input.Slug,
	).Scan(&row.ID, &row.Slug, &row.Name, &row.Status, &row.Summary)
	if err != nil {
		return store.AskContextRef{}, nil, nil, err
	}
	payload := map[string]any{"project_id": row.ID, "slug": row.Slug, "name": row.Name, "status": row.Status}
	var summary map[string]any
	if json.Unmarshal([]byte(row.Summary), &summary) == nil {
		payload["summary"] = summary
	}
	raw, _ := json.Marshal(payload)
	ref := store.AskContextRef{RefKind: "project", RefID: row.Slug, Label: row.Name, PayloadJSON: string(raw)}
	display := map[string]any{"kind": "project", "slug": row.Slug, "label": row.Name}
	return ref, payload, display, nil
}

func (s *Server) validateConceptAskContext(ctx context.Context, input contextRefInput) (store.AskContextRef, map[string]any, map[string]any, error) {
	var row struct {
		ID       int64
		Slug     string
		Name     string
		Status   string
		Scope    string
		Messages int64
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT concept_id, slug, name, status, scope_description, message_count
		FROM memento_concepts_report WHERE slug = ?`, input.Slug,
	).Scan(&row.ID, &row.Slug, &row.Name, &row.Status, &row.Scope, &row.Messages)
	if err != nil {
		return store.AskContextRef{}, nil, nil, err
	}
	payload := map[string]any{
		"concept_id":        row.ID,
		"slug":              row.Slug,
		"name":              row.Name,
		"status":            row.Status,
		"scope_description": row.Scope,
		"message_count":     row.Messages,
	}
	raw, _ := json.Marshal(payload)
	ref := store.AskContextRef{RefKind: "concept", RefID: row.Slug, Label: row.Name, PayloadJSON: string(raw)}
	display := map[string]any{"kind": "concept", "slug": row.Slug, "label": row.Name}
	return ref, payload, display, nil
}

func (s *Server) validateSessionAskContext(ctx context.Context, input contextRefInput) (store.AskContextRef, map[string]any, map[string]any, error) {
	sessionID := input.SessionID
	if sessionID <= 0 && input.ID != "" {
		sessionID, _ = strconv.ParseInt(input.ID, 10, 64)
	}
	var sess store.AskSession
	var err error
	if sessionID > 0 {
		sess, err = store.GetAskSessionByID(ctx, s.db, sessionID)
	} else {
		sess, err = store.GetAskSessionBySlug(ctx, s.db, input.Slug)
	}
	if err != nil {
		return store.AskContextRef{}, nil, nil, err
	}
	turns, _ := store.ListAskTurns(ctx, s.db, sess.ID)
	recent := []map[string]any{}
	start := len(turns) - 3
	if start < 0 {
		start = 0
	}
	for _, turn := range turns[start:] {
		recent = append(recent, map[string]any{
			"turn_index":     turn.TurnIndex,
			"user_message":   turn.UserMessage,
			"answer_summary": turn.AnswerSummary,
			"status":         turn.Status,
		})
	}
	payload := map[string]any{
		"session_id":   sess.ID,
		"slug":         sess.Slug,
		"title":        sess.Title,
		"summary":      sess.Summary,
		"recent_turns": recent,
	}
	raw, _ := json.Marshal(payload)
	ref := store.AskContextRef{RefKind: "ask_session", RefID: strconv.FormatInt(sess.ID, 10), Label: sess.Title, PayloadJSON: string(raw)}
	display := map[string]any{"kind": "ask_session", "session_id": sess.ID, "slug": sess.Slug, "label": sess.Title}
	return ref, payload, display, nil
}

func askBootstrapMessages(ctx validatedAskContext) []agentrunner.ModelMessage {
	if len(ctx.Bootstrap) == 0 {
		return nil
	}
	raw, _ := json.MarshalIndent(map[string]any{
		"context_refs": ctx.Bootstrap,
		"warnings":     ctx.Warnings,
	}, "", "  ")
	return []agentrunner.ModelMessage{{
		Role: "user",
		Content: "Deterministic context loaded before this Ask Memento turn. Treat it as validated product context. Use it when sufficient; call tools for source-specific claims beyond it.\n\n```json\n" +
			string(raw) + "\n```",
	}}
}

func contextLoadedEvent(ctx validatedAskContext) agentrunner.AgentEvent {
	return agentrunner.NewContextLoadedEvent(ctx.DisplayRefs, ctx.Warnings)
}
