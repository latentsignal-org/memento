package webui

import (
	"testing"
	"testing/fstest"
)

func TestRewriteDynamicSlug(t *testing.T) {
	// Mimics the exported `out/` layout: each dynamic section has an index and
	// a "_" placeholder shell; reserved subpaths export their own pages.
	content := fstest.MapFS{
		"people/index.html":       {},
		"people/_/index.html":     {},
		"projects/index.html":     {},
		"projects/_/index.html":   {},
		"projects/new/index.html": {},
		"concepts/_/index.html":   {},
		"sessions/_/index.html":   {},
		"debug/tools/index.html":  {},
	}

	cases := []struct {
		in   string
		want string
	}{
		{"people/jane", "people/_"},        // unknown slug -> placeholder shell
		{"people/_", "people/_"},           // already the reserved placeholder
		{"people", "people"},               // section index: no slug segment
		{"projects/new", "projects/new"},   // reserved subpath, not a slug
		{"concepts/foo-bar", "concepts/_"}, // unknown slug -> placeholder
		{"sessions/abc123", "sessions/_"},
		{"debug/tools", "debug/tools"}, // non-dynamic section: untouched
		{"assets/app.js", "assets/app.js"},
	}
	for _, tc := range cases {
		if got := rewriteDynamicSlug(content, tc.in); got != tc.want {
			t.Errorf("rewriteDynamicSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Every section in dynamicSections must treat "_" as a reserved subpath so the
// rewriter never rewrites the placeholder onto itself or recurses.
func TestPlaceholderIsReserved(t *testing.T) {
	if !reservedSubpaths["_"] {
		t.Fatal(`"_" must be a reserved subpath`)
	}
}
