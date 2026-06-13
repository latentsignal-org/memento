package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestAssertMementoOnlyMigrations_AllowsCurrent(t *testing.T) {
	if err := assertMementoOnlyMigrations(); err != nil {
		t.Fatalf("current migrations should be Memento-only, got: %v", err)
	}
}

func TestAssertMementoOnlyMigrations_RejectsForeignTables(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"create messages", "CREATE TABLE messages (id INTEGER)"},
		{"alter participants", "ALTER TABLE participants ADD COLUMN foo TEXT"},
		{"drop attachments", "DROP TABLE attachments"},
		{"insert into labels", "INSERT INTO labels (id) VALUES (1)"},
		{"update conversations", "UPDATE conversations SET title = 'x'"},
		{"delete from sources", "DELETE FROM sources"},
		{"quoted identifier", `CREATE TABLE "messages" (id INTEGER)`},
	}

	original := migrations
	t.Cleanup(func() { migrations = original })

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			migrations = []Migration{{Version: 999, Name: "bad", SQL: tc.sql}}
			err := assertMementoOnlyMigrations()
			if err == nil {
				t.Fatalf("expected rejection for SQL %q", tc.sql)
			}
			if !strings.Contains(err.Error(), "non-Memento") {
				t.Fatalf("error %q should mention non-Memento", err.Error())
			}
		})
	}
}

func TestAssertMementoOnlyMigrations_AllowsMementoTables(t *testing.T) {
	original := migrations
	t.Cleanup(func() { migrations = original })

	migrations = []Migration{{
		Version: 999,
		Name:    "ok",
		SQL:     "CREATE TABLE memento_widgets (id INTEGER); INSERT INTO memento_widgets (id) VALUES (1);",
	}}
	if err := assertMementoOnlyMigrations(); err != nil {
		t.Fatalf("memento_* tables should be allowed, got: %v", err)
	}
}

func TestMigrateSocialTables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, tableName := range []string{
		"memento_social_edge",
		"memento_social_metric",
		"memento_social_cluster",
	} {
		var count int
		if err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, tableName,
		).Scan(&count); err != nil {
			t.Fatalf("query sqlite_master for %s: %v", tableName, err)
		}
		if count != 1 {
			t.Errorf("table %s not found after migration", tableName)
		}
	}
}
