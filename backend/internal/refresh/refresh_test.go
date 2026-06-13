package refresh

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

func openRefreshTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE sources (
			id INTEGER PRIMARY KEY,
			identifier TEXT NOT NULL
		);
		CREATE TABLE participants (
			id INTEGER PRIMARY KEY,
			email_address TEXT NOT NULL
		);
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			sender_id INTEGER,
			sent_at TEXT,
			conversation_id INTEGER,
			subject TEXT,
			snippet TEXT
		);
		CREATE TABLE message_recipients (
			message_id INTEGER NOT NULL,
			participant_id INTEGER NOT NULL,
			recipient_type TEXT NOT NULL
		);
		CREATE TABLE memento_person (
			id INTEGER PRIMARY KEY,
			canonical_name TEXT NOT NULL,
			primary_email TEXT NOT NULL
		);
		CREATE TABLE memento_person_email (
			email_address TEXT PRIMARY KEY,
			person_id INTEGER NOT NULL
		);
	`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func addRefreshPerson(t *testing.T, db *sql.DB, personID, participantID int64, name, email string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO participants (id, email_address) VALUES (?, ?)`, participantID, email); err != nil {
		t.Fatalf("insert participant: %v", err)
	}
	if personID != 0 {
		if _, err := db.Exec(`INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (?, ?, ?)`, personID, name, email); err != nil {
			t.Fatalf("insert person: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO memento_person_email (email_address, person_id) VALUES (?, ?)`, email, personID); err != nil {
			t.Fatalf("insert person email: %v", err)
		}
	}
}

func addRefreshMessage(t *testing.T, db *sql.DB, msgID, senderID, conversationID int64, sentAt, subject string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO messages (id, sender_id, sent_at, conversation_id, subject, snippet)
		VALUES (?, ?, ?, ?, ?, '')
	`, msgID, senderID, sentAt, conversationID, subject); err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func addRefreshRecipient(t *testing.T, db *sql.DB, msgID, participantID int64, recipientType string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO message_recipients (message_id, participant_id, recipient_type) VALUES (?, ?, ?)`, msgID, participantID, recipientType); err != nil {
		t.Fatalf("insert recipient: %v", err)
	}
}

func TestBuildPeopleReportEnvelopeSummariesFiltersTimelineRecipientTypes(t *testing.T) {
	db := openRefreshTestDB(t)
	ctx := context.Background()

	if _, err := db.Exec(`INSERT INTO sources (identifier) VALUES ('owner@example.com')`); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	addRefreshPerson(t, db, 0, 1, "", "owner@example.com")
	addRefreshPerson(t, db, 10, 2, "Allowed", "allowed@example.com")
	addRefreshPerson(t, db, 20, 3, "Reply Only", "reply@example.com")

	addRefreshMessage(t, db, 100, 1, 1000, "2024-01-02 10:00:00", "allowed")
	addRefreshRecipient(t, db, 100, 2, "to")
	addRefreshMessage(t, db, 101, 1, 1001, "2024-01-03 10:00:00", "reply-only")
	addRefreshRecipient(t, db, 101, 3, "reply-to")

	timelines, correspondents, err := buildPeopleReportEnvelopeSummaries(ctx, db, []int64{10, 20})
	if err != nil {
		t.Fatalf("build summaries: %v", err)
	}
	if got := len(timelines[10]); got != 1 {
		t.Fatalf("allowed timeline count = %d, want 1", got)
	}
	if got := len(timelines[20]); got != 0 {
		t.Fatalf("reply-only timeline count = %d, want 0", got)
	}
	if _, ok := correspondents[20]; ok {
		t.Fatalf("single-recipient reply-only message should not create correspondents: %s", mustJSON(t, correspondents[20]))
	}
}

func TestBuildPeopleReportEnvelopeSummariesRanksCorrespondentsAndCapsLargeConversations(t *testing.T) {
	db := openRefreshTestDB(t)
	ctx := context.Background()

	addRefreshPerson(t, db, 10, 10, "Target", "target@example.com")
	addRefreshPerson(t, db, 20, 20, "Top", "top@example.com")
	addRefreshPerson(t, db, 30, 30, "Second", "second@example.com")

	// Top shares two conversations with Target; Second shares one.
	addRefreshMessage(t, db, 200, 10, 2000, "2024-01-01 10:00:00", "target-top-1")
	addRefreshRecipient(t, db, 200, 20, "to")
	addRefreshMessage(t, db, 201, 20, 2001, "2024-01-02 10:00:00", "target-top-2")
	addRefreshRecipient(t, db, 201, 10, "reply-to")
	addRefreshMessage(t, db, 202, 10, 2002, "2024-01-03 10:00:00", "target-second")
	addRefreshRecipient(t, db, 202, 30, "to")

	// Oversized conversations are ignored for correspondent summaries.
	addRefreshMessage(t, db, 300, 10, 3000, "2024-01-04 10:00:00", "oversized")
	addRefreshRecipient(t, db, 300, 20, "to")
	for i := int64(0); i < maxPeopleReportCorrespondentParticipants; i++ {
		personID := int64(1000 + i)
		participantID := int64(1000 + i)
		addRefreshPerson(t, db, personID, participantID, "Bulk", fmt.Sprintf("bulk%d@example.com", i))
		addRefreshRecipient(t, db, 300, participantID, "to")
	}

	_, correspondents, err := buildPeopleReportEnvelopeSummaries(ctx, db, []int64{10})
	if err != nil {
		t.Fatalf("build summaries: %v", err)
	}
	got := correspondents[10]
	if len(got) < 2 {
		t.Fatalf("target correspondents = %s, want at least two", mustJSON(t, got))
	}
	if got[0].PersonID != 20 || got[0].SharedCount != 2 {
		t.Fatalf("top correspondent = %+v, want person 20 with shared count 2", got[0])
	}
	if got[1].PersonID != 30 || got[1].SharedCount != 1 {
		t.Fatalf("second correspondent = %+v, want person 30 with shared count 1", got[1])
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(b)
}
