package people

import (
	"testing"

	"memento/backend/internal/msgvault"
)

func TestBidirectionalScore(t *testing.T) {
	cases := []struct {
		name string
		a, b int64
		want float64
	}{
		{"both zero", 0, 0, 0},
		{"a > b", 100, 10, 0.1},
		{"a < b", 10, 100, 0.1},
		{"equal", 50, 50, 1.0},
		{"one side zero", 100, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bidirectionalScore(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("bidirectionalScore(%d, %d) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestExclusionReason(t *testing.T) {
	cases := []struct {
		name        string
		row         msgvault.PeopleCandidateRow
		inboundOnly bool
		want        string
	}{
		{
			name: "missing email",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: ""},
			want: "missing email address",
		},
		{
			name: "no-reply local part",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "noreply@example.com", Domain: "example.com"},
			want: "system or no-reply address",
		},
		{
			name: "do-not-reply variant",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "do-not-reply@example.com", Domain: "example.com"},
			want: "system or no-reply address",
		},
		{
			name: "system-pattern notification",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "billing.notification@vendor.com", Domain: "vendor.com"},
			want: "system or no-reply address",
		},
		{
			name: "newsletter domain",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "author@substack.com", Domain: "substack.com"},
			want: "newsletter or broadcast domain",
		},
		{
			name: "broadcast display name",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "anything@example.com", Domain: "example.com", CanonicalName: "Weekly Digest"},
			want: "broadcast sender display name",
		},
		{
			name:        "unidirectional sender, outbound known",
			row:         msgvault.PeopleCandidateRow{PrimaryEmail: "ping@example.com", Domain: "example.com", FromContactCount: 25, ToContactCount: 0},
			inboundOnly: false,
			want:        "unidirectional sender",
		},
		{
			name:        "unidirectional but inbound-only archive — not excluded by direction",
			row:         msgvault.PeopleCandidateRow{PrimaryEmail: "ping@example.com", Domain: "example.com", FromContactCount: 25, ToContactCount: 0},
			inboundOnly: true,
			want:        "",
		},
		{
			name: "generic role mailbox — ask@",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "ask@turingpi.com", Domain: "turingpi.com"},
			want: "generic role address",
		},
		{
			name: "generic role mailbox — permits@",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "permits@calraters.com", Domain: "calraters.com"},
			want: "generic role address",
		},
		{
			name: "plus-tagged transactional",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "messages+abc@squaremktg.com", Domain: "squaremktg.com"},
			want: "plus-tagged broadcast address",
		},
		{
			name: "role-substring inside a human name is not excluded",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "alexcontact@example.com", Domain: "example.com", CanonicalName: "Alex", FromContactCount: 5, ToContactCount: 4},
			want: "",
		},
		{
			name: "ordinary human address passes",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "alice@example.com", Domain: "example.com", CanonicalName: "Alice Example", FromContactCount: 12, ToContactCount: 4},
			want: "",
		},
		{
			name: "real human passes all filters",
			row:  msgvault.PeopleCandidateRow{PrimaryEmail: "jane.smith@gmail.com", Domain: "gmail.com", CanonicalName: "Jane Smith", FromContactCount: 288, ToContactCount: 176},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := exclusionReason(tc.row, tc.inboundOnly)
			if got != tc.want {
				t.Fatalf("exclusionReason(%+v, inboundOnly=%v) = %q, want %q", tc.row, tc.inboundOnly, got, tc.want)
			}
		})
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name          string
		row           msgvault.PeopleCandidateRow
		inboundOnly   bool
		wantClass     string
		wantReasonHas string
	}{
		{
			name:      "excluded via no-reply",
			row:       msgvault.PeopleCandidateRow{PrimaryEmail: "noreply@example.com", TotalMessages: 50},
			wantClass: "excluded",
		},
		{
			name:          "inbound-only archive promotes humans without outbound signal",
			row:           msgvault.PeopleCandidateRow{PrimaryEmail: "alice@example.com", Domain: "example.com", CanonicalName: "Alice", TotalMessages: 30, FromContactCount: 30, ToContactCount: 0},
			inboundOnly:   true,
			wantClass:     "candidate_inbound_only",
			wantReasonHas: "no outbound",
		},
		{
			// Regression guard: this row deliberately leaves BidirectionalScore
			// at its zero value so the test only passes if classify() derives
			// the score from From/To counts.
			name:      "bidirectional human qualifies — score derived from counts",
			row:       msgvault.PeopleCandidateRow{PrimaryEmail: "bob@example.com", Domain: "example.com", CanonicalName: "Bob", TotalMessages: 40, FromContactCount: 25, ToContactCount: 15},
			wantClass: "candidate",
		},
		{
			name:      "high-volume profile qualifies",
			row:       msgvault.PeopleCandidateRow{PrimaryEmail: "jane@example.com", Domain: "example.com", CanonicalName: "Jane", TotalMessages: 464, FromContactCount: 288, ToContactCount: 176},
			wantClass: "candidate",
		},
		{
			name:          "below message count threshold is weak_signal",
			row:           msgvault.PeopleCandidateRow{PrimaryEmail: "carol@example.com", Domain: "example.com", CanonicalName: "Carol", TotalMessages: 4, FromContactCount: 2, ToContactCount: 2},
			wantClass:     "weak_signal",
			wantReasonHas: "thresholds",
		},
		{
			name:          "good count but weak bidirectional ratio is weak_signal",
			row:           msgvault.PeopleCandidateRow{PrimaryEmail: "dave@example.com", Domain: "example.com", CanonicalName: "Dave", TotalMessages: 50, FromContactCount: 49, ToContactCount: 1},
			wantClass:     "weak_signal",
			wantReasonHas: "thresholds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.row, tc.inboundOnly, nil, nil)
			if got.Classification != tc.wantClass {
				t.Fatalf("classification = %q, want %q (reason=%q)", got.Classification, tc.wantClass, got.ExclusionReason)
			}
			if tc.wantReasonHas != "" && !containsFold(got.ExclusionReason, tc.wantReasonHas) {
				t.Fatalf("exclusion reason = %q, want substring %q", got.ExclusionReason, tc.wantReasonHas)
			}
		})
	}
}

func TestClassifyOverrides(t *testing.T) {
	row := msgvault.PeopleCandidateRow{
		PersonID:      123,
		PrimaryEmail:  "noreply@example.com", // naturally excluded as bot
		TotalMessages: 50,
	}

	// 1. Naturally excluded, but overridden to human.
	gotHuman := classify(row, false, nil, map[int64]string{123: "human"})
	if gotHuman.Classification == "excluded" {
		t.Fatalf("expected override to human to bypass bot check, but got excluded (reason=%q)", gotHuman.ExclusionReason)
	}

	// 2. Naturally human, but overridden to excluded.
	rowHuman := msgvault.PeopleCandidateRow{
		PersonID:         456,
		PrimaryEmail:     "real@gmail.com",
		TotalMessages:    100,
		FromContactCount: 50,
		ToContactCount:   50,
	}
	gotExcluded := classify(rowHuman, false, nil, map[int64]string{456: "excluded"})
	if gotExcluded.Classification != "excluded" {
		t.Fatalf("expected override to excluded to make person excluded, but got %q", gotExcluded.Classification)
	}
}

func containsFold(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if equalFold(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
