package social

import "testing"

func TestStableGroupIDIsMembershipStable(t *testing.T) {
	a := stableGroupID([]int64{3, 1, 2}, nil)
	b := stableGroupID([]int64{2, 3, 1}, nil)
	if a != b {
		t.Fatalf("stableGroupID changed with member order: %d != %d", a, b)
	}
}

func TestStableGroupIDProbesUsedIDs(t *testing.T) {
	first := stableGroupID([]int64{1, 2, 3}, nil)
	next := stableGroupID([]int64{1, 2, 3}, map[int]bool{first: true})
	if next == first {
		t.Fatalf("stableGroupID reused occupied id %d", first)
	}
}

func TestRefreshAllGroupSnapshotsSkipsSuppressedCandidates(t *testing.T) {
	f := newFixture(t)

	people := []struct {
		participantID int64
		personID      int64
		email         string
	}{
		{1, 10, "action-a@example.com"},
		{2, 20, "action-b@example.com"},
		{3, 30, "hidden-a@example.com"},
		{4, 40, "hidden-b@example.com"},
		{5, 50, "saved-a@example.com"},
		{6, 60, "saved-b@example.com"},
	}
	for _, p := range people {
		f.addParticipant(t, p.participantID, p.email)
		f.addPerson(t, p.personID, p.email, p.email, false)
	}

	// Group 100 is an actionable candidate, 200 is a suppressed candidate, and
	// 300 is suppressed but user-saved. Only 100 and 300 should be hydrated.
	if _, err := f.db.Exec(`
		INSERT INTO memento_social_group (group_id, size, density, is_actionable, suppression_reason, saved_at)
		VALUES
			(100, 2, 1, 1, '', NULL),
			(200, 2, 1, 0, 'too_large', NULL),
			(300, 2, 1, 0, 'too_large', '2024-01-02T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert groups: %v", err)
	}
	for _, pair := range []struct {
		groupID int64
		a       int64
		b       int64
	}{
		{100, 10, 20},
		{200, 30, 40},
		{300, 50, 60},
	} {
		if _, err := f.db.Exec(
			`INSERT INTO memento_social_group_member (group_id, person_id) VALUES (?, ?), (?, ?)`,
			pair.groupID, pair.a, pair.groupID, pair.b,
		); err != nil {
			t.Fatalf("insert group members: %v", err)
		}
	}

	for _, msg := range []struct {
		msgID        int64
		senderPartID int64
		rcptPartID   int64
	}{
		{1000, 1, 2},
		{2000, 3, 4},
		{3000, 5, 6},
	} {
		f.addConversation(t, msg.msgID)
		f.addMessageAt(t, msg.msgID, msg.senderPartID, msg.msgID, "2024-01-01 10:00:00")
		f.addRecipient(t, msg.msgID, msg.rcptPartID, "to")
	}

	if err := RefreshAllGroupSnapshots(f.ctx, f.db); err != nil {
		t.Fatalf("RefreshAllGroupSnapshots: %v", err)
	}

	got := map[int64]int{}
	rows, err := f.db.QueryContext(f.ctx, `
		SELECT group_id, message_count
		FROM memento_social_group
		WHERE group_id IN (100, 200, 300)
	`)
	if err != nil {
		t.Fatalf("query message counts: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var groupID int64
		var count int
		if err := rows.Scan(&groupID, &count); err != nil {
			t.Fatalf("scan message count: %v", err)
		}
		got[groupID] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("message count rows: %v", err)
	}

	if got[100] == 0 {
		t.Fatalf("actionable group was not hydrated: counts=%v", got)
	}
	if got[200] != 0 {
		t.Fatalf("suppressed candidate was hydrated: counts=%v", got)
	}
	if got[300] == 0 {
		t.Fatalf("saved suppressed group was not hydrated: counts=%v", got)
	}
}
