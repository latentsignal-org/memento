package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAgentDecisions_Workflow(t *testing.T) {
	os.Setenv("MEMENTO_INTERNAL_TOKEN", "test-secret-token")
	defer os.Unsetenv("MEMENTO_INTERNAL_TOKEN")

	srv, _ := newTestServer(t)

	createBody := []byte(`{
		"id":"decision-1",
		"decision_type":"backfill",
		"entity_type":"draft",
		"entity_id":"41",
		"payload_json":{"candidate_message_ids":[1,2]}
	}`)
	req := httptest.NewRequest("POST", "/api/internal/agent-decisions", bytes.NewReader(createBody))
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/internal/agent-decisions/decision-1", nil)
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", w.Code, w.Body.String())
	}
	var decision storeDecisionResponse
	if err := json.NewDecoder(w.Body).Decode(&decision); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if decision.Status != "pending" {
		t.Fatalf("status = %q, want pending", decision.Status)
	}

	resolveBody := []byte(`{"status":"accepted","result_json":{"accepted":true,"added_count":2}}`)
	req = httptest.NewRequest("PATCH", "/api/internal/agent-decisions/decision-1", bytes.NewReader(resolveBody))
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&decision); err != nil {
		t.Fatalf("decode resolve response: %v", err)
	}
	if decision.Status != "accepted" || decision.ResolvedAt == "" {
		t.Fatalf("unexpected resolved decision: %+v", decision)
	}
}

type storeDecisionResponse struct {
	Status     string `json:"status"`
	ResolvedAt string `json:"resolved_at"`
}
