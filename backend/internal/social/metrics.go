package social

import (
	"math"
	"sort"
	"time"
)

// metricRow is the per-person metric ready for insertion.
type metricRow struct {
	PersonID          int64
	Degree            int
	WeightedDegree    float64
	DirectDegree      int
	CoRecipientDegree int
	ClusterID         *int64
	DormancyDays      *int64
	StructuralRole    string
}

// computeMetrics derives per-person metrics from the finalized edge list.
// Returns a slice of metricRow (without ClusterID, which is filled by computeClusters).
func computeMetrics(edges []edgeRow) []metricRow {
	type personAgg struct {
		degree            int
		weightedDegree    float64
		directDegree      int
		coRecipientDegree int
		totalDirectCount  int
		lastTs            time.Time
	}
	agg := map[int64]*personAgg{}

	ensure := func(id int64) *personAgg {
		if a, ok := agg[id]; ok {
			return a
		}
		a := &personAgg{}
		agg[id] = a
		return a
	}

	for _, e := range edges {
		a := ensure(e.PersonAID)
		a.degree++
		a.weightedDegree += e.Weight
		if e.DirectCount > 0 {
			a.directDegree++
			a.totalDirectCount += e.DirectCount
		}
		if e.CoRecipientCount > 0 {
			a.coRecipientDegree++
		}
		if e.LastTs != "" {
			if t := parseTimestamp(e.LastTs); !t.IsZero() && t.After(a.lastTs) {
				a.lastTs = t
			}
		}

		b := ensure(e.PersonBID)
		b.degree++
		b.weightedDegree += e.Weight
		if e.DirectCount > 0 {
			b.directDegree++
			b.totalDirectCount += e.DirectCount
		}
		if e.CoRecipientCount > 0 {
			b.coRecipientDegree++
		}
		if e.LastTs != "" {
			if t := parseTimestamp(e.LastTs); !t.IsZero() && t.After(b.lastTs) {
				b.lastTs = t
			}
		}
	}

	// Compute p95 degree threshold for 'hub' classification.
	degrees := make([]int, 0, len(agg))
	for _, a := range agg {
		degrees = append(degrees, a.degree)
	}
	p95 := percentile95(degrees)

	now := time.Now().UTC()

	metrics := make([]metricRow, 0, len(agg))
	for personID, a := range agg {
		m := metricRow{
			PersonID:          personID,
			Degree:            a.degree,
			WeightedDegree:    a.weightedDegree,
			DirectDegree:      a.directDegree,
			CoRecipientDegree: a.coRecipientDegree,
			StructuralRole:    classifyRole(a.degree, a.directDegree, a.coRecipientDegree, a.totalDirectCount, p95),
		}
		if !a.lastTs.IsZero() {
			m.DormancyDays = new(int64(math.Round(now.Sub(a.lastTs).Hours() / 24.0)))
		}
		metrics = append(metrics, m)
	}
	return metrics
}

// classifyRole assigns a structural role based on degree, communication patterns, and direct count.
func classifyRole(degree, directDegree, coRecipientDegree, totalDirectCount, p95 int) string {
	switch {
	case degree <= 1:
		return "isolated"
	case degree >= p95:
		return "hub"
	case directDegree <= 2 && coRecipientDegree >= 5 && totalDirectCount <= 10:
		return "bridge"
	default:
		return "peripheral"
	}
}

// percentile95 returns the value at the 95th percentile of a slice of ints.
// Returns 1 for slices with fewer than 2 elements.
func percentile95(vals []int) int {
	if len(vals) < 2 {
		return 1
	}
	sorted := make([]int, len(vals))
	copy(sorted, vals)
	sort.Ints(sorted)
	idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
