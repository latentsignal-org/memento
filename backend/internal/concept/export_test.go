package concept

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newConceptExportTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := `
		CREATE TABLE participants (
			id INTEGER PRIMARY KEY,
			email_address TEXT,
			display_name TEXT
		);

		CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			conversation_id INTEGER NOT NULL,
			sent_at DATETIME,
			sender_id INTEGER,
			subject TEXT,
			snippet TEXT
		);

		CREATE TABLE message_bodies (
			message_id INTEGER PRIMARY KEY,
			body_text TEXT
		);

		CREATE TABLE memento_person (
			id INTEGER PRIMARY KEY,
			canonical_name TEXT NOT NULL,
			primary_email TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE memento_person_email (
			email_address TEXT PRIMARY KEY,
			person_id INTEGER NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			link_source TEXT NOT NULL,
			confidence REAL NOT NULL,
			locked INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE memento_people_report (
			person_id INTEGER PRIMARY KEY,
			canonical_name TEXT NOT NULL,
			primary_email TEXT NOT NULL,
			domain TEXT NOT NULL DEFAULT '',
			email_count INTEGER NOT NULL,
			total_messages INTEGER NOT NULL,
			from_contact_count INTEGER NOT NULL,
			to_contact_count INTEGER NOT NULL,
			bidirectional_score REAL NOT NULL,
			classification TEXT NOT NULL,
			first_message_at DATETIME,
			last_message_at DATETIME,
			slug TEXT NOT NULL,
			aliases_json TEXT NOT NULL DEFAULT '[]',
			timeline_json TEXT NOT NULL DEFAULT '[]',
			top_correspondents_json TEXT NOT NULL DEFAULT '[]',
			generated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE memento_newsletter_source (
			id INTEGER PRIMARY KEY,
			slug TEXT NOT NULL,
			display_name TEXT NOT NULL,
			sender_email TEXT NOT NULL
		);

		CREATE TABLE memento_concept (
			id INTEGER PRIMARY KEY,
			slug TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL,
			scope_description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			seed_keywords TEXT NOT NULL DEFAULT '[]',
			note TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE memento_concept_message (
			concept_id INTEGER NOT NULL,
			message_id INTEGER NOT NULL,
			added_by TEXT NOT NULL,
			query_term TEXT NOT NULL DEFAULT '',
			relevance_score REAL NOT NULL DEFAULT 1.0,
			note TEXT NOT NULL DEFAULT '',
			added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (concept_id, message_id)
		);

		CREATE TABLE memento_concept_narrative (
			concept_id INTEGER NOT NULL,
			section TEXT NOT NULL,
			content TEXT NOT NULL,
			source_message_ids TEXT NOT NULL DEFAULT '[]',
			generated_at DATETIME,
			edited_by TEXT NOT NULL DEFAULT 'llm',
			PRIMARY KEY (concept_id, section)
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestBuildConceptReportMarksOnlyPeopleReportRowsLinkable(t *testing.T) {
	db := newConceptExportTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_concept (id, slug, name, scope_description, status, seed_keywords)
		VALUES (1, 'test-concept', 'Test Concept', 'scope', 'active', '[]');

		INSERT INTO memento_person (id, canonical_name, primary_email) VALUES
			(10, 'Linked Person', 'linked@example.com'),
			(20, 'Unbacked Person', 'unbacked@example.com');

		INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence) VALUES
			('linked@example.com', 10, 'Linked Person', 'test', 1.0),
			('unbacked@example.com', 20, 'Unbacked Person', 'test', 1.0);

		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (
			10, 'Linked Person', 'linked@example.com', 'example.com', 1,
			1, 1, 0, 1.0, 'candidate', 'linked-person'
		);

		INSERT INTO participants (id, email_address, display_name) VALUES
			(100, 'linked@example.com', 'Linked Person'),
			(200, 'unbacked@example.com', 'Unbacked Person');

		INSERT INTO messages (id, conversation_id, sent_at, sender_id, subject, snippet) VALUES
			(1000, 1, '2026-01-01 00:00:00', 100, 'Linked', 'linked snippet'),
			(2000, 2, '2026-01-02 00:00:00', 200, 'Unbacked', 'unbacked snippet');

		INSERT INTO message_bodies (message_id, body_text) VALUES
			(1000, 'linked body'),
			(2000, 'unbacked body');

		INSERT INTO memento_concept_message (concept_id, message_id, added_by) VALUES
			(1, 1000, 'test'),
			(1, 2000, 'test');
	`); err != nil {
		t.Fatalf("seed data: %v", err)
	}

	report, err := BuildConceptReport(ctx, db, "test-concept")
	if err != nil {
		t.Fatalf("BuildConceptReport: %v", err)
	}

	byID := make(map[int64]PersonContribution)
	for _, person := range report.SourceMap.People {
		byID[person.PersonID] = person
	}

	linked := byID[10]
	if !linked.HasProfile {
		t.Fatalf("linked person HasProfile = false, want true")
	}
	if linked.ProfileSlug != "linked-person" || linked.Slug != "linked-person" {
		t.Fatalf("linked slugs = (%q, %q), want linked-person", linked.ProfileSlug, linked.Slug)
	}

	unbacked := byID[20]
	if unbacked.HasProfile {
		t.Fatalf("unbacked person HasProfile = true, want false")
	}
	if unbacked.ProfileSlug != "" || unbacked.Slug != "" {
		t.Fatalf("unbacked slugs = (%q, %q), want empty", unbacked.ProfileSlug, unbacked.Slug)
	}
}
