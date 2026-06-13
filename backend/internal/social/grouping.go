package social

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"time"
)

// Grouping: community detection over a strict, bot-free edge subset, persisted
// into memento_social_group(_member). This is distinct from computeClusters
// (raw connected components → memento_social_cluster), which stays for
// diagnostics. See docs/people-clusters-review-spec.md §5.2.
//
// memento_social_edge is already bot-free (the edge builder drops `excluded`
// persons), so grouping only has to strip the owner and apply the strict
// admission floor before running Louvain.

// Actionability thresholds. A group is actionable only if it is small enough to
// be a coherent "project", dense enough to be a real community, has at least a
// couple of named members, and is not just one hub plus satellites.
const (
	groupMinSize          = 3
	groupMaxSize          = 80
	groupMinNamedMembers  = 2
	groupDensitySmall     = 0.02 // size < 20
	groupDensityLarge     = 0.01 // 20 <= size <= 80
	groupSmallSizeCutoff  = 20
	groupHubDominanceFrac = 0.50 // no single member may exceed this share of total weighted degree
)

// Suppression reasons (empty string means actionable).
const (
	suppressTooLarge       = "too_large"
	suppressTooSparse      = "too_sparse"
	suppressNotEnoughNamed = "not_enough_named_members"
	suppressHubDominated   = "hub_dominated"
)

type groupRow struct {
	GroupID           int
	Size              int
	Density           float64
	IsActionable      bool
	SuppressionReason string
	MemberIDs         []int64 // all members
	TopMemberIDs      []int64 // up to 10, by weighted degree desc
}

// BuildGroups recomputes the grouping tables from the current social graph.
// Reads memento_social_edge / _metric / memento_people_report; writes
// memento_social_group(_member). Idempotent (DELETE + INSERT in one tx).
func BuildGroups(ctx context.Context, db *sql.DB) error {
	owners, err := loadOwnerPersonIDs(ctx, db)
	if err != nil {
		return fmt.Errorf("load owners: %w", err)
	}
	weightedDegree, err := loadWeightedDegrees(ctx, db)
	if err != nil {
		return fmt.Errorf("load weighted degrees: %w", err)
	}
	named, err := loadNamedPersonIDs(ctx, db)
	if err != nil {
		return fmt.Errorf("load named persons: %w", err)
	}

	// Strict edge admission, owner excluded.
	strict, err := loadStrictEdges(ctx, db, owners)
	if err != nil {
		return fmt.Errorf("load strict edges: %w", err)
	}

	communities := louvain(strict)

	// Bucket members by community.
	members := map[int][]int64{}
	for personID, comm := range communities {
		members[comm] = append(members[comm], personID)
	}

	// Edge sets for density: count strict edges fully inside each community.
	internalEdges := map[int]int{}
	for _, e := range strict {
		if communities[e.A] == communities[e.B] {
			internalEdges[communities[e.A]]++
		}
	}

	var groups []groupRow
	for comm, mem := range members {
		if len(mem) < groupMinSize {
			continue // singletons/pairs are not groups
		}
		sort.Slice(mem, func(i, j int) bool { return mem[i] < mem[j] })

		size := len(mem)
		maxEdges := size * (size - 1) / 2
		density := 0.0
		if maxEdges > 0 {
			density = float64(internalEdges[comm]) / float64(maxEdges)
		}

		// Top members + hub-dominance check by weighted degree.
		sorted := append([]int64(nil), mem...)
		sort.Slice(sorted, func(i, j int) bool {
			wi, wj := weightedDegree[sorted[i]], weightedDegree[sorted[j]]
			if wi != wj {
				return wi > wj
			}
			return sorted[i] < sorted[j]
		})
		topN := 10
		if len(sorted) < topN {
			topN = len(sorted)
		}
		var totalWD, maxWD float64
		for _, pid := range mem {
			wd := weightedDegree[pid]
			totalWD += wd
			if wd > maxWD {
				maxWD = wd
			}
		}
		namedCount := 0
		for _, pid := range mem {
			if named[pid] {
				namedCount++
			}
		}

		row := groupRow{
			GroupID:      comm,
			Size:         size,
			Density:      density,
			MemberIDs:    mem,
			TopMemberIDs: sorted[:topN],
		}
		row.SuppressionReason = groupSuppression(size, density, namedCount, maxWD, totalWD)
		row.IsActionable = row.SuppressionReason == ""
		groups = append(groups, row)
	}

	// Stable group IDs: renumber by size desc (largest = group 1), then id.
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Size != groups[j].Size {
			return groups[i].Size > groups[j].Size
		}
		return groups[i].GroupID < groups[j].GroupID
	})
	for i := range groups {
		groups[i].GroupID = i + 1
	}

	return persistGroups(ctx, db, groups)
}

