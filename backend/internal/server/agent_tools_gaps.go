// Package server — token-gated agent-tool endpoints for gap detection and
// bundle backfill. detect-gaps is called by the project/concept/dashboard
// agents. add-project-messages is the accept path for propose_backfill.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"memento/backend/internal/concept"
	"memento/backend/internal/gaps"
	"memento/backend/internal/project"
)

type detectGapsRequest struct {
	MessageIDs  []int64 `json:"message_ids"`
	Mode        string  `json:"mode"` // "chronological" | "thematic" | "participant"
	MinSeverity string  `json:"min_severity"`
	MaxGaps     int     `json:"max_gaps"`
}

type detectGapsWithResultsRequest struct {
	MessageIDs  []int64 `json:"message_ids"`
	Mode        string  `json:"mode"` // "chronological" | "thematic" | "participant"
	MinSeverity string  `json:"min_severity"`
	MaxGaps     int     `json:"max_gaps"`
}

type GapWithResults struct {
	Kind             string       `json:"kind"`
	Description      string       `json:"description"`
	AnchorMessageIDs []int64      `json:"anchor_message_ids"`
	SearchHints      []string     `json:"search_hints"`
	Severity         string       `json:"severity"`
	Results          []messageHit `json:"results"`
}

// ---- POST /api/internal/agent-tools/add-project-messages ----
// Called by the propose_backfill accept path in Next.js after the user
// clicks "Accept & Continue". Attaches the accepted message IDs to the
// project bundle using the same logic as an explicit manual add.

type addProjectMessagesRequest struct {
	ProjectSlug string  `json:"project_slug"`
	MessageIDs  []int64 `json:"message_ids"`
}

func (s *Server) handleAgentAddProjectMessages(w http.ResponseWriter, r *http.Request) {
	var req addProjectMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ProjectSlug == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("project_slug is required"))
		return
	}
	if len(req.MessageIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"added": 0})
		return
	}
	result, err := s.addProjectMessages(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) addProjectMessages(ctx context.Context, req addProjectMessagesRequest) (map[string]int, error) {
	if req.ProjectSlug == "" {
		return nil, fmt.Errorf("project_slug is required")
	}
	if len(req.MessageIDs) == 0 {
		return map[string]int{"added": 0}, nil
	}
	added := 0
	for _, id := range req.MessageIDs {
		if err := project.AddMessageExplicit(ctx, s.db, req.ProjectSlug, id, "backfill"); err != nil {
			return nil, fmt.Errorf("add message %d: %w", id, err)
		}
		added++
	}
	return map[string]int{"added": added}, nil
}

// ---- POST /api/internal/agent-tools/add-concept-messages ----

type addConceptMessagesRequest struct {
	ConceptSlug string  `json:"concept_slug"`
	MessageIDs  []int64 `json:"message_ids"`
}

func (s *Server) handleAgentAddConceptMessages(w http.ResponseWriter, r *http.Request) {
	var req addConceptMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ConceptSlug == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("concept_slug is required"))
		return
	}
	if len(req.MessageIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"added": 0})
		return
	}
	result, err := s.addConceptMessages(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) addConceptMessages(ctx context.Context, req addConceptMessagesRequest) (map[string]int, error) {
	if req.ConceptSlug == "" {
		return nil, fmt.Errorf("concept_slug is required")
	}
	if len(req.MessageIDs) == 0 {
		return map[string]int{"added": 0}, nil
	}
	added := 0
	for _, id := range req.MessageIDs {
		if err := concept.AddMessageExplicit(ctx, s.db, req.ConceptSlug, id, "backfill", "", 1.0); err != nil {
			return nil, fmt.Errorf("add message %d: %w", id, err)
		}
		added++
	}
	return map[string]int{"added": added}, nil
}

// ---- POST /api/internal/agent-tools/detect-gaps ----

func severityInt(sev string) int {
	switch strings.ToLower(sev) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 1
	}
}

func (s *Server) handleAgentDetectGaps(w http.ResponseWriter, r *http.Request) {
	var req detectGapsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.MessageIDs) == 0 {
		writeJSON(w, http.StatusOK, []gaps.Gap{})
		return
	}
	if req.Mode == "" {
		req.Mode = "chronological"
	}
	if req.MinSeverity == "" {
		req.MinSeverity = "low"
	}
	if req.MaxGaps <= 0 {
		req.MaxGaps = 5
	}

	detected, err := gaps.Detect(r.Context(), s.reader.DB(), req.MessageIDs, req.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("detect gaps: %w", err))
		return
	}
	if detected == nil {
		detected = []gaps.Gap{}
	}

	var filtered []gaps.Gap
	minSevVal := severityInt(req.MinSeverity)
	for _, gap := range detected {
		if severityInt(gap.Severity) >= minSevVal {
			filtered = append(filtered, gap)
		}
	}
	if len(filtered) > req.MaxGaps {
		filtered = filtered[:req.MaxGaps]
	}
	if filtered == nil {
		filtered = []gaps.Gap{}
	}

	writeJSON(w, http.StatusOK, filtered)
}

// ---- POST /api/internal/agent-tools/detect-gaps-with-results ----

func (s *Server) handleAgentDetectGapsWithResults(w http.ResponseWriter, r *http.Request) {
	var req detectGapsWithResultsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.MessageIDs) == 0 {
		writeJSON(w, http.StatusOK, []GapWithResults{})
		return
	}
	if req.Mode == "" {
		req.Mode = "chronological"
	}
	if req.MinSeverity == "" {
		req.MinSeverity = "low"
	}
	if req.MaxGaps <= 0 {
		req.MaxGaps = 5
	}

	detected, err := gaps.Detect(r.Context(), s.reader.DB(), req.MessageIDs, req.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("detect gaps: %w", err))
		return
	}

	var filtered []gaps.Gap
	minSevVal := severityInt(req.MinSeverity)
	for _, gap := range detected {
		if severityInt(gap.Severity) >= minSevVal {
			filtered = append(filtered, gap)
		}
	}
	if len(filtered) > req.MaxGaps {
		filtered = filtered[:req.MaxGaps]
	}

	var results []GapWithResults
	for _, gap := range filtered {
		var gapRes []messageHit
		seen := make(map[int64]bool)
		for _, hint := range gap.SearchHints {
			ids, err := runMsgvaultSearchCLI(r.Context(), hint, 5)
			if err != nil {
				continue
			}
			var uniqIDs []int64
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					uniqIDs = append(uniqIDs, id)
				}
			}
			if len(uniqIDs) > 0 {
				hits, err := enrichMessageIDs(r.Context(), s.reader.DB(), uniqIDs)
				if err == nil {
					gapRes = append(gapRes, hits...)
				}
			}
		}
		if gapRes == nil {
			gapRes = []messageHit{}
		}
		results = append(results, GapWithResults{
			Kind:             gap.Kind,
			Description:      gap.Description,
			AnchorMessageIDs: gap.AnchorMessageIDs,
			SearchHints:      gap.SearchHints,
			Severity:         gap.Severity,
			Results:          gapRes,
		})
	}
	if results == nil {
		results = []GapWithResults{}
	}
	writeJSON(w, http.StatusOK, results)
}
