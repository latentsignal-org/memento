package social

import "sort"

// clusterRow is one connected component ready for insertion.
type clusterRow struct {
	ClusterID    int64
	Size         int
	Density      float64
	TopMemberIDs []int64
}

// computeClusters assigns connected-component IDs using BFS, updates metricRow
// ClusterID fields in place, and returns the cluster rows.
// Components of size 1 are skipped (ClusterID stays nil on the metric).
// Excludes owner person IDs from the adjacency graph traversal to prevent
// the owner node from merging all sub-components into a single giant cluster.
func computeClusters(edges []edgeRow, metrics []metricRow, ownerIDs map[int64]bool) []clusterRow {
	// Build adjacency map.
	adj := map[int64][]int64{}
	for _, e := range edges {
		// Ignore edges to/from owner when building adjacency for clustering.
		if ownerIDs[e.PersonAID] || ownerIDs[e.PersonBID] {
			continue
		}
		adj[e.PersonAID] = append(adj[e.PersonAID], e.PersonBID)
		adj[e.PersonBID] = append(adj[e.PersonBID], e.PersonAID)
	}

	// Build edge set for density computation.
	type ekey struct{ a, b int64 }
	edgeSet := make(map[ekey]bool, len(edges))
	for _, e := range edges {
		edgeSet[ekey{e.PersonAID, e.PersonBID}] = true
	}

	// Build weighted-degree lookup from metrics.
	wdByPerson := make(map[int64]float64, len(metrics))
	for _, m := range metrics {
		wdByPerson[m.PersonID] = m.WeightedDegree
	}

	visited := map[int64]bool{}
	personToCluster := map[int64]int64{}
	var clusterID int64 = 1

	// Collect all person IDs that have at least one edge.
	persons := make([]int64, 0, len(adj))
	for pid := range adj {
		persons = append(persons, pid)
	}
	sort.Slice(persons, func(i, j int) bool { return persons[i] < persons[j] })

	var clusters []clusterRow

	for _, startPerson := range persons {
		if visited[startPerson] {
			continue
		}
		// BFS.
		component := []int64{}
		queue := []int64{startPerson}
		visited[startPerson] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			component = append(component, cur)
			for _, nb := range adj[cur] {
				if !visited[nb] {
					visited[nb] = true
					queue = append(queue, nb)
				}
			}
		}

		// Skip isolated nodes (component of 1).
		if len(component) < 2 {
			continue
		}

		// Assign cluster ID to each member.
		cid := clusterID
		clusterID++
		for _, pid := range component {
			personToCluster[pid] = cid
		}

		// Compute density: actual_edges / max_possible_edges.
		componentSet := map[int64]bool{}
		for _, pid := range component {
			componentSet[pid] = true
		}
		actualEdges := 0
		for _, e := range edges {
			if componentSet[e.PersonAID] && componentSet[e.PersonBID] {
				actualEdges++
			}
		}
		maxEdges := len(component) * (len(component) - 1) / 2
		density := 0.0
		if maxEdges > 0 {
			density = float64(actualEdges) / float64(maxEdges)
		}

		// Top members by weighted_degree (up to 10).
		type memberWD struct {
			id int64
			wd float64
		}
		mwds := make([]memberWD, 0, len(component))
		for _, pid := range component {
			mwds = append(mwds, memberWD{pid, wdByPerson[pid]})
		}
		sort.Slice(mwds, func(i, j int) bool { return mwds[i].wd > mwds[j].wd })
		topN := 10
		if len(mwds) < topN {
			topN = len(mwds)
		}
		topIDs := make([]int64, topN)
		for i := 0; i < topN; i++ {
			topIDs[i] = mwds[i].id
		}

		clusters = append(clusters, clusterRow{
			ClusterID:    cid,
			Size:         len(component),
			Density:      density,
			TopMemberIDs: topIDs,
		})
	}

	// Back-fill ClusterID into the metric rows.
	for i := range metrics {
		if cid, ok := personToCluster[metrics[i].PersonID]; ok {
			metrics[i].ClusterID = new(cid)
		}
	}

	return clusters
}
