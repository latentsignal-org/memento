package person

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LoadLockedEmails returns the set of email addresses that the matcher must
// leave alone — manual links, user-locked rows, etc.
func LoadLockedEmails(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT lower(email_address) FROM memento_person_email WHERE locked = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out[e] = true
	}
	return out, rows.Err()
}

// PersistClusters incrementally reconciles memento_person_email and
// memento_person against the new clustering, preserving existing person_ids
// for clusters whose membership overlaps with an existing person.
//
// Stability contract:
//   - If any email in a new cluster is already mapped to a person, that
//     person's id is reused for the whole cluster. The cluster's canonical
//     name and primary email are refreshed to the new run's pick.
//   - Locked rows always win id-attribution conflicts (manual decisions are
//     never silently overridden).
//   - Non-locked email rows whose participants are no longer present in
//     msgvault (an email vanished) are removed. Persons left without any
//     remaining email are removed.
//   - The whole operation runs in one transaction.
//
// Why this matters: downstream tables (candidate report, future People-page
// reviews/labels) key on memento_person.id. If id values churn across runs,
// those references go stale. Incremental preservation is the contract.
func PersistClusters(ctx context.Context, db *sql.DB, clusters []cluster) (created, linked int, err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	existingMap, err := loadEmailToPerson(ctx, tx)
	if err != nil {
		return 0, 0, err
	}

	insertPerson, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_person (canonical_name, primary_email, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return 0, 0, err
	}
	defer insertPerson.Close()

	updatePerson, err := tx.PrepareContext(ctx, `
		UPDATE memento_person
		SET canonical_name = ?, primary_email = ?, updated_at = ?
		WHERE id = ?
	`)
	if err != nil {
		return 0, 0, err
	}
	defer updatePerson.Close()

	// UPSERT non-locked rows; never touch locked rows.
	upsertEmail, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_person_email
			(email_address, person_id, display_name, link_source, confidence, locked, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(email_address) DO UPDATE SET
			person_id = excluded.person_id,
			display_name = excluded.display_name,
			link_source = excluded.link_source,
			confidence = excluded.confidence,
			updated_at = excluded.updated_at
		WHERE memento_person_email.locked = 0
	`)
	if err != nil {
		return 0, 0, err
	}
	defer upsertEmail.Close()

	seenEmails := make(map[string]bool, 2048)

	for _, c := range clusters {
		if len(c.Members) == 0 {
			continue
		}
		personID, reused := pickPersonID(c, existingMap)
		canonicalName := CanonicalNameFor(c)
		primaryEmail := PrimaryEmailFor(c)

		if reused {
			if _, err := updatePerson.ExecContext(ctx, canonicalName, primaryEmail, now, personID); err != nil {
				return 0, 0, fmt.Errorf("update person %d: %w", personID, err)
			}
		} else {
			res, err := insertPerson.ExecContext(ctx, canonicalName, primaryEmail, now, now)
			if err != nil {
				return 0, 0, fmt.Errorf("insert person: %w", err)
			}
			id, err := res.LastInsertId()
			if err != nil {
				return 0, 0, err
			}
			personID = id
			created++
		}

		singleton := len(c.Members) == 1
		for _, m := range c.Members {
			email := strings.ToLower(m.Participant.EmailAddress)
			seenEmails[email] = true
			source := m.LinkSource
			confidence := m.Confidence
			if source == "" || singleton {
				source = LinkSourceSingleton
				if confidence == 0 {
					confidence = 1.0
				}
			}
			if _, err := upsertEmail.ExecContext(ctx,
				email,
				personID,
				m.Participant.DisplayName,
				source,
				confidence,
				now, now,
			); err != nil {
				return 0, 0, fmt.Errorf("upsert email %s: %w", email, err)
			}
			linked++
		}
	}

	// Sweep: drop non-locked rows whose email vanished from the participant
	// set this run (e.g., msgvault removed the participant). Locked rows are
	// always preserved.
	if err := sweepStaleEmails(ctx, tx, seenEmails); err != nil {
		return 0, 0, err
	}

	// Drop any orphaned persons (no remaining emails). Locked rows protected
	// the persons we want to keep.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM memento_person
		WHERE id NOT IN (SELECT DISTINCT person_id FROM memento_person_email)
	`); err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return created, linked, nil
}

// existingMapping holds enough of a memento_person_email row to drive the
// id-stability heuristic in pickPersonID.
type existingMapping struct {
	PersonID int64
	Locked   bool
}

// pickPersonID is the id-stability heuristic. It returns (id, reused=true) if
// any current member of the cluster is already mapped — preferring locked
// rows, then majority, then lowest id as a deterministic tie-break. If
// nothing is mapped yet, returns (0, false) and the caller creates a new
// person.
func pickPersonID(c cluster, existing map[string]existingMapping) (int64, bool) {
	counts := map[int64]int{}
	var lockedID int64
	for _, m := range c.Members {
		ex, ok := existing[strings.ToLower(m.Participant.EmailAddress)]
		if !ok {
			continue
		}
		counts[ex.PersonID]++
		if ex.Locked {
			lockedID = ex.PersonID
		}
	}
	if lockedID != 0 {
		return lockedID, true
	}
	if len(counts) == 0 {
		return 0, false
	}
	var best int64
	var bestN int
	for id, n := range counts {
		switch {
		case n > bestN:
			best, bestN = id, n
		case n == bestN && (best == 0 || id < best):
			best = id
		}
	}
	return best, true
}

