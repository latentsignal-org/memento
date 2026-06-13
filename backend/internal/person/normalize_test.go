package person

import (
	"reflect"
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{"alice@gmail.com", "alice@gmail.com"},
		{"Alice@Gmail.COM", "alice@gmail.com"},
		{"alice+news@gmail.com", "alice@gmail.com"},
		{"alice+a+b@gmail.com", "alice@gmail.com"},
		{"ann.jose+home@gmail.com", "ann.jose@gmail.com"},
		{"buzz+z130hdmimpvmytetu22pjdew1prlhrgi504@gmail.com", "buzz+z130hdmimpvmytetu22pjdew1prlhrgi504@gmail.com"},
		{"annj+tag@gmail.com", "annj+tag@gmail.com"},
		{"alice+news@example.com", "alice+news@example.com"},
		{"  alice@gmail.com  ", "alice@gmail.com"},
		{"noplus", "noplus"},
	}
	for _, tc := range cases {
		if got := normalizeEmail(tc.in); got != tc.want {
			t.Fatalf("normalizeEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Jane Smith", "jane smith"},
		{"Jane Smith (via Google Photos)", "jane smith"},
		{"Jane Smith (Google Docs)", "jane smith"},
		{"Jane Smith (SMS)", "jane smith"},
		{"Bill Gates (via the Gates Notes)", "bill gates"},
		{"  multiple   spaces  ", "multiple spaces"},
		{"", ""},
		{"No Parens Here", "no parens here"},
		{"Random (some random text)", "random (some random text)"}, // not a forwarder marker — keep
	}
	for _, tc := range cases {
		if got := normalizeName(tc.in); got != tc.want {
			t.Fatalf("normalizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDisplayNameTokens(t *testing.T) {
	got := displayNameTokens("Jane Smith")
	want := []string{"jane", "smith"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("displayNameTokens = %v, want %v", got, want)
	}

	got = displayNameTokens("Kent C. Dodds")
	want = []string{"kent", "dodds"} // single-letter initial dropped
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("displayNameTokens with initial = %v, want %v", got, want)
	}

	if got := displayNameTokens(""); got != nil {
		t.Fatalf("displayNameTokens(\"\") = %v, want nil", got)
	}
}

func TestStripForwarderParenthetical(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Jane Smith (via Google Photos)", "Jane Smith"},
		{"Jane Smith (Google Docs)", "Jane Smith"},
		{"Bill Gates (via the Gates Notes)", "Bill Gates"},
		{"Just A Name", ""},
		{"Some User (random note)", ""}, // not a forwarder marker — leave alone
	}
	for _, tc := range cases {
		if got := stripForwarderParenthetical(tc.in); got != tc.want {
			t.Fatalf("stripForwarderParenthetical(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
