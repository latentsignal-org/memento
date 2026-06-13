package agentrunner

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"memento/backend/internal/store"

	_ "modernc.org/sqlite"
)

type testRegistry map[string]Tool

func (r testRegistry) LookupTool(name string) (Tool, bool) {
	tool, ok := r[name]
	return tool, ok
}

type scriptedProvider struct {
	calls int
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Stream(_ context.Context, req ModelRequest, emit func(ModelEvent) error) error {
	p.calls++
	if req.Input.IsToolResult {
		if err := emit(ModelEvent{Type: ModelTextDelta, Text: "done"}); err != nil {
			return err
		}
		return emit(ModelEvent{Type: ModelDone, InteractionID: "interaction-2", ProviderState: json.RawMessage(`{"interaction_id":"interaction-2"}`), Usage: ModelUsage{InputTokens: 30, OutputTokens: 5, TotalTokens: 35}})
	}
	if err := emit(ModelEvent{Type: ModelToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "echo", Args: json.RawMessage(`{"value":"abc"}`)}}); err != nil {
		return err
	}
	return emit(ModelEvent{Type: ModelDone, InteractionID: "interaction-1", ProviderState: json.RawMessage(`{"interaction_id":"interaction-1"}`), Usage: ModelUsage{InputTokens: 20, OutputTokens: 3, TotalTokens: 23}})
}

