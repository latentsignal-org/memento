package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/store"
)

const testInternalToken = "test-secret-token"

// scriptedAgentProvider returns a deterministic event stream per Stream call.
// The supplied function receives the 1-based call index and the request and
// returns the events to emit. Tests use this to drive the runner without a
// real model.
type scriptedAgentProvider struct {
	name string
	mu   sync.Mutex
	step int32
	emit func(step int, req agentrunner.ModelRequest) []agentrunner.ModelEvent
}

func (p *scriptedAgentProvider) Name() string { return p.name }

func (p *scriptedAgentProvider) Stream(ctx context.Context, req agentrunner.ModelRequest, emit func(agentrunner.ModelEvent) error) error {
	step := int(atomic.AddInt32(&p.step, 1))
	p.mu.Lock()
	events := p.emit(step, req)
	p.mu.Unlock()
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := emit(ev); err != nil {
			return err
		}
	}
	return nil
}

// doneOnce returns a script that emits a text delta and a done event on the
// first call and refuses to be called a second time (which would mean the run
// looped unexpectedly).
func doneOnce(t *testing.T, interactionID, text string) func(int, agentrunner.ModelRequest) []agentrunner.ModelEvent {
	t.Helper()
	return func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
		if step > 1 {
			t.Errorf("provider invoked an unexpected extra step (%d)", step)
			return []agentrunner.ModelEvent{
				{Type: agentrunner.ModelDone, InteractionID: interactionID, ProviderState: rawJSON(`{}`)},
			}
		}
		return []agentrunner.ModelEvent{
			{Type: agentrunner.ModelTextDelta, Text: text},
			{Type: agentrunner.ModelDone, InteractionID: interactionID, ProviderState: rawJSON(fmt.Sprintf(`{"interaction_id":%q}`, interactionID))},
		}
	}
}

func successProviderForAgentType(agentType string) *scriptedAgentProvider {
	return &scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			if step > 1 {
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelTextDelta, Text: "Stub response."},
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(fmt.Sprintf(`{"interaction_id":%q}`, "fake-interaction"))},
				}
			}
			events := requiredToolEventsForAgentType(agentType)
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)})
			return events
		},
	}
}

func requiredToolEventsForAgentType(agentType string) []agentrunner.ModelEvent {
	switch agentType {
	case "collector":
		return []agentrunner.ModelEvent{
			{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
				ID:   "bundle-1",
				Name: "propose_bundle",
				Args: rawJSON(`{"name":"Test bundle","summary_hint":"Test","people":[],"messages":[],"threads":[]}`),
			}},
		}
	case "project_compile":
		sections := []string{"summary", "phases", "friction_points", "current_understanding"}
		events := make([]agentrunner.ModelEvent, 0, len(sections))
		for i, section := range sections {
			content := `Test content [msg:1]`
			if section == "phases" {
				content = `[{"title":"Phase","date_range":"2026","content":"Test phase [msg:1]","source_message_ids":[1]}]`
			}
			if section == "friction_points" {
				content = `[{"text":"Test friction [msg:1]","source_message_ids":[1]}]`
			}
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
				ID:   fmt.Sprintf("project-section-%d", i),
				Name: "write_section",
				Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":%q,"source_message_ids":[1]}`, section, content)),
			}})
		}
		return events
	case "concept_compile":
		sections := []string{"scope_summary", "distilled_insights", "evolving_understanding"}
		events := make([]agentrunner.ModelEvent, 0, len(sections))
		for i, section := range sections {
			content := `Test content [msg:1]`
			if section == "distilled_insights" {
				content = `[{"title":"Insight","content":"Test insight [msg:1]","source_message_ids":[1]}]`
			}
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
				ID:   fmt.Sprintf("concept-section-%d", i),
				Name: "write_concept_section",
				Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":%q,"source_message_ids":[1]}`, section, content)),
			}})
		}
		return events
	case "person_enrich":
		events := []agentrunner.ModelEvent{
			{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
				ID:   "person-facet-1",
				Name: "write_facet",
				Args: rawJSON(`{"facet_type":"fact","content":"Test generated facet [msg:1].","source_message_ids":[1],"confidence":1}`),
			}},
			{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
				ID:   "no-attrs",
				Name: "record_no_person_attributes",
				Args: rawJSON(`{"reason":"No strong evidence for structured attributes in test data."}`),
			}},
		}
		for i, section := range []string{"summary", "relationship_arc", "current_status"} {
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
				ID:   fmt.Sprintf("person-section-%d", i),
				Name: "write_person_section",
				Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":"Test content [msg:1].","source_message_ids":[1]}`, section)),
			}})
		}
		return events
	default:
		return nil
	}
}

