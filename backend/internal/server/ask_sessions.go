// Package server — ask_sessions.go: HTTP handlers and run-wiring helpers for
// the user-facing Ask Sessions dimension. Every dashboard ("Ask Memento") run
// is recorded as a turn on a product Ask Session so answers survive
// navigation, reloads, and debug purges. Raw memento_agent_* rows remain
// debug-only; the linkage is the nullable memento_ask_turn.run_id.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"memento/backend/internal/store"
)

// resolveAskSessionForRun finds the session named by ask_session_id metadata
// or creates a new one titled from the user's first message, then appends the
// new turn in 'running' state.
func (s *Server) resolveAskSessionForRun(ctx context.Context, meta map[string]any, userMessage string) (store.AskSession, store.AskTurn, error) {
	var session store.AskSession
	if raw, ok := meta["ask_session_id"]; ok && raw != nil {
		id, err := metadataInt64(raw)
		if err != nil {
			return store.AskSession{}, store.AskTurn{}, fmt.Errorf("invalid ask_session_id: %w", err)
		}
		session, err = store.GetAskSessionByID(ctx, s.db, id)
		if err != nil {
			return store.AskSession{}, store.AskTurn{}, fmt.Errorf("ask session %d not found: %w", id, err)
		}
	} else {
		var err error
		session, err = store.CreateAskSession(ctx, s.db, store.AskSessionTitleFromQuery(userMessage))
		if err != nil {
			return store.AskSession{}, store.AskTurn{}, fmt.Errorf("create ask session: %w", err)
		}
	}
	turn, err := store.AppendAskTurn(ctx, s.db, session.ID, userMessage)
	if err != nil {
		return store.AskSession{}, store.AskTurn{}, fmt.Errorf("append ask turn: %w", err)
	}
	return session, turn, nil
}

// metadataInt64 coerces a JSON-decoded metadata value (float64, string, or
// integer) into an int64 id.
func metadataInt64(v any) (int64, error) {
	switch val := v.(type) {
	case float64:
		return int64(val), nil
	case int64:
		return val, nil
	case int:
		return int64(val), nil
	case string:
		return strconv.ParseInt(val, 10, 64)
	case json.Number:
		return val.Int64()
	default:
		return 0, fmt.Errorf("unsupported id type %T", v)
	}
}

var citedMessageRe = regexp.MustCompile(`\[msg:(\d+)\]`)
var citedMessageStripRe = regexp.MustCompile(`\s*\[msg:\d+\]`)

