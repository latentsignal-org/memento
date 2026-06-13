package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGuardLocal(t *testing.T) {
	s := &Server{}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := s.guardLocal(ok)

	cases := []struct {
		name   string
		method string
		host   string
		origin string
		want   int
	}{
		{"local GET no origin", http.MethodGet, "127.0.0.1:8787", "", http.StatusOK},
		{"local GET localhost host", http.MethodGet, "localhost:8787", "", http.StatusOK},
		{"local POST same-origin", http.MethodPost, "127.0.0.1:8787", "http://127.0.0.1:8787", http.StatusOK},
		{"local POST localhost origin", http.MethodPost, "localhost:8787", "http://localhost:8787", http.StatusOK},
		{"tooling POST no origin", http.MethodPost, "127.0.0.1:8787", "", http.StatusOK},
		{"ipv6 loopback host", http.MethodGet, "[::1]:8787", "", http.StatusOK},

		// DNS rebinding: TCP is local but Host carries the attacker domain.
		{"rebinding GET foreign host", http.MethodGet, "evil.example.com:8787", "", http.StatusForbidden},
		{"rebinding POST foreign host", http.MethodPost, "evil.example.com", "", http.StatusForbidden},

		// CSRF: a foreign page POSTs to loopback; the browser attaches Origin.
		{"csrf POST foreign origin", http.MethodPost, "127.0.0.1:8787", "https://evil.example.com", http.StatusForbidden},
		{"csrf DELETE foreign origin", http.MethodDelete, "127.0.0.1:8787", "https://evil.example.com", http.StatusForbidden},
		// A foreign Origin on a safe method is not a state-change vector.
		{"safe GET foreign origin allowed", http.MethodGet, "127.0.0.1:8787", "https://evil.example.com", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://example/api/projects", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
