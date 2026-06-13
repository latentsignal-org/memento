package agentrunner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestParseGeminiSSE(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"event_type":"step.delta","delta":{"type":"text","text":"hello"}}`,
		``,
		`data: {"event_type":"step.start","index":0,"step":{"type":"function_call","id":"call-1","name":"get_message"}}`,
		``,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"arguments_delta","arguments":"{\"message_id\":12}"}}`,
		``,
		`data: {"event_type":"step.stop","index":0}`,
		``,
		`data: {"event_type":"interaction.completed","interaction":{"id":"interaction-1"},"usage_metadata":{"prompt_token_count":11,"candidates_token_count":7,"total_token_count":18}}`,
		``,
	}, "\n")

	var events []ModelEvent
	err := ParseGeminiSSE(strings.NewReader(raw), func(ev ModelEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != ModelTextDelta || events[0].Text != "hello" {
		t.Fatalf("unexpected text event: %+v", events[0])
	}
	if events[1].Type != ModelToolCall || events[1].ToolCall == nil || events[1].ToolCall.Name != "get_message" || string(events[1].ToolCall.Args) != `{"message_id":12}` {
		t.Fatalf("unexpected tool event: %+v", events[1])
	}
	if events[2].Type != ModelDone || events[2].InteractionID != "interaction-1" {
		t.Fatalf("unexpected done event: %+v", events[2])
	}
	if events[2].Usage.InputTokens != 11 || events[2].Usage.OutputTokens != 7 || events[2].Usage.TotalTokens != 18 {
		t.Fatalf("unexpected usage: %+v", events[2].Usage)
	}
}

func TestParseGeminiSSE_UsageCamelCase(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"event_type":"step.delta","delta":{"type":"text","text":"hi"}}`,
		``,
		`data: {"event_type":"interaction.completed","interaction":{"id":"x"},"usageMetadata":{"promptTokenCount":42,"candidatesTokenCount":11,"totalTokenCount":53}}`,
		``,
	}, "\n")
	var got []ModelEvent
	if err := ParseGeminiSSE(strings.NewReader(raw), func(ev ModelEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	done := got[len(got)-1]
	if done.Type != ModelDone {
		t.Fatalf("last event = %+v, want done", done)
	}
	if done.Usage.InputTokens != 42 || done.Usage.OutputTokens != 11 || done.Usage.TotalTokens != 53 {
		t.Fatalf("camelCase usage not captured: %+v", done.Usage)
	}
}

func TestParseGeminiSSE_UsageOnSeparateEvent(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"event_type":"step.delta","delta":{"type":"text","text":"hi"}}`,
		``,
		`data: {"event_type":"interaction.usage","usage_metadata":{"prompt_token_count":7,"candidates_token_count":2,"total_token_count":9}}`,
		``,
		`data: {"event_type":"interaction.completed","interaction":{"id":"x"}}`,
		``,
	}, "\n")
	var got []ModelEvent
	if err := ParseGeminiSSE(strings.NewReader(raw), func(ev ModelEvent) error {
		got = append(got, ev)
		return nil
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	done := got[len(got)-1]
	if done.Usage.TotalTokens != 9 {
		t.Fatalf("usage from sidecar event not captured: %+v", done.Usage)
	}
}

func TestParseOpenAICompatibleSSE(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"hi"}}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"fts_search","arguments":"{\"query\":"}}]}}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"abc\"}"}}]}}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var events []ModelEvent
	err := ParseOpenAICompatibleSSE(strings.NewReader(raw), func(ev ModelEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != ModelTextDelta || events[0].Text != "hi" {
		t.Fatalf("unexpected text event: %+v", events[0])
	}
	if events[1].Type != ModelToolCall || events[1].ToolCall.Name != "fts_search" || string(events[1].ToolCall.Args) != `{"query":"abc"}` {
		t.Fatalf("unexpected tool event: %+v", events[1])
	}
	if events[2].Type != ModelDone || events[2].InteractionID != "chatcmpl-1" {
		t.Fatalf("unexpected done event: %+v", events[2])
	}
	if events[2].Usage.InputTokens != 13 || events[2].Usage.OutputTokens != 5 || events[2].Usage.TotalTokens != 18 {
		t.Fatalf("unexpected usage: %+v", events[2].Usage)
	}
}

func TestParseOpenAICompatibleSSEUsesSparseToolCallIndexes(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":2,"id":"call-2","function":{"name":"get_message","arguments":"{\"message_id\":12}"}}]}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var events []ModelEvent
	err := ParseOpenAICompatibleSSE(strings.NewReader(raw), func(ev ModelEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected tool call and done events, got %d: %+v", len(events), events)
	}
	if events[0].Type != ModelToolCall || events[0].ToolCall == nil || events[0].ToolCall.ID != "call-2" {
		t.Fatalf("unexpected sparse tool call event: %+v", events[0])
	}
}

