package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	AgentRunQueued         = "queued"
	AgentRunRunning        = "running"
	AgentRunWaitingForUser = "waiting_for_user"
	AgentRunSucceeded      = "succeeded"
	AgentRunFailed         = "failed"
	AgentRunCancelled      = "cancelled"
)

type AgentRun struct {
	ID                             int64  `json:"id"`
	SessionType                    string `json:"session_type"`
	EntityID                       string `json:"entity_id"`
	InteractionID                  string `json:"interaction_id"`
	Status                         string `json:"status"`
	Provider                       string `json:"provider"`
	Model                          string `json:"model"`
	RunInputJSON                   string `json:"run_input_json"`
	RequestMetadataJSON            string `json:"request_metadata_json"`
	ProviderStateJSON              string `json:"provider_state_json"`
	ErrorMessage                   string `json:"error_message"`
	TotalEstimatedInputTokens      int64  `json:"total_estimated_input_tokens"`
	TotalEstimatedOutputTokens     int64  `json:"total_estimated_output_tokens"`
	TotalEstimatedToolResultTokens int64  `json:"total_estimated_tool_result_tokens"`
	TotalModelInputTokens          int64  `json:"total_model_input_tokens"`
	TotalModelOutputTokens         int64  `json:"total_model_output_tokens"`
	TotalModelTokens               int64  `json:"total_model_tokens"`
	CreatedAt                      string `json:"created_at"`
	UpdatedAt                      string `json:"updated_at"`
	HeartbeatAt                    string `json:"heartbeat_at"`
	StartedAt                      string `json:"started_at"`
	FinishedAt                     string `json:"finished_at"`
}

type AgentUsageDelta struct {
	EstimatedInputTokens      int64 `json:"estimated_input_tokens"`
	EstimatedOutputTokens     int64 `json:"estimated_output_tokens"`
	EstimatedToolResultTokens int64 `json:"estimated_tool_result_tokens"`
	ModelInputTokens          int64 `json:"model_input_tokens"`
	ModelOutputTokens         int64 `json:"model_output_tokens"`
	ModelTotalTokens          int64 `json:"model_total_tokens"`
}

type AgentEvent struct {
	ID          int64  `json:"id"`
	SessionID   int64  `json:"session_id"`
	Seq         int64  `json:"seq"`
	EventType   string `json:"event_type"`
	PayloadJSON string `json:"payload_json"`
	CreatedAt   string `json:"created_at"`
}

type AgentToolCallTrace struct {
	ID                    int64  `json:"id"`
	SessionID             int64  `json:"session_id"`
	StepIndex             int    `json:"step_index"`
	CallIndex             int    `json:"call_index"`
	CallID                string `json:"call_id"`
	ToolName              string `json:"tool_name"`
	ToolKind              string `json:"tool_kind"`
	LockKey               string `json:"lock_key"`
	ArgsJSON              string `json:"args_json"`
	ResultJSON            string `json:"result_json"`
	ErrorMessage          string `json:"error_message"`
	QueuedAt              string `json:"queued_at"`
	StartedAt             string `json:"started_at"`
	FinishedAt            string `json:"finished_at"`
	DurationMs            int64  `json:"duration_ms"`
	QueueWaitMs           int64  `json:"queue_wait_ms"`
	LockWaitMs            int64  `json:"lock_wait_ms"`
	ParallelLimit         int    `json:"parallel_limit"`
	BatchSize             int    `json:"batch_size"`
	ArgsBytes             int64  `json:"args_bytes"`
	ResultBytes           int64  `json:"result_bytes"`
	EstimatedResultTokens int64  `json:"estimated_result_tokens"`
	CreatedAt             string `json:"created_at"`
}