// groupSuppression returns the first failing actionability reason, or "" if the
// group is actionable.
func groupSuppression(size int, density float64, namedCount int, maxWD, totalWD float64) string {
	if size > groupMaxSize {
		return suppressTooLarge
	}
	floor := groupDensitySmall
	if size >= groupSmallSizeCutoff {
		floor = groupDensityLarge
	}
	if density < floor {
		return suppressTooSparse
	}
	if namedCount < groupMinNamedMembers {
		return suppressNotEnoughNamed
	}
	if totalWD > 0 && maxWD > groupHubDominanceFrac*totalWD {
		return suppressHubDominated
	}
	return ""
}

// loadStrictEdges reads memento_social_edge and applies the grouping admission
// rule, dropping any edge that touches an owner.
func loadStrictEdges(ctx context.Context, db *sql.DB, owners map[int64]bool) ([]louvainEdge, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT person_a_id, person_b_id, direct_count, co_recipient_count, thread_count
		FROM memento_social_edge
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []louvainEdge
	for rows.Next() {
		var a, b int64
		var direct, coRecip, thread int
		if err := rows.Scan(&a, &b, &direct, &coRecip, &thread); err != nil {
			return nil, err
		}
		if owners[a] || owners[b] {
			continue
		}
		if direct >= 2 || (coRecip >= 3 && thread >= 3 && direct >= 1) {
			// Weight by direct contact; the raw `weight` column barely
			// discriminates (see spec §5.2). log1p(co) nudges co-recipient ties.
			w := float64(direct) + math.Log1p(float64(coRecip))
			out = append(out, louvainEdge{A: a, B: b, Weight: w})
		}
	}
	return out, rows.Err()
}

func loadWeightedDegrees(ctx context.Context, db *sql.DB) (map[int64]float64, error) {
	rows, err := db.QueryContext(ctx, `SELECT person_id, weighted_degree FROM memento_social_metric`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]float64{}
	for rows.Next() {
		var id int64
		var wd float64
		if err := rows.Scan(&id, &wd); err != nil {
			return nil, err
		}
		out[id] = wd
	}
	return out, rows.Err()
}

func loadNamedPersonIDs(ctx context.Context, db *sql.DB) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT person_id FROM memento_people_report WHERE COALESCE(canonical_name, '') <> ''
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// GroupMember is one member of a group, with display fields for the UI.
type GroupMember struct {
	PersonID       int64   `json:"person_id"`
	CanonicalName  string  `json:"canonical_name"`
	PrimaryEmail   string  `json:"primary_email"`
	Slug           string  `json:"slug"`
	WeightedDegree float64 `json:"weighted_degree"`
	Excluded       bool    `json:"excluded,omitempty"`
	AddedByUser    bool    `json:"added_by_user,omitempty"`
}

// GroupThread is a cached preview row for the "Top recent threads" section of
// the group card. Persisted to memento_social_group.top_threads_json.
type GroupThread struct {
	ThreadID      string `json:"thread_id"`
	MessageID     string `json:"message_id"`      // source_message_id (external/Gmail)
	InternalMsgID int64  `json:"internal_msg_id"` // internal DB messages.id for preview API
	Subject       string `json:"subject"`
	FromName      string `json:"from_name"`
	FromEmail     string `json:"from_email"`
	InternalTS    int64  `json:"internal_ts"` // unix seconds
}

