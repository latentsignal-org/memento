package agentrunner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"memento/backend/internal/config"
	"memento/backend/internal/store"

	"golang.org/x/sync/errgroup"
)

type Runner struct {
	db               *sql.DB
	registry         ToolRegistry
	providers        map[string]Provider
	maxParallelTools int

	mu           sync.Mutex
	broadcasters map[int64]*Broadcaster
	cancels      map[int64]context.CancelFunc
	locks        map[string]*sync.Mutex
}

func NewRunner(db *sql.DB, registry ToolRegistry, providers ...Provider) *Runner {
	r := &Runner{
		db:               db,
		registry:         registry,
		providers:        map[string]Provider{},
		maxParallelTools: 4,
		broadcasters:     map[int64]*Broadcaster{},
		cancels:          map[int64]context.CancelFunc{},
		locks:            map[string]*sync.Mutex{},
	}
	for _, p := range providers {
		r.providers[p.Name()] = p
	}
	if _, ok := r.providers["fake"]; !ok {
		r.providers["fake"] = &FakeProvider{}
	}
	if _, ok := r.providers["gemini"]; !ok {
		r.providers["gemini"] = &GeminiProvider{}
	}
	if _, ok := r.providers["openai_compatible"]; !ok {
		r.providers["openai_compatible"] = &OpenAICompatibleProvider{}
	}
	return r
}

func (r *Runner) SetMaxParallelTools(n int) {
	if n > 0 {
		r.maxParallelTools = n
	}
}

// RegisterProvider installs (or overrides) a model provider by name. Used by
// tests to swap a deterministic provider into a fully configured Runner.
func (r *Runner) RegisterProvider(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	r.providers[p.Name()] = p
	r.mu.Unlock()
}

func (r *Runner) Start(ctx context.Context, spec RunSpec) (int64, error) {
	if spec.AgentType == "" || spec.EntityID == "" {
		return 0, fmt.Errorf("agent_type and entity_id are required")
	}
	if spec.Provider == "" {
		spec.Provider = "gemini"
	}
	if spec.Model == "" {
		spec.Model = defaultModelForProvider(spec.Provider)
	}
	if spec.MaxSteps <= 0 {
		spec.MaxSteps = 20
	}
	if spec.RequestMetadata == nil {
		spec.RequestMetadata = map[string]any{}
	}
	inputRaw, err := json.Marshal(map[string]any{
		"agent_type":              spec.AgentType,
		"entity_id":               spec.EntityID,
		"user_message":            spec.UserMessage,
		"previous_interaction_id": spec.PreviousInteractionID,
	})
	if err != nil {
		return 0, err
	}
	metaRaw, err := json.Marshal(spec.RequestMetadata)
	if err != nil {
		return 0, err
	}
	run, err := store.CreateAgentRun(ctx, r.db, store.AgentRun{
		SessionType:         string(spec.AgentType),
		EntityID:            spec.EntityID,
		InteractionID:       spec.PreviousInteractionID,
		Status:              store.AgentRunQueued,
		Provider:            spec.Provider,
		Model:               spec.Model,
		RunInputJSON:        string(inputRaw),
		RequestMetadataJSON: string(metaRaw),
	})
	if err != nil {
		return 0, err
	}
	agentInfof("[agentrunner] start run_id=%d agent=%s entity=%s provider=%s model=%s max_steps=%d tools=%d initial_transcript=%d",
		run.ID, spec.AgentType, spec.EntityID, spec.Provider, spec.Model, spec.MaxSteps, len(spec.Tools), len(spec.InitialTranscript))
	runCtx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancels[run.ID] = cancel
	r.broadcasters[run.ID] = NewBroadcaster()
	r.mu.Unlock()
	go r.run(runCtx, run.ID, spec)
	return run.ID, nil
}

