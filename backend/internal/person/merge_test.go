package person

import (
	"context"
	"database/sql"
	"testing"

	"memento/backend/internal/store"

	_ "modernc.org/sqlite"
)

// newMergeTestDB opens an in-memory SQLite, applies the real migrations
// (so all FKs and cascade rules behave like production), and enables
// foreign-key enforcement which is off-by-default in modernc.org/sqlite.
func newMergeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable fks: %v", err)
	}
	ctx := context.Background()
	if _, err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Seed a stub `sources` table so loadOwnerPersonIDs has something to
	// query. Tests that need an actual owner row should INSERT into it.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sources (
		  id INTEGER PRIMARY KEY,
		  identifier TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create sources stub: %v", err)
	}
	return db
}

// seedOwner registers an account email so loadOwnerPersonIDs returns
// the corresponding person as an owner, mirroring production sources.
func seedOwner(t *testing.T, db *sql.DB, email string, personID int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO sources (identifier) VALUES (?)`, email); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	seedEmail(t, db, email, personID, true)
}

func seedPerson(t *testing.T, db *sql.DB, name, primary string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO memento_person (canonical_name, primary_email) VALUES (?, ?)`, name, primary)
	if err != nil {
		t.Fatalf("seed person: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func seedEmail(t *testing.T, db *sql.DB, email string, personID int64, locked bool) {
	t.Helper()
	l := 0
	if locked {
		l = 1
	}
	_, err := db.Exec(`INSERT INTO memento_person_email
		(email_address, person_id, display_name, link_source, confidence, locked)
		VALUES (?, ?, '', 'exact_name', 1.0, ?)`, email, personID, l)
	if err != nil {
		t.Fatalf("seed email: %v", err)
	}
}

func seedEdge(t *testing.T, db *sql.DB, a, b int64, weight float64) {
	t.Helper()
	if a > b {
		a, b = b, a
	}
	_, err := db.Exec(`INSERT INTO memento_social_edge
		(person_a_id, person_b_id, direct_count, thread_count, weight)
		VALUES (?, ?, 1, 1, ?)`, a, b, weight)
	if err != nil {
		t.Fatalf("seed edge: %v", err)
	}
}

// TestWeightedJaccard_Basic walks through small cases to lock down the math.
func TestWeightedJaccard_Basic(t *testing.T) {
	a := Signature{10: 100, 20: 50}
	b := Signature{10: 80, 20: 60}
	// intersection sums: min(100,80) + min(50,60) = 80 + 50 = 130
	// union sums:        max(100,80) + max(50,60) = 100 + 60 = 160
	got := weightedJaccard(a, b)
	want := 130.0 / 160.0
	if !approxEq(got, want, 1e-9) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Empty signature returns 0.
	if weightedJaccard(Signature{}, b) != 0 {
		t.Error("empty signature should score 0")
	}

	// Disjoint signatures return 0.
	disj := weightedJaccard(Signature{1: 10}, Signature{2: 10})
	if disj != 0 {
		t.Errorf("disjoint should be 0, got %v", disj)
	}

	// Identical signatures return 1.
	id := weightedJaccard(Signature{1: 10, 2: 5}, Signature{1: 10, 2: 5})
	if !approxEq(id, 1.0, 1e-9) {
		t.Errorf("identical should be 1, got %v", id)
	}
}

// TestCoRecipientSignature reads back what we wrote, ordered by weight.
func TestCoRecipientSignature(t *testing.T) {
	db := newMergeTestDB(t)
	p1 := seedPerson(t, db, "Alice", "a@x.com")
	p2 := seedPerson(t, db, "Bob", "b@x.com")
	p3 := seedPerson(t, db, "Carol", "c@x.com")
	seedEdge(t, db, p1, p2, 50.0)
	seedEdge(t, db, p1, p3, 20.0)

	sig, err := CoRecipientSignature(context.Background(), db, p1, 10)
	if err != nil {
		t.Fatalf("CoRecipientSignature: %v", err)
	}
	if len(sig) != 2 {
		t.Errorf("got %d neighbors, want 2", len(sig))
	}
	if sig[p2] != 50.0 || sig[p3] != 20.0 {
		t.Errorf("unexpected signature contents: %+v", sig)
	}
}

// TestFindMergeCandidates_HighOverlap surfaces a near-duplicate pair.
// Ann1 and Ann2 share the same name tokens and the same heavy neighbors,
// so they should be merged.
func TestFindMergeCandidates_HighOverlap(t *testing.T) {
	db := newMergeTestDB(t)
	ann1 := seedPerson(t, db, "Ann Catherine Jose", "ann@gmail.com")
	ann2 := seedPerson(t, db, "Ann Catherine Jose", "acj@outlook.com")
	liza := seedPerson(t, db, "Liza George", "liza@x.com")
	mary := seedPerson(t, db, "Mary Ann Ck", "mary@x.com")
	jose := seedPerson(t, db, "Jose Philip", "jose@x.com")
	seedEmail(t, db, "ann@gmail.com", ann1, false)
	seedEmail(t, db, "acj@outlook.com", ann2, false)
	seedEmail(t, db, "liza@x.com", liza, false)
	seedEmail(t, db, "mary@x.com", mary, false)
	seedEmail(t, db, "jose@x.com", jose, false)

	// Both Anns are heavily connected to the same three contacts.
	for _, ann := range []int64{ann1, ann2} {
		seedEdge(t, db, ann, liza, 600)
		seedEdge(t, db, ann, mary, 180)
		seedEdge(t, db, ann, jose, 160)
	}

	cands, err := FindMergeCandidates(context.Background(), db, DefaultMergeOptions())
	if err != nil {
		t.Fatalf("FindMergeCandidates: %v", err)
	}
	if len(cands) == 0 {
		t.Fatal("expected at least one candidate")
	}
	// Find the ann1/ann2 pair.
	var found *MergeCandidate
	for i := range cands {
		c := &cands[i]
		if (c.FromID == ann1 && c.IntoID == ann2) || (c.FromID == ann2 && c.IntoID == ann1) {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatalf("expected ann1/ann2 candidate, got %+v", cands)
	}
	if found.SignatureScore < 0.9 {
		t.Errorf("signature score should be ~1.0, got %v", found.SignatureScore)
	}
	if found.NameScore < 0.9 {
		t.Errorf("name score should be ~1.0, got %v", found.NameScore)
	}
}

// TestFindMergeCandidates_DifferentNeighborhoodsRejected protects against
// the same-name false-positive case: two distinct Sarahs in different
// communities should NOT be merged.
func TestFindMergeCandidates_DifferentNeighborhoodsRejected(t *testing.T) {
	db := newMergeTestDB(t)
	sarah1 := seedPerson(t, db, "Sarah Smith", "sarah@work.com")
	sarah2 := seedPerson(t, db, "Sarah Smith", "sarah@family.org")
	work1 := seedPerson(t, db, "Alex Engineer", "alex@work.com")
	work2 := seedPerson(t, db, "Beth PM", "beth@work.com")
	fam1 := seedPerson(t, db, "Uncle Joe", "joe@family.org")
	fam2 := seedPerson(t, db, "Aunt Mae", "mae@family.org")

	// sarah1 is entirely in the work cluster.
	seedEdge(t, db, sarah1, work1, 200)
	seedEdge(t, db, sarah1, work2, 150)
	// sarah2 is entirely in the family cluster.
	seedEdge(t, db, sarah2, fam1, 200)
	seedEdge(t, db, sarah2, fam2, 150)

	cands, err := FindMergeCandidates(context.Background(), db, DefaultMergeOptions())
	if err != nil {
		t.Fatalf("FindMergeCandidates: %v", err)
	}
	for _, c := range cands {
		if (c.FromID == sarah1 && c.IntoID == sarah2) || (c.FromID == sarah2 && c.IntoID == sarah1) {
			t.Errorf("disjoint-neighborhood Sarahs should not be surfaced: %+v", c)
		}
	}
}

// TestFindMergeCandidates_NameMismatchRejected protects against the case
// where two people happen to share an email-local token by coincidence
// ("info@a.com" vs "info@b.com") — names should disambiguate.
func TestFindMergeCandidates_NameMismatchRejected(t *testing.T) {
	db := newMergeTestDB(t)
	p1 := seedPerson(t, db, "Alice Wonderland", "alice@x.com")
	p2 := seedPerson(t, db, "Bob Burger", "bob@x.com")
	c1 := seedPerson(t, db, "Carol C", "c1@x.com")
	c2 := seedPerson(t, db, "Carol C", "c2@x.com")
	// p1 and p2 share all neighbors but have totally different names.
	seedEdge(t, db, p1, c1, 100)
	seedEdge(t, db, p1, c2, 100)
	seedEdge(t, db, p2, c1, 100)
	seedEdge(t, db, p2, c2, 100)

	cands, err := FindMergeCandidates(context.Background(), db, DefaultMergeOptions())
	if err != nil {
		t.Fatalf("FindMergeCandidates: %v", err)
	}
	for _, c := range cands {
		if (c.FromID == p1 && c.IntoID == p2) || (c.FromID == p2 && c.IntoID == p1) {
			t.Errorf("disjoint-name pair should not be surfaced: %+v", c)
		}
	}
}

// TestMergePersons_HappyPath verifies emails, facets, narratives, notes,
// and project memberships all transfer correctly.
func TestMergePersons_HappyPath(t *testing.T) {
	db := newMergeTestDB(t)
	from := seedPerson(t, db, "Ann J", "ann@a.com")
	into := seedPerson(t, db, "Ann Catherine Jose", "ann@b.com")

	seedEmail(t, db, "ann@a.com", from, false)
	seedEmail(t, db, "ann@a-alt.com", from, true) // locked alias
	seedEmail(t, db, "ann@b.com", into, false)

	if _, err := db.Exec(`INSERT INTO memento_person_facet (person_id, facet_type, content) VALUES (?, 'role', 'spouse')`, from); err != nil {
		t.Fatalf("seed facet: %v", err)
	}

	// Narrative conflict case: both persons have a "summary" section; into's
	// should be preserved, from's discarded.
	if _, err := db.Exec(`INSERT INTO memento_person_narrative (person_id, section, content) VALUES (?, 'summary', 'old summary'), (?, 'summary', 'canonical summary')`, from, into); err != nil {
		t.Fatalf("seed narratives: %v", err)
	}
	// Non-conflicting narrative section: should transfer.
	if _, err := db.Exec(`INSERT INTO memento_person_narrative (person_id, section, content) VALUES (?, 'timeline', 'long timeline')`, from); err != nil {
		t.Fatalf("seed narrative timeline: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO memento_note (dimension, entity_id, content) VALUES ('person', ?, 'note on from')`, from); err != nil {
		t.Fatalf("seed note: %v", err)
	}

	// Project membership: both persons in the same project should dedupe;
	// from-only membership transfers.
	if _, err := db.Exec(`INSERT INTO memento_project (id, name, slug) VALUES (1, 'P1', 'p1'), (2, 'P2', 'p2')`); err != nil {
		t.Fatalf("seed projects: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO memento_project_member (project_id, person_id) VALUES (1, ?), (1, ?), (2, ?)`, from, into, from); err != nil {
		t.Fatalf("seed memberships: %v", err)
	}

	result, err := MergePersons(context.Background(), db, from, into)
	if err != nil {
		t.Fatalf("MergePersons: %v", err)
	}

	// Emails: both ann@a.com and ann@a-alt.com transferred and locked.
	var emailRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memento_person_email WHERE person_id = ? AND locked = 1 AND link_source = ?`, into, LinkSourceSignatureMerge).Scan(&emailRows); err != nil {
		t.Fatalf("count emails: %v", err)
	}
	if emailRows != 2 {
		t.Errorf("emails transferred = %d, want 2 (locked + signature_merge)", emailRows)
	}
	if result.EmailsTransferred != 2 {
		t.Errorf("result.EmailsTransferred = %d, want 2", result.EmailsTransferred)
	}

	// Facet transferred.
	var facetRows int
	db.QueryRow(`SELECT COUNT(*) FROM memento_person_facet WHERE person_id = ?`, into).Scan(&facetRows)
	if facetRows != 1 {
		t.Errorf("facet on into = %d, want 1", facetRows)
	}

	// Narrative: into has both 'summary' (its own) and 'timeline' (transferred);
	// from's 'summary' was discarded due to conflict.
	var narrativeRows int
	db.QueryRow(`SELECT COUNT(*) FROM memento_person_narrative WHERE person_id = ?`, into).Scan(&narrativeRows)
	if narrativeRows != 2 {
		t.Errorf("narratives on into = %d, want 2 (summary + timeline)", narrativeRows)
	}
	var canonical string
	db.QueryRow(`SELECT content FROM memento_person_narrative WHERE person_id = ? AND section = 'summary'`, into).Scan(&canonical)
	if canonical != "canonical summary" {
		t.Errorf("summary on into = %q, want canonical_summary", canonical)
	}
	if result.NarrativesTransferred != 1 || result.NarrativesSkippedConflict != 1 {
		t.Errorf("narrative counts: transferred=%d skipped=%d, want 1/1",
			result.NarrativesTransferred, result.NarrativesSkippedConflict)
	}

	// Note transferred.
	var noteRows int
	db.QueryRow(`SELECT COUNT(*) FROM memento_note WHERE dimension = 'person' AND entity_id = ?`, into).Scan(&noteRows)
	if noteRows != 1 {
		t.Errorf("notes on into = %d, want 1", noteRows)
	}

	// Project membership: into is in projects 1 and 2; from's project 1
	// row was deduped, from's project 2 row was transferred.
	var memberRows int
	db.QueryRow(`SELECT COUNT(*) FROM memento_project_member WHERE person_id = ?`, into).Scan(&memberRows)
	if memberRows != 2 {
		t.Errorf("memberships for into = %d, want 2", memberRows)
	}
	if result.ProjectMembersSkipped != 1 || result.ProjectMembersTransferred != 1 {
		t.Errorf("project member counts: transferred=%d skipped=%d, want 1/1",
			result.ProjectMembersTransferred, result.ProjectMembersSkipped)
	}

	// From person is gone.
	var fromRows int
	db.QueryRow(`SELECT COUNT(*) FROM memento_person WHERE id = ?`, from).Scan(&fromRows)
	if fromRows != 0 {
		t.Errorf("from person still exists (rows=%d)", fromRows)
	}
}

// TestMergePersons_RejectsSameID guards against the identity merge.
func TestMergePersons_RejectsSameID(t *testing.T) {
	db := newMergeTestDB(t)
	p := seedPerson(t, db, "Solo", "solo@x.com")
	if _, err := MergePersons(context.Background(), db, p, p); err == nil {
		t.Error("expected error merging person into itself")
	}
}

// TestMergePersons_RejectsMissingPerson guards against typo'd IDs.
func TestMergePersons_RejectsMissingPerson(t *testing.T) {
	db := newMergeTestDB(t)
	p := seedPerson(t, db, "Real", "real@x.com")
	if _, err := MergePersons(context.Background(), db, 9999, p); err == nil {
		t.Error("expected error for missing `from` person")
	}
	if _, err := MergePersons(context.Background(), db, p, 9999); err == nil {
		t.Error("expected error for missing `into` person")
	}
}

// TestFindMergeCandidates_OwnerOnlyOverlapRejected guards against the
// most common false-positive pattern observed against real data: two
// low-volume contacts that share nothing beyond a single edge to the
// owner. Their post-filter signatures are empty, so they must NOT be
// surfaced — the existing person-resolve --fuzzy pass already handles
// name-only matches and Phase 6 should add evidence beyond that.
func TestFindMergeCandidates_OwnerOnlyOverlapRejected(t *testing.T) {
	db := newMergeTestDB(t)
	owner := seedPerson(t, db, "Owner", "owner@me.com")
	seedOwner(t, db, "owner@me.com", owner)

	tesla1 := seedPerson(t, db, "Tesla", "tesla-no-reply@x.com")
	tesla2 := seedPerson(t, db, "Tesla", "tesla-owners@x.com")
	seedEmail(t, db, "tesla-no-reply@x.com", tesla1, false)
	seedEmail(t, db, "tesla-owners@x.com", tesla2, false)

	// Each Tesla alias has exactly one edge — to the owner.
	seedEdge(t, db, owner, tesla1, 50)
	seedEdge(t, db, owner, tesla2, 50)

	cands, err := FindMergeCandidates(context.Background(), db, DefaultMergeOptions())
	if err != nil {
		t.Fatalf("FindMergeCandidates: %v", err)
	}
	for _, c := range cands {
		if (c.FromID == tesla1 && c.IntoID == tesla2) || (c.FromID == tesla2 && c.IntoID == tesla1) {
			t.Errorf("owner-only-overlap pair should not be surfaced: %+v", c)
		}
	}
}

// TestFindMergeCandidates_RealSharedNeighborSurfaced confirms the
// happy path still works once the owner-edge filter is on: two Ann
// rows sharing real human collaborators (not just the owner) come
// through cleanly.
func TestFindMergeCandidates_RealSharedNeighborSurfaced(t *testing.T) {
	db := newMergeTestDB(t)
	owner := seedPerson(t, db, "Owner", "owner@me.com")
	seedOwner(t, db, "owner@me.com", owner)

	ann1 := seedPerson(t, db, "Ann Catherine Jose", "ann@gmail.com")
	ann2 := seedPerson(t, db, "Ann Catherine Jose", "acj@outlook.com")
	liza := seedPerson(t, db, "Liza George", "liza@x.com")
	mary := seedPerson(t, db, "Mary Ann Ck", "mary@x.com")
	for _, ann := range []int64{ann1, ann2} {
		seedEdge(t, db, owner, ann, 500) // owner edges — should be filtered out
		seedEdge(t, db, ann, liza, 600)
		seedEdge(t, db, ann, mary, 180)
	}

	cands, err := FindMergeCandidates(context.Background(), db, DefaultMergeOptions())
	if err != nil {
		t.Fatalf("FindMergeCandidates: %v", err)
	}
	var found *MergeCandidate
	for i := range cands {
		c := &cands[i]
		if (c.FromID == ann1 && c.IntoID == ann2) || (c.FromID == ann2 && c.IntoID == ann1) {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatalf("expected ann1/ann2 candidate, got %d candidates", len(cands))
	}
	if found.SharedNeighbor < 2 {
		t.Errorf("shared neighbors after owner-filter = %d, want >= 2", found.SharedNeighbor)
	}
	if found.SignatureScore < 0.9 {
		t.Errorf("signature score = %v, want >= 0.9", found.SignatureScore)
	}
}

func approxEq(a, b, eps float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}
