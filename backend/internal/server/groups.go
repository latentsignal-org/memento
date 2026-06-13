package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"memento/backend/internal/social"
)

// Lifecycle + curation endpoints for the People → Groups card.
// See docs/spec-current-state.md for the user-facing model.

// parseGroupID parses {id} from the path and returns 400 if missing/invalid.
func parseGroupID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid group id %q", raw))
		return 0, false
	}
	return id, true
}

// parsePersonID parses {personId} from the path.
func parsePersonID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("personId")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid person id %q", raw))
		return 0, false
	}
	return id, true
}

// GET /api/social/groups/{id} — load one group after a card mutation.
func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r)
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
	writeJSON(w, http.StatusOK, map[string]any{"group": g})
}

// POST /api/social/groups/{id}/save — promote a candidate to a saved group.
// Idempotent: saving an already-saved group is a no-op and returns 200.
func (s *Server) handleSaveGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_social_group
		SET saved_at = COALESCE(saved_at, ?), dismissed_at = NULL
		WHERE group_id = ?
	`, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("group %d not found", id))
		return
	}
	// Snapshots may already exist from refresh; recompute to reflect any
	// edits made before save.
	if err := social.RefreshGroupSnapshots(r.Context(), s.db, id); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh snapshots: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved_at": now})
}

// DELETE /api/social/groups/{id}/save — demote back to candidate.
func (s *Server) handleUnsaveGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_social_group SET saved_at = NULL WHERE group_id = ?
	`, id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/social/groups/{id}/dismiss — soft-delete.
func (s *Server) handleDismissGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_social_group
		SET dismissed_at = COALESCE(dismissed_at, ?)
		WHERE group_id = ?
	`, now, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("group %d not found", id))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/social/groups/{id}/dismiss — restore.
func (s *Server) handleRestoreGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_social_group SET dismissed_at = NULL WHERE group_id = ?
	`, id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PATCH /api/social/groups/{id} — partial update of display_name and/or note.
type groupPatchRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Note        *string `json:"note,omitempty"`
}

func (s *Server) handlePatchGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	var body groupPatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	if body.DisplayName == nil && body.Note == nil {
		writeError(w, http.StatusBadRequest, errors.New("nothing to update"))
		return
	}

	var sets []string
	var args []any
	if body.DisplayName != nil {
		name := strings.TrimSpace(*body.DisplayName)
		if len(name) > 200 {
			writeError(w, http.StatusBadRequest, errors.New("display_name must be 200 characters or fewer"))
			return
		}
		sets = append(sets, "display_name = ?")
		args = append(args, name)
	}
	if body.Note != nil {
		note := *body.Note
		if len(note) > 4000 {
			writeError(w, http.StatusBadRequest, errors.New("note must be 4000 characters or fewer"))
			return
		}
		sets = append(sets, "note = ?")
		args = append(args, note)
	}
	args = append(args, id)
	q := fmt.Sprintf(`UPDATE memento_social_group SET %s WHERE group_id = ?`, strings.Join(sets, ", "))
	res, err := s.db.ExecContext(r.Context(), q, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("group %d not found", id))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/social/groups/{id}/members/{personId}/exclude — soft-exclude a
// member. Triggers a snapshot refresh so cadence + top threads update.
func (s *Server) handleExcludeGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	personID, ok := parsePersonID(w, r)
	if !ok {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_social_group_member
		SET excluded_at = COALESCE(excluded_at, ?)
		WHERE group_id = ? AND person_id = ?
	`, now, groupID, personID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("member not found in group"))
		return
	}
	if err := social.RefreshGroupSnapshots(r.Context(), s.db, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh snapshots: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/social/groups/{id}/members/{personId}/exclude — restore.
func (s *Server) handleRestoreGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	personID, ok := parsePersonID(w, r)
	if !ok {
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_social_group_member SET excluded_at = NULL
		WHERE group_id = ? AND person_id = ?
	`, groupID, personID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := social.RefreshGroupSnapshots(r.Context(), s.db, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh snapshots: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/social/groups/{id}/members — add a person to the group.
// Body: { "person_id": <int64> }. Idempotent; restores an excluded member if
// the row already exists.
type addMemberRequest struct {
	PersonID int64 `json:"person_id"`
}

func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseGroupID(w, r)
	if !ok {
		return
	}
	var body addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	if body.PersonID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("person_id is required"))
		return
	}
	// Existence check on the group.
	var exists int
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT 1 FROM memento_social_group WHERE group_id = ?`, groupID,
	).Scan(&exists); err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("group %d not found", groupID))
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO memento_social_group_member (group_id, person_id, added_by_user, excluded_at)
		VALUES (?, ?, 1, NULL)
		ON CONFLICT(group_id, person_id) DO UPDATE SET excluded_at = NULL
	`, groupID, body.PersonID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := social.RefreshGroupSnapshots(r.Context(), s.db, groupID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh snapshots: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
