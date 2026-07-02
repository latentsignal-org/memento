// Package person — Phase 6: identity-resolution feedback via social-graph
// signatures.
//
// The social communication graph captures, for each canonical person, a
// "neighborhood signature": the set of other people they regularly appear
// alongside on email envelopes, weighted by interaction strength. Two
// memento_person rows that represent the same human will share nearly
// identical signatures, even when their email addresses look unrelated.
//
// This file surfaces merge candidates by:
//
//  1. Reading each person's signature from memento_social_edge.
//  2. Pre-filtering candidate pairs via shared display-name/email tokens and
//     shared non-owner graph neighbors, so we never compute O(N²) scores on a
//     9k-person graph.
//  3. Scoring each candidate pair on three independent signals:
//     - weighted Jaccard of their signatures (the strongest signal)
//     - name-token Jaccard (soft corroborating evidence)
//     - temporal overlap of their active windows (catches role changes)
//  4. Combining the signals into a single score in [0, 1].
//
// The output is advisory. MergePersons is callable only via the
// person-merge CLI command, which surfaces candidates for confirmation.
// Manual user decisions (locked rows) are preserved through every step.
package person

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Signature is a person's weighted neighborhood in the social graph.
// Keys are neighbor person IDs; values are edge weights.
type Signature map[int64]float64

// CoRecipientSignature returns the top-K neighbors of personID from
// memento_social_edge, weighted by edge weight. Returns an empty map if
// the person has no edges or the social graph hasn't been built yet.
//
// topK bounds the cost of subsequent Jaccard math — 25 neighbors captures
// the meaningful part of any signature and keeps comparisons O(K).
//
// This raw signature INCLUDES the owner. Callers comparing two persons
// should pass through filterOwner to remove owner edges first — the owner
// is connected to nearly every person and contributes only noise to the
// Jaccard score.
func CoRecipientSignature(ctx context.Context, db *sql.DB, personID int64, topK int) (Signature, error) {
	if topK <= 0 {
		topK = 25
	}
	rows, err := db.QueryContext(ctx, `
		SELECT
		  CASE WHEN person_a_id = ?1 THEN person_b_id ELSE person_a_id END AS neighbor_id,
		  weight
		FROM memento_social_edge
		WHERE person_a_id = ?1 OR person_b_id = ?1
		ORDER BY weight DESC
		LIMIT ?2
	`, personID, topK)
	if err != nil {
		return nil, fmt.Errorf("load signature for person %d: %w", personID, err)
	}
	defer rows.Close()
	sig := Signature{}
	for rows.Next() {
		var nid int64
		var w float64
		if err := rows.Scan(&nid, &w); err != nil {
			return nil, err
		}
		sig[nid] = w
	}
	return sig, rows.Err()
}

// loadOwnerPersonIDs returns the set of memento_person ids that represent
// the account owner across their configured email sources. Used by
// FindMergeCandidates to strip the owner from signatures — every person
// has an edge to the owner, so leaving the owner in collapses the
// weighted Jaccard to ~1.0 for any two low-volume contacts (matching the
// false-positive pattern observed against real data).
//
// The query mirrors the account_emails / account_participants CTE used
// throughout the social-graph extractor. Returns an empty map (not nil,
// not error) when no sources are configured — useful for the test path.
func loadOwnerPersonIDs(ctx context.Context, db *sql.DB) (map[int64]bool, error) {
	out := map[int64]bool{}
	rows, err := db.QueryContext(ctx, `
		WITH account_emails AS (
		  SELECT lower(identifier) AS email FROM sources WHERE identifier LIKE '%@%'
		)
		SELECT DISTINCT pe.person_id
		FROM memento_person_email pe
		JOIN account_emails ae ON ae.email = lower(pe.email_address)
	`)
	if err != nil {
		// sources table belongs to msgvault and may not exist in test envs.
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		if err := rows.Scan(&pid); err != nil {
			return nil, err
		}
		out[pid] = true
	}
	return out, rows.Err()
}

// filterOwner returns a copy of sig with any owner person IDs removed.
// Returns sig unchanged if owners is empty.
func filterOwner(sig Signature, owners map[int64]bool) Signature {
	if len(owners) == 0 || len(sig) == 0 {
		return sig
	}
	out := make(Signature, len(sig))
	for n, w := range sig {
		if owners[n] {
			continue
		}
		out[n] = w
	}
	return out
}

