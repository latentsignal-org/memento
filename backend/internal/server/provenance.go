package server

import (
	"database/sql"
	"errors"
	"net/http"

	"memento/backend/internal/store"
)

type draftProvenanceResponse struct {
	DraftID           int64                 `json:"draft_id"`
	Kind              string                `json:"kind"`
	Status            string                `json:"status"`
	CommittedEntityID *int64                `json:"committed_entity_id,omitempty"`
	Revisions         []store.DraftRevision `json:"revisions"`
	CollectorLoops    []collectorLoopLog    `json:"collector_loops"`
}

type collectorLoopLog struct {
	SessionID       int64  `json:"session_id"`
	StepIndex       int    `json:"step_index"`
	InputType       string `json:"input_type"`
	InputContent    string `json:"input_content"`
	AssistantText   string `json:"assistant_text"`
	ReasoningText   string `json:"reasoning_text"`
	ToolCallsJSON   string `json:"tool_calls_json"`
	ToolResultsJSON string `json:"tool_results_json"`
	DurationMs      int    `json:"duration_ms"`
	CreatedAt       string `json:"created_at"`
}

func (s *Server) handleGetProjectProvenance(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var draftID sql.NullInt64
	err := s.db.QueryRowContext(r.Context(), `SELECT origin_draft_id FROM memento_project WHERE slug = ?`, slug).Scan(&draftID)
	if err == sql.ErrNoRows || !draftID.Valid {
		writeError(w, http.StatusNotFound, errors.New("project provenance not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeDraftProvenance(w, r, draftID.Int64)
}

func (s *Server) handleGetConceptProvenance(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var draftID sql.NullInt64
	err := s.db.QueryRowContext(r.Context(), `SELECT origin_draft_id FROM memento_concept WHERE slug = ?`, slug).Scan(&draftID)
	if err == sql.ErrNoRows || !draftID.Valid {
		writeError(w, http.StatusNotFound, errors.New("concept provenance not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.writeDraftProvenance(w, r, draftID.Int64)
}

func (s *Server) writeDraftProvenance(w http.ResponseWriter, r *http.Request, draftID int64) {
	draft, err := store.GetDraft(r.Context(), s.db, draftID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, draft_id, revision_kind, transcript_json, entities_json, created_at
		FROM memento_draft_revision
		WHERE draft_id = ?
		ORDER BY id ASC
	`, draftID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var revisions []store.DraftRevision
	for rows.Next() {
		var rev store.DraftRevision
		if err := rows.Scan(&rev.ID, &rev.DraftID, &rev.RevisionKind, &rev.TranscriptJSON, &rev.EntitiesJSON, &rev.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		revisions = append(revisions, rev)
	}
	if revisions == nil {
		revisions = []store.DraftRevision{}
	}

	loopRows, err := s.db.QueryContext(r.Context(), `
		SELECT l.session_id, l.step_index, l.input_type, l.input_content, l.assistant_text,
		       l.reasoning_text, l.tool_calls_json, l.tool_results_json, COALESCE(l.duration_ms, 0), l.created_at
		FROM memento_agent_loop l
		JOIN memento_agent_session s ON s.id = l.session_id
		WHERE s.session_type = 'collector' AND s.entity_id = ?
		ORDER BY l.created_at ASC, l.id ASC
	`, draftID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer loopRows.Close()

	var collectorLoops []collectorLoopLog
	for loopRows.Next() {
		var loop collectorLoopLog
		if err := loopRows.Scan(
			&loop.SessionID, &loop.StepIndex, &loop.InputType, &loop.InputContent, &loop.AssistantText,
			&loop.ReasoningText, &loop.ToolCallsJSON, &loop.ToolResultsJSON, &loop.DurationMs, &loop.CreatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		collectorLoops = append(collectorLoops, loop)
	}
	if collectorLoops == nil {
		collectorLoops = []collectorLoopLog{}
	}

	writeJSON(w, http.StatusOK, draftProvenanceResponse{
		DraftID:           draft.ID,
		Kind:              draft.Kind,
		Status:            draft.Status,
		CommittedEntityID: draft.CommittedEntityID,
		Revisions:         revisions,
		CollectorLoops:    collectorLoops,
	})
}
