package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newMigratedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestMigrateAgentrunnerDurability(t *testing.T) {
	db := newMigratedTestDB(t)
	ctx := context.Background()

	for _, col := range []string{
		"provider",
		"model",
		"run_input_json",
		"request_metadata_json",
		"provider_state_json",
		"error_message",
		"heartbeat_at",
		"started_at",
		"finished_at",
		"total_estimated_input_tokens",
		"total_estimated_output_tokens",
		"total_estimated_tool_result_tokens",
		"total_model_input_tokens",
		"total_model_output_tokens",
		"total_model_tokens",
	} {
		var found int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pragma_table_info('memento_agent_session')
			WHERE name = ?`, col).Scan(&found); err != nil {
			t.Fatalf("pragma table_info %s: %v", col, err)
		}
		if found != 1 {
			t.Fatalf("column %s not found on memento_agent_session", col)
		}
	}

	for _, col := range []string{
		"estimated_input_tokens",
		"estimated_output_tokens",
		"estimated_tool_result_tokens",
		"model_input_tokens",
		"model_output_tokens",
		"model_total_tokens",
		"usage_json",
	} {
		var found int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pragma_table_info('memento_agent_loop')
			WHERE name = ?`, col).Scan(&found); err != nil {
			t.Fatalf("pragma table_info loop %s: %v", col, err)
		}
		if found != 1 {
			t.Fatalf("column %s not found on memento_agent_loop", col)
		}
	}

	var eventsTable int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'memento_agent_event'`).Scan(&eventsTable); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if eventsTable != 1 {
		t.Fatalf("memento_agent_event table not found")
	}

	var toolCallTable int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'memento_agent_tool_call'`).Scan(&toolCallTable); err != nil {
		t.Fatalf("query sqlite_master tool call: %v", err)
	}
	if toolCallTable != 1 {
		t.Fatalf("memento_agent_tool_call table not found")
	}
	for _, col := range []string{
		"session_id",
		"step_index",
		"call_index",
		"call_id",
		"tool_name",
		"tool_kind",
		"lock_key",
		"args_json",
		"result_json",
		"error_message",
		"queued_at",
		"started_at",
		"finished_at",
		"duration_ms",
		"queue_wait_ms",
		"lock_wait_ms",
		"parallel_limit",
		"batch_size",
		"args_bytes",
		"result_bytes",
		"estimated_result_tokens",
	} {
		var found int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pragma_table_info('memento_agent_tool_call')
			WHERE name = ?`, col).Scan(&found); err != nil {
			t.Fatalf("pragma table_info tool call %s: %v", col, err)
		}
		if found != 1 {
			t.Fatalf("column %s not found on memento_agent_tool_call", col)
		}
	}
}

func TestAgentRunStatusAndEventReplay(t *testing.T) {
	db := newMigratedTestDB(t)
	ctx := context.Background()

	run, err := CreateAgentRun(ctx, db, AgentRun{
		SessionType:         "collector",
		EntityID:            "42",
		Status:              AgentRunQueued,
		Provider:            "fake",
		Model:               "fake-model",
		RunInputJSON:        `{"message":"hello"}`,
		RequestMetadataJSON: `{"route":"/api/drafts/42/turn"}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.ID == 0 {
		t.Fatalf("expected run id")
	}
	if run.Provider != "fake" || run.Model != "fake-model" || run.Status != AgentRunQueued {
		t.Fatalf("unexpected run fields: %+v", run)
	}

	first, err := AppendAgentEvent(ctx, db, run.ID, "text_delta", `{"text":"hi"}`)
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	second, err := AppendAgentEvent(ctx, db, run.ID, "done", `{"interaction_id":"abc"}`)
	if err != nil {
		t.Fatalf("append second event: %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("unexpected seqs: first=%d second=%d", first.Seq, second.Seq)
	}

	events, err := ListAgentEventsAfter(ctx, db, run.ID, 0)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	events, err = ListAgentEventsAfter(ctx, db, run.ID, 1)
	if err != nil {
		t.Fatalf("list events after 1: %v", err)
	}
	if len(events) != 1 || events[0].EventType != "done" {
		t.Fatalf("unexpected replay after seq 1: %+v", events)
	}

	if err := UpdateAgentRunStatus(ctx, db, run.ID, AgentRunRunning, "", `{}`, ""); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := UpdateAgentRunStatus(ctx, db, run.ID, AgentRunSucceeded, "abc", `{"interaction_id":"abc"}`, ""); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	updated, err := GetAgentRun(ctx, db, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.Status != AgentRunSucceeded || updated.InteractionID != "abc" || updated.FinishedAt == "" {
		t.Fatalf("unexpected updated run: %+v", updated)
	}

	if err := AddAgentRunUsage(ctx, db, run.ID, AgentUsageDelta{
		EstimatedInputTokens:      10,
		EstimatedOutputTokens:     3,
		EstimatedToolResultTokens: 7,
		ModelInputTokens:          8,
		ModelOutputTokens:         2,
		ModelTotalTokens:          10,
	}); err != nil {
		t.Fatalf("add usage: %v", err)
	}
	updated, err = GetAgentRun(ctx, db, run.ID)
	if err != nil {
		t.Fatalf("get run with usage: %v", err)
	}
	if updated.TotalEstimatedInputTokens != 10 ||
		updated.TotalEstimatedOutputTokens != 3 ||
		updated.TotalEstimatedToolResultTokens != 7 ||
		updated.TotalModelInputTokens != 8 ||
		updated.TotalModelOutputTokens != 2 ||
		updated.TotalModelTokens != 10 {
		t.Fatalf("unexpected usage totals: %+v", updated)
	}
}

func TestAgentRunStatusOnlyPreservesProviderState(t *testing.T) {
	db := newMigratedTestDB(t)
	ctx := context.Background()
	run, err := CreateAgentRun(ctx, db, AgentRun{
		SessionType:       "collector",
		EntityID:          "42",
		InteractionID:     "interaction-1",
		Status:            AgentRunRunning,
		Provider:          "gemini",
		Model:             "model",
		ProviderStateJSON: `{"interaction_id":"interaction-1"}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := UpdateAgentRunStatusOnly(ctx, db, run.ID, AgentRunWaitingForUser); err != nil {
		t.Fatalf("status only waiting: %v", err)
	}
	if err := UpdateAgentRunStatusOnly(ctx, db, run.ID, AgentRunRunning); err != nil {
		t.Fatalf("status only running: %v", err)
	}
	updated, err := GetAgentRun(ctx, db, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.InteractionID != "interaction-1" || updated.ProviderStateJSON != `{"interaction_id":"interaction-1"}` {
		t.Fatalf("status-only update clobbered durable provider state: %+v", updated)
	}
}

func TestCancelAgentRunDoesNotOverwriteTerminalRun(t *testing.T) {
	db := newMigratedTestDB(t)
	ctx := context.Background()
	run, err := CreateAgentRun(ctx, db, AgentRun{
		SessionType:       "dashboard",
		EntityID:          "dashboard",
		Status:            AgentRunRunning,
		InteractionID:     "interaction-1",
		ProviderStateJSON: `{"interaction_id":"interaction-1"}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	finished, err := FinishAgentRun(ctx, db, run.ID, AgentRunSucceeded, "interaction-2", `{"interaction_id":"interaction-2"}`, "")
	if err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if !finished {
		t.Fatalf("expected finish to update row")
	}
	cancelled, err := CancelAgentRun(ctx, db, run.ID)
	if err != nil {
		t.Fatalf("cancel terminal run: %v", err)
	}
	if cancelled {
		t.Fatalf("cancel should not update an already-terminal run")
	}
	updated, err := GetAgentRun(ctx, db, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if updated.Status != AgentRunSucceeded || updated.InteractionID != "interaction-2" || updated.ProviderStateJSON != `{"interaction_id":"interaction-2"}` {
		t.Fatalf("cancel overwrote terminal run: %+v", updated)
	}
}

func TestFailStaleAgentRunsOnlyMarksOldNonTerminalRuns(t *testing.T) {
	db := newMigratedTestDB(t)
	ctx := context.Background()
	stale, err := CreateAgentRun(ctx, db, AgentRun{
		SessionType: "dashboard",
		EntityID:    "dashboard",
		Status:      AgentRunRunning,
	})
	if err != nil {
		t.Fatalf("create stale run: %v", err)
	}
	fresh, err := CreateAgentRun(ctx, db, AgentRun{
		SessionType: "dashboard",
		EntityID:    "dashboard",
		Status:      AgentRunRunning,
	})
	if err != nil {
		t.Fatalf("create fresh run: %v", err)
	}
	done, err := CreateAgentRun(ctx, db, AgentRun{
		SessionType: "dashboard",
		EntityID:    "dashboard",
		Status:      AgentRunSucceeded,
	})
	if err != nil {
		t.Fatalf("create terminal run: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE memento_agent_session
		SET heartbeat_at = datetime('now', '-10 minutes'),
		    updated_at = datetime('now', '-10 minutes')
		WHERE id IN (?, ?)`, stale.ID, done.ID); err != nil {
		t.Fatalf("age runs: %v", err)
	}
	n, err := FailStaleAgentRuns(ctx, db, time.Minute)
	if err != nil {
		t.Fatalf("fail stale: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked %d runs, want 1", n)
	}
	staleRun, _ := GetAgentRun(ctx, db, stale.ID)
	freshRun, _ := GetAgentRun(ctx, db, fresh.ID)
	doneRun, _ := GetAgentRun(ctx, db, done.ID)
	if staleRun.Status != AgentRunFailed {
		t.Fatalf("stale status = %s, want failed", staleRun.Status)
	}
	if freshRun.Status != AgentRunRunning {
		t.Fatalf("fresh status = %s, want running", freshRun.Status)
	}
	if doneRun.Status != AgentRunSucceeded {
		t.Fatalf("terminal status = %s, want succeeded", doneRun.Status)
	}
}

func TestActiveAgentRunForEntity(t *testing.T) {
	ctx := context.Background()
	db := newMigratedTestDB(t)
	run, err := CreateAgentRun(ctx, db, AgentRun{
		SessionType: "person_enrich",
		EntityID:    "alice",
		Status:      AgentRunRunning,
		Provider:    "fake",
		Model:       "fake",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	activeID, err := ActiveAgentRunForEntity(ctx, db, "person_enrich", "alice")
	if err != nil {
		t.Fatalf("active lookup: %v", err)
	}
	if activeID != run.ID {
		t.Fatalf("activeID = %d, want %d", activeID, run.ID)
	}
	if err := UpdateAgentRunStatus(ctx, db, run.ID, AgentRunSucceeded, "", `{}`, ""); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	activeID, err = ActiveAgentRunForEntity(ctx, db, "person_enrich", "alice")
	if err != nil {
		t.Fatalf("active lookup after finish: %v", err)
	}
	if activeID != 0 {
		t.Fatalf("activeID after finish = %d, want 0", activeID)
	}
}
