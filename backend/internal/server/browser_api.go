// browser_api.go — browser-facing routes that the statically exported web UI
// calls directly. Historically these lived in the Next.js server as thin
// proxies onto the token-gated /api/internal/* endpoints; with the UI served
// by this binary on the same localhost origin, they are first-class routes.
//
// The /api/internal/* registrations remain for tests and tooling; nothing
// here weakens them. The server still binds to 127.0.0.1 only.
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
	"time"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/store"
)

func (s *Server) registerBrowserRoutes() {
	// Composed agent runs: start a run and stream its SSE events in one
	// response (the contract the UI components were built against).
	s.mux.HandleFunc("POST /api/projects/{slug}/generate", s.handleProjectGenerate)
	s.mux.HandleFunc("POST /api/concepts/{slug}/generate", s.handleConceptGenerate)
	s.mux.HandleFunc("POST /api/people/{slug}/enrich", s.handlePersonEnrich)
	s.mux.HandleFunc("POST /api/agents/memento/turn", s.handleMementoTurn)
	s.mux.HandleFunc("POST /api/drafts/{id}/turn", s.handleDraftTurn)

	// Backfill decisions.
	s.mux.HandleFunc("POST /api/drafts/{id}/backfill", s.handleDraftBackfill)
	s.mux.HandleFunc("POST /api/projects/{slug}/backfill", s.handleProjectBackfill)
	s.mux.HandleFunc("POST /api/concepts/{slug}/backfill", s.handleConceptBackfill)

	// Agent debug/admin surface.
	s.mux.HandleFunc("GET /api/agents/sessions", s.handleListAgentSessions)
	s.mux.HandleFunc("GET /api/agents/sessions/{id}", s.handleGetAgentSessionByID)
	s.mux.HandleFunc("GET /api/agents/logs", s.handleAgentLogsQuery)
	s.mux.HandleFunc("GET /api/agents/runs/{id}/events", s.handleAgentRunEvents)
	s.mux.HandleFunc("POST /api/agents/runs/{id}/cancel", s.handleCancelAgentRun)
	s.mux.HandleFunc("DELETE /api/agents/runs/{id}", s.handleDeleteAgentSession)

	// Setup wizard job streaming (the wizard polls under /api/setup/jobs).
	s.mux.HandleFunc("GET /api/setup/jobs/{id}", s.handleJobStream)
	s.mux.HandleFunc("GET /api/setup/jobs/{id}/status", s.handleJobStatus)

	// Debug pages.
	s.mux.HandleFunc("GET /api/debug/system", s.handleDebugSystemInfo)
	s.mux.HandleFunc("GET /api/debug/tools/manifest", s.handleAgentToolsManifest)
	s.mux.HandleFunc("POST /api/debug/tools/invoke", s.handleDebugToolsInvokeTimed)

	// Social group label one-shot.
	s.mux.HandleFunc("POST /api/social/groups/{id}/label", s.handleGenerateGroupLabel)
}

// ---- composed start+stream ----

