package social

import (
	"context"
	"database/sql"
	"testing"

	"memento/backend/internal/store"

	_ "modernc.org/sqlite"
)

// openTestDB creates an in-memory SQLite DB, applies all Memento migrations
// (including the social tables), and seeds the required msgvault schema.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sources (
			id INTEGER PRIMARY KEY,
			identifier TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS participants (
			id INTEGER PRIMARY KEY,
			email_address TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE IF NOT EXISTS conversations (
			id INTEGER PRIMARY KEY
		);
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY,
			source_message_id TEXT DEFAULT '',
			sender_id INTEGER,
			sent_at TEXT,
			conversation_id INTEGER,
			subject TEXT,
			snippet TEXT
		);
		CREATE TABLE IF NOT EXISTS message_recipients (
			message_id INTEGER NOT NULL,
			participant_id INTEGER NOT NULL,
			recipient_type TEXT NOT NULL,
			PRIMARY KEY (message_id, participant_id)
		);
	`); err != nil {
		t.Fatalf("create msgvault schema: %v", err)
	}
	return db
}

// fixture is a self-contained test world.
type fixture struct {
	db  *sql.DB
	ctx context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{db: openTestDB(t), ctx: context.Background()}
}

// addSource seeds a source (owner account email).
func (f *fixture) addSource(t *testing.T, email string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO sources (identifier) VALUES (?)`, email); err != nil {
		t.Fatalf("addSource: %v", err)
	}
}

// addParticipant seeds a participant and returns its id.
func (f *fixture) addParticipant(t *testing.T, id int64, email string) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO participants (id, email_address) VALUES (?, ?)`, id, email); err != nil {
		t.Fatalf("addParticipant: %v", err)
	}
}

// addPerson seeds a memento_person row and its email mapping.
func (f *fixture) addPerson(t *testing.T, personID int64, name, email string, dismissed bool) {
	t.Helper()
	dismissedAt := "NULL"
	if dismissed {
		dismissedAt = "CURRENT_TIMESTAMP"
	}
	if _, err := f.db.Exec(
		`INSERT INTO memento_person (id, canonical_name, primary_email, dismissed_at)
		 VALUES (?, ?, ?, `+dismissedAt+`)`,
		personID, name, email,
	); err != nil {
		t.Fatalf("addPerson: %v", err)
	}
	if _, err := f.db.Exec(
		`INSERT INTO memento_person_email (email_address, person_id, link_source, confidence)
		 VALUES (?, ?, 'test', 1.0)`,
		email, personID,
	); err != nil {
		t.Fatalf("addPersonEmail: %v", err)
	}
}

// addPersonEmailAlias attaches another email address to an existing person.
func (f *fixture) addPersonEmailAlias(t *testing.T, personID int64, email string) {
	t.Helper()
	if _, err := f.db.Exec(
		`INSERT INTO memento_person_email (email_address, person_id, link_source, confidence)
		 VALUES (?, ?, 'test_alias', 1.0)`,
		email, personID,
	); err != nil {
		t.Fatalf("addPersonEmailAlias: %v", err)
	}
}

// addCandidate seeds a memento_people_candidates row.
func (f *fixture) addCandidate(t *testing.T, personID int64, classification string) {
	t.Helper()
	if _, err := f.db.Exec(`
		INSERT INTO memento_people_candidates
		(person_id, canonical_name, primary_email, domain, email_count,
		 total_messages, from_contact_count, to_contact_count, bidirectional_score, classification)
		VALUES (?, 'n', 'e', 'd', 1, 1, 0, 1, 0, ?)
	`, personID, classification); err != nil {
		t.Fatalf("addCandidate: %v", err)
	}
}

// addConversation seeds a conversation row.
func (f *fixture) addConversation(t *testing.T, id int64) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO conversations (id) VALUES (?)`, id); err != nil {
		t.Fatalf("addConversation: %v", err)
	}
}

// addMessage seeds a message row.
func (f *fixture) addMessage(t *testing.T, id, senderParticipantID, conversationID int64) {
	t.Helper()
	if _, err := f.db.Exec(
		`INSERT INTO messages (id, sender_id, sent_at, conversation_id)
		 VALUES (?, ?, '2024-01-01 10:00:00', ?)`,
		id, senderParticipantID, conversationID,
	); err != nil {
		t.Fatalf("addMessage: %v", err)
	}
}

