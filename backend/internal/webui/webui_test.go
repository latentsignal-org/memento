package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestServeHTMLSetsContentSecurityPolicy(t *testing.T) {
	content := fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><html><body>Memento</body></html>")},
	}
	rec := httptest.NewRecorder()

	serveHTML(rec, content, "index.html", http.StatusOK)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	policy := rec.Header().Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("Content-Security-Policy header missing")
	}
	for _, want := range []string{
		"default-src 'self'",
		"img-src 'self' data: https://www.gravatar.com",
		"connect-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("Content-Security-Policy = %q, missing %q", policy, want)
		}
	}
	for _, blocked := range []string{
		"attacker.example",
		"https://www.google.com/url",
	} {
		if strings.Contains(policy, blocked) {
			t.Fatalf("Content-Security-Policy = %q, unexpectedly allows %q", policy, blocked)
		}
	}
}
