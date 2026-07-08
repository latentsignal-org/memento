// Package server hosts the HTTP API consumed by the embedded web UI. Bound to
// 127.0.0.1 only — local-first guardrail per spec §1.4.
//
// Binding to loopback stops remote TCP connections but is NOT the whole auth
// story: a web page in the user's browser can still reach 127.0.0.1. Two
// browser-driven vectors remain and are closed by guardLocal — DNS rebinding
// (validated away by the Host header) and CSRF (validated away by the Origin
// header on state-changing requests). If you ever change the bind address,
// that is a larger design call requiring real per-request authentication.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/avatar"
	"memento/backend/internal/config"
	"memento/backend/internal/msgvault"
	"memento/backend/internal/store"
)

// Options configures a running Server instance.
type Options struct {
	Port           int
	DBPath         string
	AllowedOrigins []string // CORS allow-list for the FE dev server
}

// Server bundles the HTTP mux, the read-write DB handle (for Memento tables),
// and the read-only msgvault reader (for raw archive queries via FTS).
type Server struct {
	opts     Options
	mux      *http.ServeMux
	db       *sql.DB
	reader   *msgvault.Reader
	jobs     *JobStore
	agents   *agentrunner.Runner
	avatars  *avatar.Service
	demoMode bool
}

// New returns a configured Server. Callers run via Server.Run.
func New(opts Options, db *sql.DB, reader *msgvault.Reader) *Server {
	if opts.Port == 0 {
		opts.Port = config.DefaultBackendPort
	}
	if len(opts.AllowedOrigins) == 0 {
		// These default origins exist only for the `pnpm run dev` frontend on
		// :3000 (cross-origin to this server). In the shipped single-binary
		// deployment the browser is same-origin, so CORS never applies and this
		// list is inert. guardLocal still governs which requests are accepted.
		opts.AllowedOrigins = []string{
			"http://localhost:3000",
			"http://127.0.0.1:3000",
		}
	}
	s := &Server{
		opts:    opts,
		mux:     http.NewServeMux(),
		db:      db,
		reader:  reader,
		jobs:    NewJobStore(),
		avatars: avatar.NewService(db, nil),
	}
	var providers []agentrunner.Provider
	if demoMode, err := store.GetConfig(context.Background(), db, "demo_mode"); err == nil && demoMode == "true" {
		providers = append(providers, &agentrunner.FakeProvider{Demo: true})
		s.demoMode = true
	}
	s.agents = agentrunner.NewRunner(db, serverToolRegistry{s: s}, providers...)
	s.agents.SetMaxParallelTools(agentMaxParallelTools())
	if n, err := store.FailStaleAgentRuns(context.Background(), db, staleAgentRunAfter()); err != nil {
		log.Printf("agent stale-run reconciliation failed: %v", err)
	} else if n > 0 {
		log.Printf("marked %d stale agent run(s) failed after backend restart", n)
	}
	s.registerRoutes()
	return s
}

// Run starts the HTTP server bound to 127.0.0.1 and blocks until ctx is
// cancelled. Graceful shutdown waits up to 5 seconds for in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.opts.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.cors(s.logging(s.guardLocal(s.mux))),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := store.FailStaleAgentRuns(ctx, s.db, staleAgentRunAfter())
				if err != nil {
					log.Printf("agent stale-run sweep failed: %v", err)
				} else if n > 0 {
					log.Printf("marked %d stale agent run(s) failed (heartbeat timeout)", n)
				}
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("memento serve listening on http://%s", addr)
		log.Printf("  bound to 127.0.0.1 only — no external connections accepted")
		log.Printf("  CORS allowed origins: %v", s.opts.AllowedOrigins)
		log.Printf("  Database: %s", s.opts.DBPath)
		log.Printf("  Archive: %s", archiveSummary(ctx, s.reader))
		log.Printf("  Verbose agent logs: %s | Demo mode: %s",
			onOff(config.AgentVerboseLogs()), onOff(s.demoMode))
		log.Printf("  LLM: model=%s, base_url=%s, provider=%s, api_key=%s",
			modelForAgentRun("", defaultModelProvider()),
			llmBaseURL(defaultModelProvider()),
			defaultModelProvider(),
			apiKeyPresence())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Println("shutting down...")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// cors wraps a handler with CORS headers permitting the configured origins.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			for _, allowed := range s.opts.AllowedOrigins {
				if origin == allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// guardLocal rejects requests that did not originate from this machine's
// browser talking to this server. It defends the loopback bind against the
// two vectors that survive it:
//
//   - DNS rebinding: a remote page rebinds its hostname to 127.0.0.1 and
//     scripts the API. The TCP connection is local, but the Host header still
//     carries the attacker's domain — so we require a loopback Host.
//   - CSRF: any page the user visits can POST to 127.0.0.1. Browsers attach an
//     Origin header to such cross-site requests, so we reject state-changing
//     requests whose Origin is present and non-local.
//
// Non-browser clients (curl, the e2e fixture, tooling) send no Origin and are
// unaffected; the Host check still applies to them but loopback tooling sends
// a loopback Host by construction.
func (s *Server) guardLocal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostIsLocal(r.Host) {
			http.Error(w, "forbidden: non-local Host header", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			// Safe methods: no state change, so CSRF is moot.
		default:
			if origin := r.Header.Get("Origin"); origin != "" && !originIsLocal(origin) {
				http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// localHostnames are the hostnames that resolve to this machine's loopback.
var localHostnames = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"::1":       true,
	"[::1]":     true,
}

// hostIsLocal reports whether an HTTP Host header names the loopback
// interface. The port is ignored — Memento may run on any port.
func hostIsLocal(host string) bool {
	if host == "" {
		return false
	}
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	return localHostnames[hostname]
}

// originIsLocal reports whether an Origin header points at the loopback
// interface (any scheme, any port).
func originIsLocal(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return localHostnames[u.Hostname()]
}

// logging is a tiny request logger; deliberately minimal — local dev tool.
func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		if shouldLogRequest(r) {
			log.Printf("%s %s -> %s (%s)", r.Method, r.URL.Path, statusForLog(rw.status), time.Since(start))
		}
	})
}

func statusForLog(status int) string {
	switch {
	case status >= 400:
		return fmt.Sprintf("\x1b[31m%d\x1b[0m", status)
	case status >= 200 && status < 300:
		return fmt.Sprintf("\x1b[32m%d\x1b[0m", status)
	default:
		return fmt.Sprintf("%d", status)
	}
}

func shouldLogRequest(r *http.Request) bool {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/jobs/") && strings.HasSuffix(r.URL.Path, "/status") {
		return false
	}
	// Web UI assets (everything outside /api) are too chatty to log.
	if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	return true
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func apiKeyPresence() string {
	if config.ModelAPIKey() == "" {
		return "MISSING"
	}
	return "configured"
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func archiveSummary(ctx context.Context, reader *msgvault.Reader) string {
	stats, err := reader.Stats(ctx)
	if err != nil {
		return "unavailable: " + err.Error()
	}
	return fmt.Sprintf("%d messages (%d sources)", stats.Messages, stats.Sources)
}

// Flush forwards to the underlying ResponseWriter so SSE streaming works
// through the middleware chain. Without this the wrapped writer hides the
// http.Flusher interface and SSE endpoints fail to stream.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// pathSegments splits "/api/people/ann-catherine-jose" into ["api","people","ann-catherine-jose"].
func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