func rawJSON(s string) json.RawMessage { return json.RawMessage(s) }

func setupIntegrationServer(t *testing.T) (*Server, func()) {
	t.Helper()
	old := os.Getenv("MEMENTO_INTERNAL_TOKEN")
	os.Setenv("MEMENTO_INTERNAL_TOKEN", testInternalToken)
	cleanup := func() {
		if old == "" {
			os.Unsetenv("MEMENTO_INTERNAL_TOKEN")
		} else {
			os.Setenv("MEMENTO_INTERNAL_TOKEN", old)
		}
	}
	srv, _ := newTestServer(t)
	return srv, cleanup
}

func postCreateAgentRun(t *testing.T, srv *Server, body any) (int64, *httptest.ResponseRecorder) {
	t.Helper()
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/agent-runs", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Internal-Token", testInternalToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		return 0, w
	}
	var resp struct {
		RunID     int64 `json:"run_id"`
		SessionID int64 `json:"session_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode run response: %v", err)
	}
	if resp.RunID == 0 || resp.RunID != resp.SessionID {
		t.Fatalf("expected non-zero run_id == session_id, got %+v", resp)
	}
	return resp.RunID, w
}

func waitForRunTerminal(t *testing.T, srv *Server, runID int64) store.AgentRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetAgentRun(context.Background(), srv.db, runID)
		if err == nil && (run.Status == store.AgentRunSucceeded || run.Status == store.AgentRunFailed || run.Status == store.AgentRunCancelled) {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	run, _ := store.GetAgentRun(context.Background(), srv.db, runID)
	t.Fatalf("run %d did not reach terminal status: %+v", runID, run)
	return run
}

func eventTypes(t *testing.T, srv *Server, runID int64) []string {
	t.Helper()
	events, err := store.ListAgentEventsAfter(context.Background(), srv.db, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.EventType)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestAgentRunsIntegration_AllAgentTypes(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()

	// Fixtures for each agent type.
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_draft (id, kind, name_hint, transcript_json, entities_json, status)
		VALUES (1, 'project', 'Test draft', '[{"role":"user","content":"hi"}]', '{}', 'collecting')`); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_project (id, slug, name, aliases, status, note)
		VALUES (10, 'test-project', 'Test Project', '[]', 'active', '')`); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_concept (id, slug, name, scope_description, status, seed_keywords)
		VALUES (20, 'test-concept', 'Test Concept', 'scope text', 'active', '[]')`); err != nil {
		t.Fatalf("seed concept: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com')`); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person')`); err != nil {
		t.Fatalf("seed people report: %v", err)
	}

	// Register a fake-provider override that emits a clean text+done sequence.
	provider := &scriptedAgentProvider{
		name: "fake",
		emit: doneOnce(t, "fake-interaction", "Stub response."),
	}
	srv.agents.RegisterProvider(provider)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"collector", map[string]any{
			"agent_type":   "collector",
			"entity_id":    "1",
			"user_message": "tell me about test",
			"provider":     "fake",
			"model":        "fake",
		}},
		{"project_compile", map[string]any{
			"agent_type": "project_compile",
			"entity_id":  "test-project",
			"provider":   "fake",
			"model":      "fake",
		}},
		{"concept_compile", map[string]any{
			"agent_type": "concept_compile",
			"entity_id":  "test-concept",
			"provider":   "fake",
			"model":      "fake",
		}},
		{"person_enrich", map[string]any{
			"agent_type": "person_enrich",
			"entity_id":  "test-person",
			"provider":   "fake",
			"model":      "fake",
		}},
		{"dashboard", map[string]any{
			"agent_type":   "dashboard",
			"entity_id":    "dashboard",
			"user_message": "hello memento",
			"provider":     "fake",
			"model":        "fake",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider.mu.Lock()
			atomic.StoreInt32(&provider.step, 0)
			provider.mu.Unlock()

			if tc.name == "dashboard" {
				srv.agents.RegisterProvider(provider)
			} else {
				srv.agents.RegisterProvider(successProviderForAgentType(tc.name))
			}

			runID, w := postCreateAgentRun(t, srv, tc.body)
			if w.Code != http.StatusAccepted {
				t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
			}
			run := waitForRunTerminal(t, srv, runID)
			if run.Status != store.AgentRunSucceeded {
				t.Fatalf("run status = %s (%s)", run.Status, run.ErrorMessage)
			}
			if run.InteractionID != "fake-interaction" {
				t.Fatalf("interaction_id = %q, want fake-interaction", run.InteractionID)
			}
			types := eventTypes(t, srv, runID)
			if !contains(types, "text_delta") {
				t.Fatalf("expected text_delta in event log, got %v", types)
			}
			if !contains(types, "done") {
				t.Fatalf("expected done in event log, got %v", types)
			}
		})
	}
}