// startAgentRunStream builds and starts an agent run, then streams its SSE
// events on the same response. Mirrors handleCreateAgentRun's error contract
// for the start phase (409 + active_run_id on concurrent runs).
func (s *Server) startAgentRunStream(w http.ResponseWriter, r *http.Request, req createAgentRunRequest) {
	spec, err := s.buildAgentRunSpec(r.Context(), req)
	if err != nil {
		if concurrent, ok := errors.AsType[*concurrentAgentRunError](err); ok {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":         err.Error(),
				"active_run_id": concurrent.ActiveRunID,
			})
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	askTurnID, hasAskTurn := askTurnIDFromMetadata(spec.RequestMetadata)
	hasAskTurn = hasAskTurn && spec.AgentType == agentrunner.AgentDashboard
	runID, err := s.agents.Start(r.Context(), spec)
	if err != nil {
		if hasAskTurn {
			_ = store.MarkAskTurnFailed(r.Context(), s.db, askTurnID)
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if hasAskTurn {
		_ = store.LinkAskTurnRun(r.Context(), s.db, askTurnID, runID)
		if v, ok := spec.RequestMetadata["ask_session_id"]; ok {
			w.Header().Set("X-Memento-Ask-Session-ID", fmt.Sprint(v))
		}
		if v, ok := spec.RequestMetadata["ask_session_slug"]; ok {
			w.Header().Set("X-Memento-Ask-Session-Slug", fmt.Sprint(v))
		}
		w.Header().Set("X-Memento-Ask-Turn-ID", strconv.FormatInt(askTurnID, 10))
	}
	s.streamAgentRun(w, r, runID, 0)
}

func (s *Server) handleSlugAgentRun(w http.ResponseWriter, r *http.Request, agentType string) {
	slug := r.PathValue("slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing slug"))
		return
	}
	s.startAgentRunStream(w, r, createAgentRunRequest{
		AgentType: agentType,
		EntityID:  slug,
	})
}

func (s *Server) handleProjectGenerate(w http.ResponseWriter, r *http.Request) {
	s.handleSlugAgentRun(w, r, "project_compile")
}

func (s *Server) handleConceptGenerate(w http.ResponseWriter, r *http.Request) {
	s.handleSlugAgentRun(w, r, "concept_compile")
}

func (s *Server) handlePersonEnrich(w http.ResponseWriter, r *http.Request) {
	s.handleSlugAgentRun(w, r, "person_enrich")
}

type mementoTurnRequest struct {
	Message               string `json:"message"`
	AskSessionID          int64  `json:"ask_session_id"`
	PreviousInteractionID string `json:"previous_interaction_id"`
	History               []struct {
		Role    string `json:"role"`
		Text    string `json:"text"`
		Content string `json:"content"`
	} `json:"history"`
	ContextRefs []any `json:"context_refs"`
}

func (s *Server) handleMementoTurn(w http.ResponseWriter, r *http.Request) {
	var body mementoTurnRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	userMessage := strings.TrimSpace(body.Message)
	if userMessage == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("message is required"))
		return
	}
	history := make([]map[string]string, 0, len(body.History))
	for _, line := range body.History {
		role := ""
		switch line.Role {
		case "assistant":
			role = "assistant"
		case "user":
			role = "user"
		}
		content := strings.TrimSpace(line.Content)
		if content == "" {
			content = strings.TrimSpace(line.Text)
		}
		if role == "" || content == "" {
			continue
		}
		history = append(history, map[string]string{"role": role, "content": content})
	}
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	contextRefs := body.ContextRefs
	if contextRefs == nil {
		contextRefs = []any{}
	}
	metadata := map[string]any{
		"history":      history,
		"context_refs": contextRefs,
	}
	if body.AskSessionID > 0 {
		metadata["ask_session_id"] = body.AskSessionID
	}
	s.startAgentRunStream(w, r, createAgentRunRequest{
		AgentType:             "dashboard",
		EntityID:              "dashboard",
		UserMessage:           userMessage,
		PreviousInteractionID: body.PreviousInteractionID,
		RequestMetadata:       metadata,
	})
}

func (s *Server) handleDraftTurn(w http.ResponseWriter, r *http.Request) {
	draftID, err := parseDraftID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	userMessage := strings.TrimSpace(body.Message)
	if userMessage == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("message is required"))
		return
	}
	draft, err := store.GetDraft(r.Context(), s.db, draftID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("draft %d not found", draftID))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.startAgentRunStream(w, r, createAgentRunRequest{
		AgentType:             "collector",
		EntityID:              strconv.FormatInt(draftID, 10),
		UserMessage:           userMessage,
		PreviousInteractionID: draft.InteractionID,
	})
}

// ---- backfill decisions ----

type backfillRequest struct {
	Decision   string  `json:"decision"`
	DecisionID string  `json:"decision_id"`
	MessageIDs []int64 `json:"message_ids"`
}