func (r *Runner) Cancel(ctx context.Context, runID int64) error {
	r.mu.Lock()
	cancel := r.cancels[runID]
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	cancelled, err := store.CancelAgentRun(ctx, r.db, runID)
	if err != nil {
		return err
	}
	if cancelled {
		agentInfof("[agentrunner] cancel run_id=%d", runID)
		_ = r.Emit(ctx, runID, NewErrorEvent("cancelled"))
		r.closeRun(runID)
	}
	return nil
}

func (r *Runner) Emit(ctx context.Context, runID int64, ev AgentEvent) error {
	eventType, _ := ev["type"].(string)
	if eventType == "" {
		return fmt.Errorf("agent event missing type")
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	stored, err := store.AppendAgentEvent(ctx, r.db, runID, eventType, string(payload))
	if err != nil {
		return err
	}
	r.mu.Lock()
	b := r.broadcasters[runID]
	r.mu.Unlock()
	if b != nil {
		b.Broadcast(stored)
	}
	return nil
}

func (r *Runner) Subscribe(runID int64) (<-chan store.AgentEvent, func()) {
	r.mu.Lock()
	b := r.broadcasters[runID]
	if b == nil {
		b = NewBroadcaster()
		r.broadcasters[runID] = b
	}
	r.mu.Unlock()
	return b.Subscribe()
}

func (r *Runner) IsTerminal(ctx context.Context, runID int64) bool {
	run, err := store.GetAgentRun(ctx, r.db, runID)
	if err != nil {
		return true
	}
	return isTerminalStatus(run.Status)
}

func (r *Runner) run(ctx context.Context, runID int64, spec RunSpec) {
	runStart := time.Now()
	defer r.closeRun(runID)
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	defer stopHeartbeat()
	go r.heartbeatRun(heartbeatCtx, runID)

	interactionID := spec.PreviousInteractionID
	providerState := json.RawMessage(`{}`)
	if err := store.UpdateAgentRunStatus(ctx, r.db, runID, store.AgentRunRunning, interactionID, string(providerState), ""); err != nil {
		return
	}
	r.mu.Lock()
	provider := r.providers[spec.Provider]
	r.mu.Unlock()
	if provider == nil {
		r.failRun(ctx, runID, interactionID, providerState, fmt.Errorf("unknown model provider %q", spec.Provider))
		return
	}
	for _, ev := range spec.InitialEvents {
		if err := r.Emit(context.Background(), runID, ev); err != nil {
			r.failRun(context.Background(), runID, interactionID, providerState, err)
			return
		}
	}

	input := ModelInput{UserMessage: spec.UserMessage}
	var fullAssistantText string
	transcript := append([]ModelMessage(nil), spec.InitialTranscript...)
	outcomes := newOutcomeTracker(spec.RequiredOutcomes)
	maxRepairAttempts := spec.MaxRepairAttempts
	if len(spec.RequiredOutcomes) > 0 && maxRepairAttempts <= 0 {
		maxRepairAttempts = 1
	}
	repairAttempts := 0
	for step := 0; step < spec.MaxSteps; step++ {
		stepStart := time.Now()
		var calls []ToolCall
		var assistantText, reasoningText string
		var stepDone bool
		var modelUsage ModelUsage
		req := ModelRequest{
			RunID:                 runID,
			Model:                 spec.Model,
			System:                spec.System,
			Tools:                 spec.Tools,
			Input:                 input,
			Transcript:            transcript,
			PreviousInteractionID: interactionID,
			ProviderState:         providerState,
			StepIndex:             step + 1,
		}
		agentInfof("[agentrunner] model step start run_id=%d step=%d provider=%s model=%s input_tool_result=%t transcript=%d tools=%d",
			runID, step+1, spec.Provider, spec.Model, input.IsToolResult, len(transcript), len(spec.Tools))
		err := provider.Stream(ctx, req, func(ev ModelEvent) error {
			switch ev.Type {
			case ModelTextDelta:
				assistantText += ev.Text
				fullAssistantText += ev.Text
				return r.Emit(ctx, runID, NewTextDeltaEvent(ev.Text))
			case ModelReasoningDelta:
				reasoningText += ev.ReasoningText
			case ModelToolCall:
				if ev.ToolCall != nil {
					calls = append(calls, *ev.ToolCall)
				}
			case ModelDone:
				stepDone = true
				modelUsage = ev.Usage
				if ev.InteractionID != "" {
					interactionID = ev.InteractionID
				}
				if len(ev.ProviderState) > 0 && json.Valid(ev.ProviderState) {
					providerState = ev.ProviderState
				}
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				_, _ = store.CancelAgentRun(context.Background(), r.db, runID)
				agentInfof("[agentrunner] model step cancelled run_id=%d step=%d provider=%s model=%s duration=%s",
					runID, step+1, spec.Provider, spec.Model, time.Since(stepStart))
				return
			}
			agentInfof("[agentrunner] model step error run_id=%d step=%d provider=%s model=%s duration=%s error=%v",
				runID, step+1, spec.Provider, spec.Model, time.Since(stepStart), err)
			r.failRun(context.Background(), runID, interactionID, providerState, err)
			return
		}
		agentInfof("[agentrunner] model step done run_id=%d step=%d provider=%s model=%s duration=%s text_chars=%d tool_calls=%d usage_total=%d interaction_id=%s",
			runID, step+1, spec.Provider, spec.Model, time.Since(stepStart), len(assistantText), len(calls), modelUsage.TotalTokens, interactionID)
		transcript = appendTranscriptInput(transcript, input)
		if assistantText != "" || len(calls) > 0 {
			transcript = append(transcript, ModelMessage{Role: "assistant", Content: assistantText, ToolCalls: calls, Reasoning: reasoningText})
		}
		if len(calls) == 0 {
			_ = r.logLoop(context.Background(), runID, step+1, input, assistantText, reasoningText, calls, nil, modelUsage, time.Since(stepStart))
			missing := outcomes.Missing()
			if len(missing) > 0 {
				// Conversational agents can satisfy termination by ending a
				// turn with a visible clarifying question. The prompt is
				// responsible for ensuring the text is actually a useful
				// question; the runtime only checks that the model produced
				// some user-facing text instead of bailing silently.
				if !(spec.AllowClarifyingText && strings.TrimSpace(assistantText) != "") {
					_ = r.Emit(context.Background(), runID, NewRequirementsStatusEvent(missing))
					if repairAttempts < maxRepairAttempts {
						repairAttempts++
						_ = r.Emit(context.Background(), runID, NewRepairStartedEvent(repairAttempts, maxRepairAttempts, missing))
						input = ModelInput{UserMessage: buildRepairPrompt(missing, repairAttempts, maxRepairAttempts)}
						continue
					}
					r.failRun(context.Background(), runID, interactionID, providerState, fmt.Errorf("agent finished without required outcomes after %d repair attempt(s): %s", repairAttempts, strings.Join(missing, "; ")))
					return
				}
			}
			if spec.AfterDone != nil {
				if err := spec.AfterDone(context.Background(), AfterDoneContext{
					RunID:         runID,
					InteractionID: interactionID,
					AssistantText: fullAssistantText,
				}); err != nil {
					r.failRun(context.Background(), runID, interactionID, providerState, err)
					return
				}
			}
			if err := ctx.Err(); err != nil {
				_, _ = store.CancelAgentRun(context.Background(), r.db, runID)
				return
			}
			if finished, _ := store.FinishAgentRun(context.Background(), r.db, runID, store.AgentRunSucceeded, interactionID, string(providerState), ""); finished {
				_ = r.Emit(context.Background(), runID, NewDoneEvent(interactionID))
				agentInfof("[agentrunner] run succeeded run_id=%d agent=%s entity=%s provider=%s model=%s steps=%d duration=%s interaction_id=%s",
					runID, spec.AgentType, spec.EntityID, spec.Provider, spec.Model, step+1, time.Since(runStart), interactionID)
			}
			_ = stepDone
			return
		}
		results, err := r.dispatchTools(ctx, runID, step+1, spec, calls)
		_ = r.logLoop(context.Background(), runID, step+1, input, assistantText, reasoningText, calls, results, modelUsage, time.Since(stepStart))
		if err != nil {
			r.failRun(context.Background(), runID, interactionID, providerState, err)
			return
		}
		outcomes.Record(calls, results)
		input = ModelInput{ToolResults: results, IsToolResult: true}
	}
	r.failRun(context.Background(), runID, interactionID, providerState, fmt.Errorf("agent exceeded step limit (%d)", spec.MaxSteps))
}

func appendTranscriptInput(transcript []ModelMessage, input ModelInput) []ModelMessage {
	if !input.IsToolResult {
		if input.UserMessage != "" {
			return append(transcript, ModelMessage{Role: "user", Content: input.UserMessage})
		}
		return transcript
	}
	for _, result := range input.ToolResults {
		raw, _ := json.Marshal(result.Result)
		transcript = append(transcript, ModelMessage{
			Role:       "tool",
			Content:    string(raw),
			ToolCallID: result.CallID,
			ToolName:   result.Name,
		})
	}
	return transcript
}

type outcomeTracker struct {
	requirements []OutcomeRequirement
	counts       []int
}

func newOutcomeTracker(requirements []OutcomeRequirement) *outcomeTracker {
	reqs := make([]OutcomeRequirement, 0, len(requirements))
	for _, req := range requirements {
		if req.ToolName == "" {
			continue
		}
		if req.RequiredCount <= 0 {
			req.RequiredCount = 1
		}
		reqs = append(reqs, req)
	}
	return &outcomeTracker{
		requirements: reqs,
		counts:       make([]int, len(reqs)),
	}
}

func (t *outcomeTracker) Record(calls []ToolCall, results []ToolResult) {
	if t == nil || len(t.requirements) == 0 {
		return
	}
	for i, call := range calls {
		if i >= len(results) || toolResultBlocksOutcome(results[i]) {
			continue
		}
		var args map[string]any
		if len(call.Args) > 0 {
			_ = json.Unmarshal(call.Args, &args)
		}
		for idx, req := range t.requirements {
			if call.Name != req.ToolName || !argsMatch(req.ArgEquals, args) {
				continue
			}
			t.counts[idx]++
		}
	}
}

func (t *outcomeTracker) Missing() []string {
	if t == nil {
		return nil
	}
	var missing []string
	groups := map[string][]int{}
	for i, req := range t.requirements {
		if req.AnyOfGroup != "" {
			groups[req.AnyOfGroup] = append(groups[req.AnyOfGroup], i)
			continue
		}
		if t.counts[i] >= req.RequiredCount {
			continue
		}
		if req.Description != "" {
			missing = append(missing, req.Description)
			continue
		}
		missing = append(missing, describeOutcomeRequirement(req))
	}
	groupNames := make([]string, 0, len(groups))
	for groupName := range groups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)
	for _, groupName := range groupNames {
		indexes := groups[groupName]
		satisfied := false
		descriptions := make([]string, 0, len(indexes))
		for _, idx := range indexes {
			req := t.requirements[idx]
			if t.counts[idx] >= req.RequiredCount {
				satisfied = true
				break
			}
			if req.Description != "" {
				descriptions = append(descriptions, req.Description)
			} else {
				descriptions = append(descriptions, describeOutcomeRequirement(req))
			}
		}
		if !satisfied {
			missing = append(missing, "one of: "+strings.Join(descriptions, " OR "))
		}
	}
	return missing
}

func toolResultBlocksOutcome(result ToolResult) bool {
	return toolResultHasError(result) || toolResultSkipped(result)
}

func toolResultHasError(result ToolResult) bool {
	if m, ok := result.Result.(map[string]any); ok {
		if _, exists := m["error"]; exists {
			return true
		}
	}
	if m, ok := result.Result.(map[string]string); ok {
		if _, exists := m["error"]; exists {
			return true
		}
	}
	return false
}

func toolResultSkipped(result ToolResult) bool {
	switch v := result.Result.(type) {
	case map[string]any:
		return toolResultFlagTruthy(v["skipped"])
	case map[string]string:
		return v["skipped"] == "true"
	default:
		raw, err := json.Marshal(result.Result)
		if err != nil {
			return false
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			return false
		}
		return toolResultFlagTruthy(m["skipped"])
	}
}

func toolResultFlagTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	case float64:
		return t != 0
	}
	return false
}