func TestAgentRunsIntegration_PersonEnrichFailsWithoutOutput(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com')`); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person')`); err != nil {
		t.Fatalf("seed people report: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person_facet (person_id, facet_type, content, source_message_ids, edited_by)
		VALUES (30, 'fact', 'Existing facet [msg:1].', '[1]', 'llm')`); err != nil {
		t.Fatalf("seed existing facet: %v", err)
	}

	srv.agents.RegisterProvider(&scriptedAgentProvider{
		name: "fake",
		emit: func(_ int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			return []agentrunner.ModelEvent{
				{Type: agentrunner.ModelTextDelta, Text: "No persisted output."},
				{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
			}
		},
	})

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type": "person_enrich",
		"entity_id":  "test-person",
		"provider":   "fake",
		"model":      "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunFailed {
		t.Fatalf("run status = %s, want failed", run.Status)
	}
	if !strings.Contains(run.ErrorMessage, "without required outcomes after 1 repair attempt") {
		t.Fatalf("unexpected error message: %q", run.ErrorMessage)
	}
	if contains(eventTypes(t, srv, runID), "done") {
		t.Fatalf("failed run should not emit done")
	}
	var count int
	if err := srv.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM memento_person_facet WHERE person_id = 30 AND content = 'Existing facet [msg:1].'
	`).Scan(&count); err != nil {
		t.Fatalf("count existing facet: %v", err)
	}
	if count != 1 {
		t.Fatalf("failed run deleted existing facet; count = %d", count)
	}
}

func TestAgentRunsIntegration_PersonEnrichBootstrapAndDeferredCleanup(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com');
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person');
		INSERT INTO memento_person_facet (id, person_id, facet_type, content, source_message_ids, edited_by)
		VALUES
			(100, 30, 'fact', 'Old LLM facet [msg:1].', '[1]', 'llm'),
			(101, 30, 'fact', 'User facet [msg:1].', '[1]', 'user');
		INSERT INTO memento_person_attribute (id, person_id, category, label, value, source_message_ids, edited_by)
		VALUES
			(200, 30, 'preference', 'Old', 'Old LLM attribute', '[1]', 'llm'),
			(201, 30, 'preference', 'User', 'User attribute', '[1]', 'user');
	`); err != nil {
		t.Fatalf("seed person memory: %v", err)
	}

	srv.agents.RegisterProvider(&scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			if step > 1 {
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			}
			events := []agentrunner.ModelEvent{
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID:   "person-facet-new",
					Name: "write_facet",
					Args: rawJSON(`{"facet_type":"fact","content":"New LLM facet [msg:1].","source_message_ids":[1],"confidence":1}`),
				}},
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID:   "person-attr-new",
					Name: "write_person_attribute",
					Args: rawJSON(`{"category":"preference","label":"New","value":"New LLM attribute","source_message_ids":[1],"confidence":1}`),
				}},
			}
			for i, section := range []string{"summary", "relationship_arc", "current_status"} {
				events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID:   fmt.Sprintf("person-section-%d", i),
					Name: "write_person_section",
					Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":"Test content [msg:1].","source_message_ids":[1]}`, section)),
				}})
			}
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)})
			return events
		},
	})

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type": "person_enrich",
		"entity_id":  "test-person",
		"provider":   "fake",
		"model":      "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("run status = %s (%s)", run.Status, run.ErrorMessage)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(run.RequestMetadataJSON), &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if _, ok := meta["person_enrich_bootstrap"]; !ok {
		t.Fatalf("metadata missing person_enrich_bootstrap: %s", run.RequestMetadataJSON)
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{`SELECT COUNT(*) FROM memento_person_facet WHERE id = 100`, 0},
		{`SELECT COUNT(*) FROM memento_person_facet WHERE id = 101`, 1},
		{`SELECT COUNT(*) FROM memento_person_facet WHERE content = 'New LLM facet [msg:1].'`, 1},
		{`SELECT COUNT(*) FROM memento_person_attribute WHERE id = 200`, 0},
		{`SELECT COUNT(*) FROM memento_person_attribute WHERE id = 201`, 1},
		{`SELECT COUNT(*) FROM memento_person_attribute WHERE value = 'New LLM attribute'`, 1},
	} {
		var got int
		if err := srv.db.QueryRowContext(ctx, tc.query).Scan(&got); err != nil {
			t.Fatalf("query %q: %v", tc.query, err)
		}
		if got != tc.want {
			t.Fatalf("query %q got %d, want %d", tc.query, got, tc.want)
		}
	}
}

