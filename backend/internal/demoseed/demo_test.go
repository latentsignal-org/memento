package demoseed

import (
	"context"
	"path/filepath"
	"testing"

	"memento/backend/internal/store"
)

func TestSeedDemoLoadsAuthoredCorpusWithoutDerivedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := CreateMsgvaultTables(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := SeedDemo(ctx, db); err != nil {
		t.Fatal(err)
	}

	checks := []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM messages`, 129},
		{`SELECT COUNT(*) FROM memento_person`, 12},
		{`SELECT COUNT(*) FROM memento_project`, 3},
		{`SELECT COUNT(*) FROM memento_concept`, 3},
		{`SELECT COUNT(*) FROM memento_people_report`, 0},
		{`SELECT COUNT(*) FROM memento_newsletter_source`, 0},
		{`SELECT COUNT(*) FROM memento_social_edge`, 0},
	}
	for _, check := range checks {
		var got int
		if err := db.QueryRowContext(ctx, check.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != check.want {
			t.Fatalf("%s = %d, want %d", check.query, got, check.want)
		}
	}
	var marker string
	if err := db.QueryRowContext(ctx, `SELECT value FROM memento_config WHERE key = 'demo_mode'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "true" {
		t.Fatalf("demo marker = %q, want true", marker)
	}
	if err := db.QueryRowContext(ctx, `SELECT value FROM memento_config WHERE key = 'onboarding_status'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "complete" {
		t.Fatalf("onboarding marker = %q, want complete", marker)
	}
}
