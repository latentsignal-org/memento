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
)

type noteRow struct {
	ID        int64  `json:"id"`
	Dimension string `json:"dimension"`
	EntityID  int64  `json:"entity_id"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (s *Server) handleGetNotes(w http.ResponseWriter, r *http.Request) {
	dimension := strings.TrimSpace(r.URL.Query().Get("dimension"))
	entityID, err := strconv.ParseInt(r.URL.Query().Get("entity_id"), 10, 64)
	if dimension == "" || err != nil || entityID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("dimension and entity_id are required"))
		return
	}
	notes, err := loadNotes(r.Context(), s.db, dimension, entityID)
	if err != nil {
		if isNotSetUp(err) {
			writeJSON(w, http.StatusOK, map[string]any{"notes": []noteRow{}})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}

func (s *Server) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dimension string `json:"dimension"`
		EntityID  int64  `json:"entity_id"`
		Content   string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Dimension) == "" || req.EntityID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("dimension and entity_id are required"))
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO memento_note (dimension, entity_id, content, created_at, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, strings.TrimSpace(req.Dimension), req.EntityID, req.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	id, _ := res.LastInsertId()
	n, err := getNoteByID(r.Context(), s.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      int64  `json:"id"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("id is required"))
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_note
		SET content = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, req.Content, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("note %d not found", req.ID))
		return
	}
	n, err := getNoteByID(r.Context(), s.db, req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("id is required"))
		return
	}
	res, err := s.db.ExecContext(r.Context(), `DELETE FROM memento_note WHERE id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, fmt.Errorf("note %d not found", id))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func getNoteByID(ctx context.Context, db *sql.DB, id int64) (noteRow, error) {
	var n noteRow
	err := db.QueryRowContext(ctx, `
		SELECT id, dimension, entity_id, content, created_at, updated_at
		FROM memento_note
		WHERE id = ?
	`, id).Scan(&n.ID, &n.Dimension, &n.EntityID, &n.Content, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}

func loadNotes(ctx context.Context, db *sql.DB, dimension string, entityID int64) ([]noteRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, dimension, entity_id, content, created_at, updated_at
		FROM memento_note
		WHERE dimension = ? AND entity_id = ?
		ORDER BY updated_at DESC, id DESC
	`, dimension, entityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []noteRow{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	var notes []noteRow
	for rows.Next() {
		var n noteRow
		if err := rows.Scan(&n.ID, &n.Dimension, &n.EntityID, &n.Content, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if notes == nil {
		notes = []noteRow{}
	}
	return notes, nil
}
