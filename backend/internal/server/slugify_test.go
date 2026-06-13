package server

import "testing"

// The statically exported web UI reserves "_" as the placeholder slug for
// dynamic routes (see backend/internal/webui). A real project/concept slug must
// therefore never be "_", or it would collide with the placeholder shell.
// slugify emits only [a-z0-9-] (underscores become "-") and trims dashes, so
// the empty string is the only degenerate output — never "_". This locks that.
func TestSlugifyNeverPlaceholder(t *testing.T) {
	adversarial := []string{
		"_", "__", "___", "_ _", " _ ", "-_-", "a_b", "  ", "***", "💡",
		"_placeholder_", "Project _", "C++", "Q&A",
	}
	for _, in := range adversarial {
		if got := slugify(in); got == "_" {
			t.Errorf("slugify(%q) = %q, must never equal the reserved placeholder", in, got)
		}
	}
}