func TestOpenAIChatCompletionsEndpointUsesConfiguredAPIRoot(t *testing.T) {
	got := openAIChatCompletionsEndpoint("https://api.cerebras.ai/v1/")
	if got != "https://api.cerebras.ai/v1/chat/completions" {
		t.Fatalf("unexpected endpoint: %s", got)
	}
}

func TestParseOpenAICompatibleSSECapturesReasoning(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"reasoning_content":"thinking"}}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"reasoning":" more"}}]}`,
		``,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"hi"}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var events []ModelEvent
	err := ParseOpenAICompatibleSSE(strings.NewReader(raw), func(ev ModelEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != ModelReasoningDelta || events[0].ReasoningText != "thinking" {
		t.Fatalf("unexpected first reasoning event: %+v", events[0])
	}
	if events[1].Type != ModelReasoningDelta || events[1].ReasoningText != " more" {
		t.Fatalf("unexpected second reasoning event: %+v", events[1])
	}
	if events[2].Type != ModelTextDelta || events[2].Text != "hi" {
		t.Fatalf("unexpected text event: %+v", events[2])
	}
}

func TestOpenAIMessagesOmitAssistantReasoningByDefault(t *testing.T) {
	messages := openAIMessages(ModelRequest{
		System: "test",
		Transcript: []ModelMessage{{
			Role:      "assistant",
			Content:   "",
			Reasoning: "let me search",
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fts_search",
				Args: []byte(`{"query":"abc"}`),
			}},
		}},
		Input: ModelInput{
			IsToolResult: true,
			ToolResults: []ToolResult{{
				CallID: "call-1",
				Name:   "fts_search",
				Result: map[string]any{"ok": true},
			}},
		},
	})
	assistant := messages[1]
	if _, ok := assistant["reasoning_content"]; ok {
		t.Fatalf("assistant reasoning_content replayed by default: %+v", assistant)
	}
}

func TestOpenAIMessagesReplayAssistantReasoningWhenEnabled(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_REPLAY_REASONING", "1")

	messages := openAIMessages(ModelRequest{
		System: "test",
		Transcript: []ModelMessage{{
			Role:      "assistant",
			Content:   "",
			Reasoning: "let me search",
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fts_search",
				Args: []byte(`{"query":"abc"}`),
			}},
		}},
		Input: ModelInput{
			IsToolResult: true,
			ToolResults: []ToolResult{{
				CallID: "call-1",
				Name:   "fts_search",
				Result: map[string]any{"ok": true},
			}},
		},
	})
	assistant := messages[1]
	if assistant["reasoning_content"] != "let me search" {
		t.Fatalf("assistant reasoning_content not replayed: %+v", assistant)
	}
}

func TestOpenAICompatibleChatOmitsReasoningControlsForGenericEndpoint(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_THINKING", "enabled")
	t.Setenv("MEMENTO_MODEL_REASONING_EFFORT", "low")

	body := captureOpenAICompatibleRequest(t, "https://example.test/v1", ModelRequest{
		Model:  "local-model",
		System: "test",
		Input:  ModelInput{UserMessage: "hello"},
	})

	if _, ok := body["reasoning"]; ok {
		t.Fatalf("Responses reasoning object should not be sent to generic endpoints: %+v", body)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort should not be sent to generic endpoints: %+v", body)
	}
	if _, ok := body["thinking"]; ok {
		t.Fatalf("thinking should not be sent to generic endpoints: %+v", body)
	}
}

func TestOpenAICompatibleChatSendsDeepSeekV4ThinkingEffort(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_THINKING", "enabled")
	t.Setenv("MEMENTO_MODEL_REASONING_EFFORT", "max")

	body := captureOpenAICompatibleRequest(t, "https://api.deepseek.com/v1", ModelRequest{
		Model:  "deepseek-v4-flash",
		System: "test",
		Input:  ModelInput{UserMessage: "hello"},
	})

	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %+v, want enabled; body=%+v", body["thinking"], body)
	}
	if body["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %v, want max; body=%+v", body["reasoning_effort"], body)
	}
}

func TestOpenAICompatibleChatNormalizesDeepSeekV4EffortAliases(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_REASONING_EFFORT", "xhigh")

	body := captureOpenAICompatibleRequest(t, "https://api.deepseek.com/v1", ModelRequest{
		Model:  "deepseek/deepseek-v4-pro",
		System: "test",
		Input:  ModelInput{UserMessage: "hello"},
	})

	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %+v, want enabled; body=%+v", body["thinking"], body)
	}
	if body["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %v, want max; body=%+v", body["reasoning_effort"], body)
	}
}

func TestOpenAICompatibleChatCanDisableDeepSeekV4Thinking(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_THINKING", "disabled")
	t.Setenv("MEMENTO_MODEL_REASONING_EFFORT", "max")

	body := captureOpenAICompatibleRequest(t, "https://api.deepseek.com/v1", ModelRequest{
		Model:  "deepseek-v4-flash",
		System: "test",
		Input:  ModelInput{UserMessage: "hello"},
	})

	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("thinking = %+v, want disabled; body=%+v", body["thinking"], body)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort should not be sent when thinking is disabled: %+v", body)
	}
}

func TestOpenAICompatibleUsesResponsesAPIForOpenAIReasoningEffort(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_REASONING_EFFORT", "high")

	var path string
	var body map[string]any
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`data: {"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"))),
		}, nil
	})}
	provider := &OpenAICompatibleProvider{BaseURL: "https://api.openai.com/v1", HTTPClient: client}
	err := provider.Stream(context.Background(), ModelRequest{
		Model:                 "gpt-5.1",
		System:                "test",
		PreviousInteractionID: "resp_prev",
		Tools: []ToolSchema{{
			Name:        "fts_search",
			Description: "Search",
			Parameters:  []byte(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		}},
		Transcript: []ModelMessage{{Role: "user", Content: "old"}},
		Input: ModelInput{
			IsToolResult: true,
			ToolResults: []ToolResult{{
				CallID: "call_1",
				Name:   "fts_search",
				Result: map[string]any{"ok": true},
			}},
		},
	}, func(ModelEvent) error { return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	if path != "/v1/responses" {
		t.Fatalf("path = %s, want /v1/responses", path)
	}
	if body["previous_response_id"] != "resp_prev" {
		t.Fatalf("previous_response_id = %v, want resp_prev; body=%+v", body["previous_response_id"], body)
	}
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning = %+v, want effort=high; body=%+v", body["reasoning"], body)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("Chat Completions reasoning_effort should not be sent to Responses API: %+v", body)
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("responses tools malformed: %+v", body["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["type"] != "function" || tool["name"] != "fts_search" {
		t.Fatalf("responses function tool malformed: %+v", tools[0])
	}
	input, ok := body["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("previous_response_id should keep input to current turn only: %+v", body["input"])
	}
	functionOutput, ok := input[0].(map[string]any)
	if !ok || functionOutput["type"] != "function_call_output" || functionOutput["call_id"] != "call_1" {
		t.Fatalf("responses function output malformed: %+v", input[0])
	}
}

func TestOpenAICompatibleRequestOmitsReasoningEffortByDefault(t *testing.T) {
	body := captureOpenAICompatibleRequest(t, "https://api.deepseek.com/v1", ModelRequest{
		Model:  "deepseek-v4-flash",
		System: "test",
		Input:  ModelInput{UserMessage: "hello"},
	})

	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("reasoning_effort included by default: %+v", body)
	}
}

func TestOpenAICompatibleReasoningEffortDoesNotDisableReasoningReplay(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_THINKING", "enabled")
	t.Setenv("MEMENTO_MODEL_REASONING_EFFORT", "high")
	t.Setenv("MEMENTO_MODEL_REPLAY_REASONING", "1")

	body := captureOpenAICompatibleRequest(t, "https://api.deepseek.com/v1", ModelRequest{
		Model:  "deepseek-v4-flash",
		System: "test",
		Transcript: []ModelMessage{{
			Role:      "assistant",
			Content:   "",
			Reasoning: "let me search",
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fts_search",
				Args: []byte(`{"query":"abc"}`),
			}},
		}},
		Input: ModelInput{
			IsToolResult: true,
			ToolResults: []ToolResult{{
				CallID: "call-1",
				Name:   "fts_search",
				Result: map[string]any{"ok": true},
			}},
		},
	})

	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %v, want high; body=%+v", body["reasoning_effort"], body)
	}
	thinking, ok := body["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %+v, want enabled; body=%+v", body["thinking"], body)
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("messages missing or malformed: %+v", body["messages"])
	}
	assistant, ok := messages[1].(map[string]any)
	if !ok {
		t.Fatalf("assistant message malformed: %+v", messages[1])
	}
	if assistant["reasoning_content"] != "let me search" {
		t.Fatalf("assistant reasoning_content not replayed with reasoning_effort set: %+v", assistant)
	}
}