// weightedJaccard returns the weighted Jaccard similarity between two
// signatures in [0, 1].
//
// For each neighbor present in either signature:
//   - numerator   adds min(weightA, weightB)
//   - denominator adds max(weightA, weightB)
//
// Result = numerator / denominator. Returns 0 if either signature is empty.
//
// Why weighted Jaccard over plain set Jaccard: edge weight captures volume
// of co-correspondence, so a shared "Liza George at weight 600" should
// dominate a shared "casual contact at weight 5". Plain set Jaccard would
// treat them identically.
func weightedJaccard(a, b Signature) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var num, den float64
	for n, wa := range a {
		wb := b[n]
		if wa < wb {
			num += wa
			den += wb
		} else {
			num += wb
			den += wa
		}
	}
	for n, wb := range b {
		if _, present := a[n]; present {
			continue // already accounted for above
		}
		// neighbor only in b
		den += wb
	}
	if den == 0 {
		return 0
	}
	return num / den
}

// weightedOverlap computes the Szymkiewicz-Simpson weighted overlap coefficient.
// It is useful when matching active profiles (large signature) with inactive profiles
// (small signature), avoiding the size penalty of the Jaccard denominator.
func weightedOverlap(a, b Signature) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var num float64
	var sumA, sumB float64
	for n, wa := range a {
		sumA += wa
		wb := b[n]
		if wa < wb {
			num += wa
		} else {
			num += wb
		}
	}
	for _, wb := range b {
		sumB += wb
	}
	minDen := sumA
	if sumB < minDen {
		minDen = sumB
	}
	if minDen == 0 {
		return 0
	}
	return num / minDen
}

// MergeCandidate is one proposed merge of fromID → intoID, with the
// component scores that produced the recommendation.
type MergeCandidate struct {
	FromID         int64    `json:"from_id"`
	IntoID         int64    `json:"into_id"`
	FromName       string   `json:"from_name"`
	IntoName       string   `json:"into_name"`
	FromEmail      string   `json:"from_email"`
	IntoEmail      string   `json:"into_email"`
	FromAliases    []string `json:"from_aliases"`
	IntoAliases    []string `json:"into_aliases"`
	SignatureScore float64  `json:"signature_score"` // weighted Jaccard
	NameScore      float64  `json:"name_score"`      // name-token Jaccard
	TemporalScore  float64  `json:"temporal_score"`  // [0,1] overlap
	CombinedScore  float64  `json:"combined_score"`  // weighted combination
	SharedNeighbor int      `json:"shared_neighbors"`
	FromLocked     int      `json:"from_locked_emails"`
	IntoLocked     int      `json:"into_locked_emails"`
	FromMessages   int64    `json:"from_messages"`
	IntoMessages   int64    `json:"into_messages"`
}

// FindMergeOptions controls FindMergeCandidates.
type FindMergeOptions struct {
	MinSignatureScore float64 // weighted Jaccard threshold (default 0.30)
	MinCombinedScore  float64 // combined-score threshold (default 0.55)
	TopK              int     // neighbors per signature (default 25)
	Limit             int     // max candidates returned (default 50)
}

// DefaultMergeOptions returns conservative defaults that surface obvious
// duplicates without flooding the user with false positives.
func DefaultMergeOptions() FindMergeOptions {
	return FindMergeOptions{
		MinSignatureScore: 0.30,
		MinCombinedScore:  0.55,
		TopK:              250,
		Limit:             50,
	}
}

// personRecord is one row of person metadata pulled once at the top of
// FindMergeCandidates and reused for every comparison.
type personRecord struct {
	id            int64
	name          string
	primary       string
	nameTokens    []string
	emailLocals   []string
	tokens        []string // union of nameTokens + emailLocals (dedup)
	firstSeen     time.Time
	lastSeen      time.Time
	lockedCount   int
	aliases       []string
	totalMessages int64
}