func loadEmailToPerson(ctx context.Context, tx *sql.Tx) (map[string]existingMapping, error) {
	rows, err := tx.QueryContext(ctx, `SELECT lower(email_address), person_id, locked FROM memento_person_email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]existingMapping{}
	for rows.Next() {
		var email string
		var id int64
		var locked int64
		if err := rows.Scan(&email, &id, &locked); err != nil {
			return nil, err
		}
		out[email] = existingMapping{PersonID: id, Locked: locked != 0}
	}
	return out, rows.Err()
}

func sweepStaleEmails(ctx context.Context, tx *sql.Tx, seen map[string]bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT lower(email_address) FROM memento_person_email WHERE locked = 0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var stale []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return err
		}
		if !seen[e] {
			stale = append(stale, e)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, e := range stale {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memento_person_email WHERE lower(email_address) = ? AND locked = 0`, e); err != nil {
			return err
		}
	}
	return nil
}

// LookupByEmail returns the Person and PersonEmail rows for a given address,
// or sql.ErrNoRows if the email is not mapped.
func LookupByEmail(ctx context.Context, db *sql.DB, email string) (Person, PersonEmail, error) {
	var p Person
	var pe PersonEmail
	row := db.QueryRowContext(ctx, `
		SELECT p.id, p.canonical_name, p.primary_email, p.note,
		       pe.email_address, pe.display_name, pe.link_source, pe.confidence, pe.locked
		FROM memento_person_email pe
		JOIN memento_person p ON p.id = pe.person_id
		WHERE pe.email_address = lower(?)
	`, email)
	var lockedInt int64
	if err := row.Scan(
		&p.ID, &p.CanonicalName, &p.PrimaryEmail, &p.Note,
		&pe.EmailAddress, &pe.DisplayName, &pe.LinkSource, &pe.Confidence, &lockedInt,
	); err != nil {
		return Person{}, PersonEmail{}, err
	}
	pe.PersonID = p.ID
	pe.Locked = lockedInt != 0
	return p, pe, nil
}

// EmailsForPerson returns every email mapped to the given person, ordered by
// link source rank desc (so manual/locked rows come first).
func EmailsForPerson(ctx context.Context, db *sql.DB, personID int64) ([]PersonEmail, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT email_address, person_id, display_name, link_source, confidence, locked
		FROM memento_person_email
		WHERE person_id = ?
		ORDER BY locked DESC, link_source
	`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonEmail
	for rows.Next() {
		var pe PersonEmail
		var lockedInt int64
		if err := rows.Scan(
			&pe.EmailAddress, &pe.PersonID, &pe.DisplayName, &pe.LinkSource, &pe.Confidence, &lockedInt,
		); err != nil {
			return nil, err
		}
		pe.Locked = lockedInt != 0
		out = append(out, pe)
	}
	return out, rows.Err()
}

// LinkEmailToPerson is the manual override: associates `email` with `personID`,
// marking the row locked so the next matcher run leaves it alone.
func LinkEmailToPerson(ctx context.Context, db *sql.DB, email string, personID int64, note string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
		INSERT INTO memento_person_email
			(email_address, person_id, display_name, link_source, confidence, locked, created_at, updated_at)
		VALUES (lower(?), ?, '', ?, 1.0, 1, ?, ?)
		ON CONFLICT(email_address) DO UPDATE SET
			person_id = excluded.person_id,
			link_source = excluded.link_source,
			confidence = excluded.confidence,
			locked = 1,
			updated_at = excluded.updated_at
	`, email, personID, LinkSourceManual, now, now)
	return err
}

// SplitEmailToNewPerson creates a fresh memento_person row that owns only the
// supplied email, locking the new row. Used when the matcher incorrectly
// merged an address into someone else's person.
func SplitEmailToNewPerson(ctx context.Context, db *sql.DB, email, canonicalName, note string) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	if canonicalName == "" {
		canonicalName = email
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO memento_person (canonical_name, primary_email, note, created_at, updated_at)
		VALUES (?, lower(?), ?, ?, ?)
	`, canonicalName, email, note, now, now)
	if err != nil {
		return 0, err
	}
	personID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memento_person_email
			(email_address, person_id, display_name, link_source, confidence, locked, created_at, updated_at)
		VALUES (lower(?), ?, '', ?, 1.0, 1, ?, ?)
		ON CONFLICT(email_address) DO UPDATE SET
			person_id = excluded.person_id,
			link_source = excluded.link_source,
			confidence = excluded.confidence,
			locked = 1,
			updated_at = excluded.updated_at
	`, email, personID, LinkSourceManual, now, now); err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM memento_person
		WHERE id NOT IN (SELECT DISTINCT person_id FROM memento_person_email)
	`); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return personID, nil
}