func TestAgentRunsIntegration_ToolCallResultHasDuration(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	// Script a provider that requests one tool call on the first step, then
	// finishes on the second step once it receives the tool result. The tool
	// itself may return an error result against the empty test archive; we only
	// care that a tool_call_result event is emitted carrying duration_ms.
	provider := &scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			if step == 1 {
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
						ID:   "call-1",
						Name: "fts_search",
						Args: rawJSON(`{"query":"hello"}`),
					}},
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			}
			return []agentrunner.ModelEvent{
				{Type: agentrunner.ModelTextDelta, Text: "done"},
				{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
			}
		},
	}
	srv.agents.RegisterProvider(provider)

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type":   "dashboard",
		"entity_id":    "dashboard",
		"user_message": "search please",
		"provider":     "fake",
		"model":        "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("run status = %s (%s)", run.Status, run.ErrorMessage)
	}

	events, err := store.ListAgentEventsAfter(context.Background(), srv.db, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.EventType != "tool_call_result" {
			continue
		}
		found = true
		var payload map[string]any
		if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
			t.Fatalf("unmarshal tool_call_result payload: %v", err)
		}
		if _, ok := payload["duration_ms"]; !ok {
			t.Fatalf("tool_call_result event missing duration_ms: %s", ev.PayloadJSON)
		}
		if d, ok := payload["duration_ms"].(float64); !ok || d < 0 {
			t.Fatalf("tool_call_result duration_ms not a non-negative number: %v", payload["duration_ms"])
		}
	}
	if !found {
		t.Fatalf("expected a tool_call_result event, got %v", eventTypes(t, srv, runID))
	}
}

func TestAgentRunsIntegration_UnauthorizedWithoutToken(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	body, _ := json.Marshal(map[string]any{
		"agent_type":   "dashboard",
		"entity_id":    "dashboard",
		"user_message": "hi",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/agent-runs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAgentRunsIntegration_InvalidAgentType(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()
	body, _ := json.Marshal(map[string]any{
		"agent_type":   "bogus",
		"entity_id":    "x",
		"user_message": "hi",
		"provider":     "fake",
		"model":        "fake",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/agent-runs", bytes.NewReader(body))
	req.Header.Set("X-Internal-Token", testInternalToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported agent_type, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported agent_type") {
		t.Fatalf("expected unsupported message, got %s", w.Body.String())
	}
}

func TestAgentRunsIntegration_PersonEnrichRecordNoAttributes(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com');
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv.agents.RegisterProvider(&scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			if step > 1 {
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			}
			events := []agentrunner.ModelEvent{
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID: "person-facet-1", Name: "write_facet",
					Args: rawJSON(`{"facet_type":"fact","content":"Fact [msg:1].","source_message_ids":[1],"confidence":1}`),
				}},
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID: "no-attrs", Name: "record_no_person_attributes",
					Args: rawJSON(`{"reason":"No strong evidence in test data."}`),
				}},
			}
			for i, section := range []string{"summary", "relationship_arc", "current_status"} {
				events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID:   fmt.Sprintf("person-section-%d", i),
					Name: "write_person_section",
					Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":"Test content [msg:1].","source_message_ids":[1]}`, section)),
				}})
			}
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)})
			return events
		},
	})

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type": "person_enrich",
		"entity_id":  "test-person",
		"provider":   "fake",
		"model":      "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("run status = %s (%s)", run.Status, run.ErrorMessage)
	}
}

