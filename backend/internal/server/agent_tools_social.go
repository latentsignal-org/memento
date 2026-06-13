// Token-gated agent-tool endpoints for the social communication graph.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"memento/backend/internal/llm"
	"memento/backend/internal/social"
)

// --- GET /api/people/{slug}/network ---

func (s *Server) handleGetPersonNetwork(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ctx := r.Context()
	limit := parseIntQuery(r, "limit", 10)

	var personID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT person_id FROM memento_people_report WHERE slug = ?`, slug,
	).Scan(&personID)
	if err == sql.ErrNoRows || isNotSetUp(err) {
		writeError(w, http.StatusNotFound, fmt.Errorf("person not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	pn, err := social.LoadPersonNetwork(ctx, s.db, personID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if pn == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no social data for person %d — run refresh to build the graph", personID))
		return
	}
	writeJSON(w, http.StatusOK, pn)
}

// --- POST /api/internal/agent-tools/get-person-network ---

type getPersonNetworkRequest struct {
	PersonID int64 `json:"person_id"`
	Limit    int   `json:"limit"`
}

func (s *Server) handleAgentGetPersonNetwork(w http.ResponseWriter, r *http.Request) {
	var req getPersonNetworkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PersonID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_id is required"))
		return
	}
	pn, err := social.LoadPersonNetwork(r.Context(), s.db, req.PersonID, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if pn == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("person %d not found", req.PersonID))
		return
	}
	writeJSON(w, http.StatusOK, pn)
}

// --- POST /api/internal/agent-tools/get-cluster ---

type getClusterRequest struct {
	PersonID  int64 `json:"person_id"`
	ClusterID int64 `json:"cluster_id"`
}

type getGroupRequest struct {
	PersonID int64 `json:"person_id"`
	GroupID  int64 `json:"group_id"`
}

func (s *Server) handleAgentGetCluster(w http.ResponseWriter, r *http.Request) {
	var req getClusterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cd, err := s.getClusterForAgent(r.Context(), req)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, cd)
}

func (s *Server) getClusterForAgent(ctx context.Context, req getClusterRequest) (any, error) {
	clusterID := req.ClusterID
	if clusterID <= 0 && req.PersonID > 0 {
		// Resolve person -> cluster_id directly from the metric table.
		var cid sql.NullInt64
		err := s.db.QueryRowContext(ctx,
			`SELECT cluster_id FROM memento_social_metric WHERE person_id = ?`, req.PersonID,
		).Scan(&cid)
		if err == sql.ErrNoRows || (err == nil && !cid.Valid) {
			return map[string]any{
				"cluster": nil,
				"message": "person is not assigned to a cluster",
			}, nil
		}
		if err != nil {
			return nil, err
		}
		clusterID = cid.Int64
	}
	if clusterID <= 0 {
		return nil, fmt.Errorf("person_id or cluster_id is required")
	}

	cd, err := social.LoadCluster(ctx, s.db, clusterID)
	if err != nil {
		return nil, err
	}
	if cd == nil {
		return nil, fmt.Errorf("cluster %d not found", clusterID)
	}
	// Large clusters are catch-all groups with no meaningful group identity.
	// Return metadata only so the model is not overwhelmed by a noisy member list.
	if cd.Size > 100 {
		return map[string]any{
			"cluster_id": cd.ClusterID,
			"size":       cd.Size,
			"density":    cd.Density,
			"label":      cd.Label,
			"members":    []any{},
			"note":       fmt.Sprintf("Cluster has %d members and is too large for individual relationship analysis. Member list omitted. Use get_person_network for direct neighbor context instead.", cd.Size),
		}, nil
	}
	return cd, nil
}

func (s *Server) getGroupForAgent(ctx context.Context, req getGroupRequest) (any, error) {
	groupID := req.GroupID
	if groupID <= 0 && req.PersonID > 0 {
		err := s.db.QueryRowContext(ctx, `
			SELECT gm.group_id
			FROM memento_social_group_member gm
			JOIN memento_social_group g ON g.group_id = gm.group_id
			WHERE gm.person_id = ? AND g.is_actionable = 1
		`, req.PersonID).Scan(&groupID)
		if err == sql.ErrNoRows {
			return map[string]any{
				"group":   nil,
				"message": "person is not assigned to an actionable group",
			}, nil
		}
		if err != nil {
			return nil, err
		}
	}
	if groupID <= 0 {
		return nil, fmt.Errorf("person_id or group_id is required")
	}

	g, err := social.LoadGroup(ctx, s.db, groupID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, fmt.Errorf("group %d not found", groupID)
	}
	if g.Size > 100 {
		return map[string]any{
			"group_id":           g.GroupID,
			"size":               g.Size,
			"density":            g.Density,
			"label":              g.Label,
			"is_actionable":      g.IsActionable,
			"suppression_reason": g.SuppressionReason,
			"members":            []any{},
			"note":               fmt.Sprintf("Group has %d members and is too large for individual relationship analysis. Member list omitted. Use get_person_network for direct neighbor context instead.", g.Size),
		}, nil
	}
	return g, nil
}

// --- POST /api/internal/agent-tools/find-bridges-between ---

type findBridgesRequest struct {
	PersonAID int64 `json:"person_a_id"`
	PersonBID int64 `json:"person_b_id"`
	Limit     int   `json:"limit"`
}

func (s *Server) handleAgentFindBridgesBetween(w http.ResponseWriter, r *http.Request) {
	var req findBridgesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.PersonAID <= 0 || req.PersonBID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_a_id and person_b_id are required"))
		return
	}
	if req.PersonAID == req.PersonBID {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_a_id and person_b_id must differ"))
		return
	}

	bridges, err := social.FindBridgesBetween(r.Context(), s.db, req.PersonAID, req.PersonBID, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"person_a_id": req.PersonAID,
		"person_b_id": req.PersonBID,
		"bridges":     bridges,
	})
}

// --- POST /api/internal/agent-tools/find-missing-collaborators ---

type findMissingCollaboratorsRequest struct {
	PersonIDs         []int64 `json:"person_ids"`
	Limit             int     `json:"limit"`
	MinCombinedWeight float64 `json:"min_combined_weight"`
}

func (s *Server) handleAgentFindMissingCollaborators(w http.ResponseWriter, r *http.Request) {
	var req findMissingCollaboratorsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.PersonIDs) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("person_ids must be non-empty"))
		return
	}

	missing, err := social.FindMissingCollaborators(r.Context(), s.db, req.PersonIDs, req.Limit, req.MinCombinedWeight)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"input_person_ids":      req.PersonIDs,
		"missing_collaborators": missing,
	})
}

// --- GET /api/social/clusters ---
func (s *Server) handleGetClusters(w http.ResponseWriter, r *http.Request) {
	clusters, err := social.LoadClusters(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"clusters": clusters,
	})
}

// --- GET /api/social/groups ---
//
// Community-detected groups (strict edges + Louvain), plus user-saved groups.
// Saved groups render first; candidates follow. Hidden by default: suppressed
// (non-actionable) groups and dismissed (soft-deleted) groups. Toggle with
// ?include_suppressed=1 and ?include_dismissed=1.
func (s *Server) handleGetGroups(w http.ResponseWriter, r *http.Request) {
	opts := social.LoadGroupsOptions{
		IncludeSuppressed: r.URL.Query().Get("include_suppressed") == "1",
		IncludeDismissed:  r.URL.Query().Get("include_dismissed") == "1",
	}
	groups, err := social.LoadGroups(r.Context(), s.db, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups": groups,
	})
}

// --- POST /api/internal/social/groups/{id}/generate-label ---
//
// Generates a concise label suggestion for an actionable group from its member
// names. Suppressed (non-actionable) groups are refused — they are too broad to
// label meaningfully.
func (s *Server) handleGenerateGroupLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r)
	if !ok {
		return
	}
	g, err := social.LoadGroup(r.Context(), s.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if g == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("group %d not found", id))
		return
	}
	if !g.IsActionable {
		writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("group %d is too broad to label (%s)", id, g.SuppressionReason))
		return
	}

	names := groupMemberNames(g)
	if len(names) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("group %d has no named members to label", id))
		return
	}
	// Reuse the cluster label prompt; groups carry no thread subjects.
	prompt := buildClusterLabelPrompt(&social.ClusterLabelContext{MemberNames: names})
	resp, err := llm.OneShot(r.Context(), llm.OneShotRequest{
		Prompt: prompt,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("model label generation failed: %w", err))
		return
	}
	label := cleanGeneratedText(resp.Text)
	writeJSON(w, http.StatusOK, map[string]any{"label": label})
}

// parsePathID extracts the {id} path value as int64, writing a 400 on failure.
func parsePathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	idStr := r.PathValue("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid id: %s", idStr))
		return 0, false
	}
	return id, true
}

// groupMemberNames returns the non-empty canonical names of a group's members.
func groupMemberNames(g *social.GroupDetail) []string {
	var names []string
	for _, m := range g.Members {
		if n := strings.TrimSpace(m.CanonicalName); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// --- GET /api/social/leaderboard ---
func (s *Server) handleGetLeaderboard(w http.ResponseWriter, r *http.Request) {
	lb, err := social.LoadLeaderboard(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, lb)
}

// --- GET /api/internal/social/clusters/{id}/label-context ---
func (s *Server) handleGetClusterLabelContext(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid cluster id: %s", idStr))
		return
	}

	ctx, err := social.LoadClusterLabelingContext(r.Context(), s.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}

// --- POST /api/internal/social/clusters/{id}/label ---
type postClusterLabelRequest struct {
	Label string `json:"label"`
}

func (s *Server) handlePostClusterLabel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid cluster id: %s", idStr))
		return
	}

	var req postClusterLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("label is required"))
		return
	}

	if err := s.persistClusterLabel(r.Context(), id, req.Label); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) persistClusterLabel(ctx context.Context, clusterID int64, label string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE memento_social_cluster
		SET label = ?, label_source = 'llm', computed_at = CURRENT_TIMESTAMP
		WHERE cluster_id = ?
	`, label, clusterID)
	return err
}

