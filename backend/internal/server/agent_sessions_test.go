package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"memento/backend/internal/msgvault"
	"memento/backend/internal/store"

	_ "modernc.org/sqlite"
)

func newTestServer(t *testing.T) (*Server, *sql.DB) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	// Match production behavior — modernc.org/sqlite gives each new connection
	// its own private :memory: DB, so multi-connection access from background
	// goroutines would otherwise hit empty schemas.
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	_, err = store.Migrate(ctx, db)
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	tempFile, err := os.CreateTemp("", "msgvault-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	t.Cleanup(func() { os.Remove(tempFile.Name()) })
	tempFile.Close()

	reader, err := msgvault.OpenReader(tempFile.Name())
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}

	opts := Options{
		Port: 8787,
	}
	srv := New(opts, db, reader)
	return srv, db
}

func TestAgentSessions_Workflow(t *testing.T) {
	os.Setenv("MEMENTO_INTERNAL_TOKEN", "test-secret-token")
	defer os.Unsetenv("MEMENTO_INTERNAL_TOKEN")

	srv, _ := newTestServer(t)

	// 1. Create Session
	reqBody := createAgentSessionRequest{
		SessionType: "collector",
		EntityID:    "draft_123",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/internal/agent-sessions", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w := httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var createResp createAgentSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if createResp.SessionID == 0 {
		t.Fatal("expected non-zero session ID")
	}

	// 2. Update Session
	updateReq := updateAgentSessionRequest{
		Status:        "succeeded",
		InteractionID: "int_123",
	}
	bodyBytes, _ = json.Marshal(updateReq)
	req = httptest.NewRequest("PATCH", "/api/internal/agent-sessions/"+strconv.FormatInt(createResp.SessionID, 10), bytes.NewReader(bodyBytes))
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w = httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var status, interactionID string
	if err := srv.db.QueryRowContext(context.Background(),
		`SELECT status, interaction_id FROM memento_agent_session WHERE id = ?`,
		createResp.SessionID,
	).Scan(&status, &interactionID); err != nil {
		t.Fatalf("failed to fetch session: %v", err)
	}
	if status != "succeeded" || interactionID != "int_123" {
		t.Fatalf("unexpected session state: status=%q interaction_id=%q", status, interactionID)
	}

	// 3. Log Loop
	loopReq := logAgentLoopRequest{
		StepIndex:       1,
		InputType:       "user_input",
		InputContent:    "hello world",
		AssistantText:   "hi",
		ReasoningText:   "thinking hard",
		ToolCallsJSON:   `[{"id":"call_1","name":"fts_search","args":{"query":"hello"}}]`,
		ToolResultsJSON: `[{"name":"fts_search","call_id":"call_1","result":[]}]`,
		DurationMs:      500,
	}
	bodyBytes, _ = json.Marshal(loopReq)
	req = httptest.NewRequest("POST", "/api/internal/agent-sessions/"+strconv.FormatInt(createResp.SessionID, 10)+"/loops", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w = httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	// 4. Get Logs
	req = httptest.NewRequest("GET", "/api/internal/agent-sessions/"+strconv.FormatInt(createResp.SessionID, 10)+"/logs", nil)
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w = httptest.NewRecorder()

	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var logsResp agentSessionLogsResponse
	if err := json.NewDecoder(w.Body).Decode(&logsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(logsResp.Loops) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logsResp.Loops))
	}
	if len(logsResp.ToolCalls) != 0 {
		t.Fatalf("expected no normalized tool calls from direct loop log, got %d", len(logsResp.ToolCalls))
	}

	log := logsResp.Loops[0]
	if log.StepIndex != 1 || log.InputType != "user_input" || log.InputContent != "hello world" ||
		log.AssistantText != "hi" || log.ReasoningText != "thinking hard" ||
		log.ToolCallsJSON != `[{"id":"call_1","name":"fts_search","args":{"query":"hello"}}]` ||
		log.ToolResultsJSON != `[{"name":"fts_search","call_id":"call_1","result":[]}]` ||
		log.DurationMs != 500 {
		t.Fatalf("unexpected log details: %+v", log)
	}
}