// FindMergeCandidates scans memento_person for pairs likely to represent
// the same human and returns them sorted by combined score, highest first.
//
// Performance contract: never quadratic on persons. Uses inverted indexes over
// display/email tokens and shared graph neighbors so only pairs with some cheap
// evidence are scored.
func FindMergeCandidates(ctx context.Context, db *sql.DB, opts FindMergeOptions) ([]MergeCandidate, error) {
	if opts.MinSignatureScore <= 0 {
		opts.MinSignatureScore = 0.30
	}
	if opts.MinCombinedScore <= 0 {
		opts.MinCombinedScore = 0.55
	}
	if opts.TopK <= 0 {
		opts.TopK = 25
	}
	if opts.Limit <= 0 {
		opts.Limit = 50
	}

	persons, err := loadPersonRecords(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(persons) < 2 {
		return nil, nil
	}

	// Owner edges are noise for disambiguation — every person connects
	// to the owner, so they always overlap. Strip the owner from every
	// signature before scoring.
	ownerIDs, err := loadOwnerPersonIDs(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load owner ids: %w", err)
	}

	// Build inverted index: token -> [personID, ...].
	tokenIndex := map[string][]int64{}
	for _, p := range persons {
		for _, t := range p.tokens {
			tokenIndex[t] = append(tokenIndex[t], p.id)
		}
	}

	seen := map[[2]int64]bool{} // canonical (min, max)
	var pairs [][2]int64
	addPair := func(a, b int64) {
		if a == b {
			return
		}
		if a > b {
			a, b = b, a
		}
		key := [2]int64{a, b}
		if seen[key] {
			return
		}
		seen[key] = true
		pairs = append(pairs, key)
	}

	// Generate candidate pairs by walking token buckets.
	for _, ids := range tokenIndex {
		// Skip buckets larger than 200 — these are noisy generic tokens
		// like "info" or "team" that won't yield useful merges and would
		// blow up pair generation quadratically.
		if len(ids) > 200 {
			continue
		}
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				addPair(ids[i], ids[j])
			}
		}
	}

	byID := map[int64]*personRecord{}
	for i := range persons {
		byID[persons[i].id] = &persons[i]
	}

	signatures := map[int64]Signature{}
	neighborIndex := map[int64][]int64{}
	for _, p := range persons {
		rawSig, err := CoRecipientSignature(ctx, db, p.id, opts.TopK)
		if err != nil {
			return nil, err
		}
		sig := filterOwner(rawSig, ownerIDs)
		signatures[p.id] = sig
		for neighborID := range sig {
			neighborIndex[neighborID] = append(neighborIndex[neighborID], p.id)
		}
	}
	for _, ids := range neighborIndex {
		if len(ids) > 200 {
			continue
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				addPair(ids[i], ids[j])
			}
		}
	}

	var candidates []MergeCandidate
	for _, pair := range pairs {
		a := byID[pair[0]]
		b := byID[pair[1]]
		if a == nil || b == nil {
			continue
		}

		nameScore := jaccardTokens(a.nameTokens, b.nameTokens)

		// Signature comparison — the strongest signal but the most
		// expensive signal. Strip owner edges before scoring; otherwise
		// weighted Jaccard collapses to ~1.0 for any two contacts who only
		// co-mail with the owner.
		sigA := signatures[a.id]
		sigB := signatures[b.id]
		sigJaccard := weightedJaccard(sigA, sigB)
		sigOverlap := weightedOverlap(sigA, sigB)
		sigScore := sigJaccard
		if sigOverlap > sigScore {
			sigScore = sigOverlap
		}

		if sigScore < opts.MinSignatureScore {
			continue
		}

		// Count truly shared neighbors (excluding the owner). A pair with
		// zero non-owner shared neighbors is a degenerate match and should
		// not be surfaced by the graph generator.
		shared := 0
		for n := range sigA {
			if _, ok := sigB[n]; ok {
				shared++
			}
		}
		if shared < 1 {
			continue
		}

		tempScore := temporalOverlap(a, b)

		combined := 0.65*sigScore + 0.25*nameScore + 0.1*tempScore
		if combined < opts.MinCombinedScore {
			continue
		}

		// Direction: keep the larger / older / more-locked person as
		// "into", merge the smaller one in. Locked status wins first
		// (manual decisions are anchors), then alias count, then id.
		from, into := a, b
		if shouldKeepAsTarget(a, b) {
			from, into = b, a
		}

		candidates = append(candidates, MergeCandidate{
			FromID:         from.id,
			IntoID:         into.id,
			FromName:       from.name,
			IntoName:       into.name,
			FromEmail:      from.primary,
			IntoEmail:      into.primary,
			FromAliases:    from.aliases,
			IntoAliases:    into.aliases,
			SignatureScore: round3(sigScore),
			NameScore:      round3(nameScore),
			TemporalScore:  round3(tempScore),
			CombinedScore:  round3(combined),
			SharedNeighbor: shared,
			FromLocked:     from.lockedCount,
			IntoLocked:     into.lockedCount,
			FromMessages:   from.totalMessages,
			IntoMessages:   into.totalMessages,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		messagesI := candidates[i].FromMessages
		if candidates[i].IntoMessages > messagesI {
			messagesI = candidates[i].IntoMessages
		}
		messagesJ := candidates[j].FromMessages
		if candidates[j].IntoMessages > messagesJ {
			messagesJ = candidates[j].IntoMessages
		}

		rankI := candidates[i].CombinedScore + 0.25*math.Log10(float64(messagesI+1))
		rankJ := candidates[j].CombinedScore + 0.25*math.Log10(float64(messagesJ+1))
		return rankI > rankJ
	})
	if len(candidates) > opts.Limit {
		candidates = candidates[:opts.Limit]
	}
	return candidates, nil
}

