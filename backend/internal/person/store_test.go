package person

import (
	"context"
	"database/sql"
	"testing"

	"memento/backend/internal/msgvault"

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
		CREATE TABLE memento_person (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			canonical_name TEXT NOT NULL,
			primary_email TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE memento_person_email (
			email_address TEXT PRIMARY KEY,
			person_id INTEGER NOT NULL REFERENCES memento_person(id) ON DELETE CASCADE,
			display_name TEXT NOT NULL DEFAULT '',
			link_source TEXT NOT NULL,
			confidence REAL NOT NULL,
			locked INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

func clusterOf(members ...msgvault.ParticipantForResolution) cluster {
	c := cluster{ID: 1}
	for _, p := range members {
		c.Members = append(c.Members, &clusterMember{
			Participant: p,
			LinkSource:  LinkSourceExactName,
			Confidence:  0.95,
		})
	}
	return c
}

func participantIDsForEmails(t *testing.T, db *sql.DB) map[string]int64 {
	t.Helper()
	rows, err := db.Query(`SELECT email_address, person_id FROM memento_person_email`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var e string
		var id int64
		if err := rows.Scan(&e, &id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[e] = id
	}
	return out
}

func TestPersistClusters_StableIDsAcrossReruns(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	annCluster := clusterOf(
		msgvault.ParticipantForResolution{ID: 1, EmailAddress: "jane.smith@example.com", DisplayName: "Jane Smith"},
		msgvault.ParticipantForResolution{ID: 2, EmailAddress: "jane_smith@work.example", DisplayName: "Jane Smith"},
	)
	bobCluster := clusterOf(
		msgvault.ParticipantForResolution{ID: 3, EmailAddress: "bob@example.com", DisplayName: "Bob"},
	)

	if _, _, err := PersistClusters(ctx, db, []cluster{annCluster, bobCluster}); err != nil {
		t.Fatalf("first persist: %v", err)
	}
	before := participantIDsForEmails(t, db)

	// Run twice more with the same clusters; ids must not change.
	for i := 0; i < 2; i++ {
		if _, _, err := PersistClusters(ctx, db, []cluster{annCluster, bobCluster}); err != nil {
			t.Fatalf("rerun %d persist: %v", i, err)
		}
		after := participantIDsForEmails(t, db)
		for email, id := range before {
			if after[email] != id {
				t.Fatalf("person_id for %s changed across rerun: %d -> %d", email, id, after[email])
			}
		}
	}
}

func TestPersistClusters_NewMemberJoinsExistingPerson(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	initial := clusterOf(
		msgvault.ParticipantForResolution{ID: 1, EmailAddress: "jane.smith@example.com", DisplayName: "Jane Smith"},
	)
	if _, _, err := PersistClusters(ctx, db, []cluster{initial}); err != nil {
		t.Fatalf("first persist: %v", err)
	}
	originalID := participantIDsForEmails(t, db)["jane.smith@example.com"]

	// Second run: same person, plus a newly-seen email that clusters via name.
	expanded := clusterOf(
		msgvault.ParticipantForResolution{ID: 1, EmailAddress: "jane.smith@example.com", DisplayName: "Jane Smith"},
		msgvault.ParticipantForResolution{ID: 2, EmailAddress: "jane_smith@work.example", DisplayName: "Jane Smith"},
	)
	if _, _, err := PersistClusters(ctx, db, []cluster{expanded}); err != nil {
		t.Fatalf("expand persist: %v", err)
	}
	after := participantIDsForEmails(t, db)
	if after["jane.smith@example.com"] != originalID {
		t.Fatalf("original email's person_id changed: %d -> %d", originalID, after["jane.smith@example.com"])
	}
	if after["jane_smith@work.example"] != originalID {
		t.Fatalf("new email did not join existing person: got %d, want %d", after["jane_smith@work.example"], originalID)
	}
}

func TestPersistClusters_LockedRowAnchorsPerson(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Seed a locked row pointing alice@personal.com at person #1.
	if _, err := db.Exec(`INSERT INTO memento_person (id, canonical_name, primary_email) VALUES (1, 'Alice Manual', 'alice@personal.com')`); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO memento_person_email (email_address, person_id, link_source, confidence, locked) VALUES ('alice@personal.com', 1, 'manual', 1.0, 1)`); err != nil {
		t.Fatalf("seed email: %v", err)
	}

	// New cluster contains the locked email plus a new one.
	c := clusterOf(
		msgvault.ParticipantForResolution{ID: 100, EmailAddress: "alice@personal.com", DisplayName: "Alice Example"},
		msgvault.ParticipantForResolution{ID: 101, EmailAddress: "alice@work.com", DisplayName: "Alice Example"},
	)
	if _, _, err := PersistClusters(ctx, db, []cluster{c}); err != nil {
		t.Fatalf("persist: %v", err)
	}

	mapping := participantIDsForEmails(t, db)
	if mapping["alice@personal.com"] != 1 {
		t.Fatalf("locked email moved off its person: now %d", mapping["alice@personal.com"])
	}
	if mapping["alice@work.com"] != 1 {
		t.Fatalf("new email did not join locked person: got %d, want 1", mapping["alice@work.com"])
	}
}

func TestPersistClusters_StaleEmailsRemoved(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	first := clusterOf(
		msgvault.ParticipantForResolution{ID: 1, EmailAddress: "stays@example.com", DisplayName: "Stays"},
		msgvault.ParticipantForResolution{ID: 2, EmailAddress: "gone@example.com", DisplayName: "Gone"},
	)
	if _, _, err := PersistClusters(ctx, db, []cluster{first}); err != nil {
		t.Fatalf("first: %v", err)
	}

	second := clusterOf(
		msgvault.ParticipantForResolution{ID: 1, EmailAddress: "stays@example.com", DisplayName: "Stays"},
	)
	if _, _, err := PersistClusters(ctx, db, []cluster{second}); err != nil {
		t.Fatalf("second: %v", err)
	}

	mapping := participantIDsForEmails(t, db)
	if _, ok := mapping["gone@example.com"]; ok {
		t.Fatalf("stale email was not swept")
	}
	if _, ok := mapping["stays@example.com"]; !ok {
		t.Fatalf("kept email was lost")
	}
}