func TestAgentRunsIntegration_PersonEnrichFirstEnrichMode(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com');
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv.agents.RegisterProvider(&scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			if step > 1 {
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			}
			events := []agentrunner.ModelEvent{
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID: "person-facet-1", Name: "write_facet",
					Args: rawJSON(`{"facet_type":"fact","content":"Fact [msg:1].","source_message_ids":[1],"confidence":1}`),
				}},
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID: "no-attrs", Name: "record_no_person_attributes",
					Args: rawJSON(`{"reason":"No strong evidence."}`),
				}},
			}
			for i, section := range []string{"summary", "relationship_arc", "current_status"} {
				events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID:   fmt.Sprintf("person-section-%d", i),
					Name: "write_person_section",
					Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":"First content [msg:1].","source_message_ids":[1]}`, section)),
				}})
			}
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)})
			return events
		},
	})

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type": "person_enrich",
		"entity_id":  "test-person",
		"provider":   "fake",
		"model":      "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("run status = %s (%s)", run.Status, run.ErrorMessage)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(run.RequestMetadataJSON), &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	mode, _ := meta["person_enrich_generation_mode"].(string)
	if mode != "first_enrich" {
		t.Fatalf("expected first_enrich mode, got %q (metadata: %s)", mode, run.RequestMetadataJSON)
	}
}

func TestAgentRunsIntegration_PersonEnrichRegenerateWithUserEditedNarrative(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com');
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person');
		INSERT INTO memento_person_narrative (person_id, section, content, source_message_ids, edited_by)
		VALUES (30, 'summary', 'User-edited summary [msg:1].', '[1]', 'user');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv.agents.RegisterProvider(&scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			if step > 1 {
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			}
			events := []agentrunner.ModelEvent{
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID: "person-facet-1", Name: "write_facet",
					Args: rawJSON(`{"facet_type":"fact","content":"Fact [msg:1].","source_message_ids":[1],"confidence":1}`),
				}},
				{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID: "no-attrs", Name: "record_no_person_attributes",
					Args: rawJSON(`{"reason":"No evidence."}`),
				}},
			}
			for i, section := range []string{"summary", "relationship_arc", "current_status"} {
				events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
					ID:   fmt.Sprintf("person-section-%d", i),
					Name: "write_person_section",
					Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":"Test content [msg:1].","source_message_ids":[1]}`, section)),
				}})
			}
			events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)})
			return events
		},
	})

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type": "person_enrich",
		"entity_id":  "test-person",
		"provider":   "fake",
		"model":      "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("run status = %s (%s)", run.Status, run.ErrorMessage)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(run.RequestMetadataJSON), &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	mode, _ := meta["person_enrich_generation_mode"].(string)
	if mode != "regenerate" {
		t.Fatalf("expected regenerate mode, got %q (metadata: %s)", mode, run.RequestMetadataJSON)
	}

	// User-edited summary must be preserved unchanged.
	var content string
	err := srv.db.QueryRowContext(ctx, `
		SELECT content FROM memento_person_narrative
		WHERE person_id = 30 AND section = 'summary' AND edited_by = 'user'
	`).Scan(&content)
	if err != nil {
		t.Fatalf("query user narrative: %v", err)
	}
	if content != "User-edited summary [msg:1]." {
		t.Fatalf("user-edited summary changed: %q", content)
	}

	// A new LLM-written summary should NOT exist (user-edited blocks it).
	var llmCount int
	err = srv.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM memento_person_narrative
		WHERE person_id = 30 AND section = 'summary' AND edited_by = 'llm'
	`).Scan(&llmCount)
	if err != nil {
		t.Fatalf("query llm narrative: %v", err)
	}
	if llmCount != 0 {
		t.Fatalf("expected no LLM summary, found %d", llmCount)
	}

	// Other sections should have been written by the LLM.
	var arcCount int
	err = srv.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM memento_person_narrative
		WHERE person_id = 30 AND section = 'relationship_arc' AND edited_by = 'llm'
	`).Scan(&arcCount)
	if err != nil {
		t.Fatalf("query arc: %v", err)
	}
	if arcCount != 1 {
		t.Fatalf("expected 1 LLM relationship_arc, found %d", arcCount)
	}

	events, err := store.ListAgentEventsAfter(ctx, srv.db, runID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var sawSkippedSummary bool
	for _, ev := range events {
		if ev.EventType != "tool_call_result" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(ev.PayloadJSON), &payload); err != nil {
			t.Fatalf("unmarshal tool_call_result: %v", err)
		}
		if payload["name"] != "write_person_section" {
			continue
		}
		result, _ := payload["result"].(map[string]any)
		if result == nil {
			continue
		}
		if result["skipped"] == true && result["skip_reason"] != "" {
			sawSkippedSummary = true
		}
	}
	if !sawSkippedSummary {
		t.Fatalf("expected skipped write_person_section tool result with skip_reason in events")
	}
}

