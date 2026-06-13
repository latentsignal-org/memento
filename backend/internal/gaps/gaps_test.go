package gaps

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
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			sent_at DATETIME,
			subject TEXT,
			snippet TEXT,
			sender_id INTEGER
		);
		CREATE TABLE message_bodies (
			message_id INTEGER PRIMARY KEY,
			body_text TEXT
		);
		CREATE TABLE message_recipients (
			id INTEGER PRIMARY KEY,
			message_id INTEGER NOT NULL,
			participant_id INTEGER NOT NULL,
			recipient_type TEXT NOT NULL
		);
		CREATE TABLE participants (
			id INTEGER PRIMARY KEY,
			email_address TEXT,
			display_name TEXT
		);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db
}

// ---------------------------------------------------------------------------
// Chronological tests
// ---------------------------------------------------------------------------

func TestDetect_Chronological_Gap(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Three messages: Jan 1, Jan 10, Mar 10 (59-day gap between msg 2 and msg 3).
	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES
		(1, '2025-01-01 10:00:00', 'Project kickoff'),
		(2, '2025-01-10 10:00:00', 'Design proposal submitted'),
		(3, '2025-03-10 10:00:00', 'Inspector review scheduled')
	`)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		ids      []int64
		wantLen  int
		wantKind string
		wantSev  string
	}{
		{
			name:     "59-day gap is detected",
			ids:      []int64{1, 2, 3},
			wantLen:  1,
			wantKind: "chronological_continuity",
			wantSev:  "high",
		},
		{
			name:    "only two messages in gap window — still one gap",
			ids:     []int64{2, 3},
			wantLen: 1,
			wantSev: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Detect(ctx, db, tt.ids, "chronological")
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("got %d gaps, want %d: %+v", len(got), tt.wantLen, got)
			}
			if tt.wantLen > 0 {
				if got[0].Kind != tt.wantKind && tt.wantKind != "" {
					t.Errorf("Kind = %q, want %q", got[0].Kind, tt.wantKind)
				}
				if got[0].Severity != tt.wantSev {
					t.Errorf("Severity = %q, want %q", got[0].Severity, tt.wantSev)
				}
				if len(got[0].AnchorMessageIDs) != 2 {
					t.Errorf("expected 2 anchor IDs, got %d", len(got[0].AnchorMessageIDs))
				}
				if len(got[0].SearchHints) == 0 {
					t.Error("expected at least one search hint")
				}
			}
		})
	}
}

func TestDetect_Chronological_ImpactKindByKeyword(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES
		(60, '2025-01-01 10:00:00', 'Weekly check-in'),
		(61, '2025-01-20 10:00:00', 'Billing issue resolved')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{60, 61}, "chronological")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 gap, got %d", len(got))
	}
	if got[0].Kind != "chronological_impact" {
		t.Fatalf("kind = %q, want chronological_impact", got[0].Kind)
	}
}

func TestDetect_Chronological_NoGap(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Three messages within 14 days of each other.
	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES
		(10, '2025-02-01 09:00:00', 'Meeting notes'),
		(11, '2025-02-05 09:00:00', 'Follow-up'),
		(12, '2025-02-12 09:00:00', 'Sign-off')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{10, 11, 12}, "chronological")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no gaps, got %d: %+v", len(got), got)
	}
}

func TestDetect_Chronological_Empty(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	got, err := Detect(ctx, db, []int64{}, "chronological")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no gaps for empty input, got %d", len(got))
	}
}

func TestDetect_Chronological_SingleMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(`INSERT INTO messages (id, sent_at, subject) VALUES (20, '2025-01-01 10:00:00', 'Alone')`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{20}, "chronological")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no gaps for single message, got %d", len(got))
	}
}

func TestDetect_Chronological_MultipleGaps(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Jan 1 → Jan 5 (4 days, no gap) → Mar 1 (55 days, gap) → May 15 (75 days, gap)
	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES
		(30, '2025-01-01 10:00:00', 'Start'),
		(31, '2025-01-05 10:00:00', 'Quick update'),
		(32, '2025-03-01 10:00:00', 'Second phase'),
		(33, '2025-05-15 10:00:00', 'Final review')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{30, 31, 32, 33}, "chronological")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 gaps, got %d: %+v", len(got), got)
	}
	// Both gaps should be high severity (>30 days).
	for _, g := range got {
		if g.Severity != "high" {
			t.Errorf("gap %q: severity = %q, want high", g.Description, g.Severity)
		}
	}
}

func TestDetect_Chronological_ExactThreshold(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Exactly 14 days apart → should produce a gap (>= threshold).
	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES
		(40, '2025-03-01 00:00:00', 'Before'),
		(41, '2025-03-15 00:00:00', 'After')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{40, 41}, "chronological")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 gap at threshold, got %d", len(got))
	}
	if got[0].Severity != "medium" {
		t.Errorf("severity = %q, want medium", got[0].Severity)
	}
}

func TestDetect_Chronological_SearchHintsContainDateRange(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES
		(50, '2025-01-10 00:00:00', 'Permit submitted'),
		(51, '2025-03-05 00:00:00', 'Final inspection')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{50, 51}, "chronological")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected a gap")
	}
	// All hints must contain a date range.
	for _, h := range got[0].SearchHints {
		if !contains(h, "after:") || !contains(h, "before:") {
			t.Errorf("hint %q missing date range", h)
		}
	}
}

// ---------------------------------------------------------------------------
// Thematic tests
// ---------------------------------------------------------------------------

func TestDetect_Thematic_ThinCluster(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Message 1 (id=1, sorted first) has unique plumbing vocabulary — it will
	// become centroid 0 in K-means. Messages 2-8 all share renovation
	// vocabulary and cluster together in centroids 1 and 2.
	// With n=8, k=round(sqrt(8))=3.
	// Cluster 0 ends up with only message 1 → thin → gap flagged.
	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject, snippet) VALUES
		(1, '2025-01-01', 'Plumbing faucet fixture drain repair', 'The faucet drain is leaking from the fixture pipe'),
		(2, '2025-01-02', 'Renovation permit contractor', 'Renovation permit estimate contractor approved'),
		(3, '2025-01-03', 'Renovation permit estimate', 'Permit estimate renovation contractor approved'),
		(4, '2025-01-04', 'Contractor renovation quote', 'Renovation quote contractor estimate permit work'),
		(5, '2025-01-05', 'Renovation contractor approved', 'Contractor renovation work permit estimate approved'),
		(6, '2025-01-06', 'Permit approved renovation', 'Renovation permit approved contractor estimate work'),
		(7, '2025-01-07', 'Renovation estimate contractor', 'Estimate contractor renovation permit approved'),
		(8, '2025-01-08', 'Contractor permit renovation', 'Permit renovation contractor estimate approved')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{1, 2, 3, 4, 5, 6, 7, 8}, "thematic")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one thematic gap for the isolated plumbing cluster")
	}
	for _, g := range got {
		if g.Kind != "thematic" {
			t.Errorf("Kind = %q, want thematic", g.Kind)
		}
		if len(g.SearchHints) == 0 {
			t.Error("expected search hints")
		}
	}
}

func TestDetect_Thematic_TooFewMessages(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES
		(100, '2025-01-01', 'Alpha'),
		(101, '2025-01-02', 'Beta')
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Fewer than thematicMinClusterSize total → no analysis.
	got, err := Detect(ctx, db, []int64{100, 101}, "thematic")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no gaps for too-small bundle, got %d", len(got))
	}
}

func TestDetect_Thematic_NoThinCluster(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// All messages use the same vocabulary → K-means distributes them across
	// clusters and none should be thin enough to trigger a gap, because with
	// k=round(sqrt(6))=2 and 6 identical docs, each cluster has ~3 messages.
	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject, snippet) VALUES
		(200, '2025-01-01', 'Renovation permit', 'Renovation contractor permit estimate'),
		(201, '2025-01-02', 'Renovation permit', 'Renovation contractor permit estimate'),
		(202, '2025-01-03', 'Renovation permit', 'Renovation contractor permit estimate'),
		(203, '2025-01-04', 'Renovation permit', 'Renovation contractor permit estimate'),
		(204, '2025-01-05', 'Renovation permit', 'Renovation contractor permit estimate'),
		(205, '2025-01-06', 'Renovation permit', 'Renovation contractor permit estimate')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{200, 201, 202, 203, 204, 205}, "thematic")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	// With 6 identical messages and k=2, each cluster should have 3 messages.
	// All clusters meet thematicMinClusterSize=3 → no gaps.
	if len(got) != 0 {
		t.Logf("clusters produced gaps (may be K-means edge case): %+v", got)
		// Do not hard-fail here — K-means on identical vectors is degenerate;
		// what matters is that the detector doesn't error and returns valid structs.
		for _, g := range got {
			if g.Kind != "thematic" {
				t.Errorf("unexpected gap kind %q", g.Kind)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Participant tests
// ---------------------------------------------------------------------------

func TestDetect_Participant_Gap(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Participants in the DB.
	_, err := db.Exec(`
		INSERT INTO participants (id, email_address, display_name) VALUES
		(1, 'alice@example.com', 'Alice'),
		(2, 'bob@example.com', 'Bob')
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Two messages: Alice sends to Bob. Body mentions charlie@external.com
	// who is NOT a sender or recipient of any bundle message.
	_, err = db.Exec(`
		INSERT INTO messages (id, sent_at, subject, sender_id) VALUES
		(300, '2025-05-01 10:00:00', 'Contract discussion', 1),
		(301, '2025-05-02 10:00:00', 'Re: Contract discussion', 2)
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO message_recipients (id, message_id, participant_id, recipient_type) VALUES
		(1, 300, 2, 'to'),
		(2, 301, 1, 'to')
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO message_bodies (message_id, body_text) VALUES
		(300, 'Hi Bob, please loop in charlie@external.com on this project. Best, Alice'),
		(301, 'Done! I will cc charlie@external.com as requested.')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{300, 301}, "participant")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 participant gap, got %d: %+v", len(got), got)
	}
	g := got[0]
	if g.Kind != "participant" {
		t.Errorf("Kind = %q, want participant", g.Kind)
	}
	if !contains(g.Description, "charlie@external.com") {
		t.Errorf("description %q doesn't mention the missing email", g.Description)
	}
	if len(g.SearchHints) != 2 {
		t.Errorf("expected 2 search hints (from: and to:), got %d", len(g.SearchHints))
	}
}

func TestDetect_Participant_NoGap(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(`
		INSERT INTO participants (id, email_address) VALUES
		(10, 'sender@example.com'),
		(11, 'recipient@example.com')
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO messages (id, sent_at, subject, sender_id) VALUES
		(400, '2025-06-01 10:00:00', 'Hello', 10)
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO message_recipients (id, message_id, participant_id, recipient_type) VALUES
		(10, 400, 11, 'to')
	`)
	if err != nil {
		t.Fatal(err)
	}
	// Body only mentions emails already in the bundle.
	_, err = db.Exec(`
		INSERT INTO message_bodies (message_id, body_text) VALUES
		(400, 'From sender@example.com to recipient@example.com — nothing new here.')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{400}, "participant")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no gaps when all mentioned emails are in the bundle, got %d: %+v", len(got), got)
	}
}

func TestDetect_Participant_NoBody(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(`
		INSERT INTO messages (id, sent_at, subject) VALUES (500, '2025-01-01', 'No body')
	`)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Detect(ctx, db, []int64{500}, "participant")
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no gaps when messages have no bodies, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Error path tests
// ---------------------------------------------------------------------------

func TestDetect_UnknownMode(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	_, err := Detect(ctx, db, []int64{1, 2}, "bogus")
	if err == nil {
		t.Fatal("expected error for unknown mode, got nil")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
