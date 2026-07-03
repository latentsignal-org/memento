package person

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func seedRepairPerson(t *testing.T, db *sql.DB, name, primary string) int64 {
	t.Helper()
	res, err := db.Exec(`INSERT INTO memento_person (canonical_name, primary_email) VALUES (?, ?)`, name, primary)
	if err != nil {
		t.Fatalf("seed person: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}

func seedRepairEmail(t *testing.T, db *sql.DB, personID int64, email, displayName, source string, locked bool) {
	t.Helper()
	lockedInt := 0
	if locked {
		lockedInt = 1
	}
	if _, err := db.Exec(`
		INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence, locked)
		VALUES (?, ?, ?, ?, 1.0, ?)
	`, email, personID, displayName, source, lockedInt); err != nil {
		t.Fatalf("seed email: %v", err)
	}
}

func TestRepairNonDeterministicLinks_DryRunDoesNotMutate(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	personID := seedRepairPerson(t, db, "Jane Smith", "jane@home.example")
	seedRepairEmail(t, db, personID, "jane@home.example", "Jane Smith", LinkSourceSingleton, false)
	seedRepairEmail(t, db, personID, "jane@work.example", "Jane Smith", LinkSourceExactName, false)

	report, err := RepairNonDeterministicLinks(ctx, db, RepairOptions{})
	if err != nil {
		t.Fatalf("repair dry-run: %v", err)
	}
	if report.Applied {
		t.Fatalf("dry-run report applied = true")
	}
	if report.PersonsScanned != 1 || report.PersonsAffected != 1 || report.EmailsSplit != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.SplitCountsBySource[LinkSourceExactName] != 1 {
		t.Fatalf("exact_name split count = %d, want 1", report.SplitCountsBySource[LinkSourceExactName])
	}
	mapping := participantIDsForEmails(t, db)
	if mapping["jane@work.example"] != personID {
		t.Fatalf("dry-run mutated mapping: got %d, want %d", mapping["jane@work.example"], personID)
	}
}

func TestRepairNonDeterministicLinks_ApplySplitsUnsafeRows(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	personID := seedRepairPerson(t, db, "Jane Smith", "jane@home.example")
	seedRepairEmail(t, db, personID, "jane@home.example", "Jane Smith", LinkSourceSingleton, false)
	seedRepairEmail(t, db, personID, "jane@work.example", "Jane Smith", LinkSourceExactName, false)

	report, err := RepairNonDeterministicLinks(ctx, db, RepairOptions{Apply: true})
	if err != nil {
		t.Fatalf("repair apply: %v", err)
	}
	if !report.Applied || report.EmailsSplit != 1 || len(report.Splits) != 1 || report.Splits[0].NewPersonID == 0 {
		t.Fatalf("unexpected report: %+v", report)
	}

	var workPersonID int64
	var source string
	var locked int
	if err := db.QueryRow(`SELECT person_id, link_source, locked FROM memento_person_email WHERE email_address = 'jane@work.example'`).Scan(&workPersonID, &source, &locked); err != nil {
		t.Fatalf("query split row: %v", err)
	}
	if workPersonID == personID {
		t.Fatalf("unsafe email stayed on original person")
	}
	if source != LinkSourceManual || locked != 1 {
		t.Fatalf("split row source/locked = %s/%d, want manual/1", source, locked)
	}
	var note string
	if err := db.QueryRow(`SELECT note FROM memento_person WHERE id = ?`, workPersonID).Scan(&note); err != nil {
		t.Fatalf("query note: %v", err)
	}
	if !strings.Contains(note, "Repair: split from person") {
		t.Fatalf("repair note = %q", note)
	}
}

func TestRepairNonDeterministicLinks_PreservesSafeAndDeterministicLinks(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	personID := seedRepairPerson(t, db, "Jane Smith", "jsmith@gmail.com")
	seedRepairEmail(t, db, personID, "jsmith@gmail.com", "Jane Smith", LinkSourceSingleton, false)
	seedRepairEmail(t, db, personID, "j.smith@gmail.com", "Jane Smith", LinkSourceExactName, false)
	seedRepairEmail(t, db, personID, "jane.manual@example.com", "Jane Manual", LinkSourceManual, true)
	seedRepairEmail(t, db, personID, "jane.manualmerge@example.com", "Jane Manual Merge", LinkSourceManualMerge, false)
	seedRepairEmail(t, db, personID, "jane.typo@example.com", "Jane Smyth", LinkSourceJaroWinkler, false)

	report, err := RepairNonDeterministicLinks(ctx, db, RepairOptions{Apply: true})
	if err != nil {
		t.Fatalf("repair apply: %v", err)
	}
	if report.EmailsSplit != 1 {
		t.Fatalf("emails split = %d, want 1: %+v", report.EmailsSplit, report)
	}
	mapping := participantIDsForEmails(t, db)
	for _, email := range []string{"jsmith@gmail.com", "j.smith@gmail.com", "jane.manual@example.com", "jane.manualmerge@example.com"} {
		if mapping[email] != personID {
			t.Fatalf("%s moved to %d, want original person %d", email, mapping[email], personID)
		}
	}
	if mapping["jane.typo@example.com"] == personID {
		t.Fatalf("unsafe jaro_winkler row was not split")
	}
}

func TestRepairNonDeterministicLinks_PreservesUnsafeRowInAnchoredEquivalentGroup(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	personID := seedRepairPerson(t, db, "Bob Jones", "bob@work.example")
	seedRepairEmail(t, db, personID, "bob@work.example", "Bob Jones", LinkSourceSingleton, false)
	seedRepairEmail(t, db, personID, "bob.jones@gmail.com", "Bob Jones", LinkSourceManual, true)
	seedRepairEmail(t, db, personID, "bob.jones+news@gmail.com", "Bob Jones", LinkSourceExactName, false)

	report, err := RepairNonDeterministicLinks(ctx, db, RepairOptions{})
	if err != nil {
		t.Fatalf("repair dry-run: %v", err)
	}
	if report.EmailsSplit != 0 {
		t.Fatalf("emails split = %d, want 0: %+v", report.EmailsSplit, report)
	}
	mapping := participantIDsForEmails(t, db)
	if mapping["bob.jones+news@gmail.com"] != personID {
		t.Fatalf("plus variant moved in dry-run: got %d, want %d", mapping["bob.jones+news@gmail.com"], personID)
	}
}

func TestApplyRepairSplit_RemovesOrphanWhenNoRowsMove(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	personID := seedRepairPerson(t, db, "Jane Smith", "jane@home.example")
	seedRepairEmail(t, db, personID, "jane@home.example", "Jane Smith", LinkSourceSingleton, false)
	seedRepairEmail(t, db, personID, "jane@work.example", "Jane Smith", LinkSourceExactName, true)

	newID, err := applyRepairSplit(ctx, db, repairSplitPlan{
		personID:        personID,
		personName:      "Jane Smith",
		normalizedEmail: "jane@work.example",
		emails: []repairEmail{{
			email:       "jane@work.example",
			displayName: "Jane Smith",
			source:      LinkSourceExactName,
			locked:      false,
		}},
	})
	if err != nil {
		t.Fatalf("apply repair split: %v", err)
	}
	if newID != 0 {
		t.Fatalf("new id = %d, want 0 when no rows moved", newID)
	}
	var orphanCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memento_person WHERE id NOT IN (SELECT DISTINCT person_id FROM memento_person_email)`).Scan(&orphanCount); err != nil {
		t.Fatalf("query orphan count: %v", err)
	}
	if orphanCount != 0 {
		t.Fatalf("orphan persons = %d, want 0", orphanCount)
	}
}