// shouldKeepAsTarget returns true when `a` is the better merge target
// (i.e., we should keep `a` and absorb `b` into it). Decision order:
//
//  1. More locked emails wins (manual decisions are sticky).
//  2. More aliases wins (more evidence accumulated).
//  3. Older id wins (stable preference).
func shouldKeepAsTarget(a, b *personRecord) bool {
	if a.lockedCount != b.lockedCount {
		return a.lockedCount > b.lockedCount
	}
	if len(a.aliases) != len(b.aliases) {
		return len(a.aliases) > len(b.aliases)
	}
	return a.id < b.id
}

// temporalOverlap returns the Jaccard-style overlap of two persons' active
// windows in [0, 1]. 1 means identical spans; 0 means disjoint.
//
// If either person has no message timestamps we return 0.5 — neutral,
// neither corroborating nor refuting.
func temporalOverlap(a, b *personRecord) float64 {
	if a.firstSeen.IsZero() || b.firstSeen.IsZero() {
		return 0.5
	}
	startA, endA := a.firstSeen, a.lastSeen
	startB, endB := b.firstSeen, b.lastSeen
	if endA.Before(startA) {
		endA = startA
	}
	if endB.Before(startB) {
		endB = startB
	}
	start := startA
	if startB.After(start) {
		start = startB
	}
	end := endA
	if endB.Before(end) {
		end = endB
	}
	overlap := end.Sub(start).Hours()
	if overlap <= 0 {
		return 0
	}
	totalEnd := endA
	if endB.After(totalEnd) {
		totalEnd = endB
	}
	totalStart := startA
	if startB.Before(totalStart) {
		totalStart = startB
	}
	total := totalEnd.Sub(totalStart).Hours()
	if total <= 0 {
		return 0
	}
	return overlap / total
}

