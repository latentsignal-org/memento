package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/store"
)

// blockingProvider waits on ctx.Done before returning. Tests use it to keep a
// run in the "running" state so the cancel endpoint can act on a live run.
type blockingProvider struct {
	started chan struct{}
}

func (p *blockingProvider) Name() string { return "fake" }

func (p *blockingProvider) Stream(ctx context.Context, _ agentrunner.ModelRequest, emit func(agentrunner.ModelEvent) error) error {
	if err := emit(agentrunner.ModelEvent{Type: agentrunner.ModelTextDelta, Text: "starting"}); err != nil {
		return err
	}
	select {
	case p.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestAgentRunsCancel_ActiveRun(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	provider := &blockingProvider{started: make(chan struct{}, 1)}
	srv.agents.RegisterProvider(provider)

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type":   "dashboard",
		"entity_id":    "dashboard",
		"user_message": "hold",
		"provider":     "fake",
		"model":        "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create run: %d %s", w.Code, w.Body.String())
	}
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("provider did not start")
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/internal/agent-runs/"+strconv.FormatInt(runID, 10)+"/cancel", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	cw := httptest.NewRecorder()
	srv.mux.ServeHTTP(cw, req)
	if cw.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", cw.Code, cw.Body.String())
	}

	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}

	types := eventTypes(t, srv, runID)
	if !contains(types, "error") {
		t.Fatalf("expected error event after cancel, got %v", types)
	}
}

func TestAgentRunsCancel_DoesNotOverwriteSucceeded(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	provider := &scriptedAgentProvider{
		name: "fake",
		emit: doneOnce(t, "fake-interaction", "done."),
	}
	srv.agents.RegisterProvider(provider)

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type":   "dashboard",
		"entity_id":    "dashboard",
		"user_message": "hi",
		"provider":     "fake",
		"model":        "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create run: %d %s", w.Code, w.Body.String())
	}
	before := waitForRunTerminal(t, srv, runID)
	if before.Status != store.AgentRunSucceeded {
		t.Fatalf("expected succeeded, got %s", before.Status)
	}

	// Snapshot fields the cancel path used to clobber under earlier bugs.
	priorInteraction := before.InteractionID
	priorFinished := before.FinishedAt

	req := httptest.NewRequest(http.MethodPost,
		"/api/internal/agent-runs/"+strconv.FormatInt(runID, 10)+"/cancel", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	cw := httptest.NewRecorder()
	srv.mux.ServeHTTP(cw, req)
	if cw.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", cw.Code, cw.Body.String())
	}

	after, err := store.GetAgentRun(context.Background(), srv.db, runID)
	if err != nil {
		t.Fatalf("get run after cancel: %v", err)
	}
	if after.Status != store.AgentRunSucceeded {
		t.Fatalf("cancel overwrote terminal status: now %s", after.Status)
	}
	if after.InteractionID != priorInteraction {
		t.Fatalf("interaction_id changed: %q -> %q", priorInteraction, after.InteractionID)
	}
	if after.FinishedAt != priorFinished {
		t.Fatalf("finished_at changed: %q -> %q", priorFinished, after.FinishedAt)
	}
}

func TestAgentRunsCancel_UnknownRunReturnsOK(t *testing.T) {
	// The current implementation no-ops cancels for unknown runs (gated
	// UPDATE matches no rows). The handler returns 200 OK. Codify the
	// contract so a regression to "404" or "500" is caught.
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodPost, "/api/internal/agent-runs/9999/cancel", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for unknown run cancel, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentRunsCancel_UnauthorizedWithoutToken(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodPost, "/api/internal/agent-runs/1/cancel", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
