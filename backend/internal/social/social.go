// Package social builds and queries the social communication graph derived
// from msgvault email history. Tables: memento_social_edge, memento_social_metric,
// memento_social_cluster.
package social

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"memento/backend/internal/people"
)

// Neighbor is one node adjacent to a queried person, with edge statistics.
type Neighbor struct {
	PersonID         int64   `json:"person_id"`
	CanonicalName    string  `json:"canonical_name"`
	Slug             string  `json:"slug"`
	PrimaryEmail     string  `json:"primary_email"`
	DirectCount      int     `json:"direct_count"`
	CoRecipientCount int     `json:"co_recipient_count"`
	ThreadCount      int     `json:"thread_count"`
	ToCount          int     `json:"to_count"`
	CcCount          int     `json:"cc_count"`
	BccCount         int     `json:"bcc_count"`
	Weight           float64 `json:"weight"`
	LastTs           string  `json:"last_ts,omitempty"`
}

// PersonNetwork is the full social-graph result for a single person.
type PersonNetwork struct {
	PersonID       int64      `json:"person_id"`
	StructuralRole string     `json:"structural_role"`
	Degree         int        `json:"degree"`
	WeightedDegree float64    `json:"weighted_degree"`
	DormancyDays   *int64     `json:"dormancy_days,omitempty"`
	ClusterID      *int64     `json:"cluster_id,omitempty"`
	ClusterSize    *int       `json:"cluster_size,omitempty"`
	ClusterLabel   string     `json:"cluster_label"`
	Neighbors      []Neighbor `json:"neighbors"`
}

// ClusterMember is a person within a cluster.
type ClusterMember struct {
	PersonID       int64   `json:"person_id"`
	CanonicalName  string  `json:"canonical_name"`
	Slug           string  `json:"slug"`
	WeightedDegree float64 `json:"weighted_degree"`
}

// ClusterDetail is the full cluster result including member list.
type ClusterDetail struct {
	ClusterID int64           `json:"cluster_id"`
	Size      int             `json:"size"`
	Density   float64         `json:"density"`
	Label     string          `json:"label"`
	Members   []ClusterMember `json:"members"`
}

// MissingCollaborator is a person strongly connected to a set of input persons
// but absent from that set.
type MissingCollaborator struct {
	PersonID        int64   `json:"person_id"`
	CanonicalName   string  `json:"canonical_name"`
	Slug            string  `json:"slug"`
	CombinedWeight  float64 `json:"combined_weight"`
	ConnectionCount int     `json:"connection_count"`
	ConnectsTo      []int64 `json:"connects_to"`
}

// BuildResult contains row counts from a BuildSocialGraph call.
type BuildResult struct {
	EdgeCount    int
	MetricCount  int
	ClusterCount int
}

// BuildSocialGraph builds the social communication graph and writes it to the
// three social tables inside a single transaction. Must be called after
// RefreshPeopleReport so person IDs and dismissed status are current.
func BuildSocialGraph(ctx context.Context, db *sql.DB) (BuildResult, error) {
	edges, err := buildEdges(ctx, db)
	if err != nil {
		return BuildResult{}, fmt.Errorf("build edges: %w", err)
	}

	owners, err := loadOwnerPersonIDs(ctx, db)
	if err != nil {
		return BuildResult{}, fmt.Errorf("load owner person ids: %w", err)
	}

	metrics := computeMetrics(edges)
	clusters := computeClusters(edges, metrics, owners)

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := persistGraph(ctx, db, edges, metrics, clusters, now)
	if err != nil {
		return BuildResult{}, fmt.Errorf("persist: %w", err)
	}

	// Community-detected groups (strict edges + Louvain) over the freshly
	// persisted graph. Distinct from the raw clusters above. Saved/dismissed
	// groups are preserved verbatim across this rebuild — see persistGroups.
	if err := BuildGroups(ctx, db); err != nil {
		return BuildResult{}, fmt.Errorf("build groups: %w", err)
	}
	// Populate top-thread + cadence snapshots for every (re)built group so
	// the card render is O(1) at request time.
	if err := RefreshAllGroupSnapshots(ctx, db); err != nil {
		return BuildResult{}, fmt.Errorf("refresh group snapshots: %w", err)
	}
	return result, nil
}