// addMessageAt seeds a message with a specific timestamp.
func (f *fixture) addMessageAt(t *testing.T, id, senderParticipantID, conversationID int64, sentAt string) {
	t.Helper()
	if _, err := f.db.Exec(
		`INSERT INTO messages (id, sender_id, sent_at, conversation_id) VALUES (?, ?, ?, ?)`,
		id, senderParticipantID, sentAt, conversationID,
	); err != nil {
		t.Fatalf("addMessage: %v", err)
	}
}

// addRecipient seeds a message_recipients row.
func (f *fixture) addRecipient(t *testing.T, msgID, participantID int64, recipientType string) {
	t.Helper()
	if _, err := f.db.Exec(
		`INSERT INTO message_recipients (message_id, participant_id, recipient_type) VALUES (?, ?, ?)`,
		msgID, participantID, recipientType,
	); err != nil {
		t.Fatalf("addRecipient: %v", err)
	}
}

func (f *fixture) countEdges(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM memento_social_edge`).Scan(&n); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	return n
}

func (f *fixture) getEdge(t *testing.T, aID, bID int64) (directCount, coRecipCount, threadCount, aToB, bToA int, found bool) {
	t.Helper()
	// Ensure canonical order.
	if aID > bID {
		aID, bID = bID, aID
	}
	err := f.db.QueryRowContext(f.ctx, `
		SELECT direct_count, co_recipient_count, thread_count, a_to_b_count, b_to_a_count
		FROM memento_social_edge WHERE person_a_id = ? AND person_b_id = ?
	`, aID, bID).Scan(&directCount, &coRecipCount, &threadCount, &aToB, &bToA)
	if err == sql.ErrNoRows {
		return 0, 0, 0, 0, 0, false
	}
	if err != nil {
		t.Fatalf("getEdge: %v", err)
	}
	return directCount, coRecipCount, threadCount, aToB, bToA, true
}

// TestCase1_OwnerSendsToTwo verifies two direct edges and one co-recipient edge.
func TestCase1_OwnerSendsToTwo(t *testing.T) {
	f := newFixture(t)

	// Seed: owner, alice, bob
	f.addSource(t, "owner@example.com")
	f.addParticipant(t, 1, "owner@example.com")
	f.addParticipant(t, 2, "alice@example.com")
	f.addParticipant(t, 3, "bob@example.com")

	f.addPerson(t, 10, "Owner", "owner@example.com", false)
	f.addPerson(t, 20, "Alice", "alice@example.com", false)
	f.addPerson(t, 30, "Bob", "bob@example.com", false)

	f.addCandidate(t, 10, "candidate")
	f.addCandidate(t, 20, "candidate")
	f.addCandidate(t, 30, "candidate")

	// Two messages in different conversations so co-recipient edge survives noise floor.
	f.addConversation(t, 1)
	f.addConversation(t, 2)
	f.addMessage(t, 1, 1, 1) // owner sends
	f.addRecipient(t, 1, 2, "to")
	f.addRecipient(t, 1, 3, "to")
	f.addMessage(t, 2, 1, 2) // owner sends again, different thread
	f.addRecipient(t, 2, 2, "to")
	f.addRecipient(t, 2, 3, "to")

	result, err := BuildSocialGraph(f.ctx, f.db)
	if err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}

	// 2 direct edges (owner-alice, owner-bob) + 1 co-recipient edge (alice-bob)
	if result.EdgeCount != 3 {
		t.Errorf("EdgeCount = %d, want 3", result.EdgeCount)
	}

	// owner-alice direct edge
	direct, coRecip, threads, _, _, found := f.getEdge(t, 10, 20)
	if !found {
		t.Fatal("owner-alice edge not found")
	}
	if direct != 2 || coRecip != 0 {
		t.Errorf("owner-alice: direct=%d coRecip=%d, want direct=2 coRecip=0", direct, coRecip)
	}

	// owner-bob direct edge
	direct, coRecip, _, _, _, found = f.getEdge(t, 10, 30)
	if !found {
		t.Fatal("owner-bob edge not found")
	}
	if direct != 2 {
		t.Errorf("owner-bob: direct=%d, want 2", direct)
	}

	// alice-bob co-recipient edge
	direct, coRecip, threads, _, _, found = f.getEdge(t, 20, 30)
	if !found {
		t.Fatal("alice-bob co-recipient edge not found")
	}
	if direct != 0 || coRecip != 2 || threads != 2 {
		t.Errorf("alice-bob: direct=%d coRecip=%d threads=%d, want 0/2/2", direct, coRecip, threads)
	}

	network, err := LoadPersonNetwork(f.ctx, f.db, 20, 5)
	if err != nil {
		t.Fatalf("LoadPersonNetwork: %v", err)
	}
	if network == nil || len(network.Neighbors) != 1 || network.Neighbors[0].PersonID != 30 {
		t.Fatalf("Alice network neighbors = %#v, want only Bob with owner excluded", network)
	}

	missing, err := FindMissingCollaborators(f.ctx, f.db, []int64{20, 30}, 5, 1)
	if err != nil {
		t.Fatalf("FindMissingCollaborators: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing collaborators = %#v, want none because owner is excluded", missing)
	}
}

// TestCase2_RecipientExplosionCap verifies that >30 recipients suppresses co-recipient pairs.
func TestCase2_RecipientExplosionCap(t *testing.T) {
	f := newFixture(t)

	f.addSource(t, "owner@example.com")
	f.addParticipant(t, 1, "owner@example.com")
	f.addPerson(t, 10, "Owner", "owner@example.com", false)
	f.addCandidate(t, 10, "candidate")

	// Create 31 recipients.
	for i := 2; i <= 32; i++ {
		email := "person" + itoa(i) + "@example.com"
		personID := int64(i * 10)
		f.addParticipant(t, int64(i), email)
		f.addPerson(t, personID, "Person"+itoa(i), email, false)
		f.addCandidate(t, personID, "candidate")
	}

	f.addConversation(t, 1)
	f.addMessage(t, 1, 1, 1)
	for i := 2; i <= 32; i++ {
		f.addRecipient(t, 1, int64(i), "to")
	}

	result, err := BuildSocialGraph(f.ctx, f.db)
	if err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}

	// 31 direct edges (owner to each recipient), 0 co-recipient pairs.
	if result.EdgeCount != 31 {
		t.Errorf("EdgeCount = %d, want 31 (31 direct, 0 co-recipient)", result.EdgeCount)
	}

	// Verify no co-recipient edge exists between any two recipients.
	var coRecipEdges int
	f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM memento_social_edge WHERE co_recipient_count > 0 AND direct_count = 0`).Scan(&coRecipEdges)
	if coRecipEdges != 0 {
		t.Errorf("got %d co-recipient edges, want 0 (explosion cap should suppress them)", coRecipEdges)
	}
}