func TestOpenAICompatibleDisabledThinkingSuppressesReasoningReplay(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_THINKING", "disabled")
	t.Setenv("MEMENTO_MODEL_REPLAY_REASONING", "1")

	messages := openAIMessages(ModelRequest{
		System: "test",
		Transcript: []ModelMessage{{
			Role:      "assistant",
			Content:   "",
			Reasoning: "let me search",
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fts_search",
				Args: []byte(`{"query":"abc"}`),
			}},
		}},
		Input: ModelInput{
			IsToolResult: true,
			ToolResults: []ToolResult{{
				CallID: "call-1",
				Name:   "fts_search",
				Result: map[string]any{"ok": true},
			}},
		},
	})
	assistant := messages[1]
	if _, ok := assistant["reasoning_content"]; ok {
		t.Fatalf("assistant reasoning_content replayed despite disabled thinking: %+v", assistant)
	}
}

func TestParseOpenAIResponsesSSE(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1"}}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
		``,
		`data: {"type":"response.output_text.delta","delta":"hi"}`,
		``,
		`data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":1,"delta":"{\"query\":"}`,
		``,
		`data: {"type":"response.function_call_arguments.done","item_id":"fc_1","output_index":1,"name":"fts_search","arguments":"{\"query\":\"abc\"}"}`,
		``,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"fts_search","arguments":"{\"query\":\"abc\"}"}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":13,"output_tokens":7,"total_tokens":20}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var events []ModelEvent
	err := ParseOpenAIResponsesSSE(strings.NewReader(raw), func(ev ModelEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != ModelReasoningDelta || events[0].ReasoningText != "thinking" {
		t.Fatalf("unexpected reasoning event: %+v", events[0])
	}
	if events[1].Type != ModelTextDelta || events[1].Text != "hi" {
		t.Fatalf("unexpected text event: %+v", events[1])
	}
	if events[2].Type != ModelToolCall || events[2].ToolCall == nil || events[2].ToolCall.ID != "call_1" || events[2].ToolCall.Name != "fts_search" || string(events[2].ToolCall.Args) != `{"query":"abc"}` {
		t.Fatalf("unexpected tool call event: %+v", events[2])
	}
	if events[3].Type != ModelDone || events[3].InteractionID != "resp_1" {
		t.Fatalf("unexpected done event: %+v", events[3])
	}
	if events[3].Usage.InputTokens != 13 || events[3].Usage.OutputTokens != 7 || events[3].Usage.TotalTokens != 20 {
		t.Fatalf("unexpected usage: %+v", events[3].Usage)
	}
}

func TestParseOpenAIResponsesSSEStreamError(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"type":"error","error":{"code":"bad_request","message":"nope"}}`,
		``,
	}, "\n")
	err := ParseOpenAIResponsesSSE(strings.NewReader(raw), func(ModelEvent) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected stream error, got %v", err)
	}
}

func TestOpenAIMessagesUseStrictToolResultRoles(t *testing.T) {
	messages := openAIMessages(ModelRequest{
		System: "test",
		Transcript: []ModelMessage{{
			Role:    "assistant",
			Content: "",
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fts_search",
				Args: []byte(`{"query":"abc"}`),
			}},
		}},
		Input: ModelInput{
			IsToolResult: true,
			ToolResults: []ToolResult{{
				CallID: "call-1",
				Name:   "fts_search",
				Result: map[string]any{"ok": true},
			}},
		},
	})
	if len(messages) != 3 {
		t.Fatalf("message count = %d, want 3: %+v", len(messages), messages)
	}
	assistant := messages[1]
	if assistant["role"] != "assistant" || assistant["tool_calls"] == nil {
		t.Fatalf("unexpected assistant message: %+v", assistant)
	}
	tool := messages[2]
	if tool["role"] != "tool" || tool["tool_call_id"] != "call-1" || tool["name"] != "fts_search" {
		t.Fatalf("unexpected tool result message: %+v", tool)
	}
}

func captureOpenAICompatibleRequest(t *testing.T, baseURL string, req ModelRequest) map[string]any {
	t.Helper()
	var body map[string]any
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"))),
		}, nil
	})}
	provider := &OpenAICompatibleProvider{BaseURL: baseURL, HTTPClient: client}
	if err := provider.Stream(context.Background(), req, func(ModelEvent) error { return nil }); err != nil {
		t.Fatalf("stream: %v", err)
	}
	return body
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
