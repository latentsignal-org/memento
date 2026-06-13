package social

import "sort"

// Deterministic weighted Louvain community detection (Blondel et al. 2008).
//
// Vendored pure-Go so the backend stays dependency-free. Determinism comes from
// processing nodes in a fixed sorted order and breaking ties by community id —
// there is no randomness, so a given graph always yields the same partition.
// At our scale (~1k nodes, ~1.5k edges) the naive implementation is instant.
//
// This is the v1 grouping algorithm. On real data it is statistically tied with
// Leiden-modularity; if finer granularity is ever needed (to dissolve an
// oversized work-network group), the upgrade path is Leiden + CPM — see
// docs/people-clusters-review-spec.md §5.2 and scripts/algo-compare.py.

// louvainEdge is one undirected weighted edge between two original node IDs.
type louvainEdge struct {
	A, B   int64
	Weight float64
}

// louvain runs community detection and returns a map from each original node ID
// to a community ID (small contiguous integers starting at 0). Nodes never seen
// in an edge are absent from the result.
func louvain(edges []louvainEdge) map[int64]int {
	if len(edges) == 0 {
		return map[int64]int{}
	}

	// Map original int64 IDs to dense indices 0..n-1 in a deterministic order.
	idSet := map[int64]struct{}{}
	for _, e := range edges {
		idSet[e.A] = struct{}{}
		idSet[e.B] = struct{}{}
	}
	ids := make([]int64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	index := make(map[int64]int, len(ids))
	for i, id := range ids {
		index[id] = i
	}

	// Build the initial working graph (one node per original node).
	g := newLouvainGraph(len(ids))
	for _, e := range edges {
		if e.A == e.B {
			continue
		}
		g.addEdge(index[e.A], index[e.B], e.Weight)
	}

	// node2comm[i] is the final community of original-node-index i, tracked
	// across aggregation levels.
	node2comm := make([]int, len(ids))
	for i := range node2comm {
		node2comm[i] = i
	}

	for {
		improved := g.localMoving()
		comms := g.renumberCommunities()
		// Propagate this level's assignment down to the original nodes.
		for i := range node2comm {
			node2comm[i] = comms[node2comm[i]]
		}
		if !improved || g.numCommunities() == g.n {
			break
		}
		g = g.aggregate(comms)
	}

	out := make(map[int64]int, len(ids))
	for i, id := range ids {
		out[id] = node2comm[i]
	}
	return out
}

// louvainGraph is one level of the Louvain hierarchy: a weighted graph plus the
// community bookkeeping needed for local moving.
type louvainGraph struct {
	n        int
	adj      [][]neighbor // neighbor lists (excludes self-loops)
	selfLoop []float64    // self-loop weight per node (from aggregation)
	degree   []float64    // weighted degree (self-loop counted twice)
	m2       float64      // 2 * total edge weight (sum of all degrees)

	comm    []int     // community of each node
	commTot []float64 // sum of degrees of nodes in each community
}

type neighbor struct {
	to     int
	weight float64
}

func newLouvainGraph(n int) *louvainGraph {
	return &louvainGraph{
		n:        n,
		adj:      make([][]neighbor, n),
		selfLoop: make([]float64, n),
		degree:   make([]float64, n),
	}
}

func (g *louvainGraph) addEdge(a, b int, w float64) {
	g.adj[a] = append(g.adj[a], neighbor{b, w})
	g.adj[b] = append(g.adj[b], neighbor{a, w})
	g.degree[a] += w
	g.degree[b] += w
	g.m2 += 2 * w
}

// initCommunities puts every node in its own community and seeds the totals.
func (g *louvainGraph) initCommunities() {
	g.comm = make([]int, g.n)
	g.commTot = make([]float64, g.n)
	for i := 0; i < g.n; i++ {
		g.comm[i] = i
		// degree already includes 2*selfLoop because aggregate() adds self-loops
		// into degree; commTot mirrors that.
		g.commTot[i] = g.degree[i]
	}
}

// localMoving repeatedly moves each node to the neighbouring community giving
// the largest positive modularity gain, until a full pass makes no move.
// Returns whether any node ever moved.
func (g *louvainGraph) localMoving() bool {
	g.initCommunities()
	if g.m2 == 0 {
		return false
	}
	anyMoved := false
	for {
		movedThisPass := false
		for node := 0; node < g.n; node++ {
			cur := g.comm[node]

			// Weight from node to each neighbouring community.
			toComm := map[int]float64{}
			for _, nb := range g.adj[node] {
				toComm[g.comm[nb.to]] += nb.weight
			}

			// Remove node from its current community.
			g.commTot[cur] -= g.degree[node]
			g.comm[node] = -1

			// Pick the best community: highest gain, ties broken by smallest
			// community id for determinism. Staying put (cur) is the baseline.
			bestComm := cur
			bestGain := toComm[cur] - g.commTot[cur]*g.degree[node]/g.m2

			// Evaluate candidate communities in sorted order.
			cands := make([]int, 0, len(toComm))
			for c := range toComm {
				cands = append(cands, c)
			}
			sort.Ints(cands)
			for _, c := range cands {
				gain := toComm[c] - g.commTot[c]*g.degree[node]/g.m2
				if gain > bestGain {
					bestGain = gain
					bestComm = c
				}
			}

			// Re-insert into the chosen community.
			g.commTot[bestComm] += g.degree[node]
			g.comm[node] = bestComm
			if bestComm != cur {
				movedThisPass = true
				anyMoved = true
			}
		}
		if !movedThisPass {
			break
		}
	}
	return anyMoved
}

// renumberCommunities maps the current community labels to dense ids 0..k-1
// (in ascending label order, for determinism) and returns node->denseComm.
func (g *louvainGraph) renumberCommunities() []int {
	remap := map[int]int{}
	labels := make([]int, 0)
	for _, c := range g.comm {
		if _, ok := remap[c]; !ok {
			remap[c] = -1
			labels = append(labels, c)
		}
	}
	sort.Ints(labels)
	for i, c := range labels {
		remap[c] = i
	}
	out := make([]int, g.n)
	for i, c := range g.comm {
		out[i] = remap[c]
	}
	return out
}

func (g *louvainGraph) numCommunities() int {
	seen := map[int]struct{}{}
	for _, c := range g.comm {
		seen[c] = struct{}{}
	}
	return len(seen)
}

// aggregate collapses each community into a single super-node, summing edge
// weights. Intra-community edges become self-loops. comms is node->denseComm.
func (g *louvainGraph) aggregate(comms []int) *louvainGraph {
	k := 0
	for _, c := range comms {
		if c+1 > k {
			k = c + 1
		}
	}
	ng := newLouvainGraph(k)

	// Accumulate inter-community weights and self-loops.
	interim := make([]map[int]float64, k)
	for i := range interim {
		interim[i] = map[int]float64{}
	}
	for node := 0; node < g.n; node++ {
		cn := comms[node]
		// carry forward existing self-loop weight
		ng.selfLoop[cn] += g.selfLoop[node]
		for _, nb := range g.adj[node] {
			cm := comms[nb.to]
			if cm == cn {
				// half, since each intra edge is seen from both endpoints
				ng.selfLoop[cn] += nb.weight / 2
			} else {
				interim[cn][cm] += nb.weight
			}
		}
	}

	// Materialise inter-community edges once (a<b) and set degrees.
	for a := 0; a < k; a++ {
		// self-loop contributes 2*weight to the degree
		ng.degree[a] += 2 * ng.selfLoop[a]
		for b, w := range interim[a] {
			if a < b {
				ng.adj[a] = append(ng.adj[a], neighbor{b, w})
				ng.adj[b] = append(ng.adj[b], neighbor{a, w})
				ng.degree[a] += w
				ng.degree[b] += w
				ng.m2 += 2 * w
			}
		}
	}
	// self-loops add to m2 as 2*weight (already counted in degree above)
	for a := 0; a < k; a++ {
		ng.m2 += 2 * ng.selfLoop[a]
	}
	return ng
}
