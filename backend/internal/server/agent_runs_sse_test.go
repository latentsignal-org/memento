package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"memento/backend/internal/store"
)

// sseEvent is one parsed SSE message from the replay stream.
type sseEvent struct {
	ID   string
	Data string
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" || strings.HasPrefix(block, ":") {
			continue
		}
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "id: "):
				ev.ID = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				ev.Data = strings.TrimPrefix(line, "data: ")
			}
		}
		if ev.Data != "" {
			events = append(events, ev)
		}
	}
	return events
}

func runCompletedRun(t *testing.T, srv *Server) int64 {
	t.Helper()
	provider := &scriptedAgentProvider{
		name: "fake",
		emit: doneOnce(t, "fake-interaction", "Reply body."),
	}
	srv.agents.RegisterProvider(provider)
	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type":   "dashboard",
		"entity_id":    "dashboard",
		"user_message": "hello",
		"provider":     "fake",
		"model":        "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create run: %d %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("run did not succeed: %+v", run)
	}
	return runID
}

func TestAgentRunsSSE_InitialReplay(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	runID := runCompletedRun(t, srv)

	req := httptest.NewRequest(http.MethodGet, "/api/internal/agent-runs/"+strconv.FormatInt(runID, 10)+"/events", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q", got)
	}

	events := parseSSE(t, w.Body.String())
	if len(events) == 0 {
		t.Fatalf("expected SSE events from initial replay, got none. raw=%q", w.Body.String())
	}
	if events[len(events)-1].Data == "" || !strings.Contains(events[len(events)-1].Data, `"type":"done"`) {
		t.Fatalf("expected final done event, got %+v", events[len(events)-1])
	}
	for _, ev := range events {
		if ev.ID == "" {
			t.Fatalf("event missing id: %+v", ev)
		}
	}
}

func TestAgentRunsSSE_AfterSeqQuery(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	runID := runCompletedRun(t, srv)

	// First, gather all event seqs from the store.
	dbEvents, err := store.ListAgentEventsAfter(t.Context(), srv.db, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(dbEvents) < 2 {
		t.Skipf("need at least 2 events to test after_seq, got %d", len(dbEvents))
	}
	cutoff := dbEvents[0].Seq

	req := httptest.NewRequest(http.MethodGet,
		"/api/internal/agent-runs/"+strconv.FormatInt(runID, 10)+"/events?after_seq="+strconv.FormatInt(cutoff, 10),
		nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	events := parseSSE(t, w.Body.String())
	if len(events) != len(dbEvents)-1 {
		t.Fatalf("after_seq=%d expected %d events, got %d (raw=%q)",
			cutoff, len(dbEvents)-1, len(events), w.Body.String())
	}
	for _, ev := range events {
		seq, _ := strconv.ParseInt(ev.ID, 10, 64)
		if seq <= cutoff {
			t.Fatalf("after_seq=%d should not include seq=%d", cutoff, seq)
		}
	}
}

func TestAgentRunsSSE_LastEventIDHeader(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	runID := runCompletedRun(t, srv)

	dbEvents, err := store.ListAgentEventsAfter(t.Context(), srv.db, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(dbEvents) < 2 {
		t.Skipf("need >=2 events to validate Last-Event-ID, got %d", len(dbEvents))
	}
	cutoff := dbEvents[len(dbEvents)-2].Seq

	req := httptest.NewRequest(http.MethodGet,
		"/api/internal/agent-runs/"+strconv.FormatInt(runID, 10)+"/events", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	req.Header.Set("Last-Event-ID", strconv.FormatInt(cutoff, 10))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	events := parseSSE(t, w.Body.String())
	if len(events) != len(dbEvents)-int(cutoff) {
		t.Fatalf("Last-Event-ID=%d expected %d events, got %d", cutoff, len(dbEvents)-int(cutoff), len(events))
	}
	// after_seq query takes priority only when present — when missing, header
	// should drive the cursor.
	for _, ev := range events {
		seq, _ := strconv.ParseInt(ev.ID, 10, 64)
		if seq <= cutoff {
			t.Fatalf("Last-Event-ID=%d should not include seq=%d", cutoff, seq)
		}
	}
}

func TestAgentRunsSSE_TerminalRunStillReplaysDone(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	runID := runCompletedRun(t, srv)

	req := httptest.NewRequest(http.MethodGet,
		"/api/internal/agent-runs/"+strconv.FormatInt(runID, 10)+"/events", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	events := parseSSE(t, w.Body.String())
	var sawDone bool
	for _, ev := range events {
		var payload map[string]any
		if err := json.Unmarshal([]byte(ev.Data), &payload); err != nil {
			t.Fatalf("decode SSE data %q: %v", ev.Data, err)
		}
		if payload["type"] == "done" {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatalf("terminal run did not replay done event: %v", events)
	}
}

func TestAgentRunsSSE_UnknownRunReturns404(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/api/internal/agent-runs/9999/events", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
