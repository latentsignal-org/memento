// Package server — drafts.go: public endpoints for the project/concept
// draft flow. Browser hits these directly. Agent-driven mutations to a draft
// (interaction_id, transcript) go through the token-gated /state endpoint.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"memento/backend/internal/concept"
	"memento/backend/internal/project"
	"memento/backend/internal/refresh"
	"memento/backend/internal/store"
	"time"
)

// EntityBundle is the canonical shape persisted in memento_draft.entities_json.
// Kept loose (json.RawMessage on people/messages) so we don't impose schema on
// the agent — only what the commit step needs is decoded strictly.
type EntityBundle struct {
	Name        string                `json:"name"`
	SummaryHint string                `json:"summary_hint,omitempty"`
	People      []EntityBundlePerson  `json:"people"`
	Messages    []EntityBundleMessage `json:"messages"`
	Threads     []EntityBundleThread  `json:"threads"`
}

type EntityBundlePerson struct {
	PersonID           int64   `json:"person_id"`
	DisplayName        string  `json:"display_name"`
	Role               string  `json:"role,omitempty"`
	EvidenceMessageIDs []int64 `json:"evidence_message_ids,omitempty"`
}

type EntityBundleMessage struct {
	MessageID     int64   `json:"message_id"`
	Subject       string  `json:"subject,omitempty"`
	Date          string  `json:"date,omitempty"`
	IncludeReason string  `json:"include_reason,omitempty"`
	Confidence    float64 `json:"agent_confidence,omitempty"`
}

type EntityBundleThread struct {
	ThreadID      int64  `json:"thread_id"`
	Subject       string `json:"subject,omitempty"`
	MessageCount  int    `json:"message_count,omitempty"`
	IncludeReason string `json:"include_reason,omitempty"`
}

