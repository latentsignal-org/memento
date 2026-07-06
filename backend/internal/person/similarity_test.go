package person

import (
	"math"
	"testing"
)

func TestJaroWinklerKnownValues(t *testing.T) {
	cases := []struct {
		a, b string
		want float64 // tolerance ±0.01
	}{
		{"MARTHA", "MARHTA", 0.961},
		{"DIXON", "DICKSONX", 0.813},
		{"DWAYNE", "DUANE", 0.840},
		{"", "", 1.0},
		{"identical", "identical", 1.0},
		{"abc", "xyz", 0.0},
	}
	for _, tc := range cases {
		got := jaroWinkler(tc.a, tc.b)
		if math.Abs(got-tc.want) > 0.01 {
			t.Fatalf("jaroWinkler(%q, %q) = %.3f, want ~%.3f", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestJaroWinklerCatchesMinorVariations(t *testing.T) {
	// Jaro-Winkler's strength: typos, missing punctuation/initials.
	pairs := [][2]string{
		{"jane elizabeth smith", "jane e smith"},
		{"robert j. fisher", "robert fisher"},
	}
	for _, p := range pairs {
		s := jaroWinkler(p[0], p[1])
		if s < 0.85 {
			t.Errorf("jaroWinkler(%q, %q) = %.3f, want >= 0.85", p[0], p[1], s)
		}
	}
}

func TestJaccardCatchesInsertedTokens(t *testing.T) {
	// Jaro-Winkler weakness: inserted middle words. Jaccard on tokens picks
	// these up for advisory merge suggestions.
	jw := jaroWinkler("tom hall", "tom fitzgerald hall")
	if jw >= 0.85 {
		t.Fatalf("test premise broken: jaroWinkler now %.3f >= 0.85", jw)
	}
	jc := jaccardTokens(
		displayNameTokens("Tom Hall"),
		displayNameTokens("Tom Fitzgerald Hall"),
	)
	if jc < 0.6 {
		t.Fatalf("jaccard(tom hall, tom fitzgerald hall) = %.3f, want >= 0.6", jc)
	}
}

func TestJaccardTokens(t *testing.T) {
	if got := jaccardTokens([]string{"a", "b", "c"}, []string{"b", "c", "d"}); math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("jaccard symmetric = %v, want 0.5", got)
	}
	if got := jaccardTokens(nil, nil); got != 0 {
		t.Fatalf("jaccard(nil,nil) = %v, want 0", got)
	}
	if got := jaccardTokens([]string{"a"}, []string{"a"}); got != 1 {
		t.Fatalf("jaccard identical = %v, want 1", got)
	}
}