// GroupDetail is one actionable (or suppressed) group with its top members,
// user-curated lifecycle state, and cached UI snapshots.
type GroupDetail struct {
	GroupID           int64         `json:"group_id"`
	Size              int           `json:"size"`
	Density           float64       `json:"density"`
	Label             string        `json:"label"`
	DisplayName       string        `json:"display_name"`
	Note              string        `json:"note"`
	IsActionable      bool          `json:"is_actionable"`
	SuppressionReason string        `json:"suppression_reason,omitempty"`
	SavedAt           string        `json:"saved_at,omitempty"`
	DismissedAt       string        `json:"dismissed_at,omitempty"`
	Members           []GroupMember `json:"members"`
	TopThreads        []GroupThread `json:"top_threads"`
	Cadence           []int         `json:"cadence"`
	// MessageCount is the all-time count of co-thread messages; LastActivityTS
	// is the unix-seconds timestamp of the most recent one. Distinct from
	// Cadence, which only covers the trailing 12 months.
	MessageCount   int   `json:"message_count"`
	LastActivityTS int64 `json:"last_activity_ts"`
}

// LoadGroupsOptions controls what subset LoadGroups returns.
type LoadGroupsOptions struct {
	// IncludeSuppressed returns Louvain-detected groups that failed the
	// actionability gate. Hidden by default.
	IncludeSuppressed bool
	// IncludeDismissed returns soft-deleted groups (dismissed_at IS NOT NULL).
	// Hidden by default; the "Excluded" tab passes true.
	IncludeDismissed bool
}