func TestLoadAgentSessionLogsIncludesAskSessionLink(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	run, err := store.CreateAgentRun(ctx, db, store.AgentRun{
		SessionType:  "dashboard",
		EntityID:     "dashboard",
		Status:       store.AgentRunSucceeded,
		RunInputJSON: `{"user_message":"What happened with Dynegy?"}`,
	})
	if err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}
	askSession, err := store.CreateAskSession(ctx, db, "Dynegy investigation")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	turn, err := store.AppendAskTurn(ctx, db, askSession.ID, "What happened with Dynegy?")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}
	if err := store.LinkAskTurnRun(ctx, db, turn.ID, run.ID); err != nil {
		t.Fatalf("LinkAskTurnRun: %v", err)
	}

	resp, err := srv.loadAgentSessionLogs(ctx, run.ID)
	if err != nil {
		t.Fatalf("loadAgentSessionLogs: %v", err)
	}
	if resp.AskSession == nil {
		t.Fatal("expected ask session debug link")
	}
	if resp.AskSession.ID != askSession.ID || resp.AskSession.Slug != askSession.Slug ||
		resp.AskSession.Title != askSession.Title || resp.AskSession.TurnID != turn.ID ||
		resp.AskSession.TurnIndex != turn.TurnIndex {
		t.Fatalf("unexpected ask session debug link: %+v", resp.AskSession)
	}
}

func TestPurgeAgentSessionDebugPreservesAskSessionAnswer(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	run, err := store.CreateAgentRun(ctx, db, store.AgentRun{
		SessionType:  "dashboard",
		EntityID:     "dashboard",
		Status:       store.AgentRunSucceeded,
		RunInputJSON: `{"user_message":"Who handled Dynegy?"}`,
	})
	if err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}
	askSession, err := store.CreateAskSession(ctx, db, "Dynegy investigation")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	turn, err := store.AppendAskTurn(ctx, db, askSession.ID, "Who handled Dynegy?")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}
	if err := store.LinkAskTurnRun(ctx, db, turn.ID, run.ID); err != nil {
		t.Fatalf("LinkAskTurnRun: %v", err)
	}
	if err := store.CompleteAskTurn(ctx, db, turn.ID, "It was discussed by the deal team [msg:4].", "It was discussed by the deal team.", "[4]", "{}"); err != nil {
		t.Fatalf("CompleteAskTurn: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_agent_loop (session_id, step_index, input_type, input_content)
		VALUES (?, 1, 'user_input', 'hello')`, run.ID); err != nil {
		t.Fatalf("seed loop: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_agent_event (session_id, seq, event_type)
		VALUES (?, 1, 'message')`, run.ID); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_agent_tool_call (session_id, step_index, call_index, tool_name)
		VALUES (?, 1, 0, 'fts_search')`, run.ID); err != nil {
		t.Fatalf("seed tool call: %v", err)
	}

	if err := srv.purgeAgentSessionDebug(ctx, run.ID); err != nil {
		t.Fatalf("purgeAgentSessionDebug: %v", err)
	}
	if _, err := store.GetAgentRun(ctx, db, run.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetAgentRun after purge err = %v, want sql.ErrNoRows", err)
	}
	gotTurn, err := store.GetAskTurn(ctx, db, turn.ID)
	if err != nil {
		t.Fatalf("GetAskTurn after purge: %v", err)
	}
	if gotTurn.RunID != nil {
		t.Fatalf("turn run_id = %v, want nil", *gotTurn.RunID)
	}
	if gotTurn.AssistantAnswer == "" || gotTurn.Status != "complete" {
		t.Fatalf("ask answer was not preserved: %+v", gotTurn)
	}
}
