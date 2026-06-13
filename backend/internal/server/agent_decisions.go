package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"memento/backend/internal/store"
)

type createAgentDecisionRequest struct {
	ID           string          `json:"id"`
	DecisionType string          `json:"decision_type"`
	EntityType   string          `json:"entity_type"`
	EntityID     string          `json:"entity_id"`
	PayloadJSON  json.RawMessage `json:"payload_json"`
}

func (s *Server) handleCreateAgentDecision(w http.ResponseWriter, r *http.Request) {
	var req createAgentDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	payload := string(req.PayloadJSON)
	if payload == "" {
		payload = "{}"
	}

	err := store.CreateAgentDecision(r.Context(), s.db, store.AgentDecision{
		ID:           req.ID,
		DecisionType: req.DecisionType,
		EntityType:   req.EntityType,
		EntityID:     req.EntityID,
		PayloadJSON:  payload,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	decision, err := store.GetAgentDecision(r.Context(), s.db, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, decision)
}

func (s *Server) handleGetAgentDecision(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing decision id"))
		return
	}
	decision, err := store.GetAgentDecision(r.Context(), s.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("decision %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

type updateAgentDecisionRequest struct {
	Status     string          `json:"status"`
	ResultJSON json.RawMessage `json:"result_json"`
}

func (s *Server) handleUpdateAgentDecision(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("missing decision id"))
		return
	}
	var req updateAgentDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result := string(req.ResultJSON)
	if result == "" {
		result = "{}"
	}
	decision, err := store.ResolveAgentDecision(r.Context(), s.db, id, req.Status, result)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("decision %s not found", id))
			return
		}
		if decision.ID != "" {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    err.Error(),
				"decision": decision,
			})
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}