func argsMatch(want map[string]string, got map[string]any) bool {
	if len(want) == 0 {
		return true
	}
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok || fmt.Sprint(gotValue) != wantValue {
			return false
		}
	}
	return true
}

func describeOutcomeRequirement(req OutcomeRequirement) string {
	if len(req.ArgEquals) == 0 {
		return fmt.Sprintf("%s x%d", req.ToolName, req.RequiredCount)
	}
	keys := make([]string, 0, len(req.ArgEquals))
	for key := range req.ArgEquals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, req.ArgEquals[key]))
	}
	return fmt.Sprintf("%s(%s) x%d", req.ToolName, strings.Join(parts, ", "), req.RequiredCount)
}

func buildRepairPrompt(missing []string, attempt, maxAttempts int) string {
	return fmt.Sprintf(`The run cannot finish yet because required persisted outputs are missing.

Missing required tool calls:
- %s

This is repair attempt %d of %d. Use the evidence already loaded whenever possible. You must now call the missing write/update tool calls. Do not answer in prose until the required tool calls have succeeded. If you truly cannot satisfy a required call from the available evidence, make the narrowest read call needed, then call the required write/update tool.`, strings.Join(missing, "\n- "), attempt, maxAttempts)
}

func (r *Runner) dispatchTools(ctx context.Context, runID int64, stepIndex int, spec RunSpec, calls []ToolCall) ([]ToolResult, error) {
	results := make([]ToolResult, len(calls))
	durations := make([]int64, len(calls))
	parsedArgs := make([]any, len(calls))
	tools := make([]Tool, len(calls))
	traceIDs := make([]int64, len(calls))
	queuedAt := time.Now()
	for i, call := range calls {
		var parsed any = map[string]any{}
		if len(call.Args) > 0 {
			_ = json.Unmarshal(call.Args, &parsed)
		}
		tool, ok := r.registry.LookupTool(call.Name)
		if !ok {
			return nil, fmt.Errorf("unknown tool %q", call.Name)
		}
		parsedArgs[i] = parsed
		tools[i] = tool
		traceID, err := store.CreateAgentToolCallTrace(ctx, r.db, store.AgentToolCallTrace{
			SessionID:     runID,
			StepIndex:     stepIndex,
			CallIndex:     i,
			CallID:        call.ID,
			ToolName:      call.Name,
			ToolKind:      string(tool.Kind),
			LockKey:       r.lockKeyForTool(spec, tool, call.Args),
			ArgsJSON:      string(call.Args),
			ResultJSON:    "{}",
			QueuedAt:      queuedAt.Format(time.RFC3339Nano),
			ParallelLimit: r.maxParallelTools,
			BatchSize:     len(calls),
		})
		if err != nil {
			agentWarnf("[agentrunner] tool trace create failed run_id=%d step=%d tool=%s error=%v",
				runID, stepIndex, call.Name, err)
		}
		traceIDs[i] = traceID
	}
	for i, call := range calls {
		if err := r.Emit(ctx, runID, NewToolCallStartEvent(call.Name, parsedArgs[i])); err != nil {
			return nil, err
		}
	}
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(r.maxParallelTools)
	for i := range calls {
		i := i
		call := calls[i]
		eg.Go(func() error {
			tool := tools[i]
			lockStart := time.Now()
			unlock := r.lockTool(spec, tool, call.Args)
			toolStart := time.Now()
			if traceIDs[i] != 0 {
				if err := store.StartAgentToolCallTrace(egCtx, r.db, traceIDs[i], toolStart.Format(time.RFC3339Nano), lockStart.Sub(queuedAt).Milliseconds(), toolStart.Sub(lockStart).Milliseconds()); err != nil {
					agentWarnf("[agentrunner] tool trace start failed run_id=%d step=%d tool=%s error=%v",
						runID, stepIndex, call.Name, err)
				}
			}
			defer func() { durations[i] = time.Since(toolStart).Milliseconds() }()
			defer unlock()
			toolCtx := ToolContext{
				RunID:   runID,
				RunSpec: spec,
				Emit: func(ctx context.Context, ev AgentEvent) error {
					return r.Emit(ctx, runID, ev)
				},
				SetStatus: func(ctx context.Context, status string) error {
					return store.UpdateAgentRunStatusOnly(ctx, r.db, runID, status)
				},
			}
			result, err := tool.Handler(egCtx, toolCtx, call.Args)
			if err != nil {
				// Context-cancellation must abort the run — that's how Cancel()
				// signals shutdown. Any other tool failure becomes an
				// error-shaped result fed back to the model so the agent can
				// recover (e.g. fall back from vector_search to fts_search).
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				agentWarnf("[agentrunner] tool soft_error run_id=%d tool=%s duration=%s error=%v",
					runID, call.Name, time.Since(toolStart), err)
				results[i] = ToolResult{
					CallID: call.ID,
					Name:   call.Name,
					Result: map[string]any{"error": err.Error()},
				}
				r.finishToolTrace(egCtx, traceIDs[i], results[i].Result, err.Error(), time.Since(toolStart))
				return nil
			}
			agentInfof("[agentrunner] tool done run_id=%d tool=%s kind=%s duration=%s",
				runID, call.Name, tool.Kind, time.Since(toolStart))
			results[i] = ToolResult{CallID: call.ID, Name: call.Name, Result: result}
			r.finishToolTrace(egCtx, traceIDs[i], result, "", time.Since(toolStart))
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	for i, call := range calls {
		if err := r.Emit(ctx, runID, NewToolCallResultEvent(call.Name, results[i].Result, durations[i])); err != nil {
			return nil, err
		}
	}
	return results, nil
}

func (r *Runner) finishToolTrace(ctx context.Context, traceID int64, result any, errorMessage string, duration time.Duration) {
	if traceID == 0 {
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"error":"marshal tool result: %s"}`, strings.ReplaceAll(err.Error(), `"`, `\"`)))
		if errorMessage == "" {
			errorMessage = err.Error()
		}
	}
	if err := store.FinishAgentToolCallTrace(ctx, r.db, traceID, string(raw), errorMessage, time.Now().Format(time.RFC3339Nano), duration.Milliseconds()); err != nil {
		agentWarnf("[agentrunner] tool trace finish failed trace_id=%d error=%v", traceID, err)
	}
}

func (r *Runner) heartbeatRun(ctx context.Context, runID int64) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = store.TouchAgentRunHeartbeat(ctx, r.db, runID)
		}
	}
}