// --- POST /api/internal/social/clusters/{id}/generate-label ---
//
// Loads the cluster's labeling context, asks the configured model provider for
// a concise label, and persists it.
func (s *Server) handleGenerateClusterLabel(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	var id int64
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid cluster id: %s", idStr))
		return
	}

	lc, err := social.LoadClusterLabelingContext(r.Context(), s.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if lc == nil || (len(lc.MemberNames) == 0 && len(lc.ThreadSubjects) == 0) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("insufficient context to generate label"))
		return
	}

	prompt := buildClusterLabelPrompt(lc)
	resp, err := llm.OneShot(r.Context(), llm.OneShotRequest{
		Prompt: prompt,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("model label generation failed: %w", err))
		return
	}
	label := cleanGeneratedText(resp.Text)
	if label == "" {
		label = "Unnamed Cluster"
	}

	if err := s.persistClusterLabel(r.Context(), id, label); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"label": label})
}

func buildClusterLabelPrompt(lc *social.ClusterLabelContext) string {
	members := bulletList(lc.MemberNames, "(None listed)")
	subjects := bulletList(lc.ThreadSubjects, "(None listed)")
	return fmt.Sprintf(`You are a social graph analyzer for a user's personal email archive.
Analyze the following group of email correspondents and their shared thread subjects to determine their relationship, shared project, affiliation, or topic.

### Key Members:
%s

### Shared Thread Subjects:
%s

Generate a concise, professional label (2-4 words) that describes this group. Examples:
- "LLM API Integration"
- "Family & Relatives"
- "Stanford Research Group"
- "Marketing & Sales Campaign"
- "Real Estate Advisory"

Output ONLY the plain text label. Do not include quotes, markdown formatting, explanation, or extra words.`, members, subjects)
}

func bulletList(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, "- "+item)
	}
	return strings.Join(lines, "\n")
}

// cleanGeneratedText trims whitespace and a single pair of surrounding quotes,
// matching the previous TypeScript post-processing of one-shot model output.
func cleanGeneratedText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return strings.TrimSpace(s)
}
