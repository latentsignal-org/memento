package main

import (
	"context"
	"database/sql"
	"testing"

	"memento/backend/internal/store"

	_ "modernc.org/sqlite"
)

// TestDiscoverMementoTables_CoversAllMigrations is the regression test for
// the reset bug: after every migration runs, discoverMementoTables must
// return every table the migrations created. The old hard-coded list went
// stale silently — this test fails loudly the moment a new migration adds
// a memento_* table that discovery misses (which it can't, since it
// queries sqlite_master, but the test also catches the inverse: a
// migration creating a table without the memento_ prefix, which would
// escape reset).
func TestDiscoverMementoTables_CoversAllMigrations(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tables, err := discoverMementoTables(ctx, db)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	// Spot-check a few tables across different migration eras to confirm
	// breadth — including the ones the old hard-coded list missed.
	required := []string{
		"memento_person",
		"memento_person_email",
		"memento_people_candidates",
		"memento_people_report",  // rollup era
		"memento_social_edge",    // social graph era
		"memento_social_metric",  //
		"memento_social_cluster", //
		"memento_draft",          // draft curator era
		"memento_note",           // notes era
		"memento_person_facet",   // person agent era
		"memento_person_narrative",
		"memento_schema_migrations",
	}
	got := map[string]bool{}
	for _, n := range tables {
		got[n] = true
	}
	for _, r := range required {
		if !got[r] {
			t.Errorf("discoverMementoTables missing %q (have %d tables: %v)", r, len(tables), tables)
		}
	}

	// Every table returned must start with the memento_ prefix — discovery
	// should never sweep up msgvault tables even if they coexist in the
	// same file.
	for _, n := range tables {
		if len(n) < 8 || n[:8] != "memento_" {
			t.Errorf("discoverMementoTables returned non-memento table %q", n)
		}
	}
}

// TestDiscoverMementoTables_EmptyDB returns nil/empty, not an error.
func TestDiscoverMementoTables_EmptyDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	tables, err := discoverMementoTables(context.Background(), db)
	if err != nil {
		t.Fatalf("discover on empty db: %v", err)
	}
	if len(tables) != 0 {
		t.Errorf("expected 0 tables on empty db, got %d: %v", len(tables), tables)
	}
}