func (s *Server) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind      string `json:"kind"`
		NameHint  string `json:"name_hint"`
		SeedQuery string `json:"seed_query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json body: %w", err))
		return
	}
	if body.Kind == "" {
		body.Kind = "project"
	}

	id, err := store.CreateDraft(r.Context(), s.db, body.Kind, body.NameHint)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":   id,
		"kind": body.Kind,
	})
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	id, err := parseDraftID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	d, err := store.GetDraft(r.Context(), s.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("draft %d not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleUpdateDraftEntities(w http.ResponseWriter, r *http.Request) {
	id, err := parseDraftID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Read & validate the body parses as EntityBundle, but persist the raw
	// JSON so we never lose fields the model emits that we haven't typed yet.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var bundle EntityBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid entity bundle: %w", err))
		return
	}

	if err := store.UpdateDraftEntities(r.Context(), s.db, id, string(raw)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("draft %d not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCommitDraft(w http.ResponseWriter, r *http.Request) {
	id, err := parseDraftID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()

	d, err := store.GetDraft(ctx, s.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("draft %d not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if d.Status == "committed" {
		writeError(w, http.StatusConflict, fmt.Errorf("draft %d already committed", id))
		return
	}

	var bundle EntityBundle
	if err := json.Unmarshal([]byte(d.EntitiesJSON), &bundle); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("draft has no valid entity bundle: %w", err))
		return
	}
	if strings.TrimSpace(bundle.Name) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("entity bundle missing 'name'"))
		return
	}

	slug := slugify(bundle.Name)
	if slug == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("could not derive slug from name %q", bundle.Name))
		return
	}

	switch d.Kind {
	case "project":
		s.commitProjectDraft(w, ctx, id, slug, bundle)
	case "concept":
		s.commitConceptDraft(w, ctx, id, slug, bundle)
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported draft kind %q", d.Kind))
	}
}

func (s *Server) commitProjectDraft(w http.ResponseWriter, ctx context.Context, draftID int64, slug string, bundle EntityBundle) {
	projectID, err := project.CreateProject(ctx, s.db, bundle.Name, slug, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("create project: %w", err))
		return
	}
	if err := store.SetCommittedEntityDraftOrigin(ctx, s.db, "project", projectID, draftID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("set project origin draft: %w", err))
		return
	}

	// Members — look up primary_email per person_id, then AddPerson handles the rest.
	for _, p := range bundle.People {
		email, err := lookupPrimaryEmail(ctx, s.db, p.PersonID)
		if err != nil {
			// Soft-fail: bundle could reference stale ids; keep going.
			continue
		}
		_ = project.AddPerson(ctx, s.db, slug, email, p.Role)
	}

	// Messages — direct attach.
	for _, m := range bundle.Messages {
		_ = project.AddMessageExplicit(ctx, s.db, slug, m.MessageID, "draft")
	}

	// Threads — fan out to every message in each thread.
	for _, t := range bundle.Threads {
		_, _ = project.AddMessagesByThread(ctx, s.db, slug, t.ThreadID)
	}

	if _, err := refresh.RefreshProjectsReport(ctx, s.db); err != nil {
		fmt.Printf("warning: projects report refresh after commit failed: %v\n", err)
	}

	if err := store.MarkDraftCommitted(ctx, s.db, draftID, projectID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := refresh.RefreshAll(bgCtx, s.db); err != nil {
			fmt.Printf("warning: background refresh after commit failed: %v\n", err)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"project_id": projectID,
		"slug":       slug,
	})
}

func (s *Server) commitConceptDraft(w http.ResponseWriter, ctx context.Context, draftID int64, slug string, bundle EntityBundle) {
	scope := strings.TrimSpace(bundle.SummaryHint)
	if scope == "" {
		scope = "User-confirmed concept draft from collector-agent."
	}
	conceptID, err := concept.CreateConcept(ctx, s.db, bundle.Name, slug, scope, conceptKeywords(bundle))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("create concept: %w", err))
		return
	}
	if err := store.SetCommittedEntityDraftOrigin(ctx, s.db, "concept", conceptID, draftID); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("set concept origin draft: %w", err))
		return
	}

	for _, m := range bundle.Messages {
		score := m.Confidence
		if score <= 0 {
			score = 1.0
		}
		_ = concept.AddMessageExplicit(ctx, s.db, slug, m.MessageID, "draft", "explicit", score)
	}

	for _, t := range bundle.Threads {
		_, _ = concept.AddMessagesByThread(ctx, s.db, slug, t.ThreadID, "draft")
	}

	if _, err := refresh.RefreshConceptsReport(ctx, s.db); err != nil {
		fmt.Printf("warning: concepts report refresh after commit failed: %v\n", err)
	}

	if err := store.MarkDraftCommitted(ctx, s.db, draftID, conceptID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := refresh.RefreshAll(bgCtx, s.db); err != nil {
			fmt.Printf("warning: background refresh after commit failed: %v\n", err)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"concept_id": conceptID,
		"slug":       slug,
	})
}

func conceptKeywords(bundle EntityBundle) []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		for _, part := range strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool {
			return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
		}) {
			if len(part) < 3 || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
			if len(out) >= 12 {
				return
			}
		}
	}
	add(bundle.Name)
	add(bundle.SummaryHint)
	return out
}

func (s *Server) handleAbandonDraft(w http.ResponseWriter, r *http.Request) {
	id, err := parseDraftID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := store.MarkDraftAbandoned(r.Context(), s.db, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("draft %d not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleUpdateDraftState is the token-gated endpoint Next.js calls after each
// agent turn finishes to persist the new interaction_id + the user-visible
// transcript line(s) it just rendered.
func (s *Server) handleUpdateDraftState(w http.ResponseWriter, r *http.Request) {
	id, err := parseDraftID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		InteractionID  string          `json:"interaction_id"`
		TranscriptJSON json.RawMessage `json:"transcript_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := store.UpdateDraftState(r.Context(), s.db, id, body.InteractionID, string(body.TranscriptJSON)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("draft %d not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func parseDraftID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	if raw == "" {
		return 0, fmt.Errorf("missing draft id")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid draft id %q", raw)
	}
	return id, nil
}

func lookupPrimaryEmail(ctx context.Context, db *sql.DB, personID int64) (string, error) {
	var email string
	err := db.QueryRowContext(ctx,
		`SELECT primary_email FROM memento_person WHERE id = ?`, personID,
	).Scan(&email)
	return email, err
}

var slugifyRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugifyRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