// loadOwnerPersonIDs finds the resolved person IDs of the vault owner.
func loadOwnerPersonIDs(ctx context.Context, db *sql.DB) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT pe.person_id
		FROM sources s
		JOIN memento_person_email pe ON pe.email_address = lower(s.identifier)
		WHERE s.identifier LIKE '%@%'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		owners[id] = true
	}
	return owners, rows.Err()
}

// persistGraph writes all three social tables and updates memento_report_meta
// inside a single transaction (DELETE then INSERT — idempotent).
func persistGraph(
	ctx context.Context,
	db *sql.DB,
	edges []edgeRow,
	metrics []metricRow,
	clusters []clusterRow,
	now string,
) (BuildResult, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return BuildResult{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_social_edge`); err != nil {
		return BuildResult{}, fmt.Errorf("delete edges: %w", err)
	}
	edgeStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_social_edge (
			person_a_id, person_b_id, direct_count, to_count, cc_count, bcc_count,
			co_recipient_count, a_to_b_count, b_to_a_count, thread_count,
			first_ts, last_ts, weight
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return BuildResult{}, err
	}
	defer edgeStmt.Close()
	for _, e := range edges {
		var firstTs, lastTs any
		if e.FirstTs != "" {
			firstTs = e.FirstTs
		}
		if e.LastTs != "" {
			lastTs = e.LastTs
		}
		if _, err := edgeStmt.ExecContext(ctx,
			e.PersonAID, e.PersonBID, e.DirectCount, e.ToCount, e.CcCount, e.BccCount,
			e.CoRecipientCount, e.AToB, e.BToA, e.ThreadCount,
			firstTs, lastTs, e.Weight,
		); err != nil {
			return BuildResult{}, fmt.Errorf("insert edge (%d,%d): %w", e.PersonAID, e.PersonBID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_social_cluster`); err != nil {
		return BuildResult{}, fmt.Errorf("delete clusters: %w", err)
	}
	clusterStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_social_cluster (cluster_id, size, density, top_member_ids_json, label, label_source, computed_at)
		VALUES (?, ?, ?, ?, '', 'none', ?)
	`)
	if err != nil {
		return BuildResult{}, err
	}
	defer clusterStmt.Close()
	for _, c := range clusters {
		topJSON, _ := json.Marshal(c.TopMemberIDs)
		if _, err := clusterStmt.ExecContext(ctx, c.ClusterID, c.Size, c.Density, string(topJSON), now); err != nil {
			return BuildResult{}, fmt.Errorf("insert cluster %d: %w", c.ClusterID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_social_metric`); err != nil {
		return BuildResult{}, fmt.Errorf("delete metrics: %w", err)
	}
	metricStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_social_metric (
			person_id, degree, weighted_degree, direct_degree, co_recipient_degree,
			cluster_id, dormancy_days, structural_role, computed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return BuildResult{}, err
	}
	defer metricStmt.Close()
	for _, m := range metrics {
		var clusterID, dormancyDays any
		if m.ClusterID != nil {
			clusterID = *m.ClusterID
		}
		if m.DormancyDays != nil {
			dormancyDays = *m.DormancyDays
		}
		if _, err := metricStmt.ExecContext(ctx,
			m.PersonID, m.Degree, m.WeightedDegree, m.DirectDegree, m.CoRecipientDegree,
			clusterID, dormancyDays, m.StructuralRole, now,
		); err != nil {
			return BuildResult{}, fmt.Errorf("insert metric for person %d: %w", m.PersonID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memento_report_meta (dimension, generated_at, row_count)
		VALUES ('social', ?, ?)
		ON CONFLICT(dimension) DO UPDATE SET
			generated_at = excluded.generated_at,
			row_count = excluded.row_count
	`, now, len(edges)); err != nil {
		return BuildResult{}, fmt.Errorf("upsert meta: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		EdgeCount:    len(edges),
		MetricCount:  len(metrics),
		ClusterCount: len(clusters),
	}, nil
}

// LoadPersonNetwork returns structural role, cluster summary, and top N neighbors
// for a person. Reused by both the public API handler and the internal agent tool.
func LoadPersonNetwork(ctx context.Context, db *sql.DB, personID int64, limit int) (*PersonNetwork, error) {
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	ownerIDs, err := loadOwnerPersonIDs(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load owner ids: %w", err)
	}

	pn := &PersonNetwork{
		PersonID:     personID,
		ClusterLabel: "",
		Neighbors:    []Neighbor{},
	}

	var clusterID sql.NullInt64
	var dormancyDays sql.NullInt64
	err = db.QueryRowContext(ctx, `
		SELECT degree, weighted_degree, structural_role, cluster_id, dormancy_days
		FROM memento_social_metric WHERE person_id = ?
	`, personID).Scan(&pn.Degree, &pn.WeightedDegree, &pn.StructuralRole, &clusterID, &dormancyDays)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load metric: %w", err)
	}
	if clusterID.Valid {
		pn.ClusterID = &clusterID.Int64
	}
	if dormancyDays.Valid {
		pn.DormancyDays = &dormancyDays.Int64
	}

	if pn.ClusterID != nil {
		var size int
		var label string
		if err := db.QueryRowContext(ctx,
			`SELECT size, label FROM memento_social_cluster WHERE cluster_id = ?`, *pn.ClusterID,
		).Scan(&size, &label); err == nil {
			pn.ClusterSize = &size
			pn.ClusterLabel = label
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT
		  CASE WHEN person_a_id = ? THEN person_b_id ELSE person_a_id END AS neighbor_id,
		  direct_count, co_recipient_count, thread_count, weight,
		  to_count, cc_count, bcc_count, COALESCE(last_ts, '')
		FROM memento_social_edge
		WHERE person_a_id = ? OR person_b_id = ?
		ORDER BY weight DESC, last_ts DESC
		LIMIT ?
	`, personID, personID, personID, limit+len(ownerIDs)+5)
	if err != nil {
		return nil, fmt.Errorf("load neighbors: %w", err)
	}

	type rawNeighbor struct {
		neighborID int64
		n          Neighbor
	}
	var rawNeighbors []rawNeighbor
	var neighborIDs []int64
	for rows.Next() {
		var r rawNeighbor
		if err := rows.Scan(
			&r.neighborID, &r.n.DirectCount, &r.n.CoRecipientCount, &r.n.ThreadCount, &r.n.Weight,
			&r.n.ToCount, &r.n.CcCount, &r.n.BccCount, &r.n.LastTs,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan neighbor: %w", err)
		}
		if ownerIDs[r.neighborID] {
			continue
		}
		r.n.PersonID = r.neighborID
		rawNeighbors = append(rawNeighbors, r)
		neighborIDs = append(neighborIDs, r.neighborID)
		if len(rawNeighbors) >= limit {
			break
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	nameByID := loadNames(ctx, db, neighborIDs)
	pn.Neighbors = make([]Neighbor, 0, len(rawNeighbors))
	for _, r := range rawNeighbors {
		n := r.n
		if info, ok := nameByID[r.neighborID]; ok {
			n.CanonicalName = info[0]
			n.Slug = info[1]
			n.PrimaryEmail = info[2]
		}
		pn.Neighbors = append(pn.Neighbors, n)
	}
	return pn, nil
}

// LoadCluster returns the cluster detail for a given cluster_id, including member list.
func LoadCluster(ctx context.Context, db *sql.DB, clusterID int64) (*ClusterDetail, error) {
	cd := &ClusterDetail{}
	var topJSON string
	err := db.QueryRowContext(ctx, `
		SELECT cluster_id, size, density, label, top_member_ids_json
		FROM memento_social_cluster WHERE cluster_id = ?
	`, clusterID).Scan(&cd.ClusterID, &cd.Size, &cd.Density, &cd.Label, &topJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load cluster: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT sm.person_id, COALESCE(pr.canonical_name, ''), COALESCE(pr.slug, ''), sm.weighted_degree
		FROM memento_social_metric sm
		LEFT JOIN memento_people_report pr ON pr.person_id = sm.person_id
		WHERE sm.cluster_id = ?
		ORDER BY sm.weighted_degree DESC
		LIMIT 50
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("load cluster members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m ClusterMember
		if err := rows.Scan(&m.PersonID, &m.CanonicalName, &m.Slug, &m.WeightedDegree); err != nil {
			return nil, err
		}
		cd.Members = append(cd.Members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if cd.Members == nil {
		cd.Members = []ClusterMember{}
	}
	return cd, nil
}

// FindBridgesBetween returns shared neighbors between person A and person B,
// ordered by combined edge weight.
func FindBridgesBetween(ctx context.Context, db *sql.DB, personAID, personBID int64, limit int) ([]Neighbor, error) {
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	ownerIDs, err := loadOwnerPersonIDs(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load owner ids: %w", err)
	}

	aNeighbors, err := getNeighborWeights(ctx, db, personAID, 50)
	if err != nil {
		return nil, err
	}
	bNeighbors, err := getNeighborWeights(ctx, db, personBID, 50)
	if err != nil {
		return nil, err
	}

	type bridge struct {
		neighborID     int64
		combinedWeight float64
	}
	var bridges []bridge
	for nid, wa := range aNeighbors {
		if nid == personAID || nid == personBID || ownerIDs[nid] {
			continue
		}
		if wb, ok := bNeighbors[nid]; ok {
			bridges = append(bridges, bridge{nid, wa + wb})
		}
	}
	sort.Slice(bridges, func(i, j int) bool {
		return bridges[i].combinedWeight > bridges[j].combinedWeight
	})
	if len(bridges) > limit {
		bridges = bridges[:limit]
	}

	ids := make([]int64, len(bridges))
	for i, b := range bridges {
		ids[i] = b.neighborID
	}
	nameByID := loadNames(ctx, db, ids)

	out := make([]Neighbor, 0, len(bridges))
	for _, b := range bridges {
		n := Neighbor{PersonID: b.neighborID, Weight: b.combinedWeight}
		if info, ok := nameByID[b.neighborID]; ok {
			n.CanonicalName = info[0]
			n.Slug = info[1]
			n.PrimaryEmail = info[2]
		}
		out = append(out, n)
	}
	return out, nil
}

// FindMissingCollaborators returns persons strongly connected to the input set
// but absent from it. connectionCount >= 2 and combinedWeight >= minWeight.
func FindMissingCollaborators(ctx context.Context, db *sql.DB, personIDs []int64, limit int, minWeight float64) ([]MissingCollaborator, error) {
	if limit <= 0 || limit > 25 {
		limit = 8
	}
	if minWeight <= 0 {
		minWeight = 10.0
	}

	inputSet := make(map[int64]bool, len(personIDs))
	for _, id := range personIDs {
		inputSet[id] = true
	}
	ownerIDs, err := loadOwnerPersonIDs(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load owner ids: %w", err)
	}

	type accumEntry struct {
		combinedWeight  float64
		connectionCount int
		connectsTo      []int64
	}
	accum := map[int64]*accumEntry{}

	for _, pid := range personIDs {
		neighbors, err := getNeighborWeights(ctx, db, pid, 25)
		if err != nil {
			return nil, err
		}
		for nid, w := range neighbors {
			if inputSet[nid] || ownerIDs[nid] {
				continue
			}
			e := accum[nid]
			if e == nil {
				e = &accumEntry{}
				accum[nid] = e
			}
			e.combinedWeight += w
			e.connectionCount++
			e.connectsTo = append(e.connectsTo, pid)
		}
	}

	type candidate struct {
		personID        int64
		combinedWeight  float64
		connectionCount int
		connectsTo      []int64
	}
	var candidates []candidate
	for nid, e := range accum {
		if e.connectionCount < 2 || e.combinedWeight < minWeight {
			continue
		}
		candidates = append(candidates, candidate{nid, e.combinedWeight, e.connectionCount, e.connectsTo})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].connectionCount != candidates[j].connectionCount {
			return candidates[i].connectionCount > candidates[j].connectionCount
		}
		return candidates[i].combinedWeight > candidates[j].combinedWeight
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	ids := make([]int64, len(candidates))
	for i, c := range candidates {
		ids[i] = c.personID
	}
	nameByID := loadNames(ctx, db, ids)

	out := make([]MissingCollaborator, 0, len(candidates))
	for _, c := range candidates {
		mc := MissingCollaborator{
			PersonID:        c.personID,
			CombinedWeight:  c.combinedWeight,
			ConnectionCount: c.connectionCount,
			ConnectsTo:      c.connectsTo,
		}
		if info, ok := nameByID[c.personID]; ok {
			mc.CanonicalName = info[0]
			mc.Slug = info[1]
		}
		out = append(out, mc)
	}
	return out, nil
}

// getNeighborWeights returns neighbor_id -> weight for the top-N edges of a person.
func getNeighborWeights(ctx context.Context, db *sql.DB, personID int64, limit int) (map[int64]float64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
		  CASE WHEN person_a_id = ? THEN person_b_id ELSE person_a_id END AS neighbor_id,
		  weight
		FROM memento_social_edge
		WHERE person_a_id = ? OR person_b_id = ?
		ORDER BY weight DESC
		LIMIT ?
	`, personID, personID, personID, limit)
	if err != nil {
		return nil, fmt.Errorf("get neighbor weights for %d: %w", personID, err)
	}
	defer rows.Close()
	result := map[int64]float64{}
	for rows.Next() {
		var nid int64
		var w float64
		if err := rows.Scan(&nid, &w); err != nil {
			return nil, err
		}
		result[nid] = w
	}
	return result, rows.Err()
}

// loadNames batch-loads canonical_name, slug, and primary_email from memento_people_report.
func loadNames(ctx context.Context, db *sql.DB, ids []int64) map[int64][3]string {
	result := map[int64][3]string{}
	if len(ids) == 0 {
		return result
	}
	ph := makePlaceholders(len(ids))
	args := int64Args(ids)
	rows, err := db.QueryContext(ctx,
		fmt.Sprintf(`SELECT person_id, canonical_name, slug, primary_email FROM memento_people_report WHERE person_id IN (%s)`, ph),
		args...,
	)
	if err != nil {
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, slug, email string
		if err := rows.Scan(&id, &name, &slug, &email); err != nil {
			continue
		}
		result[id] = [3]string{name, slug, email}
	}
	return result
}

func makePlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// ClusterThread represents a thread associated with a cluster.
type ClusterThread struct {
	ThreadID     int64  `json:"thread_id"`
	Subject      string `json:"subject"`
	MessageCount int    `json:"message_count"`
}

// ClusterLabelContext contains member names and active thread subjects for LLM labeling.
type ClusterLabelContext struct {
	ClusterID      int64           `json:"cluster_id"`
	MemberNames    []string        `json:"member_names"`
	ThreadSubjects []string        `json:"thread_subjects"`
	Threads        []ClusterThread `json:"threads,omitempty"`
}

// LoadClusterLabelingContext returns the member names, top thread subjects, and thread details for a cluster.
func LoadClusterLabelingContext(ctx context.Context, db *sql.DB, clusterID int64) (*ClusterLabelContext, error) {
	// 1. Get member names from metrics & report
	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(pr.canonical_name, '')
		FROM memento_social_metric sm
		JOIN memento_people_report pr ON pr.person_id = sm.person_id
		WHERE sm.cluster_id = ?
		ORDER BY sm.weighted_degree DESC
		LIMIT 15
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster member names: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil && name != "" {
			names = append(names, name)
		}
	}
	rows.Close()

	// 2. Get top thread subjects they participate in
	subjectsRows, err := db.QueryContext(ctx, `
		WITH cluster_members AS (
			SELECT person_id FROM memento_social_metric WHERE cluster_id = ?
		),
		member_emails AS (
			SELECT lower(email_address) AS email FROM memento_person_email
			WHERE person_id IN (SELECT person_id FROM cluster_members)
		),
		member_participants AS (
			SELECT id FROM participants WHERE lower(email_address) IN (SELECT email FROM member_emails)
		),
		member_threads AS (
			SELECT DISTINCT m.conversation_id
			FROM messages m
			WHERE m.sender_id IN (SELECT id FROM member_participants)
			  AND m.conversation_id IS NOT NULL AND m.conversation_id != 0
			LIMIT 30
		)
		SELECT DISTINCT COALESCE(m.subject, '')
		FROM messages m
		WHERE m.conversation_id IN (SELECT conversation_id FROM member_threads)
		  AND m.subject IS NOT NULL AND m.subject != ''
		LIMIT 15
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster thread subjects: %w", err)
	}
	defer subjectsRows.Close()
	var subjects []string
	for subjectsRows.Next() {
		var subject string
		if err := subjectsRows.Scan(&subject); err == nil && subject != "" {
			subjects = append(subjects, subject)
		}
	}
	subjectsRows.Close()

	// 3. Get thread details (ID, subject, message count) for label context.
	threadsRows, err := db.QueryContext(ctx, `
		WITH cluster_members AS (
			SELECT person_id FROM memento_social_metric WHERE cluster_id = ?
		),
		member_emails AS (
			SELECT lower(email_address) AS email FROM memento_person_email
			WHERE person_id IN (SELECT person_id FROM cluster_members)
		),
		member_participants AS (
			SELECT id FROM participants WHERE lower(email_address) IN (SELECT email FROM member_emails)
		),
		member_threads AS (
			SELECT DISTINCT m.conversation_id
			FROM messages m
			WHERE m.sender_id IN (SELECT id FROM member_participants)
			  AND m.conversation_id IS NOT NULL AND m.conversation_id != 0
			LIMIT 30
		)
		SELECT conversation_id, COALESCE(subject, ''), COUNT(*)
		FROM messages
		WHERE conversation_id IN (SELECT conversation_id FROM member_threads)
		GROUP BY conversation_id
		LIMIT 15
	`, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query cluster threads: %w", err)
	}
	defer threadsRows.Close()
	var threads []ClusterThread
	for threadsRows.Next() {
		var t ClusterThread
		if err := threadsRows.Scan(&t.ThreadID, &t.Subject, &t.MessageCount); err == nil {
			threads = append(threads, t)
		}
	}
	threadsRows.Close()

	return &ClusterLabelContext{
		ClusterID:      clusterID,
		MemberNames:    names,
		ThreadSubjects: subjects,
		Threads:        threads,
	}, nil
}

// LoadClusters loads all clusters with size, density, label, and top member names/slugs.
func LoadClusters(ctx context.Context, db *sql.DB) ([]ClusterDetail, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT cluster_id, size, density, label
		FROM memento_social_cluster
		ORDER BY size DESC, density DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query clusters: %w", err)
	}
	defer rows.Close()

	var clusters []ClusterDetail
	for rows.Next() {
		var cd ClusterDetail
		if err := rows.Scan(&cd.ClusterID, &cd.Size, &cd.Density, &cd.Label); err != nil {
			return nil, err
		}
		cd.Members = []ClusterMember{} // Initialized to empty slice, will load top members
		clusters = append(clusters, cd)
	}
	rows.Close()

	// For each cluster, load top 10 members
	for i := range clusters {
		c := &clusters[i]
		memberRows, err := db.QueryContext(ctx, `
			SELECT sm.person_id, COALESCE(pr.canonical_name, ''), COALESCE(pr.slug, ''), sm.weighted_degree
			FROM memento_social_metric sm
			LEFT JOIN memento_people_report pr ON pr.person_id = sm.person_id
			WHERE sm.cluster_id = ?
			ORDER BY sm.weighted_degree DESC
			LIMIT 10
		`, c.ClusterID)
		if err != nil {
			return nil, fmt.Errorf("query cluster %d members: %w", c.ClusterID, err)
		}
		for memberRows.Next() {
			var m ClusterMember
			if err := memberRows.Scan(&m.PersonID, &m.CanonicalName, &m.Slug, &m.WeightedDegree); err == nil {
				c.Members = append(c.Members, m)
			}
		}
		memberRows.Close()
	}

	return clusters, nil
}

// Leaderboard contains the three pre-sorted groups of metric-rich people.
type Leaderboard struct {
	Collaborators []people.PagePerson `json:"collaborators"`
	Dormant       []people.PagePerson `json:"dormant"`
	Bridges       []people.PagePerson `json:"bridges"`
}

// LoadLeaderboard returns the three leaderboards of people.
func LoadLeaderboard(ctx context.Context, db *sql.DB) (*Leaderboard, error) {
	lb := &Leaderboard{
		Collaborators: []people.PagePerson{},
		Dormant:       []people.PagePerson{},
		Bridges:       []people.PagePerson{},
	}

	// 1. Top Collaborators (highest weighted_degree DESC) limit 10
	rows, err := db.QueryContext(ctx, `
		SELECT pr.person_id, pr.canonical_name, pr.primary_email, pr.domain, pr.email_count,
		       pr.total_messages, pr.from_contact_count, pr.to_contact_count,
		       pr.bidirectional_score, pr.classification,
		       COALESCE(pr.first_message_at, ''), COALESCE(pr.last_message_at, ''),
		       pr.slug, pr.aliases_json, pr.timeline_json, pr.top_correspondents_json,
		       COALESCE(sm.structural_role, ''), sm.cluster_id,
		       COALESCE(sc.label, ''), sm.dormancy_days, sm.weighted_degree
		FROM memento_people_report pr
		JOIN memento_social_metric sm ON sm.person_id = pr.person_id
		LEFT JOIN memento_social_cluster sc ON sc.cluster_id = sm.cluster_id
		ORDER BY sm.weighted_degree DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, fmt.Errorf("load collaborators leaderboard: %w", err)
	}
	lb.Collaborators, err = scanPeopleRows(rows)
	if err != nil {
		return nil, err
	}

	// 2. Key Dormant Contacts (weighted degree > median, dormancy_days > 90, sorted by dormancy DESC) limit 10
	// Compute median weighted_degree first
	var median float64
	err = db.QueryRowContext(ctx, `
		WITH sorted AS (
			SELECT weighted_degree FROM memento_social_metric
			ORDER BY weighted_degree
		),
		counts AS (
			SELECT COUNT(*) AS total FROM memento_social_metric
		)
		SELECT AVG(weighted_degree)
		FROM (
			SELECT weighted_degree
			FROM sorted, counts
			LIMIT 2 - (counts.total % 2)
			OFFSET (counts.total - 1) / 2
		)
	`).Scan(&median)
	if err != nil {
		// Fallback to 0 if median fails
		median = 0.0
	}

	dormantRows, err := db.QueryContext(ctx, `
		SELECT pr.person_id, pr.canonical_name, pr.primary_email, pr.domain, pr.email_count,
		       pr.total_messages, pr.from_contact_count, pr.to_contact_count,
		       pr.bidirectional_score, pr.classification,
		       COALESCE(pr.first_message_at, ''), COALESCE(pr.last_message_at, ''),
		       pr.slug, pr.aliases_json, pr.timeline_json, pr.top_correspondents_json,
		       COALESCE(sm.structural_role, ''), sm.cluster_id,
		       COALESCE(sc.label, ''), sm.dormancy_days, sm.weighted_degree
		FROM memento_people_report pr
		JOIN memento_social_metric sm ON sm.person_id = pr.person_id
		LEFT JOIN memento_social_cluster sc ON sc.cluster_id = sm.cluster_id
		WHERE sm.dormancy_days > 90 AND sm.weighted_degree > ?
		ORDER BY sm.dormancy_days DESC
		LIMIT 10
	`, median)
	if err != nil {
		return nil, fmt.Errorf("load dormant leaderboard: %w", err)
	}
	lb.Dormant, err = scanPeopleRows(dormantRows)
	if err != nil {
		return nil, err
	}

	// 3. Structural Bridges (structural_role = 'bridge', sorted by co_recipient_degree DESC) limit 10
	bridgeRows, err := db.QueryContext(ctx, `
		SELECT pr.person_id, pr.canonical_name, pr.primary_email, pr.domain, pr.email_count,
		       pr.total_messages, pr.from_contact_count, pr.to_contact_count,
		       pr.bidirectional_score, pr.classification,
		       COALESCE(pr.first_message_at, ''), COALESCE(pr.last_message_at, ''),
		       pr.slug, pr.aliases_json, pr.timeline_json, pr.top_correspondents_json,
		       COALESCE(sm.structural_role, ''), sm.cluster_id,
		       COALESCE(sc.label, ''), sm.dormancy_days, sm.weighted_degree
		FROM memento_people_report pr
		JOIN memento_social_metric sm ON sm.person_id = pr.person_id
		LEFT JOIN memento_social_cluster sc ON sc.cluster_id = sm.cluster_id
		WHERE sm.structural_role = 'bridge'
		ORDER BY sm.co_recipient_degree DESC, sm.weighted_degree DESC
		LIMIT 10
	`)
	if err != nil {
		return nil, fmt.Errorf("load bridges leaderboard: %w", err)
	}
	lb.Bridges, err = scanPeopleRows(bridgeRows)
	if err != nil {
		return nil, err
	}

	return lb, nil
}

// scanPeopleRows is a helper to scan SQL rows into people.PagePerson slice.
func scanPeopleRows(rows *sql.Rows) ([]people.PagePerson, error) {
	defer rows.Close()
	var list []people.PagePerson
	for rows.Next() {
		var p people.PagePerson
		var first, last, slug, aliasesJSON, timelineJSON, correspondentsJSON string
		var clusterID, dormancyDays sql.NullInt64
		if err := rows.Scan(
			&p.PersonID, &p.CanonicalName, &p.PrimaryEmail, &p.Domain, &p.EmailCount,
			&p.TotalMessages, &p.FromContactCount, &p.ToContactCount,
			&p.BidirectionalScore, &p.Classification, &first, &last,
			&slug, &aliasesJSON, &timelineJSON, &correspondentsJSON,
			&p.StructuralRole, &clusterID, &p.ClusterLabel, &dormancyDays, &p.WeightedDegree,
		); err != nil {
			return nil, err
		}
		p.FirstMessageAt = first
		p.LastMessageAt = last
		if clusterID.Valid {
			p.ClusterID = &clusterID.Int64
		}
		if dormancyDays.Valid {
			p.DormancyDays = &dormancyDays.Int64
		}
		_ = json.Unmarshal([]byte(aliasesJSON), &p.Aliases)
		_ = json.Unmarshal([]byte(timelineJSON), &p.Timeline)
		_ = json.Unmarshal([]byte(correspondentsJSON), &p.TopCorrespondents)
		list = append(list, p)
	}
	return list, rows.Err()
}
