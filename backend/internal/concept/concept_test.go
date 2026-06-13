package concept

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newConceptTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE memento_concept (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			slug TEXT UNIQUE NOT NULL,
			scope_description TEXT NOT NULL DEFAULT '',
			seed_keywords TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			note TEXT NOT NULL DEFAULT '',
			dismissed_at DATETIME
		);
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func TestCreateConceptRejectsReservedSlug(t *testing.T) {
	db := newConceptTestDB(t)
	ctx := context.Background()

	for _, slug := range []string{"_", "new", "merge-review"} {
		if _, err := CreateConcept(ctx, db, "Reserved Slug", slug, "", nil); err == nil {
			t.Fatalf("CreateConcept with slug %q succeeded, want error", slug)
		}
	}
}
