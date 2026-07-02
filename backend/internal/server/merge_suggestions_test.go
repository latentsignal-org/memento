package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"memento/backend/internal/person"
	"memento/backend/internal/store"
)

func newMergeSuggestionsTestServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memento.sqlite")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return New(Options{DBPath: dbPath}, db, nil)
}

func seedMergeSuggestionPerson(t *testing.T, db *sql.DB, name, email string, dismissed bool) int64 {
	t.Helper()
	var dismissedAt any
	if dismissed {
		dismissedAt = "2026-07-02T00:00:00Z"
	}
	res, err := db.Exec(`
		INSERT INTO memento_person (canonical_name, primary_email, dismissed_at)
		VALUES (?, ?, ?)
	`, name, email, dismissedAt)
	if err != nil {
		t.Fatalf("seed person: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := db.Exec(`
		INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence, locked)
		VALUES (?, ?, ?, 'singleton', 1, 0)
	`, email, id, name); err != nil {
		t.Fatalf("seed person email: %v", err)
	}
	return id
}

func TestGetPeopleMergeSuggestionsSkipsStaleRows(t *testing.T) {
	srv := newMergeSuggestionsTestServer(t)
	ctx := context.Background()
	dismissed := seedMergeSuggestionPerson(t, srv.db, "Ghost Person", "ghost@example.com", true)
	active := seedMergeSuggestionPerson(t, srv.db, "Active Person", "active@example.com", false)
	if err := person.UpsertMergeSuggestion(ctx, srv.db, person.MergeSuggestionInput{
		PersonAID:      dismissed,
		PersonBID:      active,
		Sources:        []string{person.LinkSourceExactName},
		NameSimilarity: 1,
		CombinedScore:  1,
		ScoresStale:    true,
	}); err != nil {
		t.Fatalf("upsert suggestion: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/people/merge-suggestions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Suggestions []any `json:"suggestions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Suggestions) != 0 {
		t.Fatalf("suggestions = %d, want stale row skipped", len(response.Suggestions))
	}
	pending, err := person.ListMergeSuggestions(ctx, srv.db, person.ListMergeSuggestionOptions{})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending suggestions = %+v, want stale row resolved", pending)
	}
	rejected, err := person.ListMergeSuggestions(ctx, srv.db, person.ListMergeSuggestionOptions{Status: "rejected"})
	if err != nil {
		t.Fatalf("list rejected: %v", err)
	}
	if len(rejected) != 1 {
		t.Fatalf("rejected suggestions = %+v, want one stale row", rejected)
	}
}
