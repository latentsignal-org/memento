package refresh

import "testing"

// People slugs must never collide with the "_" placeholder slug reserved by the
// statically exported web UI (see backend/internal/webui). slugifyPersonName
// converts underscores to "-" and trims dashes, so "_" is unreachable; uniqueSlug
// substitutes "person" for the empty base. This locks both guarantees.
func TestSlugifyPersonNameNeverPlaceholder(t *testing.T) {
	for _, in := range []string{"_", "___", "_ _", "-_-", "a_b", "💡", "  "} {
		if got := slugifyPersonName(in); got == "_" {
			t.Errorf("slugifyPersonName(%q) = %q, must never equal the reserved placeholder", in, got)
		}
	}
	if got := uniqueSlug(slugifyPersonName("_"), map[string]int{}); got == "_" {
		t.Errorf("uniqueSlug for a name slugged from %q produced the reserved placeholder", "_")
	}
}