// extractCitedMessageIDs collects unique [msg:N] citations in order of first
// appearance.
func extractCitedMessageIDs(text string) []int64 {
	seen := map[int64]bool{}
	ids := []int64{}
	for _, match := range citedMessageRe.FindAllStringSubmatch(text, -1) {
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

// askAnswerSummary compacts an answer for session cards and `#` autocomplete:
// citations stripped, whitespace collapsed, truncated on a word boundary.
func askAnswerSummary(answer string) string {
	clean := citedMessageStripRe.ReplaceAllString(answer, "")
	clean = strings.Join(strings.Fields(clean), " ")
	const maxLen = 240
	if len(clean) <= maxLen {
		return clean
	}
	cut := clean[:maxLen]
	if idx := strings.LastIndex(cut, " "); idx > maxLen/2 {
		cut = cut[:idx]
	}
	return cut + "…"
}

// toolSummaryForRun aggregates per-tool call counts from the run's loop rows.
// Best-effort: a missing or unparsable trace yields an empty summary.
func (s *Server) toolSummaryForRun(ctx context.Context, runID int64) string {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_calls_json FROM memento_agent_loop WHERE session_id = ?`, runID)
	if err != nil {
		return "{}"
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var callsJSON string
		if err := rows.Scan(&callsJSON); err != nil {
			continue
		}
		var calls []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(callsJSON), &calls); err != nil {
			continue
		}
		for _, call := range calls {
			if call.Name != "" {
				counts[call.Name]++
			}
		}
	}
	raw, err := json.Marshal(map[string]any{"tool_counts": counts})
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// completeAskTurnFromRun persists the final answer onto the Ask turn. Used as
// the dashboard agent's AfterDone hook.
func (s *Server) completeAskTurnFromRun(ctx context.Context, turnID, runID int64, assistantText string) error {
	cited, err := json.Marshal(extractCitedMessageIDs(assistantText))
	if err != nil {
		cited = []byte("[]")
	}
	return store.CompleteAskTurn(ctx, s.db, turnID,
		assistantText,
		askAnswerSummary(assistantText),
		string(cited),
		s.toolSummaryForRun(ctx, runID),
	)
}

// --- HTTP handlers -----------------------------------------------------------

type askTurnResponse struct {
	store.AskTurn
	CitedMessageIDs []int64               `json:"cited_message_ids"`
	ContextRefs     []store.AskContextRef `json:"context_refs"`
	RunStatus       string                `json:"run_status,omitempty"`
}

// effectiveTurnStatus reconciles a turn that is still 'running' against its
// debug run: failed/cancelled runs surface as failed turns without requiring
// a write hook in the runner.
func effectiveTurnStatus(turnStatus, runStatus string) string {
	if turnStatus != "running" {
		return turnStatus
	}
	switch runStatus {
	case store.AgentRunFailed, store.AgentRunCancelled:
		return "failed"
	}
	return turnStatus
}

// handleListAskSessions returns the product session index.
// GET /api/sessions[?archived=1]
func (s *Server) handleListAskSessions(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("archived") == "1"
	sessions, err := store.ListAskSessions(r.Context(), s.db, includeArchived)
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// handleGetAskSession returns one session with its turns and context refs.
// GET /api/sessions/{slug}
func (s *Server) handleGetAskSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	session, err := store.GetAskSessionBySlug(r.Context(), s.db, slug)
	if isNotSetUp(err) || errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, fmt.Errorf("ask session %q not found", slug))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	turns, err := store.ListAskTurns(r.Context(), s.db, session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	turnIDs := make([]int64, len(turns))
	for i, t := range turns {
		turnIDs[i] = t.ID
	}
	refsByTurn, err := store.ListAskContextRefs(r.Context(), s.db, turnIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	out := make([]askTurnResponse, 0, len(turns))
	for _, t := range turns {
		resp := askTurnResponse{AskTurn: t, CitedMessageIDs: []int64{}, ContextRefs: refsByTurn[t.ID]}
		if resp.ContextRefs == nil {
			resp.ContextRefs = []store.AskContextRef{}
		}
		_ = json.Unmarshal([]byte(t.CitedMessageIDsJSON), &resp.CitedMessageIDs)
		if t.RunID != nil {
			if run, err := store.GetAgentRun(r.Context(), s.db, *t.RunID); err == nil {
				resp.RunStatus = run.Status
				resp.Status = effectiveTurnStatus(t.Status, run.Status)
			}
		}
		out = append(out, resp)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session": session,
		"turns":   out,
	})
}

// handleUpdateAskSession mutates user-facing session metadata.
// PATCH /api/sessions/{slug}
func (s *Server) handleUpdateAskSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	session, err := store.GetAskSessionBySlug(r.Context(), s.db, slug)
	if isNotSetUp(err) || errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, fmt.Errorf("ask session %q not found", slug))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var body struct {
		Title    *string `json:"title"`
		Pinned   *bool   `json:"pinned"`
		Archived *bool   `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json body: %w", err))
		return
	}
	updated, err := store.UpdateAskSession(r.Context(), s.db, session.ID, body.Title, body.Pinned, body.Archived)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": updated})
}

// handleDeleteAskSession removes the product session. Raw debug runs remain
// unless ?debug=1 is provided, in which case linked debug runs are purged first.
// DELETE /api/sessions/{slug}[?debug=1]
func (s *Server) handleDeleteAskSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	session, err := store.GetAskSessionBySlug(r.Context(), s.db, slug)
	if isNotSetUp(err) || errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, fmt.Errorf("ask session %q not found", slug))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if r.URL.Query().Get("debug") == "1" {
		runIDs, err := s.askSessionRunIDs(r.Context(), session.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, runID := range runIDs {
			if err := s.purgeAgentSessionDebug(r.Context(), runID); err != nil && !errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("purge debug run %d: %w", runID, err))
				return
			}
		}
	}
	if err := store.DeleteAskSession(r.Context(), s.db, session.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("ask session %q not found", slug))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handlePromoteAskSession creates a project/concept draft seeded with this
// session's transcript, then the existing draft collector flow takes over.
// POST /api/sessions/{slug}/promote {"kind":"project"|"concept"}
func (s *Server) handlePromoteAskSession(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var body struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json body: %w", err))
		return
	}
	if body.Kind != "project" && body.Kind != "concept" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("kind must be project or concept"))
		return
	}
	session, err := store.GetAskSessionBySlug(r.Context(), s.db, slug)
	if isNotSetUp(err) || errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, fmt.Errorf("ask session %q not found", slug))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	turns, err := store.ListAskTurns(r.Context(), s.db, session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	draftID, err := store.CreateDraft(r.Context(), s.db, body.Kind, session.Title)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	transcriptJSON, err := askSessionDraftTranscript(session, turns, body.Kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := store.UpdateDraftState(r.Context(), s.db, draftID, "", transcriptJSON); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	prefix := "/projects/new"
	if body.Kind == "concept" {
		prefix = "/concepts/new"
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"draft_id": draftID,
		"kind":     body.Kind,
		"url":      fmt.Sprintf("%s?draftId=%d", prefix, draftID),
	})
}

func (s *Server) askSessionRunIDs(ctx context.Context, sessionID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT run_id
		FROM memento_ask_turn
		WHERE ask_session_id = ? AND run_id IS NOT NULL`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func askSessionDraftTranscript(session store.AskSession, turns []store.AskTurn, kind string) (string, error) {
	type line struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	lines := []line{}
	for _, turn := range turns {
		if text := strings.TrimSpace(turn.UserMessage); text != "" {
			lines = append(lines, line{Role: "user", Text: text})
		}
		if text := strings.TrimSpace(turn.AssistantAnswer); text != "" {
			lines = append(lines, line{Role: "assistant", Text: cleanAskAnswerForDraft(text)})
		}
	}
	if len(lines) == 0 {
		lines = append(lines, line{
			Role: "user",
			Text: fmt.Sprintf("Create a %s draft from Ask Session: %s", kind, session.Title),
		})
	}
	raw, err := json.Marshal(lines)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func cleanAskAnswerForDraft(text string) string {
	return strings.TrimSpace(citedMessageStripRe.ReplaceAllString(text, ""))
}
