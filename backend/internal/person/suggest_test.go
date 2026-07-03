package person

import (
	"context"
	"testing"
)

func TestUpsertMergeSuggestion_DedupesPendingAndPreservesRejected(t *testing.T) {
	db := newMergeTestDB(t)
	ctx := context.Background()
	a := seedPerson(t, db, "Jane Smith", "jane@home.example")
	b := seedPerson(t, db, "Jane Smith", "jane@work.example")

	if err := UpsertMergeSuggestion(ctx, db, MergeSuggestionInput{
		PersonAID:      a,
		PersonBID:      b,
		Sources:        []string{LinkSourceExactName},
		NameSimilarity: 1,
		CombinedScore:  1,
		ScoresStale:    true,
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := UpsertMergeSuggestion(ctx, db, MergeSuggestionInput{
		PersonAID:      b,
		PersonBID:      a,
		Sources:        []string{LinkSourceGraph},
		SignatureScore: 0.9,
		CombinedScore:  0.8,
		NameSimilarity: 0.7,
		TokenOverlap:   0.6,
		ScoresStale:    false,
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	rows, err := ListMergeSuggestions(ctx, db, ListMergeSuggestionOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if !containsString(rows[0].Sources, LinkSourceExactName) || !containsString(rows[0].Sources, LinkSourceGraph) {
		t.Fatalf("sources = %v, want exact_name + graph", rows[0].Sources)
	}
	if rows[0].ScoresStale {
		t.Fatalf("scores should not be stale after graph evidence")
	}
	if _, err := MarkMergeSuggestionResolved(ctx, db, rows[0].ID, "rejected"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if err := UpsertMergeSuggestion(ctx, db, MergeSuggestionInput{
		PersonAID:      a,
		PersonBID:      b,
		Sources:        []string{LinkSourceJaroWinkler},
		NameSimilarity: 0.95,
		CombinedScore:  0.95,
		ScoresStale:    true,
	}); err != nil {
		t.Fatalf("third upsert: %v", err)
	}
	pending, err := ListMergeSuggestions(ctx, db, ListMergeSuggestionOptions{})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("rejected pair resurfaced as pending: %+v", pending)
	}
	rejected, err := ListMergeSuggestions(ctx, db, ListMergeSuggestionOptions{Status: "rejected"})
	if err != nil {
		t.Fatalf("list rejected: %v", err)
	}
	if len(rejected) != 1 || containsString(rejected[0].Sources, LinkSourceJaroWinkler) {
		t.Fatalf("rejected row was touched by rerun: %+v", rejected)
	}
}

func TestGenerateAndPersistGraphSuggestions_DivergentNames(t *testing.T) {
	db := newMergeTestDB(t)
	ctx := context.Background()
	p1 := seedPerson(t, db, "Alice Wonderland", "alice@x.com")
	p2 := seedPerson(t, db, "Bob Burger", "bob@x.com")
	c1 := seedPerson(t, db, "Carol C", "c1@x.com")
	c2 := seedPerson(t, db, "Carol C", "c2@x.com")
	for _, p := range []int64{p1, p2} {
		seedEdge(t, db, p, c1, 100)
		seedEdge(t, db, p, c2, 100)
	}

	candidates, err := GenerateAndPersistGraphSuggestions(ctx, db, DefaultMergeOptions())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatalf("expected graph candidates")
	}
	rows, err := ListMergeSuggestions(ctx, db, ListMergeSuggestionOptions{Sort: "signature"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *MergeSuggestionRow
	for i := range rows {
		row := &rows[i]
		if (row.PersonAID == p1 && row.PersonBID == p2) || (row.PersonAID == p2 && row.PersonBID == p1) {
			found = row
			break
		}
	}
	if found == nil {
		t.Fatalf("expected persisted p1/p2 suggestion, got %+v", rows)
	}
	if !containsString(found.Sources, LinkSourceGraph) || found.SignatureScore < 0.9 {
		t.Fatalf("unexpected graph suggestion: %+v", found)
	}
}

func TestPersistGraphMergeCandidates_ClearsStaleGraphOnlyPendingRows(t *testing.T) {
	db := newMergeTestDB(t)
	ctx := context.Background()
	a := seedPerson(t, db, "Jane Smith", "jane@home.example")
	b := seedPerson(t, db, "Janet Smyth", "janet@work.example")
	c := seedPerson(t, db, "Alex Lee", "alex@example")
	d := seedPerson(t, db, "Alex Li", "alex.li@example")

	if err := UpsertMergeSuggestion(ctx, db, MergeSuggestionInput{
		PersonAID:      a,
		PersonBID:      b,
		Sources:        []string{LinkSourceGraph},
		SignatureScore: 1,
		CombinedScore:  0.7,
		ScoresStale:    false,
	}); err != nil {
		t.Fatalf("upsert graph suggestion: %v", err)
	}
	if err := UpsertMergeSuggestion(ctx, db, MergeSuggestionInput{
		PersonAID:      c,
		PersonBID:      d,
		Sources:        []string{LinkSourceJaroWinkler},
		NameSimilarity: 0.95,
		CombinedScore:  0.95,
		ScoresStale:    true,
	}); err != nil {
		t.Fatalf("upsert name suggestion: %v", err)
	}

	if err := PersistGraphMergeCandidates(ctx, db, nil); err != nil {
		t.Fatalf("persist empty graph candidates: %v", err)
	}
	rows, err := ListMergeSuggestions(ctx, db, ListMergeSuggestionOptions{})
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(rows) != 1 || !containsString(rows[0].Sources, LinkSourceJaroWinkler) {
		t.Fatalf("pending rows = %+v, want only the name-sourced suggestion", rows)
	}
}
