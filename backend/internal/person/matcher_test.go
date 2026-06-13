package person

import "testing"

func TestMergeClusters_IgnoresNilDrop(t *testing.T) {
	keep := &cluster{
		ID: 1,
		Members: []*clusterMember{
			{ClusterID: 1, LinkSource: LinkSourceSingleton},
		},
	}
	all := map[int]*cluster{
		1: keep,
	}

	mergeClusters(all, keep, nil, LinkSourceExactName, 0.95)

	if len(keep.Members) != 1 {
		t.Fatalf("keep member count changed: got %d, want 1", len(keep.Members))
	}
	if keep.Members[0].LinkSource != LinkSourceSingleton {
		t.Fatalf("keep member source changed: got %q, want %q", keep.Members[0].LinkSource, LinkSourceSingleton)
	}
	if all[1] != keep {
		t.Fatalf("keep cluster disappeared from map")
	}
}