// TestCase3_ExcludedPersonProducesNoEdges verifies excluded persons are omitted.
func TestCase3_ExcludedPersonProducesNoEdges(t *testing.T) {
	f := newFixture(t)

	f.addSource(t, "owner@example.com")
	f.addParticipant(t, 1, "owner@example.com")
	f.addParticipant(t, 2, "carol@example.com")

	f.addPerson(t, 10, "Owner", "owner@example.com", false)
	f.addPerson(t, 20, "Carol", "carol@example.com", false)

	f.addCandidate(t, 10, "candidate")
	f.addCandidate(t, 20, "excluded") // excluded

	f.addConversation(t, 1)
	f.addMessage(t, 1, 1, 1)
	f.addRecipient(t, 1, 2, "to")

	result, err := BuildSocialGraph(f.ctx, f.db)
	if err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}
	if result.EdgeCount != 0 {
		t.Errorf("EdgeCount = %d, want 0 (carol is excluded)", result.EdgeCount)
	}
}

// TestCase4_DismissedPersonProducesNoEdges verifies dismissed persons are omitted.
func TestCase4_DismissedPersonProducesNoEdges(t *testing.T) {
	f := newFixture(t)

	f.addSource(t, "owner@example.com")
	f.addParticipant(t, 1, "owner@example.com")
	f.addParticipant(t, 2, "dave@example.com")

	f.addPerson(t, 10, "Owner", "owner@example.com", false)
	f.addPerson(t, 20, "Dave", "dave@example.com", true) // dismissed

	f.addCandidate(t, 10, "candidate")
	// Dave has no candidate row (or could have one, but dismissed_at is what matters).

	f.addConversation(t, 1)
	f.addMessage(t, 1, 1, 1)
	f.addRecipient(t, 1, 2, "to")

	result, err := BuildSocialGraph(f.ctx, f.db)
	if err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}
	if result.EdgeCount != 0 {
		t.Errorf("EdgeCount = %d, want 0 (dave is dismissed)", result.EdgeCount)
	}
}

