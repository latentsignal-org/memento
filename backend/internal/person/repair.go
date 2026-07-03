package person

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"memento/backend/internal/msgvault"
)

type RepairOptions struct {
	Apply bool
}

type RepairReport struct {
	Applied             bool           `json:"applied"`
	PersonsScanned      int            `json:"persons_scanned"`
	PersonsAffected     int            `json:"persons_affected"`
	EmailsSplit         int            `json:"emails_split"`
	SplitCountsBySource map[string]int `json:"split_counts_by_source"`
	Splits              []RepairSplit  `json:"splits,omitempty"`
}

type RepairSplit struct {
	OriginalPersonID int64    `json:"original_person_id"`
	NewPersonID      int64    `json:"new_person_id,omitempty"`
	NormalizedEmail  string   `json:"normalized_email"`
	Emails           []string `json:"emails"`
	PriorSources     []string `json:"prior_sources"`
}

type repairPerson struct {
	id           int64
	name         string
	primaryEmail string
	emails       []repairEmail
}

type repairEmail struct {
	email       string
	displayName string
	source      string
	confidence  float64
	locked      bool
	createdAt   string
}

// RepairNonDeterministicLinks splits historical resolver-created links that
// are no longer safe under the deterministic-only identity policy.
func RepairNonDeterministicLinks(ctx context.Context, db *sql.DB, opts RepairOptions) (RepairReport, error) {
	persons, err := loadRepairPersons(ctx, db)
	if err != nil {
		return RepairReport{}, err
	}
	report := RepairReport{
		Applied:             opts.Apply,
		PersonsScanned:      len(persons),
		SplitCountsBySource: map[string]int{},
	}

	var planned []repairSplitPlan
	for _, p := range persons {
		splits := planRepairSplits(p)
		if len(splits) == 0 {
			continue
		}
		report.PersonsAffected++
		for _, split := range splits {
			report.EmailsSplit += len(split.emails)
			out := RepairSplit{
				OriginalPersonID: p.id,
				NormalizedEmail:  split.normalizedEmail,
				Emails:           make([]string, 0, len(split.emails)),
			}
			sourceSet := map[string]bool{}
			for _, email := range split.emails {
				out.Emails = append(out.Emails, email.email)
				report.SplitCountsBySource[email.source]++
				sourceSet[email.source] = true
			}
			for source := range sourceSet {
				out.PriorSources = append(out.PriorSources, source)
			}
			sort.Strings(out.Emails)
			sort.Strings(out.PriorSources)
			report.Splits = append(report.Splits, out)
			planned = append(planned, split)
		}
	}
	if !opts.Apply || len(planned) == 0 {
		return report, nil
	}

	for i, split := range planned {
		newID, err := applyRepairSplit(ctx, db, split)
		if err != nil {
			return report, err
		}
		report.Splits[i].NewPersonID = newID
	}
	return report, nil
}

type repairSplitPlan struct {
	personID        int64
	personName      string
	normalizedEmail string
	emails          []repairEmail
}

func loadRepairPersons(ctx context.Context, db *sql.DB) ([]repairPerson, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT mp.id, mp.canonical_name, mp.primary_email,
		       mpe.email_address, mpe.display_name, mpe.link_source, mpe.confidence, mpe.locked,
		       COALESCE(mpe.created_at, '')
		FROM memento_person mp
		JOIN memento_person_email mpe ON mpe.person_id = mp.id
		ORDER BY mp.id, mpe.created_at, mpe.email_address
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[int64]*repairPerson{}
	var ordered []int64
	for rows.Next() {
		var personID int64
		var lockedInt int
		email := repairEmail{}
		var personName, primary string
		if err := rows.Scan(
			&personID, &personName, &primary,
			&email.email, &email.displayName, &email.source, &email.confidence, &lockedInt, &email.createdAt,
		); err != nil {
			return nil, err
		}
		email.email = strings.ToLower(strings.TrimSpace(email.email))
		email.locked = lockedInt != 0
		p := byID[personID]
		if p == nil {
			p = &repairPerson{id: personID, name: personName, primaryEmail: primary}
			byID[personID] = p
			ordered = append(ordered, personID)
		}
		p.emails = append(p.emails, email)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]repairPerson, 0, len(ordered))
	for _, id := range ordered {
		out = append(out, *byID[id])
	}
	return out, nil
}

func planRepairSplits(p repairPerson) []repairSplitPlan {
	if len(p.emails) == 0 {
		return nil
	}
	groups := map[string][]repairEmail{}
	for _, email := range p.emails {
		key := normalizeEmail(email.email)
		groups[key] = append(groups[key], email)
	}
	retained := retainedRepairGroup(p, groups)

	var keys []string
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var plans []repairSplitPlan
	for _, key := range keys {
		if key == retained {
			continue
		}
		if groupHasRepairAnchor(groups[key]) {
			continue
		}
		var unsafe []repairEmail
		for _, email := range groups[key] {
			if !email.locked && isUnsafeResolverSource(email.source) {
				unsafe = append(unsafe, email)
			}
		}
		if len(unsafe) == 0 {
			continue
		}
		plans = append(plans, repairSplitPlan{
			personID:        p.id,
			personName:      p.name,
			normalizedEmail: key,
			emails:          unsafe,
		})
	}
	return plans
}

