package project

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	schema := `
		CREATE TABLE sources (
			id INTEGER PRIMARY KEY,
			source_type TEXT NOT NULL,
			identifier TEXT NOT NULL
		);

		CREATE TABLE participants (
			id INTEGER PRIMARY KEY,
			email_address TEXT,
			display_name TEXT,
			domain TEXT,
			canonical_id TEXT
		);

		CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			conversation_id INTEGER NOT NULL,
			source_id INTEGER NOT NULL,
			source_message_id TEXT,
			message_type TEXT NOT NULL,
			sent_at DATETIME,
			sender_id INTEGER,
			is_from_me BOOLEAN,
			subject TEXT,
			snippet TEXT
		);

		CREATE TABLE message_bodies (
			message_id INTEGER PRIMARY KEY,
			body_text TEXT
		);

		CREATE TABLE message_recipients (
			id INTEGER PRIMARY KEY,
			message_id INTEGER NOT NULL,
			participant_id INTEGER NOT NULL,
			recipient_type TEXT NOT NULL,
			display_name TEXT
		);

		CREATE TABLE memento_person (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			canonical_name TEXT NOT NULL,
			primary_email TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE memento_person_email (
			email_address TEXT PRIMARY KEY,
			person_id INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
			display_name TEXT NOT NULL DEFAULT '',
			link_source TEXT NOT NULL,
			confidence REAL NOT NULL,
			locked INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE memento_project (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			slug TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			aliases TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			started_at DATE,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			note TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE memento_project_message (
			project_id INTEGER NOT NULL REFERENCES memento_project(id) ON DELETE CASCADE,
			message_id INTEGER NOT NULL,
			included_by TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (project_id, message_id)
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func TestCreateProjectRejectsReservedSlug(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	for _, slug := range []string{"_", "new", "merge-review"} {
		if _, err := CreateProject(ctx, db, "Reserved Slug", slug, nil); err == nil {
			t.Fatalf("CreateProject with slug %q succeeded, want error", slug)
		}
	}
}

func TestGetProjectBundle(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Seed source / account owner
	_, err := db.Exec(`INSERT INTO sources (id, source_type, identifier) VALUES (1, 'email', 'owner@example.com')`)
	if err != nil {
		t.Fatal(err)
	}

	// Seed participants
	_, err = db.Exec(`
		INSERT INTO participants (id, email_address, display_name) VALUES
		(1, 'owner@example.com', 'Alice Owner'),
		(2, 'contractor@example.com', 'Bob Contractor')
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Seed canonical person
	_, err = db.Exec(`INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (42, 'Bob Contractor', 'contractor@example.com')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO memento_person_email (email_address, person_id, link_source, confidence) VALUES ('contractor@example.com', 42, 'resolve', 1.0)`)
	if err != nil {
		t.Fatal(err)
	}

	// Create project
	projID, err := CreateProject(ctx, db, "Test Project", "test-project", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Add messages to DB
	_, err = db.Exec(`
		INSERT INTO messages (id, conversation_id, source_id, message_type, sent_at, sender_id, subject, snippet) VALUES
		(101, 1000, 1, 'email', '2025-06-11 10:00:00', 2, 'Project Update', 'Hi Alice, here is...'),
		(102, 1000, 1, 'email', '2025-06-11 11:00:00', 1, 'Re: Project Update', 'Thanks Bob!')
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`
		INSERT INTO message_bodies (message_id, body_text) VALUES
		(101, 'Dear Alice, We are excited to proceed. Best, Bob.'),
		(102, 'Thanks Bob! Sounds great.')
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Message 102 recipient is Bob (contractor)
	_, err = db.Exec(`INSERT INTO message_recipients (id, message_id, participant_id, recipient_type) VALUES (1, 102, 2, 'to')`)
	if err != nil {
		t.Fatal(err)
	}
	// Message 101 recipient is Alice (account owner)
	_, err = db.Exec(`INSERT INTO message_recipients (id, message_id, participant_id, recipient_type) VALUES (2, 101, 1, 'to')`)
	if err != nil {
		t.Fatal(err)
	}

	// Link messages to project
	err = AddMessageExplicit(ctx, db, "test-project", 101, "manual")
	if err != nil {
		t.Fatal(err)
	}
	err = AddMessageExplicit(ctx, db, "test-project", 102, "manual")
	if err != nil {
		t.Fatal(err)
	}

	// Test bundling
	bundle, err := GetProjectBundle(ctx, db, projID, "full")
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}

	if len(bundle) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(bundle))
	}

	// First message: inbound (to_account) from Brandon
	m1 := bundle[0]
	if m1.MessageID != 101 {
		t.Errorf("expected msg id 101, got %d", m1.MessageID)
	}
	if m1.SenderCanonicalName != "Bob Contractor" {
		t.Errorf("expected sender Bob Contractor, got %q", m1.SenderCanonicalName)
	}
	if m1.Direction != "to_account" {
		t.Errorf("expected direction to_account, got %q", m1.Direction)
	}
	if m1.BodyText != "Dear Alice, We are excited to proceed. Best, Bob." {
		t.Errorf("unexpected body: %q", m1.BodyText)
	}

	// Second message: outbound (from_account)
	m2 := bundle[1]
	if m2.MessageID != 102 {
		t.Errorf("expected msg id 102, got %d", m2.MessageID)
	}
	if m2.Direction != "from_account" {
		t.Errorf("expected direction from_account, got %q", m2.Direction)
	}
}

