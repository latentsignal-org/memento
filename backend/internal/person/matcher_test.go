package person

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"memento/backend/internal/msgvault"

	_ "modernc.org/sqlite"
)

func TestResolve_DoesNotMergeCrossDomainSameName(t *testing.T) {
	reader := newResolutionReader(t,
		testParticipant{Email: "jane@one.example", Name: "Jane Smith", Domain: "one.example", Messages: 6},
		testParticipant{Email: "jane@two.example", Name: "Jane Smith", Domain: "two.example", Messages: 6},
	)
	defer reader.Close()

	report, clusters, err := Resolve(context.Background(), reader, nil, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("clusters = %d, want 2", len(clusters))
	}
	if len(report.Suggestions) != 1 {
		t.Fatalf("suggestions = %d, want 1: %+v", len(report.Suggestions), report.Suggestions)
	}
	if !containsString(report.Suggestions[0].Sources, LinkSourceExactName) {
		t.Fatalf("suggestion sources = %v, want exact_name", report.Suggestions[0].Sources)
	}
}

func TestResolve_DoesNotMergeNWaySameName(t *testing.T) {
	reader := newResolutionReader(t,
		testParticipant{Email: "sam@one.example", Name: "Sam Lee", Domain: "one.example", Messages: 6},
		testParticipant{Email: "sam@two.example", Name: "Sam Lee", Domain: "two.example", Messages: 6},
		testParticipant{Email: "sam@three.example", Name: "Sam Lee", Domain: "three.example", Messages: 6},
		testParticipant{Email: "sam@four.example", Name: "Sam Lee", Domain: "four.example", Messages: 6},
	)
	defer reader.Close()

	report, clusters, err := Resolve(context.Background(), reader, nil, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(clusters) != 4 {
		t.Fatalf("clusters = %d, want 4", len(clusters))
	}
	if len(report.Suggestions) != 6 {
		t.Fatalf("suggestions = %d, want 6", len(report.Suggestions))
	}
}

func TestResolve_ForwarderEmitsSuggestionOnly(t *testing.T) {
	reader := newResolutionReader(t,
		testParticipant{Email: "jane@photos.example", Name: "Jane Smith (via Google Photos)", Domain: "photos.example", Messages: 6},
		testParticipant{Email: "jane@example.com", Name: "Jane Smith", Domain: "example.com", Messages: 6},
	)
	defer reader.Close()

	report, clusters, err := Resolve(context.Background(), reader, nil, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("clusters = %d, want 2", len(clusters))
	}
	if len(report.Suggestions) != 1 || !containsString(report.Suggestions[0].Sources, LinkSourceForwarderUnwrap) {
		t.Fatalf("suggestions = %+v, want forwarder suggestion", report.Suggestions)
	}
}

func TestResolve_GmailDotsMergeFastmailDotsDoNot(t *testing.T) {
	reader := newResolutionReader(t,
		testParticipant{Email: "j.smith@gmail.com", Name: "Jane Smith", Domain: "gmail.com", Messages: 1},
		testParticipant{Email: "jsmith@gmail.com", Name: "Jane Smith", Domain: "gmail.com", Messages: 1},
		testParticipant{Email: "a.b@fastmail.com", Name: "Alex Bee", Domain: "fastmail.com", Messages: 1},
		testParticipant{Email: "ab@fastmail.com", Name: "Alex Bee", Domain: "fastmail.com", Messages: 1},
	)
	defer reader.Close()

	_, clusters, err := Resolve(context.Background(), reader, nil, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(clusters) != 3 {
		t.Fatalf("clusters = %d, want 3", len(clusters))
	}
	var gmailClusterSize int
	for _, c := range clusters {
		for _, m := range c.Members {
			if m.Participant.EmailAddress == "j.smith@gmail.com" || m.Participant.EmailAddress == "jsmith@gmail.com" {
				gmailClusterSize = len(c.Members)
			}
		}
	}
	if gmailClusterSize != 2 {
		t.Fatalf("gmail cluster size = %d, want 2", gmailClusterSize)
	}
}

func TestResolve_LockedEmailsUntouched(t *testing.T) {
	reader := newResolutionReader(t,
		testParticipant{Email: "ann+home@gmail.com", Name: "Ann Home", Domain: "gmail.com", Messages: 1},
		testParticipant{Email: "ann@gmail.com", Name: "Ann Home", Domain: "gmail.com", Messages: 1},
	)
	defer reader.Close()

	report, clusters, err := Resolve(context.Background(), reader, map[string]bool{"ann@gmail.com": true}, DefaultResolveOptions())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if report.LockedSkipped != 1 {
		t.Fatalf("locked skipped = %d, want 1", report.LockedSkipped)
	}
	if len(clusters) != 1 || len(clusters[0].Members) != 1 {
		t.Fatalf("clusters = %+v, want one unlocked singleton", clusters)
	}
}

type testParticipant struct {
	Email    string
	Name     string
	Domain   string
	Messages int
}

func newResolutionReader(t *testing.T, participants ...testParticipant) *msgvault.Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resolution.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE participants (
			id INTEGER PRIMARY KEY,
			email_address TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			domain TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			sender_id INTEGER,
			sent_at DATETIME
		);
		CREATE TABLE message_recipients (
			message_id INTEGER,
			participant_id INTEGER
		);
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	msgID := 1
	for i, p := range participants {
		id := i + 1
		if _, err := db.Exec(`INSERT INTO participants (id, email_address, display_name, domain) VALUES (?, ?, ?, ?)`, id, p.Email, p.Name, p.Domain); err != nil {
			t.Fatalf("insert participant: %v", err)
		}
		for n := 0; n < p.Messages; n++ {
			if _, err := db.Exec(`INSERT INTO messages (id, sender_id) VALUES (?, ?)`, msgID, id); err != nil {
				t.Fatalf("insert message: %v", err)
			}
			msgID++
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture db: %v", err)
	}
	reader, err := msgvault.OpenReader(path)
	if err != nil {
		t.Fatalf("open reader: %v", err)
	}
	return reader
}
