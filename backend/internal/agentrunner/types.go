package agentrunner

import (
	"context"
	"encoding/json"
	"time"
)

type AgentType string

const (
	AgentCollector      AgentType = "collector"
	AgentProjectCompile AgentType = "project_compile"
	AgentConceptCompile AgentType = "concept_compile"
	AgentPersonEnrich   AgentType = "person_enrich"
	AgentDashboard      AgentType = "dashboard"
)

type RunSpec struct {
	AgentType             AgentType
	EntityID              string
	UserMessage           string
	PreviousInteractionID string
	Provider              string
	Model                 string
	System                string
	Tools                 []ToolSchema
	InitialTranscript     []ModelMessage
	RequestMetadata       map[string]any
	InitialEvents         []AgentEvent
	MaxSteps              int
	AfterDone             func(context.Context, AfterDoneContext) error
	RequiredOutcomes      []OutcomeRequirement
	MaxRepairAttempts     int
	// AllowClarifyingText lets a turn satisfy termination by ending with a
	// non-empty assistant text and no tool calls, even when RequiredOutcomes
	// are not met. Conversational agents (collector, dashboard) set this so
	// they can ask the user a clarifying question instead of being forced to
	// finalize. Generative agents (project/concept compile, person enrich)
	// leave it false to keep their bounded-repair guarantee.
	AllowClarifyingText bool
}

type AfterDoneContext struct {
	RunID         int64
	InteractionID string
	AssistantText string
}

type OutcomeRequirement struct {
	ToolName      string            `json:"tool_name"`
	ArgEquals     map[string]string `json:"arg_equals,omitempty"`
	AnyOfGroup    string            `json:"any_of_group,omitempty"`
	RequiredCount int               `json:"required_count,omitempty"`
	Description   string            `json:"description,omitempty"`
}

type ToolSchema struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ToolKind string

const (
	ToolReadOnly     ToolKind = "read_only"
	ToolMutating     ToolKind = "mutating"
	ToolHumanWaiting ToolKind = "human_waiting"
)

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type ToolResult struct {
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Result any    `json:"result"`
}

type ToolContext struct {
	RunID     int64
	RunSpec   RunSpec
	Emit      func(context.Context, AgentEvent) error
	SetStatus func(context.Context, string) error
}

type Tool struct {
	Schema  ToolSchema
	Kind    ToolKind
	LockKey func(spec RunSpec, args json.RawMessage) string
	Handler func(ctx context.Context, toolCtx ToolContext, args json.RawMessage) (any, error)
}

type ToolRegistry interface {
	LookupTool(name string) (Tool, bool)
}

type ModelInput struct {
	UserMessage  string
	ToolResults  []ToolResult
	IsToolResult bool
}

type ModelRequest struct {
	RunID                 int64
	Model                 string
	System                string
	Tools                 []ToolSchema
	Input                 ModelInput
	Transcript            []ModelMessage
	PreviousInteractionID string
	ProviderState         json.RawMessage
	StepIndex             int
}

type ModelMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	// Reasoning holds the model's thinking trace for an assistant turn. Some
	// providers (e.g. DeepSeek thinking mode via OpenAI-compatible gateways)
	// require it to be replayed as `reasoning_content` on the assistant message
	// when the transcript is sent back, or they reject the request.
	Reasoning string `json:"reasoning,omitempty"`
}

type ModelEventType string

const (
	ModelTextDelta      ModelEventType = "text_delta"
	ModelReasoningDelta ModelEventType = "reasoning_delta"
	ModelToolCall       ModelEventType = "tool_call"
	ModelDone           ModelEventType = "done"
)

type ModelEvent struct {
	Type          ModelEventType
	Text          string
	ReasoningText string
	ToolCall      *ToolCall
	InteractionID string
	ProviderState json.RawMessage
	Usage         ModelUsage
}

type Provider interface {
	Name() string
	Stream(ctx context.Context, req ModelRequest, emit func(ModelEvent) error) error
}

type ModelUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

type AgentEvent map[string]any

func NewTextDeltaEvent(text string) AgentEvent {
	return AgentEvent{"type": "text_delta", "text": text}
}

func NewToolCallStartEvent(name string, args any) AgentEvent {
	return AgentEvent{"type": "tool_call_start", "name": name, "args": args}
}

func NewToolCallResultEvent(name string, result any, durationMS int64) AgentEvent {
	return AgentEvent{"type": "tool_call_result", "name": name, "result": result, "duration_ms": durationMS}
}

func NewDoneEvent(interactionID string) AgentEvent {
	return AgentEvent{"type": "done", "interaction_id": interactionID}
}

func NewErrorEvent(message string) AgentEvent {
	return AgentEvent{"type": "error", "message": message}
}

func NewContextLoadedEvent(refs []map[string]any, warnings []string) AgentEvent {
	return AgentEvent{"type": "context_loaded", "refs": refs, "warnings": warnings}
}

func NewRequirementsStatusEvent(missing []string) AgentEvent {
	return AgentEvent{"type": "requirements_status", "missing": missing}
}

func NewRepairStartedEvent(attempt, maxAttempts int, missing []string) AgentEvent {
	return AgentEvent{"type": "repair_started", "attempt": attempt, "max_attempts": maxAttempts, "missing": missing}
}

func NewProposedBackfillEvent(decisionID, rationale string, candidateMessageIDs []int64, gapKind, slug string) AgentEvent {
	return AgentEvent{
		"type":                  "proposed_backfill",
		"decision_id":           decisionID,
		"rationale":             rationale,
		"candidate_message_ids": candidateMessageIDs,
		"gap_kind":              gapKind,
		"slug":                  slug,
	}
}

func NewProposedBackfillExpiredEvent(decisionID string) AgentEvent {
	return AgentEvent{"type": "proposed_backfill_expired", "decision_id": decisionID}
}

type PersistedEvent struct {
	Seq       int64
	Event     AgentEvent
	CreatedAt time.Time
}
