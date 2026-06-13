package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"memento/backend/internal/store"
)

// ---- POST /api/internal/agent-sessions ----

type createAgentSessionRequest struct {
	SessionType string `json:"session_type"`
	EntityID    string `json:"entity_id"`
}

type createAgentSessionResponse struct {
	SessionID int64 `json:"session_id"`
}

func (s *Server) handleCreateAgentSession(w http.ResponseWriter, r *http.Request) {
	var req createAgentSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.SessionType == "" || req.EntityID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("session_type and entity_id are required"))
		return
	}

	ctx := r.Context()
	// Insert new session (or get the ID if we want to retrieve existing active one; let's always insert new session for each compilation/run)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO memento_agent_session (session_type, entity_id, status)
		VALUES (?, ?, 'active')
	`, req.SessionType, req.EntityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	sessionID, err := res.LastInsertId()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, createAgentSessionResponse{SessionID: sessionID})
}

// ---- PATCH /api/internal/agent-sessions/{id} ----

type updateAgentSessionRequest struct {
	Status        string `json:"status"`
	InteractionID string `json:"interaction_id"`
}

func (s *Server) handleUpdateAgentSession(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sessionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid session id"))
		return
	}

	var req updateAgentSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Status != "active" && req.Status != "succeeded" && req.Status != "failed" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid session status %q", req.Status))
		return
	}

	res, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_agent_session
		SET status = ?, interaction_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Status, req.InteractionID, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("session not found"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- POST /api/internal/agent-sessions/{id}/loops ----

type logAgentLoopRequest struct {
	StepIndex       int    `json:"step_index"`
	InputType       string `json:"input_type"`
	InputContent    string `json:"input_content"`
	AssistantText   string `json:"assistant_text"`
	ReasoningText   string `json:"reasoning_text"`
	ToolCallsJSON   string `json:"tool_calls_json"`
	ToolResultsJSON string `json:"tool_results_json"`
	DurationMs      int    `json:"duration_ms"`
}

func (s *Server) handleLogAgentLoop(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sessionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid session id"))
		return
	}

	var req logAgentLoopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ctx := r.Context()

	// Ensure session exists
	var exists int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_agent_session WHERE id = ?`, sessionID).Scan(&exists)
	if err != nil || exists == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("session not found"))
		return
	}

	// Insert loop entry
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO memento_agent_loop (
			session_id, step_index, input_type, input_content,
			assistant_text, reasoning_text, tool_calls_json, tool_results_json,
			duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sessionID, req.StepIndex, req.InputType, req.InputContent,
		req.AssistantText, req.ReasoningText, req.ToolCallsJSON, req.ToolResultsJSON,
		req.DurationMs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- GET /api/internal/agent-sessions/{id}/logs ----

type agentLoopLog struct {
	StepIndex                 int    `json:"step_index"`
	InputType                 string `json:"input_type"`
	InputContent              string `json:"input_content"`
	AssistantText             string `json:"assistant_text"`
	ReasoningText             string `json:"reasoning_text"`
	ToolCallsJSON             string `json:"tool_calls_json"`
	ToolResultsJSON           string `json:"tool_results_json"`
	DurationMs                int    `json:"duration_ms"`
	EstimatedInputTokens      int64  `json:"estimated_input_tokens"`
	EstimatedOutputTokens     int64  `json:"estimated_output_tokens"`
	EstimatedToolResultTokens int64  `json:"estimated_tool_result_tokens"`
	ModelInputTokens          int64  `json:"model_input_tokens"`
	ModelOutputTokens         int64  `json:"model_output_tokens"`
	ModelTotalTokens          int64  `json:"model_total_tokens"`
	UsageJSON                 string `json:"usage_json"`
	CreatedAt                 string `json:"created_at"`
}

type agentSessionLogsResponse struct {
	Loops      []agentLoopLog             `json:"loops"`
	ToolCalls  []store.AgentToolCallTrace `json:"tool_calls"`
	AskSession *askSessionDebugLink       `json:"ask_session,omitempty"`
}

type askSessionDebugLink struct {
	ID        int64  `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	TurnID    int64  `json:"turn_id"`
	TurnIndex int    `json:"turn_index"`
}

var errAgentSessionActive = errors.New("cannot purge active agent session")

func (s *Server) handleGetAgentSessionLogs(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sessionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid session id"))
		return
	}

	resp, err := s.loadAgentSessionLogs(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) loadAgentSessionLogs(ctx context.Context, sessionID int64) (agentSessionLogsResponse, error) {
	logs := s.agentSessionBootstrapLogs(ctx, sessionID)
	rows, err := s.db.QueryContext(ctx, `
		SELECT step_index, input_type, input_content, assistant_text, reasoning_text,
		       tool_calls_json, tool_results_json, COALESCE(duration_ms, 0),
		       estimated_input_tokens, estimated_output_tokens, estimated_tool_result_tokens,
		       model_input_tokens, model_output_tokens, model_total_tokens, usage_json,
		       created_at
		FROM memento_agent_loop
		WHERE session_id = ?
		ORDER BY step_index ASC
	`, sessionID)
	if err != nil {
		return agentSessionLogsResponse{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var l agentLoopLog
		err := rows.Scan(
			&l.StepIndex, &l.InputType, &l.InputContent, &l.AssistantText, &l.ReasoningText,
			&l.ToolCallsJSON, &l.ToolResultsJSON, &l.DurationMs,
			&l.EstimatedInputTokens, &l.EstimatedOutputTokens, &l.EstimatedToolResultTokens,
			&l.ModelInputTokens, &l.ModelOutputTokens, &l.ModelTotalTokens, &l.UsageJSON,
			&l.CreatedAt,
		)
		if err != nil {
			return agentSessionLogsResponse{}, err
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return agentSessionLogsResponse{}, err
	}

	if logs == nil {
		logs = []agentLoopLog{}
	}

	traces, err := store.ListAgentToolCallTraces(ctx, s.db, sessionID)
	if err != nil {
		return agentSessionLogsResponse{}, err
	}
	askSession, err := s.askSessionDebugLinkForRun(ctx, sessionID)
	if err != nil {
		return agentSessionLogsResponse{}, err
	}
	return agentSessionLogsResponse{Loops: logs, ToolCalls: traces, AskSession: askSession}, nil
}

func (s *Server) askSessionDebugLinkForRun(ctx context.Context, runID int64) (*askSessionDebugLink, error) {
	var link askSessionDebugLink
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.slug, s.title, t.id, t.turn_index
		FROM memento_ask_turn t
		JOIN memento_ask_session s ON s.id = t.ask_session_id
		WHERE t.run_id = ?
		ORDER BY t.id DESC
		LIMIT 1
	`, runID).Scan(&link.ID, &link.Slug, &link.Title, &link.TurnID, &link.TurnIndex)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

// ---- GET /api/internal/agent-sessions/latest-logs ----

func (s *Server) handleGetLatestAgentSessionLogs(w http.ResponseWriter, r *http.Request) {
	sessionType := r.URL.Query().Get("session_type")
	entityID := r.URL.Query().Get("entity_id")
	if sessionType == "" || entityID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("session_type and entity_id query parameters are required"))
		return
	}

	ctx := r.Context()
	var sessionID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM memento_agent_session
		WHERE session_type = ? AND entity_id = ?
		ORDER BY id DESC LIMIT 1
	`, sessionType, entityID).Scan(&sessionID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, agentSessionLogsResponse{Loops: []agentLoopLog{}, ToolCalls: []store.AgentToolCallTrace{}})
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	resp, err := s.loadAgentSessionLogs(ctx, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDeleteAgentSession purges one raw debug run. Product Ask Session turns
// keep their saved answer; their nullable run_id is cleared explicitly because
// SQLite foreign-key enforcement is not assumed for existing local databases.
// DELETE /api/internal/agent-sessions/{id}
func (s *Server) handleDeleteAgentSession(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sessionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid session id"))
		return
	}
	if err := s.purgeAgentSessionDebug(r.Context(), sessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("session not found"))
			return
		}
		if errors.Is(err, errAgentSessionActive) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) purgeAgentSessionDebug(ctx context.Context, sessionID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM memento_agent_session WHERE id = ?`, sessionID,
	).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	switch status {
	case store.AgentRunQueued, store.AgentRunRunning, store.AgentRunWaitingForUser, "active":
		return errAgentSessionActive
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE memento_ask_turn SET run_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE run_id = ?`,
		sessionID,
	); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DELETE FROM memento_agent_tool_call WHERE session_id = ?`,
		`DELETE FROM memento_agent_event WHERE session_id = ?`,
		`DELETE FROM memento_agent_loop WHERE session_id = ?`,
		`DELETE FROM memento_agent_session WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, sessionID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) agentSessionBootstrapLogs(ctx context.Context, sessionID int64) []agentLoopLog {
	var sessionType, requestMetadataJSON, createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT session_type, request_metadata_json, created_at
		FROM memento_agent_session
		WHERE id = ?
	`, sessionID).Scan(&sessionType, &requestMetadataJSON, &createdAt)
	if err != nil || requestMetadataJSON == "" || requestMetadataJSON == "{}" {
		return nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(requestMetadataJSON), &metadata); err != nil {
		return nil
	}
	bootstrap, ok := metadata["person_enrich_bootstrap"]
	if !ok || sessionType != "person_enrich" {
		return nil
	}
	raw, err := json.MarshalIndent(bootstrap, "", "  ")
	if err != nil {
		return nil
	}
	return []agentLoopLog{{
		StepIndex:    0,
		InputType:    "deterministic_bootstrap",
		InputContent: string(raw),
		CreatedAt:    createdAt,
	}}
}