func CreateAgentRun(ctx context.Context, db *sql.DB, run AgentRun) (AgentRun, error) {
	if run.SessionType == "" || run.EntityID == "" {
		return AgentRun{}, fmt.Errorf("session_type and entity_id are required")
	}
	if run.Provider == "" {
		run.Provider = "gemini"
	}
	if run.Model == "" {
		run.Model = "gemini-3.5-flash"
	}
	if run.Status == "" {
		run.Status = AgentRunQueued
	}
	if run.RunInputJSON == "" {
		run.RunInputJSON = "{}"
	}
	if run.RequestMetadataJSON == "" {
		run.RequestMetadataJSON = "{}"
	}
	if run.ProviderStateJSON == "" {
		run.ProviderStateJSON = "{}"
	}
	if !json.Valid([]byte(run.RunInputJSON)) {
		return AgentRun{}, fmt.Errorf("run_input_json must be valid JSON")
	}
	if !json.Valid([]byte(run.RequestMetadataJSON)) {
		return AgentRun{}, fmt.Errorf("request_metadata_json must be valid JSON")
	}
	if !json.Valid([]byte(run.ProviderStateJSON)) {
		return AgentRun{}, fmt.Errorf("provider_state_json must be valid JSON")
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO memento_agent_session (
			session_type, entity_id, interaction_id, status, provider, model,
			run_input_json, request_metadata_json, provider_state_json,
			heartbeat_at, started_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		run.SessionType, run.EntityID, run.InteractionID, run.Status, run.Provider, run.Model,
		run.RunInputJSON, run.RequestMetadataJSON, run.ProviderStateJSON,
	)
	if err != nil {
		return AgentRun{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AgentRun{}, err
	}
	return GetAgentRun(ctx, db, id)
}

func CreateAgentToolCallTrace(ctx context.Context, db *sql.DB, trace AgentToolCallTrace) (int64, error) {
	if trace.SessionID <= 0 {
		return 0, fmt.Errorf("session_id is required")
	}
	if trace.ToolName == "" {
		return 0, fmt.Errorf("tool_name is required")
	}
	if trace.ArgsJSON == "" {
		trace.ArgsJSON = "{}"
	}
	if trace.ResultJSON == "" {
		trace.ResultJSON = "{}"
	}
	if !json.Valid([]byte(trace.ArgsJSON)) {
		return 0, fmt.Errorf("args_json must be valid JSON")
	}
	if !json.Valid([]byte(trace.ResultJSON)) {
		return 0, fmt.Errorf("result_json must be valid JSON")
	}
	if trace.ArgsBytes == 0 {
		trace.ArgsBytes = int64(len([]byte(trace.ArgsJSON)))
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO memento_agent_tool_call (
			session_id, step_index, call_index, call_id, tool_name, tool_kind,
			lock_key, args_json, result_json, error_message, queued_at,
			parallel_limit, batch_size, args_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, step_index, call_index) DO UPDATE SET
			call_id = excluded.call_id,
			tool_name = excluded.tool_name,
			tool_kind = excluded.tool_kind,
			lock_key = excluded.lock_key,
			args_json = excluded.args_json,
			result_json = excluded.result_json,
			error_message = excluded.error_message,
			queued_at = excluded.queued_at,
			parallel_limit = excluded.parallel_limit,
			batch_size = excluded.batch_size,
			args_bytes = excluded.args_bytes`,
		trace.SessionID, trace.StepIndex, trace.CallIndex, trace.CallID, trace.ToolName, trace.ToolKind,
		trace.LockKey, trace.ArgsJSON, trace.ResultJSON, trace.ErrorMessage, trace.QueuedAt,
		trace.ParallelLimit, trace.BatchSize, trace.ArgsBytes,
	)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRowContext(ctx, `
		SELECT id FROM memento_agent_tool_call
		WHERE session_id = ? AND step_index = ? AND call_index = ?`,
		trace.SessionID, trace.StepIndex, trace.CallIndex,
	).Scan(&id)
	return id, err
}

func StartAgentToolCallTrace(ctx context.Context, db *sql.DB, id int64, startedAt string, queueWaitMs, lockWaitMs int64) error {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_tool_call
		SET started_at = ?, queue_wait_ms = ?, lock_wait_ms = ?
		WHERE id = ?`,
		startedAt, queueWaitMs, lockWaitMs, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func FinishAgentToolCallTrace(ctx context.Context, db *sql.DB, id int64, resultJSON, errorMessage, finishedAt string, durationMs int64) error {
	if resultJSON == "" {
		resultJSON = "{}"
	}
	if !json.Valid([]byte(resultJSON)) {
		return fmt.Errorf("result_json must be valid JSON")
	}
	resultBytes := int64(len([]byte(resultJSON)))
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_tool_call
		SET result_json = ?, error_message = ?, finished_at = ?, duration_ms = ?,
		    result_bytes = ?, estimated_result_tokens = ?
		WHERE id = ?`,
		resultJSON, errorMessage, finishedAt, durationMs, resultBytes, (resultBytes+3)/4, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func ListAgentToolCallTraces(ctx context.Context, db *sql.DB, sessionID int64) ([]AgentToolCallTrace, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, session_id, step_index, call_index, call_id, tool_name, tool_kind,
		       lock_key, args_json, result_json, error_message,
		       COALESCE(queued_at, ''), COALESCE(started_at, ''), COALESCE(finished_at, ''),
		       duration_ms, queue_wait_ms, lock_wait_ms, parallel_limit, batch_size,
		       args_bytes, result_bytes, estimated_result_tokens, created_at
		FROM memento_agent_tool_call
		WHERE session_id = ?
		ORDER BY step_index ASC, call_index ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentToolCallTrace
	for rows.Next() {
		var trace AgentToolCallTrace
		if err := rows.Scan(
			&trace.ID, &trace.SessionID, &trace.StepIndex, &trace.CallIndex, &trace.CallID,
			&trace.ToolName, &trace.ToolKind, &trace.LockKey, &trace.ArgsJSON, &trace.ResultJSON,
			&trace.ErrorMessage, &trace.QueuedAt, &trace.StartedAt, &trace.FinishedAt,
			&trace.DurationMs, &trace.QueueWaitMs, &trace.LockWaitMs, &trace.ParallelLimit,
			&trace.BatchSize, &trace.ArgsBytes, &trace.ResultBytes, &trace.EstimatedResultTokens,
			&trace.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, trace)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []AgentToolCallTrace{}
	}
	return out, nil
}

func GetAgentRun(ctx context.Context, db *sql.DB, id int64) (AgentRun, error) {
	var run AgentRun
	var heartbeatAt, startedAt, finishedAt sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, session_type, entity_id, interaction_id, status, provider, model,
		       run_input_json, request_metadata_json, provider_state_json,
		       error_message,
		       total_estimated_input_tokens, total_estimated_output_tokens,
		       total_estimated_tool_result_tokens, total_model_input_tokens,
		       total_model_output_tokens, total_model_tokens,
		       created_at, updated_at, heartbeat_at, started_at, finished_at
		FROM memento_agent_session
		WHERE id = ?`, id,
	).Scan(
		&run.ID, &run.SessionType, &run.EntityID, &run.InteractionID, &run.Status,
		&run.Provider, &run.Model, &run.RunInputJSON, &run.RequestMetadataJSON,
		&run.ProviderStateJSON, &run.ErrorMessage,
		&run.TotalEstimatedInputTokens, &run.TotalEstimatedOutputTokens,
		&run.TotalEstimatedToolResultTokens, &run.TotalModelInputTokens,
		&run.TotalModelOutputTokens, &run.TotalModelTokens,
		&run.CreatedAt, &run.UpdatedAt,
		&heartbeatAt, &startedAt, &finishedAt,
	)
	if err != nil {
		return AgentRun{}, err
	}
	if heartbeatAt.Valid {
		run.HeartbeatAt = heartbeatAt.String
	}
	if startedAt.Valid {
		run.StartedAt = startedAt.String
	}
	if finishedAt.Valid {
		run.FinishedAt = finishedAt.String
	}
	return run, nil
}

func AddAgentRunUsage(ctx context.Context, db *sql.DB, id int64, usage AgentUsageDelta) error {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_session
		SET total_estimated_input_tokens = total_estimated_input_tokens + ?,
		    total_estimated_output_tokens = total_estimated_output_tokens + ?,
		    total_estimated_tool_result_tokens = total_estimated_tool_result_tokens + ?,
		    total_model_input_tokens = total_model_input_tokens + ?,
		    total_model_output_tokens = total_model_output_tokens + ?,
		    total_model_tokens = total_model_tokens + ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		usage.EstimatedInputTokens,
		usage.EstimatedOutputTokens,
		usage.EstimatedToolResultTokens,
		usage.ModelInputTokens,
		usage.ModelOutputTokens,
		usage.ModelTotalTokens,
		id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func UpdateAgentRunStatus(ctx context.Context, db *sql.DB, id int64, status, interactionID, providerStateJSON, errorMessage string) error {
	if !validAgentRunStatus(status) {
		return fmt.Errorf("invalid agent run status %q", status)
	}
	if providerStateJSON == "" {
		providerStateJSON = "{}"
	}
	if !json.Valid([]byte(providerStateJSON)) {
		return fmt.Errorf("provider_state_json must be valid JSON")
	}
	finishedSQL := "NULL"
	if status == AgentRunSucceeded || status == AgentRunFailed || status == AgentRunCancelled {
		finishedSQL = "CURRENT_TIMESTAMP"
	}
	res, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE memento_agent_session
		SET status = ?, interaction_id = ?, provider_state_json = ?,
		    error_message = ?, heartbeat_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP, finished_at = %s
		WHERE id = ?`, finishedSQL),
		status, interactionID, providerStateJSON, errorMessage, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func FinishAgentRun(ctx context.Context, db *sql.DB, id int64, status, interactionID, providerStateJSON, errorMessage string) (bool, error) {
	if status != AgentRunSucceeded && status != AgentRunFailed && status != AgentRunCancelled {
		return false, fmt.Errorf("finish status must be terminal, got %q", status)
	}
	if providerStateJSON == "" {
		providerStateJSON = "{}"
	}
	if !json.Valid([]byte(providerStateJSON)) {
		return false, fmt.Errorf("provider_state_json must be valid JSON")
	}
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_session
		SET status = ?, interaction_id = ?, provider_state_json = ?,
		    error_message = ?, heartbeat_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP, finished_at = CURRENT_TIMESTAMP
		WHERE id = ?
		  AND status NOT IN (?, ?, ?)`,
		status, interactionID, providerStateJSON, errorMessage, id,
		AgentRunSucceeded, AgentRunFailed, AgentRunCancelled,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func UpdateAgentRunStatusOnly(ctx context.Context, db *sql.DB, id int64, status string) error {
	if !validAgentRunStatus(status) {
		return fmt.Errorf("invalid agent run status %q", status)
	}
	finishedSQL := "NULL"
	if status == AgentRunSucceeded || status == AgentRunFailed || status == AgentRunCancelled {
		finishedSQL = "CURRENT_TIMESTAMP"
	}
	res, err := db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE memento_agent_session
		SET status = ?, heartbeat_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP, finished_at = %s
		WHERE id = ?`, finishedSQL),
		status, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func CancelAgentRun(ctx context.Context, db *sql.DB, id int64) (bool, error) {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_session
		SET status = ?, error_message = ?, heartbeat_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP, finished_at = CURRENT_TIMESTAMP
		WHERE id = ?
		  AND status NOT IN (?, ?, ?)`,
		AgentRunCancelled, "cancelled", id,
		AgentRunSucceeded, AgentRunFailed, AgentRunCancelled,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func FailStaleAgentRuns(ctx context.Context, db *sql.DB, staleAfter time.Duration) (int64, error) {
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	seconds := int64(staleAfter.Seconds())
	if seconds <= 0 {
		seconds = 120
	}
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_session
		SET status = ?, error_message = ?,
		    updated_at = CURRENT_TIMESTAMP, finished_at = CURRENT_TIMESTAMP
		WHERE status IN (?, ?)
		  AND COALESCE(heartbeat_at, updated_at, created_at) < datetime('now', ?)`,
		AgentRunFailed,
		"agent run stalled (heartbeat timeout) or backend restarted",
		AgentRunRunning,
		AgentRunWaitingForUser,
		fmt.Sprintf("-%d seconds", seconds),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func TouchAgentRunHeartbeat(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_session
		SET heartbeat_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func AppendAgentEvent(ctx context.Context, db *sql.DB, sessionID int64, eventType string, payloadJSON string) (AgentEvent, error) {
	if sessionID <= 0 {
		return AgentEvent{}, fmt.Errorf("session_id is required")
	}
	if eventType == "" {
		return AgentEvent{}, fmt.Errorf("event_type is required")
	}
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	if !json.Valid([]byte(payloadJSON)) {
		return AgentEvent{}, fmt.Errorf("payload_json must be valid JSON")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AgentEvent{}, err
	}
	defer tx.Rollback()

	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM memento_agent_event WHERE session_id = ?`,
		sessionID,
	).Scan(&seq); err != nil {
		return AgentEvent{}, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO memento_agent_event (session_id, seq, event_type, payload_json)
		VALUES (?, ?, ?, ?)`, sessionID, seq, eventType, payloadJSON)
	if err != nil {
		return AgentEvent{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AgentEvent{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE memento_agent_session
		SET heartbeat_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, sessionID); err != nil {
		return AgentEvent{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentEvent{}, err
	}
	return GetAgentEvent(ctx, db, id)
}

func GetAgentEvent(ctx context.Context, db *sql.DB, id int64) (AgentEvent, error) {
	var ev AgentEvent
	err := db.QueryRowContext(ctx, `
		SELECT id, session_id, seq, event_type, payload_json, created_at
		FROM memento_agent_event
		WHERE id = ?`, id,
	).Scan(&ev.ID, &ev.SessionID, &ev.Seq, &ev.EventType, &ev.PayloadJSON, &ev.CreatedAt)
	return ev, err
}

func ListAgentEventsAfter(ctx context.Context, db *sql.DB, sessionID int64, afterSeq int64) ([]AgentEvent, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, session_id, seq, event_type, payload_json, created_at
		FROM memento_agent_event
		WHERE session_id = ? AND seq > ?
		ORDER BY seq ASC`, sessionID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentEvent
	for rows.Next() {
		var ev AgentEvent
		if err := rows.Scan(&ev.ID, &ev.SessionID, &ev.Seq, &ev.EventType, &ev.PayloadJSON, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []AgentEvent{}
	}
	return out, nil
}

// ActiveAgentRunForEntity returns the newest non-terminal run id for the given
// session type and entity, or 0 when none is active.
func ActiveAgentRunForEntity(ctx context.Context, db *sql.DB, sessionType, entityID string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		SELECT id
		FROM memento_agent_session
		WHERE session_type = ? AND entity_id = ?
		  AND status IN (?, ?, ?)
		ORDER BY id DESC
		LIMIT 1
	`, sessionType, entityID, AgentRunQueued, AgentRunRunning, AgentRunWaitingForUser).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func validAgentRunStatus(status string) bool {
	switch status {
	case AgentRunQueued, AgentRunRunning, AgentRunWaitingForUser, AgentRunSucceeded, AgentRunFailed, AgentRunCancelled:
		return true
	default:
		return false
	}
}
