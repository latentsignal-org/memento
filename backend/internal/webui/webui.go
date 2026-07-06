// Package webui embeds the statically exported Next.js frontend (the `out/`
// directory produced by `pnpm build`) and serves it from the Go backend, so
// a single binary hosts both the UI and the API on one origin.
//
// Release builds copy `out/` to `backend/internal/webui/dist` before
// `go build` (see `pnpm stage:webui` / `pnpm package`). When the embedded
// copy is absent (e.g. a bare `go build` during backend-only development),
// the handler can fall back to an on-disk directory via MEMENTO_WEB_DIR, or
// serves a short notice explaining how to build the UI.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// dynamicSections lists the app routes exported with a placeholder "_" slug.
// Requests for /people/jane/... are rewritten to the placeholder shell; the
// client reads the real slug from window.location.
var dynamicSections = map[string]bool{
	"people":      true,
	"projects":    true,
	"concepts":    true,
	"newsletters": true,
	"sessions":    true,
}

// reservedSubpaths are real exported routes nested under a dynamic section
// that must NOT be treated as slugs (e.g. /projects/new).
var reservedSubpaths = map[string]bool{
	"new":          true,
	"merge-review": true,
	"_":            true,
}

// Handler serves the exported UI. setupInitialized is consulted on HTML
// navigations; when it reports false, the user is redirected to /onboard
// (mirrors the old Next.js proxy middleware).
func Handler(setupInitialized func(r *http.Request) bool) http.Handler {
	var content fs.FS
	if dir := os.Getenv("MEMENTO_WEB_DIR"); dir != "" {
		content = os.DirFS(dir)
	} else {
		sub, err := fs.Sub(embedded, "dist")
		if err != nil {
			panic("webui: embedded dist missing: " + err.Error())
		}
		content = sub
	}
	available := pathExists(content, "index.html")
	fileServer := http.FileServer(http.FS(content))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !available {
			servePlaceholder(w)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		// Root: mirror the old "/" -> /home redirect, via onboarding check.
		if p == "" || p == "." {
			if setupInitialized != nil && !setupInitialized(r) {
				http.Redirect(w, r, "/onboard/", http.StatusFound)
				return
			}
			http.Redirect(w, r, "/home/", http.StatusFound)
			return
		}

		rewritten := rewriteDynamicSlug(content, p)

		// HTML navigations are served directly (never via http.FileServer,
		// whose canonicalizing redirects would rewrite /people/jane to the
		// placeholder /people/_/ in the address bar).
		if htmlFile := htmlFileFor(content, rewritten); htmlFile != "" {
			if !strings.HasPrefix(p, "onboard") && !strings.HasPrefix(p, "setup") &&
				setupInitialized != nil && !setupInitialized(r) {
				http.Redirect(w, r, "/onboard/", http.StatusFound)
				return
			}
			serveHTML(w, content, htmlFile, http.StatusOK)
			return
		}

		// Assets and payloads: exact (possibly slug-rewritten) file paths.
		if pathExists(content, rewritten) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + rewritten
			fileServer.ServeHTTP(w, r2)
			return
		}

		// Unknown route: exported 404 page.
		if pathExists(content, "404.html") {
			serveHTML(w, content, "404.html", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	})
}

// rewriteDynamicSlug maps /people/jane[/...] onto the exported placeholder
// shell /people/_[/...] when the literal path does not exist as a file.
func rewriteDynamicSlug(content fs.FS, p string) string {
	segs := strings.Split(p, "/")
	if len(segs) < 2 || !dynamicSections[segs[0]] || reservedSubpaths[segs[1]] {
		return p
	}
	if pathExists(content, p) || pathExists(content, p+"/index.html") {
		return p
	}
	segs[1] = "_"
	return strings.Join(segs, "/")
}

// htmlFileFor resolves a request path to the HTML document that should be
// served for it: the path itself when it names an .html file, or its
// directory index. Returns "" when the path is not an HTML navigation.
func htmlFileFor(content fs.FS, p string) string {
	if strings.HasSuffix(p, ".html") && pathExists(content, p) {
		return p
	}
	if strings.Contains(path.Base(p), ".") {
		return ""
	}
	if pathExists(content, p+"/index.html") {
		return p + "/index.html"
	}
	return ""
}

func serveHTML(w http.ResponseWriter, content fs.FS, file string, status int) {
	data, err := fs.ReadFile(content, file)
	if err != nil {
		http.Error(w, "failed to read page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Security-Policy", cspPolicy)
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

const cspPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"img-src 'self' data:; " +
	"font-src 'self' data: https://fonts.gstatic.com; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

func pathExists(content fs.FS, p string) bool {
	_, err := fs.Stat(content, p)
	return err == nil
}

func servePlaceholder(w http.ResponseWriter) {
	data, err := embedded.ReadFile("dist/placeholder.html")
	if err != nil {
		http.Error(w, "Memento web UI not bundled. Build the frontend and rebuild the binary.", http.StatusNotImplemented)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", cspPolicy)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
