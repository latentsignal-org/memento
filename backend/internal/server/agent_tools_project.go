// Package server — token-gated agent-tool endpoints for the project agent
// (Phase 3). Reads the message bundle for an existing project and persists
// generated narrative sections back to memento_project_narrative, honoring
// user-edited sections.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"memento/backend/internal/project"
	"memento/backend/internal/refresh"
)

// ---- POST /api/internal/agent-tools/get-project-bundle ----

type getProjectBundleRequest struct {
	ProjectID int64  `json:"project_id"`
	Detail    string `json:"detail"` // "full" | "index"
}

func (s *Server) handleAgentGetProjectBundle(w http.ResponseWriter, r *http.Request) {
	var req getProjectBundleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ProjectID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("project_id is required"))
		return
	}
	if req.Detail == "" {
		req.Detail = "full"
	}
	bundle, err := project.GetProjectBundle(r.Context(), s.db, req.ProjectID, req.Detail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if bundle == nil {
		bundle = []project.MessageBundleItem{}
	}
	writeJSON(w, http.StatusOK, bundle)
}

// ---- POST /api/internal/agent-tools/write-section ----

type writeSectionRequest struct {
	ProjectID        int64   `json:"project_id"`
	Section          string  `json:"section"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

// allowedProjectSections mirrors the existing schema in project/llm.go.
// Other sections will be rejected so the frontend's renderer never has to
// guess at the shape of an unknown section.
var allowedProjectSections = map[string]bool{
	"summary":               true,
	"phases":                true,
	"friction_points":       true,
	"current_understanding": true,
}

func (s *Server) handleAgentWriteSection(w http.ResponseWriter, r *http.Request) {
	var req writeSectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ProjectID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("project_id is required"))
		return
	}
	if !allowedProjectSections[req.Section] {
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("section %q not allowed (want one of summary|phases|friction_points|current_understanding)", req.Section))
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("content is required"))
		return
	}
	// Source attribution is non-negotiable — see plan §4.7.
	if len(req.SourceMessageIDs) == 0 {
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("source_message_ids must contain at least one message id"))
		return
	}

	result, err := saveProjectSection(r.Context(), s.db, req.ProjectID, req.Section, req.Content, req.SourceMessageIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type saveSectionResult struct {
	OK          bool   `json:"ok"`
	Skipped     bool   `json:"skipped,omitempty"`
	SkipReason  string `json:"skip_reason,omitempty"`
	CitationCnt int    `json:"citation_count"`
}

// saveProjectSection upserts a narrative section, refusing to overwrite
// rows marked edited_by='user'. Returns a structured result so the agent
// sees whether its write actually landed.
func saveProjectSection(
	ctx context.Context, db *sql.DB,
	projectID int64, section, content string, msgIDs []int64,
) (saveSectionResult, error) {
	var editedBy string
	err := db.QueryRowContext(ctx,
		`SELECT edited_by FROM memento_project_narrative WHERE project_id = ? AND section = ?`,
		projectID, section,
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
		INSERT INTO memento_project_narrative (project_id, section, content, source_message_ids, generated_at, edited_by)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')
		ON CONFLICT(project_id, section) DO UPDATE SET
		    content = excluded.content,
		    source_message_ids = excluded.source_message_ids,
		    generated_at = CURRENT_TIMESTAMP,
		    edited_by = 'llm'`,
		projectID, section, content, string(ids),
	)
	if err != nil {
		return saveSectionResult{}, err
	}
	return saveSectionResult{OK: true, CitationCnt: len(msgIDs)}, nil
}

// ---- POST /api/internal/agent-tools/refresh-projects-rollup ----

// handleAgentRefreshProjects refreshes the projects rollup table so the UI
// reads the freshly-written narrative immediately. Called by the project-agent
// SSE route after the agent run finishes successfully.
func (s *Server) handleAgentRefreshProjects(w http.ResponseWriter, r *http.Request) {
	if _, err := refresh.RefreshProjectsReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