// ---- GET /api/internal/agent-sessions ----

type agentSession struct {
	ID                             int64  `json:"id"`
	SessionType                    string `json:"session_type"`
	EntityID                       string `json:"entity_id"`
	Status                         string `json:"status"`
	Provider                       string `json:"provider"`
	Model                          string `json:"model"`
	UserMessage                    string `json:"user_message"`
	TotalEstimatedInputTokens      int64  `json:"total_estimated_input_tokens"`
	TotalEstimatedOutputTokens     int64  `json:"total_estimated_output_tokens"`
	TotalEstimatedToolResultTokens int64  `json:"total_estimated_tool_result_tokens"`
	TotalModelInputTokens          int64  `json:"total_model_input_tokens"`
	TotalModelOutputTokens         int64  `json:"total_model_output_tokens"`
	TotalModelTokens               int64  `json:"total_model_tokens"`
	CreatedAt                      string `json:"created_at"`
	UpdatedAt                      string `json:"updated_at"`
}

func (s *Server) handleListAgentSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_type, entity_id, status,
		       provider, model, run_input_json,
		       total_estimated_input_tokens, total_estimated_output_tokens,
		       total_estimated_tool_result_tokens, total_model_input_tokens,
		       total_model_output_tokens, total_model_tokens,
		       created_at, updated_at
		FROM memento_agent_session
		ORDER BY id DESC LIMIT 50
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var sessions []agentSession
	for rows.Next() {
		var s agentSession
		var runInputJSON string
		err := rows.Scan(
			&s.ID, &s.SessionType, &s.EntityID, &s.Status,
			&s.Provider, &s.Model, &runInputJSON,
			&s.TotalEstimatedInputTokens, &s.TotalEstimatedOutputTokens,
			&s.TotalEstimatedToolResultTokens, &s.TotalModelInputTokens,
			&s.TotalModelOutputTokens, &s.TotalModelTokens,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.UserMessage = extractRunInputUserMessage(runInputJSON)
		sessions = append(sessions, s)
	}

	if sessions == nil {
		sessions = []agentSession{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

// extractRunInputUserMessage pulls the user_message field out of the
// stored run_input_json blob. Used so the debug UI can display a
// recognizable title for collector and dashboard runs whose entity_id
// is an opaque integer (draft id) or constant ("dashboard"). Returns
// empty string on any failure — callers fall back to entity_id.
func extractRunInputUserMessage(runInputJSON string) string {
	if runInputJSON == "" || runInputJSON == "{}" {
		return ""
	}
	var input struct {
		UserMessage string `json:"user_message"`
	}
	if err := json.Unmarshal([]byte(runInputJSON), &input); err != nil {
		return ""
	}
	return input.UserMessage
}
