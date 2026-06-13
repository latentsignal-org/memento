// Package server — internal endpoints that the Next.js agent runtime calls
// during tool execution. Gated by a shared-secret header so the surface stays
// off-limits to the browser.
package server

import (
	"crypto/subtle"
	"fmt"
	"net/http"

	"memento/backend/internal/config"
)

// requireInternalToken wraps a handler so it only runs when the request
// carries an X-Internal-Token header matching the configured internal token.
func (s *Server) requireInternalToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := config.InternalToken()
		if expected == "" {
			writeError(w, http.StatusInternalServerError,
				fmt.Errorf("%s not configured on server", config.EnvInternalToken))
			return
		}
		got := r.Header.Get("X-Internal-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, fmt.Errorf("invalid internal token"))
			return
		}
		next(w, r)
	}
}

// handleAgentToolsPing is the Phase 1 smoke endpoint: it confirms the
// TS ↔ Go bridge is wired correctly when called from the agent runtime.
func (s *Server) handleAgentToolsPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