// LoadGroups returns the persisted groups with their members and cached
// snapshots. Ordering: saved groups first (most-recently-saved on top), then
// candidate groups (smallest first — small dense groups are the most useful).
// Dismissed groups are filtered out unless opts.IncludeDismissed is set.
func LoadGroups(ctx context.Context, db *sql.DB, opts LoadGroupsOptions) ([]GroupDetail, error) {
	q := `
		SELECT group_id, size, density, label,
		       COALESCE(display_name, ''), COALESCE(note, ''),
		       is_actionable, suppression_reason,
		       COALESCE(saved_at, ''), COALESCE(dismissed_at, ''),
		       COALESCE(top_threads_json, '[]'), COALESCE(cadence_json, '[]'),
		       message_count, last_activity_ts
		FROM memento_social_group
		WHERE 1=1
	`
	if !opts.IncludeSuppressed {
		// Saved groups are always shown even if no longer "actionable" — once
		// the user committed, the group is theirs.
		q += ` AND (is_actionable = 1 OR saved_at IS NOT NULL)`
	}
	if !opts.IncludeDismissed {
		q += ` AND dismissed_at IS NULL`
	}
	q += `
		ORDER BY (saved_at IS NOT NULL) DESC,
		         saved_at DESC,
		         size ASC,
		         group_id ASC
	`

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query groups: %w", err)
	}
	defer rows.Close()

	var groups []GroupDetail
	for rows.Next() {
		var g GroupDetail
		var actionable int
		var topJSON, cadenceJSON string
		if err := rows.Scan(
			&g.GroupID, &g.Size, &g.Density, &g.Label,
			&g.DisplayName, &g.Note,
			&actionable, &g.SuppressionReason,
			&g.SavedAt, &g.DismissedAt,
			&topJSON, &cadenceJSON,
			&g.MessageCount, &g.LastActivityTS,
		); err != nil {
			return nil, err
		}
		g.IsActionable = actionable != 0
		g.Members = []GroupMember{}
		g.TopThreads = []GroupThread{}
		g.Cadence = []int{}
		if topJSON != "" {
			_ = json.Unmarshal([]byte(topJSON), &g.TopThreads)
		}
		if cadenceJSON != "" {
			_ = json.Unmarshal([]byte(cadenceJSON), &g.Cadence)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	for i := range groups {
		members, err := loadGroupMembers(ctx, db, groups[i].GroupID, 10)
		if err != nil {
			return nil, fmt.Errorf("group %d members: %w", groups[i].GroupID, err)
		}
		groups[i].Members = members
		// Update size to reflect any user-excluded members (UI reads this).
		active := 0
		for _, m := range members {
			if !m.Excluded {
				active++
			}
		}
		if active > 0 {
			groups[i].Size = active
		}
	}
	return groups, nil
}

// LoadGroup returns a single group with ALL its members (ordered by weighted
// degree desc, excluded members at the end), or nil if the group does not
// exist. Used by handlers that need the complete member list.
func LoadGroup(ctx context.Context, db *sql.DB, groupID int64) (*GroupDetail, error) {
	var g GroupDetail
	var actionable int
	var topJSON, cadenceJSON string
	err := db.QueryRowContext(ctx, `
		SELECT group_id, size, density, label,
		       COALESCE(display_name, ''), COALESCE(note, ''),
		       is_actionable, suppression_reason,
		       COALESCE(saved_at, ''), COALESCE(dismissed_at, ''),
		       COALESCE(top_threads_json, '[]'), COALESCE(cadence_json, '[]'),
		       message_count, last_activity_ts
		FROM memento_social_group WHERE group_id = ?
	`, groupID).Scan(
		&g.GroupID, &g.Size, &g.Density, &g.Label,
		&g.DisplayName, &g.Note,
		&actionable, &g.SuppressionReason,
		&g.SavedAt, &g.DismissedAt,
		&topJSON, &cadenceJSON,
		&g.MessageCount, &g.LastActivityTS,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.IsActionable = actionable != 0
	g.Members = []GroupMember{}
	g.TopThreads = []GroupThread{}
	g.Cadence = []int{}
	if topJSON != "" {
		_ = json.Unmarshal([]byte(topJSON), &g.TopThreads)
	}
	if cadenceJSON != "" {
		_ = json.Unmarshal([]byte(cadenceJSON), &g.Cadence)
	}

	members, err := loadGroupMembers(ctx, db, groupID, 0)
	if err != nil {
		return nil, err
	}
	g.Members = members
	return &g, nil
}

// loadGroupMembers returns members ordered by (excluded last, then weighted
// degree desc). Pass limit=0 for all.
func loadGroupMembers(ctx context.Context, db *sql.DB, groupID int64, limit int) ([]GroupMember, error) {
	q := `
		SELECT gm.person_id,
		       COALESCE(NULLIF(pr.canonical_name, ''), mp.canonical_name, ''),
		       COALESCE(NULLIF(pr.primary_email, ''), mp.primary_email, ''),
		       COALESCE(pr.slug, ''),
		       COALESCE(sm.weighted_degree, 0),
		       gm.excluded_at IS NOT NULL,
		       gm.added_by_user
		FROM memento_social_group_member gm
		LEFT JOIN memento_people_report pr ON pr.person_id = gm.person_id
		LEFT JOIN memento_person mp ON mp.id = gm.person_id
		LEFT JOIN memento_social_metric sm ON sm.person_id = gm.person_id
		WHERE gm.group_id = ?
		ORDER BY (gm.excluded_at IS NOT NULL) ASC,
		         sm.weighted_degree DESC
	`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.QueryContext(ctx, q, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []GroupMember{}
	for rows.Next() {
		var m GroupMember
		var excluded bool
		var addedByUser int
		if err := rows.Scan(&m.PersonID, &m.CanonicalName, &m.PrimaryEmail, &m.Slug, &m.WeightedDegree, &excluded, &addedByUser); err != nil {
			return nil, err
		}
		m.Excluded = excluded
		m.AddedByUser = addedByUser != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func persistGroups(ctx context.Context, db *sql.DB, groups []groupRow) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Preserve user-curated state across refresh: saved groups (saved_at IS
	// NOT NULL) and dismissed groups (dismissed_at IS NOT NULL) are kept
	// verbatim — including their members, per-member excluded_at, display
	// name, and note. Only auto-detected candidates are wiped and replaced.
	// Candidate group_ids are deterministic hashes of membership, with collision
	// probing against preserved IDs, so card mutation targets survive refreshes.
	preservedRows, err := tx.QueryContext(ctx, `
		SELECT group_id FROM memento_social_group
		WHERE saved_at IS NOT NULL OR dismissed_at IS NOT NULL
	`)
	if err != nil {
		return fmt.Errorf("load preserved group ids: %w", err)
	}
	var preservedIDs []int64
	usedIDs := map[int]bool{}
	for preservedRows.Next() {
		var id int
		if err := preservedRows.Scan(&id); err != nil {
			preservedRows.Close()
			return err
		}
		usedIDs[id] = true
		preservedIDs = append(preservedIDs, int64(id))
	}
	preservedRows.Close()

	// Build a membership fingerprint for every preserved group so we can drop
	// any freshly-detected candidate that is the same community — otherwise the
	// user sees their saved group AND its re-detected twin. The fingerprint is
	// the sorted set of member person_ids; a candidate is a dup if its members
	// are a subset of (or equal to) a preserved group's members. Subset rather
	// than strict-equality so a saved group the user trimmed still absorbs the
	// (possibly larger) re-detected community.
	preservedFingerprints := make([]map[int64]bool, 0, len(preservedIDs))
	for _, pid := range preservedIDs {
		mrows, err := tx.QueryContext(ctx,
			`SELECT person_id FROM memento_social_group_member WHERE group_id = ?`, pid)
		if err != nil {
			return fmt.Errorf("load preserved members for %d: %w", pid, err)
		}
		set := map[int64]bool{}
		for mrows.Next() {
			var mid int64
			if err := mrows.Scan(&mid); err != nil {
				mrows.Close()
				return err
			}
			set[mid] = true
		}
		mrows.Close()
		if len(set) > 0 {
			preservedFingerprints = append(preservedFingerprints, set)
		}
	}

	// isDupOfPreserved reports whether a candidate's members substantially
	// overlap a preserved group. We treat ≥60% Jaccard-style overlap (by the
	// smaller set) as the same community to absorb minor membership drift
	// between refreshes.
	isDupOfPreserved := func(memberIDs []int64) bool {
		if len(memberIDs) == 0 {
			return false
		}
		for _, set := range preservedFingerprints {
			overlap := 0
			for _, mid := range memberIDs {
				if set[mid] {
					overlap++
				}
			}
			smaller := len(memberIDs)
			if len(set) < smaller {
				smaller = len(set)
			}
			if smaller > 0 && float64(overlap)/float64(smaller) >= 0.6 {
				return true
			}
		}
		return false
	}

	// FK CASCADE is not enabled on this connection, so drop members explicitly.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM memento_social_group_member
		WHERE group_id IN (
			SELECT group_id FROM memento_social_group
			WHERE saved_at IS NULL AND dismissed_at IS NULL
		)
	`); err != nil {
		return fmt.Errorf("clear candidate members: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM memento_social_group
		WHERE saved_at IS NULL AND dismissed_at IS NULL
	`); err != nil {
		return fmt.Errorf("clear candidate groups: %w", err)
	}

	groupStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_social_group
			(group_id, size, density, label, label_source, is_actionable, suppression_reason, top_member_ids_json, computed_at)
		VALUES (?, ?, ?, '', 'none', ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer groupStmt.Close()
	memberStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_social_group_member (group_id, person_id) VALUES (?, ?)
	`)
	if err != nil {
		return err
	}
	defer memberStmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, g := range groups {
		// Skip candidates that duplicate a saved/dismissed group the user has
		// already triaged.
		if isDupOfPreserved(g.MemberIDs) {
			continue
		}
		// Candidate IDs are deterministic by membership so card mutation targets
		// survive refreshes. Probe on collision with preserved or sibling groups.
		g.GroupID = stableGroupID(g.MemberIDs, usedIDs)
		usedIDs[g.GroupID] = true
		topJSON, _ := json.Marshal(g.TopMemberIDs)
		actionable := 0
		if g.IsActionable {
			actionable = 1
		}
		if _, err := groupStmt.ExecContext(ctx,
			g.GroupID, g.Size, g.Density, actionable, g.SuppressionReason, string(topJSON), now,
		); err != nil {
			return fmt.Errorf("insert group %d: %w", g.GroupID, err)
		}
		for _, pid := range g.MemberIDs {
			if _, err := memberStmt.ExecContext(ctx, g.GroupID, pid); err != nil {
				return fmt.Errorf("insert group %d member %d: %w", g.GroupID, pid, err)
			}
		}
	}
	return tx.Commit()
}

func stableGroupID(memberIDs []int64, used map[int]bool) int {
	h := fnv.New32a()
	sorted := append([]int64(nil), memberIDs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, id := range sorted {
		_, _ = h.Write([]byte(strconv.FormatInt(id, 10)))
		_, _ = h.Write([]byte{0})
	}
	candidate := int(h.Sum32() & 0x7fffffff)
	if candidate == 0 {
		candidate = 1
	}
	for used[candidate] {
		candidate++
		if candidate <= 0 {
			candidate = 1
		}
	}
	return candidate
}