func personEnrichPartialEventsWithoutAttrDecision() []agentrunner.ModelEvent {
	events := []agentrunner.ModelEvent{
		{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
			ID: "person-facet-1", Name: "write_facet",
			Args: rawJSON(`{"facet_type":"fact","content":"Fact [msg:1].","source_message_ids":[1],"confidence":1}`),
		}},
	}
	for i, section := range []string{"summary", "relationship_arc", "current_status"} {
		events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
			ID:   fmt.Sprintf("person-section-%d", i),
			Name: "write_person_section",
			Args: rawJSON(fmt.Sprintf(`{"section":%q,"content":"Test content [msg:1].","source_message_ids":[1]}`, section)),
		}})
	}
	events = append(events, agentrunner.ModelEvent{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)})
	return events
}

func TestAgentRunsIntegration_PersonEnrichRepairMissingAttributeDecision(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com');
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv.agents.RegisterProvider(&scriptedAgentProvider{
		name: "fake",
		emit: func(step int, _ agentrunner.ModelRequest) []agentrunner.ModelEvent {
			switch step {
			case 1:
				return personEnrichPartialEventsWithoutAttrDecision()
			case 2:
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelTextDelta, Text: "Finished without attribute decision."},
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			case 3:
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelToolCall, ToolCall: &agentrunner.ToolCall{
						ID: "no-attrs", Name: "record_no_person_attributes",
						Args: rawJSON(`{"reason":"No strong evidence after repair."}`),
					}},
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			default:
				return []agentrunner.ModelEvent{
					{Type: agentrunner.ModelDone, InteractionID: "fake-interaction", ProviderState: rawJSON(`{}`)},
				}
			}
		},
	})

	runID, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type": "person_enrich",
		"entity_id":  "test-person",
		"provider":   "fake",
		"model":      "fake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	run := waitForRunTerminal(t, srv, runID)
	if run.Status != store.AgentRunSucceeded {
		t.Fatalf("run status = %s (%s)", run.Status, run.ErrorMessage)
	}
	if !contains(eventTypes(t, srv, runID), "repair_started") {
		t.Fatalf("expected repair_started event, got %v", eventTypes(t, srv, runID))
	}
}

func TestAgentRunsIntegration_PersonEnrichRejectsConcurrentRun(t *testing.T) {
	srv, cleanup := setupIntegrationServer(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (30, 'Test Person', 'tp@example.com');
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, slug
		) VALUES (30, 'Test Person', 'tp@example.com', 'example.com', 1, 1, 0, 0, 0.0, 'candidate', 'test-person');
		INSERT INTO memento_agent_session (session_type, entity_id, status, provider, model)
		VALUES ('person_enrich', 'test-person', 'running', 'fake', 'fake');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, w := postCreateAgentRun(t, srv, map[string]any{
		"agent_type": "person_enrich",
		"entity_id":  "test-person",
		"provider":   "fake",
		"model":      "fake",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["active_run_id"] == nil {
		t.Fatalf("expected active_run_id in conflict response: %s", w.Body.String())
	}
}

// helper for FormatInt in case strconv import is otherwise unused.
var _ = strconv.FormatInt
