package newsletter

import "testing"

func TestCleanNarrativeJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain json",
			in:   `{"coverage_summary":"A [msg:1].","recurring_themes":[],"notable_recent":[]}`,
			want: `{"coverage_summary":"A [msg:1].","recurring_themes":[],"notable_recent":[]}`,
		},
		{
			name: "fenced json",
			in:   "```json\n{\"coverage_summary\":\"A [msg:1].\",\"recurring_themes\":[],\"notable_recent\":[]}\n```",
			want: `{"coverage_summary":"A [msg:1].","recurring_themes":[],"notable_recent":[]}`,
		},
		{
			name: "preface",
			in:   "Here is the JSON:\n{\"coverage_summary\":\"A [msg:1].\",\"recurring_themes\":[],\"notable_recent\":[]}",
			want: `{"coverage_summary":"A [msg:1].","recurring_themes":[],"notable_recent":[]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanNarrativeJSON(tc.in); got != tc.want {
				t.Fatalf("cleanNarrativeJSON() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeCoverageSummaryCitations(t *testing.T) {
	in := "The program focuses on prevention [29345, 29115]. It also highlights nutrition [msg:29101, 29041]."
	got := normalizeCoverageSummaryCitations(in)
	want := "The program focuses on prevention [msg:29345, msg:29115]. It also highlights nutrition [msg:29101, msg:29041]."
	if got != want {
		t.Fatalf("normalizeCoverageSummaryCitations() = %q, want %q", got, want)
	}
}

func TestValidateEverySentenceCited_AllowsNormalizedNumericGroups(t *testing.T) {
	text := normalizeCoverageSummaryCitations(
		"The program focuses on prevention [29345, 29115]. Recent issues emphasize sleep [29041].",
	)
	if err := validateEverySentenceCited(text); err != nil {
		t.Fatalf("validateEverySentenceCited() error = %v", err)
	}
	ids := extractCitations(text)
	if len(ids) != 3 {
		t.Fatalf("extractCitations() count = %d, want 3", len(ids))
	}
}

func TestValidateEverySentenceCited_IgnoresDrAbbreviationBoundary(t *testing.T) {
	text := `The book club will read "The Diabetes Code" by Dr. Jason Fung [msg:29041].`
	if err := validateEverySentenceCited(text); err != nil {
		t.Fatalf("validateEverySentenceCited() error = %v", err)
	}
}

func TestSanitizeCoverageSummary_DropsUncitedTrailingFragment(t *testing.T) {
	in := `The program focuses on prevention [msg:29345]. Members are encouraged to participate in Q&A calls and book clubs, which have discussed`
	got := sanitizeCoverageSummary(in)
	want := `The program focuses on prevention [msg:29345].`
	if got != want {
		t.Fatalf("sanitizeCoverageSummary() = %q, want %q", got, want)
	}
}

func TestSanitizeCoverageSummary_MergesCitationOnlyFragment(t *testing.T) {
	in := "Members are encouraged to participate in Q&A calls and book clubs. [29345, 29115]"
	got := sanitizeCoverageSummary(normalizeCoverageSummaryCitations(in))
	want := "Members are encouraged to participate in Q&A calls and book clubs. [msg:29345, msg:29115]"
	if got != want {
		t.Fatalf("sanitizeCoverageSummary() = %q, want %q", got, want)
	}
}
