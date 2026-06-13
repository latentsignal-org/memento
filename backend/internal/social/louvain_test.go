package social

import (
	"reflect"
	"testing"
)

// clique returns the edges of a complete graph over ids, weight 1.
func clique(ids ...int64) []louvainEdge {
	var out []louvainEdge
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			out = append(out, louvainEdge{ids[i], ids[j], 1})
		}
	}
	return out
}

func TestLouvain_TwoCliques(t *testing.T) {
	// Two 4-cliques {1..4} and {5..8} joined by a single weak bridge edge.
	edges := append(clique(1, 2, 3, 4), clique(5, 6, 7, 8)...)
	edges = append(edges, louvainEdge{4, 5, 0.1})

	comm := louvain(edges)

	// Each clique must be one community, and the two must differ.
	c1 := comm[1]
	for _, n := range []int64{2, 3, 4} {
		if comm[n] != c1 {
			t.Fatalf("node %d in community %d, expected same as node 1 (%d)", n, comm[n], c1)
		}
	}
	c5 := comm[5]
	for _, n := range []int64{6, 7, 8} {
		if comm[n] != c5 {
			t.Fatalf("node %d in community %d, expected same as node 5 (%d)", n, comm[n], c5)
		}
	}
	if c1 == c5 {
		t.Fatalf("the two cliques collapsed into one community (%d); bridge should not merge them", c1)
	}
}

func TestLouvain_Deterministic(t *testing.T) {
	edges := append(clique(1, 2, 3, 4), clique(5, 6, 7, 8)...)
	edges = append(edges, clique(9, 10, 11, 12)...)
	edges = append(edges, louvainEdge{4, 5, 0.2}, louvainEdge{8, 9, 0.2})

	first := louvain(edges)
	for i := 0; i < 5; i++ {
		again := louvain(edges)
		if !reflect.DeepEqual(first, again) {
			t.Fatalf("louvain is non-deterministic: run %d differs from run 0", i+1)
		}
	}
	// Three cliques → three communities.
	seen := map[int]struct{}{}
	for _, c := range first {
		seen[c] = struct{}{}
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 communities, got %d", len(seen))
	}
}

func TestLouvain_Empty(t *testing.T) {
	if got := louvain(nil); len(got) != 0 {
		t.Fatalf("empty input should yield empty result, got %v", got)
	}
}
