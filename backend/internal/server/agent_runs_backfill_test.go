package server

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/store"
)

// TestAgentRunsIntegration_ProposeBackfillResume covers the end-to-end durable
// human-decision path: model emits propose_backfill, runner enters
// waiting_for_user, an external decision resolves the row, the runner resumes
// and finishes, and the replay log contains the proposed_backfill plus the
// final terminal event.
func TestAgentRunsIntegration_ProposeBackfillResume(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_draft (id, kind, name_hint, transcript_json, entities_json, status)
		VALUES (1, 'project', 'Backfill draft', '[]', '{}', 'collecting')`); err != nil {
		t.Fatalf("seed draft: %v", err)
	}

	// Scripted provider:
	// step 1 -> emit propose_backfill tool call, then done.
	// step 2 -> we get tool result back, emit text + done.
	provider := &scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			switch step {
			case 1:
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
						ID:   "call-1",
						Name: "propose_backfill",
						Args: json.RawMessage(`{"rationale":"needs context","candidate_message_ids":[101,102],"gap_kind":"chronological"}`),
					}},
					{Type: agentrunner.ModelDone, InteractionID: "after-call", ProviderState: rawJSON(`{"interaction_id":"after-call"}`)},
				}
			default:
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelTextDelta, Text: "Final answer."},
					{Type: agentrunner.ModelDone, InteractionID: "final", ProviderState: rawJSON(`{"interaction_id":"final"}`)},
				}
			}
		},
	}
	srv.agents.RegisterProvider(provider)

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type":   "collector",
		"entity_id":    "1",
		"user_message": "find supporting messages",
		"provider":     "fake",
		"model":        "fake",
	})
	if w.Code != 202 {
		t.Fatalf("create run failed: %d %s", w.Code, w.Body.String())
	}

	// Wait until propose_backfill flips the run into waiting_for_user.
	decision := waitForPendingDecision(t, srv, "draft", "1", 2*time.Second)
	run, err := store.GetAgentRun(ctx, srv.db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunWaitingForUser {
		t.Fatalf("expected waiting_for_user, got %s", run.Status)
	}

	// External actor accepts the proposal — same path the UI takes.
	if _, err := store.ResolveAgentDecision(ctx, srv.db, decision.ID, "accepted", `{"accepted":true,"added_count":2}`); err != nil {
		t.Fatalf("resolve decision: %v", err)
	}

	final := waitForRunTerminal(t, srv, runID)
	if final.Status != store.AgentRunSucceeded {
		t.Fatalf("expected succeeded, got %s (%s)", final.Status, final.ErrorMessage)
	}
	if final.InteractionID != "final" {
		t.Fatalf("interaction_id = %q, want final", final.InteractionID)
	}
	if atomic.LoadInt32(&provider.step) != 2 {
		t.Fatalf("provider should have streamed twice, got step=%d", provider.step)
	}

	types := eventTypes(t, srv, runID)
	mustContain := []string{"tool_call_start", "proposed_backfill", "tool_call_result", "text_delta", "done"}
	for _, want := range mustContain {
		if !contains(types, want) {
			t.Fatalf("event log missing %s, got %v", want, types)
		}
	}
}

// TestAgentRunsIntegration_ProposeBackfillSkipped verifies that a "skipped"
// decision still resumes the run cleanly (the model just sees the result and
// continues).
func TestAgentRunsIntegration_ProposeBackfillSkipped(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_draft (id, kind, name_hint, transcript_json, entities_json, status)
		VALUES (2, 'concept', 'Skip draft', '[]', '{}', 'collecting')`); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	provider := &scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			if step == 1 {
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
						ID:   "call-2",
						Name: "propose_backfill",
						Args: json.RawMessage(`{"rationale":"r","candidate_message_ids":[1],"gap_kind":"thematic"}`),
					}},
					{Type: agentrunner.ModelDone, InteractionID: "x", ProviderState: rawJSON(`{}`)},
				}
			}
			return []agentrunner.ModelEvent{
				{Type: agentrunner.ModelTextDelta, Text: "ok"},
				{Type: agentrunner.ModelDone, InteractionID: "x", ProviderState: rawJSON(`{}`)},
			}
		},
	}
	srv.agents.RegisterProvider(provider)

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type":   "collector",
		"entity_id":    "2",
		"user_message": "go",
		"provider":     "fake",
		"model":        "fake",
	})
	if w.Code != 202 {
		t.Fatalf("create run: %d %s", w.Code, w.Body.String())
	}
	dec := waitForPendingDecision(t, srv, "draft", "2", 2*time.Second)
	if _, err := store.ResolveAgentDecision(ctx, srv.db, dec.ID, "skipped", `{"accepted":false,"added_count":0}`); err != nil {
		t.Fatalf("resolve skipped: %v", err)
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("status = %s (%s)", run.Status, run.ErrorMessage)
	}
}

func waitForPendingDecision(t *testing.T, srv *Server, entityType, entityID string, timeout time.Duration) store.AgentDecision {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var id string
		err := srv.db.QueryRowContext(context.Background(), `
			SELECT id FROM memento_agent_decision
			WHERE entity_type = ? AND entity_id = ? AND status = 'pending'
			ORDER BY created_at DESC LIMIT 1`, entityType, entityID).Scan(&id)
		if err == nil && id != "" {
			dec, err := store.GetAgentDecision(context.Background(), srv.db, id)
			if err == nil {
				return dec
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no pending decision found for %s/%s within %s", entityType, entityID, timeout)
	return store.AgentDecision{}
}