// loadPersonRecords pulls one row per memento_person with all the metadata
// FindMergeCandidates needs, in two queries. Avoids N+1.
func loadPersonRecords(ctx context.Context, db *sql.DB) ([]personRecord, error) {
	// First pass: persons + their aliases + locked counts.
	rows, err := db.QueryContext(ctx, `
		SELECT mp.id, mp.canonical_name, mp.primary_email,
		       COALESCE(GROUP_CONCAT(mpe.email_address), '') AS aliases_csv,
		       SUM(mpe.locked) AS locked_count
		FROM memento_person mp
		LEFT JOIN memento_person_email mpe ON mpe.person_id = mp.id
		WHERE mp.dismissed_at IS NULL
		GROUP BY mp.id, mp.canonical_name, mp.primary_email
	`)
	if err != nil {
		return nil, fmt.Errorf("load persons: %w", err)
	}
	defer rows.Close()

	var persons []personRecord
	for rows.Next() {
		var r personRecord
		var aliasesCSV string
		var locked sql.NullInt64
		if err := rows.Scan(&r.id, &r.name, &r.primary, &aliasesCSV, &locked); err != nil {
			return nil, err
		}
		if locked.Valid {
			r.lockedCount = int(locked.Int64)
		}
		if aliasesCSV != "" {
			r.aliases = strings.Split(aliasesCSV, ",")
		}
		r.nameTokens = displayNameTokens(r.name)
		r.emailLocals = emailLocalTokens(r.aliases)
		r.tokens = mergeTokens(r.nameTokens, r.emailLocals)
		persons = append(persons, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Second pass: time windows and total messages from report or candidates.
	tsRows, err := db.QueryContext(ctx, `
		SELECT mp.id,
		       COALESCE(pr.first_message_at, mpc.first_message_at, ''),
		       COALESCE(pr.last_message_at, mpc.last_message_at, ''),
		       COALESCE(pr.total_messages, mpc.total_messages, 0)
		FROM memento_person mp
		LEFT JOIN memento_people_report pr ON pr.person_id = mp.id
		LEFT JOIN memento_people_candidates mpc ON mpc.person_id = mp.id
		WHERE mp.dismissed_at IS NULL
	`)
	if err == nil {
		defer tsRows.Close()
		idx := map[int64]*personRecord{}
		for i := range persons {
			idx[persons[i].id] = &persons[i]
		}
		for tsRows.Next() {
			var pid int64
			var first, last string
			var totalMsgs int64
			if err := tsRows.Scan(&pid, &first, &last, &totalMsgs); err != nil {
				return nil, err
			}
			r := idx[pid]
			if r == nil {
				continue
			}
			r.firstSeen = parseSomeTime(first)
			r.lastSeen = parseSomeTime(last)
			r.totalMessages = totalMsgs
		}
	}
	// Swallowing the candidates error is intentional: the table may be
	// empty post-reset, in which case we still want to surface merge
	// candidates using only signature + name evidence.

	return persons, nil
}

// emailLocalTokens extracts useful tokens from the local parts of a
// person's email addresses, e.g. "ann.catherine.jose@x" -> ["ann",
// "catherine", "jose"]. Skips numeric-only and very short tokens which
// would create noisy buckets.
func emailLocalTokens(aliases []string) []string {
	out := map[string]bool{}
	for _, a := range aliases {
		a = strings.ToLower(strings.TrimSpace(a))
		at := strings.Index(a, "@")
		if at <= 0 {
			continue
		}
		local := a[:at]
		// Strip plus-tag.
		if plus := strings.Index(local, "+"); plus > 0 {
			local = local[:plus]
		}
		// Split on common separators.
		parts := strings.FieldsFunc(local, func(r rune) bool {
			return r == '.' || r == '_' || r == '-'
		})
		for _, p := range parts {
			if len(p) < 3 {
				continue
			}
			if isAllDigits(p) {
				continue
			}
			out[p] = true
		}
	}
	tokens := make([]string, 0, len(out))
	for t := range out {
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)
	return tokens
}

// mergeTokens unions two token slices, deduplicating.
func mergeTokens(a, b []string) []string {
	set := map[string]bool{}
	for _, t := range a {
		set[t] = true
	}
	for _, t := range b {
		set[t] = true
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseSomeTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05+00:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func round3(f float64) float64 {
	return float64(int(f*1000+0.5)) / 1000.0
}

// MergePersons absorbs `fromID` into `intoID` in a single transaction:
// transfers all emails, facets, narratives, notes, and project memberships,
// then deletes the now-empty `fromID` row. Cascades take care of derived
// tables (memento_people_candidates, memento_people_report,
// memento_social_edge, memento_social_metric) — these will be rebuilt the
// next time `./memento refresh` runs.
//
// Locked emails on `fromID` are transferred and remain locked, with
// link_source rewritten to LinkSourceSignatureMerge. The user invoked the
// merge; they own the consequences.
//
// Returns the number of rows transferred per table.
type MergeResult struct {
	EmailsTransferred         int
	FacetsTransferred         int
	NarrativesTransferred     int
	NarrativesSkippedConflict int
	NotesTransferred          int
	ProjectMembersTransferred int
	ProjectMembersSkipped     int
}

// MergePersons performs the transactional merge. Returns an error if either
// person is missing, the IDs are equal, or any step fails. The transaction
// is rolled back on any error.
func MergePersons(ctx context.Context, db *sql.DB, fromID, intoID int64) (MergeResult, error) {
	var result MergeResult
	if fromID == intoID {
		return result, errors.New("from and into must differ")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	// Validate both persons exist and aren't dismissed.
	if err := assertPersonExists(ctx, tx, fromID); err != nil {
		return result, fmt.Errorf("from: %w", err)
	}
	if err := assertPersonExists(ctx, tx, intoID); err != nil {
		return result, fmt.Errorf("into: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// 1. Transfer emails. Mark them as locked + signature_merge so the
	//    next resolver run leaves them alone — this was a user decision.
	res, err := tx.ExecContext(ctx, `
		UPDATE memento_person_email
		SET person_id = ?,
		    locked = 1,
		    link_source = ?,
		    updated_at = ?
		WHERE person_id = ?
	`, intoID, LinkSourceSignatureMerge, now, fromID)
	if err != nil {
		return result, fmt.Errorf("transfer emails: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.EmailsTransferred = int(n)
	}

	// 2. Transfer facets (PK is autoincrement id — no conflicts).
	res, err = tx.ExecContext(ctx, `
		UPDATE memento_person_facet SET person_id = ? WHERE person_id = ?
	`, intoID, fromID)
	if err != nil {
		return result, fmt.Errorf("transfer facets: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.FacetsTransferred = int(n)
	}

	// 3. Transfer narratives. PK is (person_id, section). If `into`
	//    already has a section, keep theirs (it's the canonical narrative
	//    on the surviving page) and drop `from`'s for that section.
	res, err = tx.ExecContext(ctx, `
		DELETE FROM memento_person_narrative
		WHERE person_id = ?
		  AND section IN (
		    SELECT section FROM memento_person_narrative WHERE person_id = ?
		  )
	`, fromID, intoID)
	if err != nil {
		return result, fmt.Errorf("dedup narratives: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.NarrativesSkippedConflict = int(n)
	}
	res, err = tx.ExecContext(ctx, `
		UPDATE memento_person_narrative SET person_id = ? WHERE person_id = ?
	`, intoID, fromID)
	if err != nil {
		return result, fmt.Errorf("transfer narratives: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.NarrativesTransferred = int(n)
	}

	// 4. Transfer notes (entity_id is freeform, not an FK).
	res, err = tx.ExecContext(ctx, `
		UPDATE memento_note
		SET entity_id = ?, updated_at = ?
		WHERE dimension = 'person' AND entity_id = ?
	`, intoID, now, fromID)
	if err != nil {
		return result, fmt.Errorf("transfer notes: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.NotesTransferred = int(n)
	}

	// 5. Transfer project memberships. PK is (project_id, person_id).
	//    Drop `from`'s row in any project where `into` is already a
	//    member, then transfer the rest.
	res, err = tx.ExecContext(ctx, `
		DELETE FROM memento_project_member
		WHERE person_id = ?
		  AND project_id IN (
		    SELECT project_id FROM memento_project_member WHERE person_id = ?
		  )
	`, fromID, intoID)
	if err != nil {
		return result, fmt.Errorf("dedup project members: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.ProjectMembersSkipped = int(n)
	}
	res, err = tx.ExecContext(ctx, `
		UPDATE memento_project_member SET person_id = ? WHERE person_id = ?
	`, intoID, fromID)
	if err != nil {
		return result, fmt.Errorf("transfer project members: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		result.ProjectMembersTransferred = int(n)
	}

	pairA, pairB := fromID, intoID
	if pairA > pairB {
		pairA, pairB = pairB, pairA
	}

	// 6. Resolve pending merge suggestions affected by this merge while
	//    both person IDs still exist. The merged pair is accepted; other
	//    pending suggestions involving the absorbed person are no longer
	//    actionable and should not become ghost rows in the review queue.
	if _, err := tx.ExecContext(ctx, `
		UPDATE memento_merge_suggestion
		SET status = 'accepted', resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE status = 'pending' AND person_a_id = ? AND person_b_id = ?
	`, pairA, pairB); err != nil {
		return result, fmt.Errorf("accept merge suggestion: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE memento_merge_suggestion
		SET status = 'rejected', resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE status = 'pending' AND (person_a_id = ? OR person_b_id = ?)
	`, fromID, fromID); err != nil {
		return result, fmt.Errorf("resolve stale merge suggestions: %w", err)
	}

	// 7. Touch the target person's updated_at so downstream consumers
	//    notice the change.
	if _, err := tx.ExecContext(ctx, `
		UPDATE memento_person SET updated_at = ? WHERE id = ?
	`, now, intoID); err != nil {
		return result, fmt.Errorf("touch into: %w", err)
	}

	// 8. Delete the source person. memento_people_candidates,
	//    memento_people_report, memento_social_edge, and
	//    memento_social_metric cascade. They'll be rebuilt on refresh.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM memento_person WHERE id = ?
	`, fromID); err != nil {
		return result, fmt.Errorf("delete from person: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

// assertPersonExists returns sql.ErrNoRows-equivalent if the person is
// missing or has been dismissed.
func assertPersonExists(ctx context.Context, tx *sql.Tx, id int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM memento_person WHERE id = ? AND dismissed_at IS NULL
	`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("person %d not found or dismissed", id)
	}
	return nil
}