func (r *Runner) lockTool(spec RunSpec, tool Tool, args json.RawMessage) func() {
	if tool.Kind == ToolReadOnly {
		return func() {}
	}
	key := r.lockKeyForTool(spec, tool, args)
	r.mu.Lock()
	mu := r.locks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		r.locks[key] = mu
	}
	r.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func (r *Runner) lockKeyForTool(spec RunSpec, tool Tool, args json.RawMessage) string {
	key := string(tool.Kind) + ":" + tool.Schema.Name
	if tool.Kind == ToolReadOnly {
		return ""
	}
	if tool.LockKey != nil {
		if v := tool.LockKey(spec, args); v != "" {
			key = v
		}
	}
	return key
}

func (r *Runner) logLoop(ctx context.Context, runID int64, stepIndex int, input ModelInput, assistantText, reasoningText string, calls []ToolCall, results []ToolResult, modelUsage ModelUsage, d time.Duration) error {
	inputType := "user_input"
	inputContent := input.UserMessage
	if input.IsToolResult {
		inputType = "tool_results"
		raw, _ := json.Marshal(input.ToolResults)
		inputContent = string(raw)
	}
	callsRaw, _ := json.Marshal(calls)
	resultsRaw, _ := json.Marshal(results)
	usage := estimateLoopUsage(inputContent, assistantText, reasoningText, results, modelUsage)
	usageRaw, _ := json.Marshal(usage)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO memento_agent_loop (
			session_id, step_index, input_type, input_content,
			assistant_text, reasoning_text, tool_calls_json, tool_results_json, duration_ms,
			estimated_input_tokens, estimated_output_tokens, estimated_tool_result_tokens,
			model_input_tokens, model_output_tokens, model_total_tokens, usage_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, stepIndex, inputType, inputContent,
		assistantText, reasoningText, string(callsRaw), string(resultsRaw), int(d.Milliseconds()),
		usage.EstimatedInputTokens, usage.EstimatedOutputTokens, usage.EstimatedToolResultTokens,
		usage.ModelInputTokens, usage.ModelOutputTokens, usage.ModelTotalTokens, string(usageRaw),
	)
	if err != nil {
		return err
	}
	return store.AddAgentRunUsage(ctx, r.db, runID, usage)
}