// TestCase5_DirectionCounters verifies a_to_b and b_to_a are correctly set
// for the same canonical pair regardless of who sends.
func TestCase5_DirectionCounters(t *testing.T) {
	f := newFixture(t)

	f.addSource(t, "owner@example.com")
	f.addParticipant(t, 1, "owner@example.com")
	f.addParticipant(t, 2, "alice@example.com")

	// person IDs: owner=10, alice=20. Canonical pair: (10,20) since 10 < 20.
	f.addPerson(t, 10, "Owner", "owner@example.com", false)
	f.addPerson(t, 20, "Alice", "alice@example.com", false)
	f.addCandidate(t, 10, "candidate")
	f.addCandidate(t, 20, "candidate")

	f.addConversation(t, 1)
	f.addConversation(t, 2)

	// Message A: owner (participant 1, person 10) sends to alice (participant 2, person 20).
	// Person 10 < 20, so this is a_to_b (from person_a=10 to person_b=20).
	f.addMessage(t, 1, 1, 1)
	f.addRecipient(t, 1, 2, "to")

	// Message B: alice (participant 2, person 20) sends to owner (participant 1, person 10).
	// Person 20 > 10, so this is b_to_a (from person_b=20 to person_a=10).
	f.addMessage(t, 2, 2, 2)
	f.addRecipient(t, 2, 1, "to")

	_, err := BuildSocialGraph(f.ctx, f.db)
	if err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}

	_, _, _, aToB, bToA, found := f.getEdge(t, 10, 20)
	if !found {
		t.Fatal("owner-alice edge not found")
	}
	// owner (10) -> alice (20): a_to_b++ since 10 < 20
	// alice (20) -> owner (10): b_to_a++ since sender 20 > receiver 10, but edge key is (10,20)
	if aToB != 1 {
		t.Errorf("a_to_b = %d, want 1", aToB)
	}
	if bToA != 1 {
		t.Errorf("b_to_a = %d, want 1", bToA)
	}
}

// TestCase6_NoiseFloor verifies that co_recipient_count=1, direct=0 is dropped.
func TestCase6_NoiseFloor(t *testing.T) {
	f := newFixture(t)

	f.addSource(t, "owner@example.com")
	f.addParticipant(t, 1, "owner@example.com")
	f.addParticipant(t, 2, "alice@example.com")
	f.addParticipant(t, 3, "bob@example.com")

	f.addPerson(t, 10, "Owner", "owner@example.com", false)
	f.addPerson(t, 20, "Alice", "alice@example.com", false)
	f.addPerson(t, 30, "Bob", "bob@example.com", false)
	f.addCandidate(t, 10, "candidate")
	f.addCandidate(t, 20, "candidate")
	f.addCandidate(t, 30, "candidate")

	// Single message: owner sends to alice and bob (one thread).
	// alice-bob: co_recipient=1, direct=0, thread=1 → below noise floor → dropped.
	f.addConversation(t, 1)
	f.addMessage(t, 1, 1, 1)
	f.addRecipient(t, 1, 2, "to")
	f.addRecipient(t, 1, 3, "to")

	_, err := BuildSocialGraph(f.ctx, f.db)
	if err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}

	// alice-bob pair should be dropped (co_recipient=1 < 2, thread=1 < 2, direct=0)
	_, _, _, _, _, found := f.getEdge(t, 20, 30)
	if found {
		t.Error("alice-bob edge should have been dropped by noise floor (co_recipient=1, thread=1, direct=0)")
	}

	// owner-alice and owner-bob should survive (direct=1).
	_, _, _, _, _, aliceFound := f.getEdge(t, 10, 20)
	_, _, _, _, _, bobFound := f.getEdge(t, 10, 30)
	if !aliceFound || !bobFound {
		t.Error("owner-alice and owner-bob direct edges should survive noise floor")
	}
}