func decodeBackfillRequest(w http.ResponseWriter, r *http.Request) (backfillRequest, bool) {
	var req backfillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return req, false
	}
	if req.Decision != "accept" && req.Decision != "skip" && req.Decision != "reject" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decision must be accept or skip"))
		return req, false
	}
	if req.DecisionID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decision_id is required"))
		return req, false
	}
	return req, true
}

// resolveBackfillDecision marks the agent decision row resolved and writes
// the final response. On a conflict (decision already resolved), responds
// 409 with the partial result, matching the old Next.js routes.
func (s *Server) resolveBackfillDecision(w http.ResponseWriter, r *http.Request, req backfillRequest, addedCount int) {
	accepted := req.Decision == "accept"
	status := "skipped"
	if accepted {
		status = "accepted"
	}
	result := map[string]any{"accepted": accepted, "added_count": addedCount}
	resultJSON, _ := json.Marshal(result)
	if _, err := store.ResolveAgentDecision(r.Context(), s.db, req.DecisionID, status, string(resultJSON)); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       err.Error(),
			"added_count": addedCount,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"accepted":    accepted,
		"added_count": addedCount,
	})
}

func (s *Server) handleDraftBackfill(w http.ResponseWriter, r *http.Request) {
	draftID, err := parseDraftID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	req, ok := decodeBackfillRequest(w, r)
	if !ok {
		return
	}
	addedCount := 0
	if req.Decision == "accept" && len(req.MessageIDs) > 0 {
		draft, err := store.GetDraft(r.Context(), s.db, draftID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, fmt.Errorf("draft %d not found", draftID))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var bundle map[string]any
		if err := json.Unmarshal([]byte(draft.EntitiesJSON), &bundle); err != nil || bundle == nil {
			bundle = map[string]any{"name": "", "people": []any{}, "messages": []any{}, "threads": []any{}}
		}
		messages, _ := bundle["messages"].([]any)
		existing := map[int64]bool{}
		for _, m := range messages {
			if obj, ok := m.(map[string]any); ok {
				if id, err := metadataInt64(obj["message_id"]); err == nil {
					existing[id] = true
				}
			}
		}
		for _, id := range req.MessageIDs {
			if existing[id] {
				continue
			}
			messages = append(messages, map[string]any{"message_id": id})
			existing[id] = true
			addedCount++
		}
		if addedCount > 0 {
			bundle["messages"] = messages
			raw, err := json.Marshal(bundle)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			if err := store.UpdateDraftEntities(r.Context(), s.db, draftID, string(raw)); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
	}
	s.resolveBackfillDecision(w, r, req, addedCount)
}

func (s *Server) handleProjectBackfill(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	req, ok := decodeBackfillRequest(w, r)
	if !ok {
		return
	}
	addedCount := 0
	if req.Decision == "accept" && len(req.MessageIDs) > 0 {
		result, err := s.addProjectMessages(r.Context(), addProjectMessagesRequest{
			ProjectSlug: slug,
			MessageIDs:  req.MessageIDs,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		addedCount = result["added"]
	}
	s.resolveBackfillDecision(w, r, req, addedCount)
}

func (s *Server) handleConceptBackfill(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	req, ok := decodeBackfillRequest(w, r)
	if !ok {
		return
	}
	addedCount := 0
	if req.Decision == "accept" && len(req.MessageIDs) > 0 {
		result, err := s.addConceptMessages(r.Context(), addConceptMessagesRequest{
			ConceptSlug: slug,
			MessageIDs:  req.MessageIDs,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		addedCount = result["added"]
	}
	s.resolveBackfillDecision(w, r, req, addedCount)
}

// ---- agent debug/admin wrappers ----

// handleGetAgentSessionByID returns one agent session from the recent list.
func (s *Server) handleGetAgentSessionByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	sessionID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || sessionID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid session id"))
		return
	}
	row := s.db.QueryRowContext(r.Context(), `
		SELECT id, session_type, entity_id, status, created_at, updated_at
		FROM memento_agent_session WHERE id = ?`, sessionID)
	var resp struct {
		ID          int64  `json:"id"`
		SessionType string `json:"session_type"`
		EntityID    string `json:"entity_id"`
		Status      string `json:"status"`
		CreatedAt   string `json:"created_at"`
		UpdatedAt   string `json:"updated_at"`
	}
	if err := row.Scan(&resp.ID, &resp.SessionType, &resp.EntityID, &resp.Status, &resp.CreatedAt, &resp.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("session not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAgentLogsQuery serves GET /api/agents/logs with either
// ?sessionId=N or ?type=...&entityId=... (the UI's query contract).
func (s *Server) handleAgentLogsQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if sessionIDStr := q.Get("sessionId"); sessionIDStr != "" {
		sessionID, err := strconv.ParseInt(sessionIDStr, 10, 64)
		if err != nil || sessionID <= 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid sessionId"))
			return
		}
		logs, err := s.loadAgentSessionLogs(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, logs)
		return
	}
	sessionType := q.Get("type")
	entityID := q.Get("entityId")
	if sessionType == "" || entityID == "" {
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("type and entityId are required, or sessionId must be provided"))
		return
	}
	// Reuse the internal handler's logic via its expected query keys.
	r2 := r.Clone(r.Context())
	q2 := r2.URL.Query()
	q2.Set("session_type", sessionType)
	q2.Set("entity_id", entityID)
	r2.URL.RawQuery = q2.Encode()
	s.handleGetLatestAgentSessionLogs(w, r2)
}

// handleDebugToolsInvokeTimed wraps the read-only debug tool invocation with
// timing and payload-size metadata for the Debug Tools UI.
func (s *Server) handleDebugToolsInvokeTimed(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req debugInvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Tool == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("tool is required"))
		return
	}
	writeTimed := func(status int, payload map[string]any) {
		payload["tool"] = req.Tool
		payload["status"] = status
		payload["duration_ms"] = time.Since(start).Milliseconds()
		writeJSON(w, status, payload)
	}
	tool, ok := s.agentTools()[req.Tool]
	if !ok {
		writeTimed(http.StatusNotFound, map[string]any{
			"response_size_bytes": 0, "estimated_tokens": 0,
			"error": fmt.Sprintf("unknown tool %q", req.Tool),
		})
		return
	}
	if !isDebugInvocableTool(req.Tool, tool) {
		writeTimed(http.StatusForbidden, map[string]any{
			"response_size_bytes": 0, "estimated_tokens": 0,
			"error": fmt.Sprintf("tool %q is not available in read-only debug mode", req.Tool),
		})
		return
	}
	raw := req.Params
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		writeTimed(http.StatusBadRequest, map[string]any{
			"response_size_bytes": 0, "estimated_tokens": 0,
			"error": "params must be valid JSON",
		})
		return
	}
	result, err := tool.Handler(r.Context(), agentrunner.ToolContext{
		RunID: 0,
		RunSpec: agentrunner.RunSpec{
			AgentType:       agentrunner.AgentDashboard,
			EntityID:        "debug-tools",
			RequestMetadata: map[string]any{},
		},
		Emit:      func(context.Context, agentrunner.AgentEvent) error { return nil },
		SetStatus: func(context.Context, string) error { return nil },
	}, raw)
	if err != nil {
		writeTimed(http.StatusBadRequest, map[string]any{
			"response_size_bytes": 0, "estimated_tokens": 0,
			"error": err.Error(),
		})
		return
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		writeTimed(http.StatusInternalServerError, map[string]any{
			"response_size_bytes": 0, "estimated_tokens": 0,
			"error": err.Error(),
		})
		return
	}
	var data any
	_ = json.Unmarshal(encoded, &data)
	writeTimed(http.StatusOK, map[string]any{
		"response_size_bytes": len(encoded),
		"estimated_tokens":    len(encoded) / 4,
		"data":                data,
	})
}