func TestGetProjectBundle_BudgetTruncation(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	projID, err := CreateProject(ctx, db, "Budget Test", "budget", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Insert messages with large body texts
	_, err = db.Exec(`
		INSERT INTO messages (id, conversation_id, source_id, message_type, sent_at, sender_id, subject, snippet) VALUES
		(201, 2000, 1, 'email', '2025-06-11 10:00:00', 1, 'Large Msg 1', 'Snippet 1'),
		(202, 2000, 1, 'email', '2025-06-11 11:00:00', 1, 'Large Msg 2', 'Snippet 2')
	`)
	if err != nil {
		t.Fatal(err)
	}

	longBody1 := make([]byte, 5000)
	for i := range longBody1 {
		longBody1[i] = 'A'
	}
	longBody2 := make([]byte, 8000)
	for i := range longBody2 {
		longBody2[i] = 'B'
	}

	_, err = db.Exec(`
		INSERT INTO message_bodies (message_id, body_text) VALUES
		(201, ?),
		(202, ?)
	`, string(longBody1), string(longBody2))
	if err != nil {
		t.Fatal(err)
	}

	err = AddMessageExplicit(ctx, db, "budget", 201, "manual")
	if err != nil {
		t.Fatal(err)
	}
	err = AddMessageExplicit(ctx, db, "budget", 202, "manual")
	if err != nil {
		t.Fatal(err)
	}

	// Fetch bundle
	bundle, err := GetProjectBundle(ctx, db, projID, "full")
	if err != nil {
		t.Fatal(err)
	}

	if len(bundle) != 2 {
		t.Fatal("expected 2 items")
	}

	// Should be truncated to 2000 + " [...]" = 2006 characters
	if len(bundle[0].BodyText) != 2006 {
		t.Errorf("expected truncated body 1 to be 2006, got %d", len(bundle[0].BodyText))
	}
	if len(bundle[1].BodyText) != 2006 {
		t.Errorf("expected truncated body 2 to be 2006, got %d", len(bundle[1].BodyText))
	}
}

func TestClearMessages(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := CreateProject(ctx, db, "EB5", "eb5", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddMessageExplicit(ctx, db, "eb5", 101, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := AddMessageExplicit(ctx, db, "eb5", 102, "manual"); err != nil {
		t.Fatal(err)
	}

	removed, err := ClearMessages(ctx, db, "eb5")
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 removed rows, got %d", removed)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memento_project_message`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected no attached messages after clear, got %d", count)
	}
}

func TestNormalizeSearchMode(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "fts"},
		{in: "fts", want: "fts"},
		{in: "FTS", want: "fts"},
		{in: "hybrid", want: "hybrid"},
		{in: "  hybrid ", want: "hybrid"},
		{in: "semantic", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeSearchMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeSearchMode(%q) expected error, got nil", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeSearchMode(%q) unexpected error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeSearchMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