// TestCase7_ConnectedComponents verifies two disjoint clusters are detected.
func TestCase7_ConnectedComponents(t *testing.T) {
	f := newFixture(t)

	// No owner source needed for this test.

	// Group 1: alice (p100) <-> bob (p200)
	f.addParticipant(t, 1, "alice@example.com")
	f.addParticipant(t, 2, "bob@example.com")
	f.addPerson(t, 100, "Alice", "alice@example.com", false)
	f.addPerson(t, 200, "Bob", "bob@example.com", false)
	f.addCandidate(t, 100, "candidate")
	f.addCandidate(t, 200, "candidate")

	// Group 2: eve (p300) <-> frank (p400)
	f.addParticipant(t, 3, "eve@example.com")
	f.addParticipant(t, 4, "frank@example.com")
	f.addPerson(t, 300, "Eve", "eve@example.com", false)
	f.addPerson(t, 400, "Frank", "frank@example.com", false)
	f.addCandidate(t, 300, "candidate")
	f.addCandidate(t, 400, "candidate")

	// Group 1 messages: alice sends to bob.
	f.addConversation(t, 1)
	f.addMessage(t, 1, 1, 1) // alice -> bob
	f.addRecipient(t, 1, 2, "to")

	// Group 2 messages: eve sends to frank.
	f.addConversation(t, 2)
	f.addMessage(t, 2, 3, 2) // eve -> frank
	f.addRecipient(t, 2, 4, "to")

	result, err := BuildSocialGraph(f.ctx, f.db)
	if err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}
	if result.EdgeCount != 2 {
		t.Errorf("EdgeCount = %d, want 2", result.EdgeCount)
	}

	// Should produce exactly 2 clusters, each of size 2.
	if result.ClusterCount != 2 {
		t.Errorf("ClusterCount = %d, want 2", result.ClusterCount)
	}

	var sizes []int
	rows, err := f.db.QueryContext(f.ctx, `SELECT size FROM memento_social_cluster ORDER BY size`)
	if err != nil {
		t.Fatalf("query clusters: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s int
		rows.Scan(&s)
		sizes = append(sizes, s)
	}
	if len(sizes) != 2 || sizes[0] != 2 || sizes[1] != 2 {
		t.Errorf("cluster sizes = %v, want [2 2]", sizes)
	}
}

func TestCase8_AliasesDedupPerMessageAndAvoidSelfEdges(t *testing.T) {
	f := newFixture(t)

	f.addSource(t, "owner@example.com")
	f.addParticipant(t, 1, "owner@example.com")
	f.addParticipant(t, 2, "alice@example.com")
	f.addParticipant(t, 3, "bob@example.com")
	f.addParticipant(t, 4, "alice.alias@example.com")

	f.addPerson(t, 10, "Owner", "owner@example.com", false)
	f.addPerson(t, 20, "Alice", "alice@example.com", false)
	f.addPersonEmailAlias(t, 20, "alice.alias@example.com")
	f.addPerson(t, 30, "Bob", "bob@example.com", false)

	f.addCandidate(t, 10, "candidate")
	f.addCandidate(t, 20, "candidate")
	f.addCandidate(t, 30, "candidate")

	f.addConversation(t, 1)
	f.addMessage(t, 1, 1, 1)
	f.addRecipient(t, 1, 2, "to")
	f.addRecipient(t, 1, 4, "cc")
	f.addRecipient(t, 1, 3, "to")

	if _, err := BuildSocialGraph(f.ctx, f.db); err != nil {
		t.Fatalf("BuildSocialGraph: %v", err)
	}

	direct, _, _, _, _, found := f.getEdge(t, 10, 20)
	if !found {
		t.Fatal("owner-alice edge not found")
	}
	if direct != 1 {
		t.Errorf("owner-alice direct_count = %d, want 1 despite two Alice aliases on one message", direct)
	}

	var selfEdges int
	if err := f.db.QueryRowContext(f.ctx,
		`SELECT COUNT(*) FROM memento_social_edge WHERE person_a_id = person_b_id`,
	).Scan(&selfEdges); err != nil {
		t.Fatalf("count self edges: %v", err)
	}
	if selfEdges != 0 {
		t.Fatalf("selfEdges = %d, want 0", selfEdges)
	}
}

func TestCase9_LoadRecipientsBatchChunksLargeInputs(t *testing.T) {
	f := newFixture(t)

	const n = recipientLookupBatchSize + 10
	msgIDs := make([]int64, 0, n)
	for i := 1; i <= n; i++ {
		msgID := int64(i)
		msgIDs = append(msgIDs, msgID)
		f.addRecipient(t, msgID, 1, "to")
	}

	recipientsByMsg, err := loadRecipientsBatch(f.ctx, f.db, msgIDs)
	if err != nil {
		t.Fatalf("loadRecipientsBatch: %v", err)
	}
	if len(recipientsByMsg) != n {
		t.Fatalf("loaded recipient sets = %d, want %d", len(recipientsByMsg), n)
	}
	for _, msgID := range msgIDs {
		rows := recipientsByMsg[msgID]
		if len(rows) != 1 || rows[0].participantID != 1 || rows[0].recipientType != "to" {
			t.Fatalf("message %d recipients = %#v, want one to-recipient", msgID, rows)
		}
	}
}

// itoa converts int to string without importing strconv in the test file body.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