func retainedRepairGroup(p repairPerson, groups map[string][]repairEmail) string {
	primaryKey := normalizeEmail(p.primaryEmail)
	if _, ok := groups[primaryKey]; ok {
		return primaryKey
	}
	var safeKeys []string
	for key, emails := range groups {
		if groupHasRepairAnchor(emails) {
			safeKeys = append(safeKeys, key)
		}
	}
	if len(safeKeys) > 0 {
		sort.Strings(safeKeys)
		return safeKeys[0]
	}
	type ranked struct {
		key       string
		count     int
		createdAt string
	}
	var ranks []ranked
	for key, emails := range groups {
		oldest := ""
		for _, email := range emails {
			if oldest == "" || (email.createdAt != "" && email.createdAt < oldest) {
				oldest = email.createdAt
			}
		}
		ranks = append(ranks, ranked{key: key, count: len(emails), createdAt: oldest})
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].count != ranks[j].count {
			return ranks[i].count > ranks[j].count
		}
		if ranks[i].createdAt != ranks[j].createdAt {
			return ranks[i].createdAt < ranks[j].createdAt
		}
		return ranks[i].key < ranks[j].key
	})
	return ranks[0].key
}

func groupHasRepairAnchor(emails []repairEmail) bool {
	for _, email := range emails {
		if email.locked || email.source == LinkSourceManual || email.source == LinkSourceManualMerge {
			return true
		}
	}
	return false
}

func isUnsafeResolverSource(source string) bool {
	switch source {
	case LinkSourceExactName, LinkSourceForwarderUnwrap, LinkSourceJaroWinkler, LinkSourceJaccard:
		return true
	default:
		return false
	}
}

func applyRepairSplit(ctx context.Context, db *sql.DB, split repairSplitPlan) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	canonicalName := repairCanonicalName(split)
	primaryEmail := split.emails[0].email
	note := fmt.Sprintf("Repair: split from person #%d (%s) because prior resolver link was non-deterministic.", split.personID, split.personName)
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO memento_person (canonical_name, primary_email, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, canonicalName, primaryEmail, note, now, now)
	if err != nil {
		return 0, err
	}
	newID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	var moved int64
	for _, email := range split.emails {
		res, err := tx.ExecContext(ctx, `
			UPDATE memento_person_email
			SET person_id = ?, link_source = ?, confidence = 1.0, locked = 1, updated_at = ?
			WHERE lower(email_address) = lower(?) AND person_id = ? AND locked = 0
		`, newID, LinkSourceManual, now, email.email, split.personID)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			moved += n
		}
	}
	if err := refreshPersonIdentity(ctx, tx, split.personID); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM memento_person
		WHERE id NOT IN (SELECT DISTINCT person_id FROM memento_person_email)
	`); err != nil {
		return 0, err
	}
	if moved == 0 {
		newID = 0
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return newID, nil
}

func repairCanonicalName(split repairSplitPlan) string {
	for _, email := range split.emails {
		name := strings.TrimSpace(email.displayName)
		if name != "" && !strings.Contains(name, "@") {
			return normalizeLastFirst(name)
		}
	}
	return nameFromEmail(split.emails[0].email)
}

func refreshPersonIdentity(ctx context.Context, tx *sql.Tx, personID int64) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT email_address, display_name, link_source, confidence
		FROM memento_person_email
		WHERE person_id = ?
	`, personID)
	if err != nil {
		return err
	}
	defer rows.Close()
	c := cluster{ID: 1}
	for rows.Next() {
		var email, displayName, source string
		var confidence float64
		if err := rows.Scan(&email, &displayName, &source, &confidence); err != nil {
			return err
		}
		c.Members = append(c.Members, &clusterMember{
			Participant: structParticipant(email, displayName),
			LinkSource:  source,
			Confidence:  confidence,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(c.Members) == 0 {
		_, err := tx.ExecContext(ctx, `DELETE FROM memento_person WHERE id = ?`, personID)
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE memento_person
		SET canonical_name = ?, primary_email = ?, updated_at = ?
		WHERE id = ?
	`, CanonicalNameFor(c), PrimaryEmailFor(c), time.Now().UTC().Format(time.RFC3339), personID)
	return err
}

func structParticipant(email, displayName string) msgvault.ParticipantForResolution {
	return msgvault.ParticipantForResolution{EmailAddress: email, DisplayName: displayName}
}