func estimateLoopUsage(inputContent, assistantText, reasoningText string, results []ToolResult, modelUsage ModelUsage) store.AgentUsageDelta {
	toolRaw, _ := json.Marshal(results)
	outputText := assistantText + reasoningText
	return store.AgentUsageDelta{
		EstimatedInputTokens:      estimateTokens(inputContent),
		EstimatedOutputTokens:     estimateTokens(outputText),
		EstimatedToolResultTokens: estimateTokens(string(toolRaw)),
		ModelInputTokens:          modelUsage.InputTokens,
		ModelOutputTokens:         modelUsage.OutputTokens,
		ModelTotalTokens:          modelUsage.TotalTokens,
	}
}

func estimateTokens(text string) int64 {
	if text == "" {
		return 0
	}
	return int64((len(text) + 3) / 4)
}

func (r *Runner) failRun(ctx context.Context, runID int64, interactionID string, providerState json.RawMessage, err error) {
	msg := err.Error()
	failed, _ := store.FinishAgentRun(ctx, r.db, runID, store.AgentRunFailed, interactionID, string(providerState), msg)
	if failed {
		agentErrorf("[agentrunner] run failed run_id=%d error=%s", runID, msg)
		_ = r.Emit(ctx, runID, NewErrorEvent(msg))
	}
}

func (r *Runner) closeRun(runID int64) {
	r.mu.Lock()
	if cancel := r.cancels[runID]; cancel != nil {
		delete(r.cancels, runID)
	}
	b := r.broadcasters[runID]
	delete(r.broadcasters, runID)
	r.mu.Unlock()
	if b != nil {
		b.Close()
	}
}

func isTerminalStatus(status string) bool {
	return status == store.AgentRunSucceeded || status == store.AgentRunFailed || status == store.AgentRunCancelled
}

func defaultModelForProvider(provider string) string {
	return config.DefaultModelForProvider(provider)
}

func SortedToolSchemas(registry map[string]Tool) []ToolSchema {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ToolSchema, 0, len(names))
	for _, name := range names {
		out = append(out, registry[name].Schema)
	}
	return out
}

func ParseAfterSeq(raw string) int64 {
	if raw == "" {
		return 0
	}
	n, _ := strconv.ParseInt(raw, 10, 64)
	if n < 0 {
		return 0
	}
	return n
}