func TestRunnerFakeProviderPersistsReplayableEvents(t *testing.T) {
	db := migratedRunnerDB(t)
	registry := testRegistry{
		"echo": {
			Schema: ToolSchema{Name: "echo", Type: "function", Parameters: json.RawMessage(`{"type":"object"}`)},
			Kind:   ToolReadOnly,
			Handler: func(_ context.Context, _ ToolContext, args json.RawMessage) (any, error) {
				var in map[string]any
				_ = json.Unmarshal(args, &in)
				return map[string]any{"echo": in["value"]}, nil
			},
		},
	}
	provider := &scriptedProvider{}
	runner := NewRunner(db, registry, provider)
	runID, err := runner.Start(context.Background(), RunSpec{
		AgentType:   AgentDashboard,
		EntityID:    "dashboard",
		UserMessage: "hello",
		Provider:    "scripted",
		Model:       "fake",
		System:      "test",
		Tools:       []ToolSchema{registry["echo"].Schema},
		MaxSteps:    3,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	waitForTerminal(t, db, runID)
	events, err := store.ListAgentEventsAfter(context.Background(), db, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var types []string
	for _, ev := range events {
		types = append(types, ev.EventType)
	}
	want := []string{"tool_call_start", "tool_call_result", "text_delta", "done"}
	if len(types) != len(want) {
		t.Fatalf("event count mismatch: got %v want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("event %d = %s, want %s (all: %v)", i, types[i], want[i], types)
		}
	}
	run, err := store.GetAgentRun(context.Background(), db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunSucceeded || run.InteractionID != "interaction-2" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.TotalModelInputTokens != 50 || run.TotalModelOutputTokens != 8 || run.TotalModelTokens != 58 {
		t.Fatalf("unexpected persisted usage: %+v", run)
	}
}

func TestRunnerCancel(t *testing.T) {
	db := migratedRunnerDB(t)
	runner := NewRunner(db, testRegistry{}, &FakeProvider{
		Events: []ModelEvent{{Type: ModelTextDelta, Text: "started"}},
	})
	runID, err := runner.Start(context.Background(), RunSpec{
		AgentType:   AgentDashboard,
		EntityID:    "dashboard",
		UserMessage: "hello",
		Provider:    "fake",
		Model:       "fake",
		System:      "test",
		MaxSteps:    2,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := runner.Cancel(context.Background(), runID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	run, err := store.GetAgentRun(context.Background(), db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}
}

func TestRunnerCancelDoesNotOverwriteSucceededRun(t *testing.T) {
	db := migratedRunnerDB(t)
	runner := NewRunner(db, testRegistry{}, &FakeProvider{})
	runID, err := runner.Start(context.Background(), RunSpec{
		AgentType:   AgentDashboard,
		EntityID:    "dashboard",
		UserMessage: "hello",
		Provider:    "fake",
		Model:       "fake",
		System:      "test",
		MaxSteps:    2,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, db, runID)
	if err := runner.Cancel(context.Background(), runID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	run, err := store.GetAgentRun(context.Background(), db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("status = %s, want succeeded", run.Status)
	}
}

func TestDispatchToolsPersistsStartAndResultEventsInCallOrder(t *testing.T) {
	db := migratedRunnerDB(t)
	run, err := store.CreateAgentRun(context.Background(), db, store.AgentRun{
		SessionType: "dashboard",
		EntityID:    "dashboard",
		Status:      store.AgentRunRunning,
		Provider:    "fake",
		Model:       "fake",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	registry := testRegistry{
		"slow": {
			Schema: ToolSchema{Name: "slow", Type: "function"},
			Kind:   ToolReadOnly,
			Handler: func(_ context.Context, _ ToolContext, _ json.RawMessage) (any, error) {
				time.Sleep(25 * time.Millisecond)
				return map[string]any{"name": "slow"}, nil
			},
		},
		"fast": {
			Schema: ToolSchema{Name: "fast", Type: "function"},
			Kind:   ToolReadOnly,
			Handler: func(_ context.Context, _ ToolContext, _ json.RawMessage) (any, error) {
				return map[string]any{"name": "fast"}, nil
			},
		},
	}
	runner := NewRunner(db, registry)
	_, err = runner.dispatchTools(context.Background(), run.ID, 7, RunSpec{AgentType: AgentDashboard, EntityID: "dashboard"}, []ToolCall{
		{ID: "call-1", Name: "slow", Args: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "fast", Args: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("dispatch tools: %v", err)
	}
	events, err := store.ListAgentEventsAfter(context.Background(), db, run.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var got []string
	for _, ev := range events {
		var payload map[string]any
		if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
		got = append(got, ev.EventType+":"+payload["name"].(string))
	}
	want := []string{"tool_call_start:slow", "tool_call_start:fast", "tool_call_result:slow", "tool_call_result:fast"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %s, want %s (all: %v)", i, got[i], want[i], got)
		}
	}
	traces, err := store.ListAgentToolCallTraces(context.Background(), db, run.ID)
	if err != nil {
		t.Fatalf("list traces: %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("traces len = %d, want 2", len(traces))
	}
	if traces[0].StepIndex != 7 || traces[0].CallIndex != 0 || traces[0].CallID != "call-1" || traces[0].ToolName != "slow" {
		t.Fatalf("unexpected first trace: %+v", traces[0])
	}
	if traces[1].StepIndex != 7 || traces[1].CallIndex != 1 || traces[1].CallID != "call-2" || traces[1].ToolName != "fast" {
		t.Fatalf("unexpected second trace: %+v", traces[1])
	}
	if traces[0].ParallelLimit <= 0 || traces[0].BatchSize != 2 || traces[0].StartedAt == "" || traces[0].FinishedAt == "" {
		t.Fatalf("trace missing concurrency/timing metadata: %+v", traces[0])
	}
}

// TestDispatchToolsSoftFailsToolErrors verifies that a tool error becomes a
// structured tool result the model can read, instead of aborting the run.
// This matches the standard agent-loop pattern and lets the model recover by
// trying a different tool.
func TestDispatchToolsSoftFailsToolErrors(t *testing.T) {
	db := migratedRunnerDB(t)
	run, err := store.CreateAgentRun(context.Background(), db, store.AgentRun{
		SessionType: "dashboard",
		EntityID:    "dashboard",
		Status:      store.AgentRunRunning,
		Provider:    "fake",
		Model:       "fake",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	registry := testRegistry{
		"failing": {
			Schema: ToolSchema{Name: "failing", Type: "function"},
			Kind:   ToolReadOnly,
			Handler: func(_ context.Context, _ ToolContext, _ json.RawMessage) (any, error) {
				return nil, fmt.Errorf("upstream service down")
			},
		},
		"ok": {
			Schema: ToolSchema{Name: "ok", Type: "function"},
			Kind:   ToolReadOnly,
			Handler: func(_ context.Context, _ ToolContext, _ json.RawMessage) (any, error) {
				return map[string]any{"name": "ok"}, nil
			},
		},
	}
	runner := NewRunner(db, registry)
	results, err := runner.dispatchTools(context.Background(), run.ID, 3, RunSpec{AgentType: AgentDashboard, EntityID: "dashboard"}, []ToolCall{
		{ID: "call-1", Name: "failing", Args: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "ok", Args: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("dispatch returned err: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %v, want 2", results)
	}
	failed, _ := results[0].Result.(map[string]any)
	if failed == nil || failed["error"] != "upstream service down" {
		t.Fatalf("expected error result for first call, got %+v", results[0])
	}
	okResult, _ := results[1].Result.(map[string]any)
	if okResult == nil || okResult["name"] != "ok" {
		t.Fatalf("expected ok result for second call, got %+v", results[1])
	}
	traces, err := store.ListAgentToolCallTraces(context.Background(), db, run.ID)
	if err != nil {
		t.Fatalf("list traces: %v", err)
	}
	if len(traces) != 2 {
		t.Fatalf("traces len = %d, want 2", len(traces))
	}
	if traces[0].ErrorMessage != "upstream service down" {
		t.Fatalf("first trace error = %q", traces[0].ErrorMessage)
	}
	if traces[0].ResultJSON == "" || !strings.Contains(traces[0].ResultJSON, "upstream service down") {
		t.Fatalf("first trace result json missing error: %s", traces[0].ResultJSON)
	}
}

type repairProvider struct {
	alwaysTextOnly bool
}

func (p *repairProvider) Name() string { return "repair" }

func (p *repairProvider) Stream(_ context.Context, req ModelRequest, emit func(ModelEvent) error) error {
	if req.Input.IsToolResult {
		if err := emit(ModelEvent{Type: ModelTextDelta, Text: "finished"}); err != nil {
			return err
		}
		return emit(ModelEvent{Type: ModelDone, InteractionID: "repair-done"})
	}
	if !p.alwaysTextOnly && req.StepIndex > 1 && strings.Contains(req.Input.UserMessage, "required persisted outputs are missing") {
		if err := emit(ModelEvent{Type: ModelToolCall, ToolCall: &ToolCall{
			ID:   "repair-call",
			Name: "write_section",
			Args: json.RawMessage(`{"section":"summary","content":"fixed","source_message_ids":[1]}`),
		}}); err != nil {
			return err
		}
		return emit(ModelEvent{Type: ModelDone, InteractionID: "repair-tool"})
	}
	if err := emit(ModelEvent{Type: ModelTextDelta, Text: "I am done without tools."}); err != nil {
		return err
	}
	return emit(ModelEvent{Type: ModelDone, InteractionID: "missing"})
}

func TestRunnerRepairsMissingRequiredOutcomeOnce(t *testing.T) {
	db := migratedRunnerDB(t)
	registry := testRegistry{
		"write_section": {
			Schema: ToolSchema{Name: "write_section", Type: "function"},
			Kind:   ToolMutating,
			Handler: func(_ context.Context, _ ToolContext, _ json.RawMessage) (any, error) {
				return map[string]any{"ok": true}, nil
			},
		},
	}
	runner := NewRunner(db, registry, &repairProvider{})
	runID, err := runner.Start(context.Background(), RunSpec{
		AgentType: AgentProjectCompile,
		EntityID:  "proj",
		Provider:  "repair",
		Model:     "fake",
		System:    "test",
		Tools:     []ToolSchema{registry["write_section"].Schema},
		MaxSteps:  4,
		RequiredOutcomes: []OutcomeRequirement{{
			ToolName:    "write_section",
			ArgEquals:   map[string]string{"section": "summary"},
			Description: "summary section must be written",
		}},
		MaxRepairAttempts: 1,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, db, runID)
	run, err := store.GetAgentRun(context.Background(), db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("status = %s (%s), want succeeded", run.Status, run.ErrorMessage)
	}
	types := eventTypesForRun(t, db, runID)
	for _, want := range []string{"requirements_status", "repair_started", "tool_call_start", "tool_call_result", "done"} {
		if !containsString(types, want) {
			t.Fatalf("events missing %s: %v", want, types)
		}
	}
}

func TestRunnerFailsAfterBoundedRequiredOutcomeRepair(t *testing.T) {
	db := migratedRunnerDB(t)
	runner := NewRunner(db, testRegistry{}, &repairProvider{alwaysTextOnly: true})
	runID, err := runner.Start(context.Background(), RunSpec{
		AgentType: AgentProjectCompile,
		EntityID:  "proj",
		Provider:  "repair",
		Model:     "fake",
		System:    "test",
		MaxSteps:  3,
		RequiredOutcomes: []OutcomeRequirement{{
			ToolName:    "write_section",
			ArgEquals:   map[string]string{"section": "summary"},
			Description: "summary section must be written",
		}},
		MaxRepairAttempts: 1,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, db, runID)
	run, err := store.GetAgentRun(context.Background(), db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunFailed {
		t.Fatalf("status = %s, want failed", run.Status)
	}
	if !strings.Contains(run.ErrorMessage, "after 1 repair attempt") {
		t.Fatalf("unexpected error: %s", run.ErrorMessage)
	}
	types := eventTypesForRun(t, db, runID)
	if countString(types, "repair_started") != 1 {
		t.Fatalf("repair attempts should be bounded to one, events: %v", types)
	}
	if containsString(types, "done") {
		t.Fatalf("failed repair run should not emit done: %v", types)
	}
}

func TestRunnerAllowClarifyingTextSucceedsDespiteMissingOutcomes(t *testing.T) {
	db := migratedRunnerDB(t)
	var capturedAssistantText string
	runner := NewRunner(db, testRegistry{}, &repairProvider{alwaysTextOnly: true})
	runID, err := runner.Start(context.Background(), RunSpec{
		AgentType: AgentCollector,
		EntityID:  "draft",
		Provider:  "repair",
		Model:     "fake",
		System:    "test",
		MaxSteps:  3,
		RequiredOutcomes: []OutcomeRequirement{{
			ToolName:    "propose_bundle",
			Description: "propose_bundle must stage the curated draft bundle",
		}},
		MaxRepairAttempts:   1,
		AllowClarifyingText: true,
		AfterDone: func(_ context.Context, done AfterDoneContext) error {
			capturedAssistantText = done.AssistantText
			return nil
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, db, runID)
	run, err := store.GetAgentRun(context.Background(), db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("status = %s (%s), want succeeded", run.Status, run.ErrorMessage)
	}
	if capturedAssistantText == "" {
		t.Fatalf("AfterDone should receive the clarifying assistant text")
	}
	types := eventTypesForRun(t, db, runID)
	if !containsString(types, "done") {
		t.Fatalf("clarify-only run must emit done event: %v", types)
	}
	if containsString(types, "requirements_status") || containsString(types, "repair_started") {
		t.Fatalf("clarify-only termination should skip the repair path, events: %v", types)
	}
}

func TestRunnerAllowClarifyingTextDoesNotBypassMissingOutcomesWhenTextEmpty(t *testing.T) {
	db := migratedRunnerDB(t)
	runner := NewRunner(db, testRegistry{}, &silentProvider{})
	runID, err := runner.Start(context.Background(), RunSpec{
		AgentType: AgentCollector,
		EntityID:  "draft",
		Provider:  "silent",
		Model:     "fake",
		System:    "test",
		MaxSteps:  3,
		RequiredOutcomes: []OutcomeRequirement{{
			ToolName:    "propose_bundle",
			Description: "propose_bundle must stage the curated draft bundle",
		}},
		MaxRepairAttempts:   1,
		AllowClarifyingText: true,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitForTerminal(t, db, runID)
	run, err := store.GetAgentRun(context.Background(), db, runID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.Status != store.AgentRunFailed {
		t.Fatalf("silent termination should still fail when outcomes missing, got %s", run.Status)
	}
}

type silentProvider struct{}

func (p *silentProvider) Name() string { return "silent" }

func (p *silentProvider) Stream(_ context.Context, req ModelRequest, emit func(ModelEvent) error) error {
	return emit(ModelEvent{Type: ModelDone, InteractionID: "silent"})
}

func TestOutcomeTrackerIgnoresSkippedToolResult(t *testing.T) {
	tracker := newOutcomeTracker([]OutcomeRequirement{{
		ToolName:      "write_person_section",
		ArgEquals:     map[string]string{"section": "summary"},
		RequiredCount: 1,
		Description:   "summary must persist",
	}})
	tracker.Record(
		[]ToolCall{{ID: "1", Name: "write_person_section", Args: json.RawMessage(`{"section":"summary"}`)}},
		[]ToolResult{{CallID: "1", Name: "write_person_section", Result: map[string]any{
			"ok":          true,
			"skipped":     true,
			"skip_reason": "section is user-edited; agent writes preserved as draft only",
		}}},
	)
	if missing := tracker.Missing(); len(missing) != 1 {
		t.Fatalf("skipped write should not satisfy outcome; missing = %v", missing)
	}
}

func migratedRunnerDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func waitForTerminal(t *testing.T, db *sql.DB, runID int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetAgentRun(context.Background(), db, runID)
		if err == nil && (run.Status == store.AgentRunSucceeded || run.Status == store.AgentRunFailed || run.Status == store.AgentRunCancelled) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, _ := store.GetAgentRun(context.Background(), db, runID)
	t.Fatalf("run did not finish: %+v", run)
}

func eventTypesForRun(t *testing.T, db *sql.DB, runID int64) []string {
	t.Helper()
	events, err := store.ListAgentEventsAfter(context.Background(), db, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.EventType)
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func countString(values []string, want string) int {
	var count int
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
}
