package agentrunner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"memento/backend/internal/config"
)

type FakeProvider struct {
	Events []ModelEvent
	Err    error
	Demo   bool
}

func (p *FakeProvider) Name() string { return "fake" }

func (p *FakeProvider) Stream(ctx context.Context, req ModelRequest, emit func(ModelEvent) error) error {
	if p.Err != nil {
		return p.Err
	}
	events := p.Events
	if len(events) == 0 {
		text := "Fake agent run complete."
		if p.Demo {
			text = demoReplayText(req.Input.UserMessage)
		}
		events = []ModelEvent{
			{Type: ModelTextDelta, Text: text},
			{Type: ModelDone, InteractionID: "fake-interaction"},
		}
	}
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

func demoReplayText(question string) string {
	question = strings.ToLower(question)
	switch {
	case strings.Contains(question, "maya"), strings.Contains(question, "relationship"):
		return "Maya Chen is a product lead and recurring planning partner. The relationship centers on the Atlas workspace roadmap, product decisions, and customer interviews. [msg:2001][msg:2002]"
	case strings.Contains(question, "newsletter"), strings.Contains(question, "knowledge system"):
		return "Signal Weekly covers retrieval, synthesis, and source-aware knowledge systems, while Local Current focuses on practical local-first software and user-owned archives. [msg:2071][msg:2051]"
	case strings.Contains(question, "project"), strings.Contains(question, "atlas"):
		return "Atlas Workspace is the main active product narrative in the demo archive, spanning architecture, citation design, and the five dimensional memory surfaces. [msg:2041][msg:2043][msg:2044]"
	default:
		return "The demo archive contains cited relationship, project, newsletter, and concept history. Try asking about Maya Chen, Atlas Workspace, or the newsletters covering knowledge systems. [msg:2001][msg:2041][msg:2071]"
	}
}

type GeminiProvider struct {
	APIKey     string
	HTTPClient *http.Client
	Endpoint   string
}

func (p *GeminiProvider) Name() string { return "gemini" }

func (p *GeminiProvider) Stream(ctx context.Context, req ModelRequest, emit func(ModelEvent) error) error {
	apiKey := p.APIKey
	if apiKey == "" {
		apiKey = config.ModelAPIKey()
	}
	if apiKey == "" {
		return fmt.Errorf("%s is not set", config.EnvModelAPIKey)
	}
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = config.GeminiEndpoint()
	}
	body := map[string]any{
		"model":              req.Model,
		"input":              geminiInput(req.Input),
		"system_instruction": req.System,
		"tools":              req.Tools,
		"stream":             true,
		"generation_config": map[string]any{
			"thinking_level": "high",
		},
	}
	if req.PreviousInteractionID != "" {
		body["previous_interaction_id"] = req.PreviousInteractionID
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	agentInfof("[agentrunner] LLM provider request run_id=%d provider=gemini model=%s endpoint=%s step=%d messages=%d tools=%d previous_interaction=%t input_tool_result=%t",
		req.RunID, req.Model, redactURL(endpoint), req.StepIndex, len(req.Transcript)+1, len(req.Tools), req.PreviousInteractionID != "", req.Input.IsToolResult)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("x-goog-api-key", apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Api-Revision", "2026-05-20")
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		agentInfof("[agentrunner] LLM provider error run_id=%d provider=gemini model=%s step=%d duration=%s error=%v",
			req.RunID, req.Model, req.StepIndex, time.Since(start), err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		agentInfof("[agentrunner] LLM provider response run_id=%d provider=gemini model=%s step=%d status=%s duration=%s",
			req.RunID, req.Model, req.StepIndex, resp.Status, time.Since(start))
		return fmt.Errorf("gemini interactions request failed: %s url=%s: %s", resp.Status, redactURL(endpoint), summarizeErrorBody(resp.Header.Get("Content-Type"), text))
	}
	err = ParseGeminiSSE(resp.Body, emit)
	if err != nil {
		agentInfof("[agentrunner] LLM provider stream_error run_id=%d provider=gemini model=%s step=%d status=%s duration=%s error=%v",
			req.RunID, req.Model, req.StepIndex, resp.Status, time.Since(start), err)
		return err
	}
	agentInfof("[agentrunner] LLM provider response run_id=%d provider=gemini model=%s step=%d status=%s duration=%s",
		req.RunID, req.Model, req.StepIndex, resp.Status, time.Since(start))
	return nil
}

func geminiInput(input ModelInput) any {
	if !input.IsToolResult {
		return []map[string]string{{"type": "text", "text": input.UserMessage}}
	}
	out := make([]map[string]any, 0, len(input.ToolResults))
	for _, r := range input.ToolResults {
		text, _ := json.Marshal(r.Result)
		out = append(out, map[string]any{
			"type":    "function_result",
			"name":    r.Name,
			"call_id": r.CallID,
			"result":  []map[string]string{{"type": "text", "text": string(text)}},
		})
	}
	return out
}

type geminiStreamEvent struct {
	EventType string `json:"event_type"`
	Index     *int   `json:"index"`
	Step      *struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"step"`
	Delta *struct {
		Type      string `json:"type"`
		Text      string `json:"text"`
		Thought   string `json:"thought"`
		Thinking  string `json:"thinking"`
		Arguments string `json:"arguments"`
	} `json:"delta"`
	Interaction *struct {
		ID string `json:"id"`
	} `json:"interaction"`
	UsageMetadata *struct {
		PromptTokenCount     int64 `json:"prompt_token_count"`
		CandidatesTokenCount int64 `json:"candidates_token_count"`
		TotalTokenCount      int64 `json:"total_token_count"`
	} `json:"usage_metadata"`
}

// extractGeminiUsage searches the raw event payload for token-usage data
// using every spelling Gemini has shipped: top-level `usage_metadata` /
// `usageMetadata`, nested under `interaction`, or nested under `step`. The
// typed struct above only catches snake_case at the top level; newer Api-
// Revisions deliver the block in different shapes and we'd silently lose
// model token totals.
func extractGeminiUsage(raw []byte) ModelUsage {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ModelUsage{}
	}
	for _, candidate := range []any{
		doc["usage_metadata"], doc["usageMetadata"],
		nestedField(doc["interaction"], "usage_metadata", "usageMetadata", "usage"),
		nestedField(doc["step"], "usage_metadata", "usageMetadata", "usage"),
		doc["usage"],
	} {
		if u, ok := geminiUsageFromAny(candidate); ok {
			return u
		}
	}
	return ModelUsage{}
}

func nestedField(parent any, keys ...string) any {
	m, ok := parent.(map[string]any)
	if !ok {
		return nil
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func geminiUsageFromAny(v any) (ModelUsage, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return ModelUsage{}, false
	}
	input := numericField(m, "prompt_token_count", "promptTokenCount", "input_tokens", "inputTokens")
	output := numericField(m, "candidates_token_count", "candidatesTokenCount", "output_tokens", "outputTokens", "completion_tokens", "completionTokens")
	total := numericField(m, "total_token_count", "totalTokenCount", "total_tokens", "totalTokens")
	if input == 0 && output == 0 && total == 0 {
		return ModelUsage{}, false
	}
	if total == 0 {
		total = input + output
	}
	return ModelUsage{InputTokens: input, OutputTokens: output, TotalTokens: total}, true
}

func numericField(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		case json.Number:
			n, _ := v.Int64()
			return n
		}
	}
	return 0
}

func ParseGeminiSSE(r io.Reader, emit func(ModelEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	pending := map[int]*ToolCall{}
	argBuffers := map[int]string{}
	debug := config.AgentVerboseLogs()
	var runningUsage ModelUsage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		if debug {
			fmt.Fprintf(os.Stderr, "[gemini-sse] %s\n", data)
		}
		if u := extractGeminiUsage([]byte(data)); u.TotalTokens > 0 || u.InputTokens > 0 || u.OutputTokens > 0 {
			runningUsage = u
		}
		var ev geminiStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("parse gemini SSE: %w", err)
		}
		switch ev.EventType {
		case "step.start":
			if ev.Step != nil && ev.Step.Type == "function_call" && ev.Index != nil {
				args := ev.Step.Arguments
				if len(args) == 0 {
					args = json.RawMessage(`{}`)
				}
				pending[*ev.Index] = &ToolCall{ID: ev.Step.ID, Name: ev.Step.Name, Args: args}
			}
		case "step.delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text":
				if err := emit(ModelEvent{Type: ModelTextDelta, Text: ev.Delta.Text}); err != nil {
					return err
				}
			case "thought", "thinking":
				text := ev.Delta.Thought
				if text == "" {
					text = ev.Delta.Thinking
				}
				if text == "" {
					text = ev.Delta.Text
				}
				if err := emit(ModelEvent{Type: ModelReasoningDelta, ReasoningText: text}); err != nil {
					return err
				}
			case "arguments_delta":
				if ev.Index != nil {
					argBuffers[*ev.Index] += ev.Delta.Arguments
				}
			}
		case "step.stop":
			if ev.Index == nil {
				continue
			}
			call := pending[*ev.Index]
			if call == nil {
				continue
			}
			if buf := strings.TrimSpace(argBuffers[*ev.Index]); buf != "" {
				call.Args = json.RawMessage(buf)
			}
			if len(call.Args) == 0 || !json.Valid(call.Args) {
				call.Args = json.RawMessage(`{}`)
			}
			if err := emit(ModelEvent{Type: ModelToolCall, ToolCall: call}); err != nil {
				return err
			}
		case "interaction.completed", "interaction.complete":
			id := ""
			if ev.Interaction != nil {
				id = ev.Interaction.ID
			}
			state, _ := json.Marshal(map[string]string{"interaction_id": id})
			usage := runningUsage
			if usage.TotalTokens == 0 && usage.InputTokens == 0 && usage.OutputTokens == 0 && ev.UsageMetadata != nil {
				usage = ModelUsage{
					InputTokens:  ev.UsageMetadata.PromptTokenCount,
					OutputTokens: ev.UsageMetadata.CandidatesTokenCount,
					TotalTokens:  ev.UsageMetadata.TotalTokenCount,
				}
			}
			if err := emit(ModelEvent{Type: ModelDone, InteractionID: id, ProviderState: state, Usage: usage}); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

type OpenAICompatibleProvider struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func (p *OpenAICompatibleProvider) Name() string { return "openai_compatible" }

func (p *OpenAICompatibleProvider) Stream(ctx context.Context, req ModelRequest, emit func(ModelEvent) error) error {
	baseURL := strings.TrimRight(p.BaseURL, "/")
	if baseURL == "" {
		baseURL = config.ModelBaseURL()
	}
	if baseURL == "" {
		return fmt.Errorf("%s is not set", config.EnvModelBaseURL)
	}
	apiKey := p.APIKey
	if apiKey == "" {
		apiKey = config.ModelAPIKey()
	}
	if useOpenAIResponsesAPI(baseURL) {
		return p.streamOpenAIResponses(ctx, req, emit, baseURL, apiKey)
	}
	return p.streamOpenAIChatCompletions(ctx, req, emit, baseURL, apiKey)
}

func (p *OpenAICompatibleProvider) streamOpenAIChatCompletions(ctx context.Context, req ModelRequest, emit func(ModelEvent) error, baseURL, apiKey string) error {
	body := map[string]any{
		"model":          req.Model,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
		"messages":       openAIMessages(req),
		"tools":          openAITools(req.Tools),
	}
	applyDeepSeekV4Thinking(body, req.Model)
	return p.postOpenAICompatibleStream(ctx, req, emit, openAIChatCompletionsEndpoint(baseURL), apiKey, body, ParseOpenAICompatibleSSE)
}

func (p *OpenAICompatibleProvider) streamOpenAIResponses(ctx context.Context, req ModelRequest, emit func(ModelEvent) error, baseURL, apiKey string) error {
	body := openAIResponsesBody(req)
	return p.postOpenAICompatibleStream(ctx, req, emit, openAIResponsesEndpoint(baseURL), apiKey, body, ParseOpenAIResponsesSSE)
}

func (p *OpenAICompatibleProvider) postOpenAICompatibleStream(ctx context.Context, req ModelRequest, emit func(ModelEvent) error, endpoint, apiKey string, body map[string]any, parse func(io.Reader, func(ModelEvent) error) error) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	agentInfof("[agentrunner] LLM provider request run_id=%d provider=openai_compatible model=%s endpoint=%s step=%d messages=%d tools=%d input_tool_result=%t",
		req.RunID, req.Model, redactURL(endpoint), req.StepIndex, openAIRequestInputCount(body), len(req.Tools), req.Input.IsToolResult)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		agentInfof("[agentrunner] LLM provider error run_id=%d provider=openai_compatible model=%s step=%d duration=%s error=%v",
			req.RunID, req.Model, req.StepIndex, time.Since(start), err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		agentInfof("[agentrunner] LLM provider response run_id=%d provider=openai_compatible model=%s step=%d status=%s duration=%s",
			req.RunID, req.Model, req.StepIndex, resp.Status, time.Since(start))
		return fmt.Errorf("openai-compatible request failed: %s url=%s: %s", resp.Status, redactURL(endpoint), summarizeErrorBody(resp.Header.Get("Content-Type"), text))
	}
	err = parse(resp.Body, emit)
	if err != nil {
		agentInfof("[agentrunner] LLM provider stream_error run_id=%d provider=openai_compatible model=%s step=%d status=%s duration=%s error=%v",
			req.RunID, req.Model, req.StepIndex, resp.Status, time.Since(start), err)
		return err
	}
	agentInfof("[agentrunner] LLM provider response run_id=%d provider=openai_compatible model=%s step=%d status=%s duration=%s",
		req.RunID, req.Model, req.StepIndex, resp.Status, time.Since(start))
	return nil
}

func openAIRequestInputCount(body map[string]any) int {
	if messages, ok := body["messages"].([]map[string]any); ok {
		return len(messages)
	}
	if input, ok := body["input"].([]map[string]any); ok {
		return len(input)
	}
	return 0
}

func redactURL(raw string) string {
	u, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil || u.URL == nil {
		return raw
	}
	u.URL.RawQuery = ""
	return u.URL.String()
}

func summarizeErrorBody(contentType string, body []byte) string {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return fmt.Sprintf("<HTML response, %d bytes; open the URL in a browser to inspect>", len(body))
	}
	return strings.TrimSpace(string(body))
}

func openAIChatCompletionsEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/chat/completions"
}

func openAIResponsesEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/responses"
}

func useOpenAIResponsesAPI(baseURL string) bool {
	if config.ModelReasoningEffort() == "" {
		return false
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	return strings.ToLower(u.Hostname()) == "api.openai.com"
}

func applyDeepSeekV4Thinking(body map[string]any, model string) {
	if !isDeepSeekV4Model(model) {
		return
	}
	thinking := normalizeDeepSeekThinking(config.ModelThinking())
	effort := normalizeDeepSeekReasoningEffort(config.ModelReasoningEffort())
	if thinking == "disabled" {
		body["thinking"] = map[string]any{"type": "disabled"}
		return
	}
	if thinking == "enabled" || effort != "" {
		body["thinking"] = map[string]any{"type": "enabled"}
	}
	if effort != "" {
		body["reasoning_effort"] = effort
	}
}

func isDeepSeekV4Model(model string) bool {
	normalized := strings.ToLower(model)
	return strings.Contains(normalized, "deepseek") && strings.Contains(normalized, "v4")
}

func normalizeDeepSeekThinking(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enabled", "enable", "on", "true", "1":
		return "enabled"
	case "disabled", "disable", "off", "false", "0":
		return "disabled"
	default:
		return ""
	}
}

func normalizeDeepSeekReasoningEffort(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high":
		return "high"
	case "xhigh", "max":
		return "max"
	default:
		return ""
	}
}

func openAIMessages(req ModelRequest) []map[string]any {
	messages := []map[string]any{{"role": "system", "content": req.System}}
	for _, msg := range req.Transcript {
		role := msg.Role
		if role != "user" && role != "assistant" && role != "tool" {
			role = "user"
		}
		entry := map[string]any{"role": role, "content": msg.Content}
		if role == "assistant" && len(msg.ToolCalls) > 0 {
			entry["tool_calls"] = openAIMessageToolCalls(msg.ToolCalls)
		}
		// OpenAI spec has no `reasoning_content` on assistant messages. DeepSeek
		// thinking mode requires it replayed; all other providers reject it.
		if role == "assistant" && msg.Reasoning != "" && config.ModelReplayReasoning() && normalizeDeepSeekThinking(config.ModelThinking()) != "disabled" {
			entry["reasoning_content"] = msg.Reasoning
		}
		if role == "tool" {
			entry["tool_call_id"] = msg.ToolCallID
			if msg.ToolName != "" {
				entry["name"] = msg.ToolName
			}
		}
		messages = append(messages, entry)
	}
	if req.Input.IsToolResult {
		messages = append(messages, openAIToolResultMessages(req.Input.ToolResults)...)
	} else {
		messages = append(messages, openAIInputMessage(req.Input))
	}
	return messages
}

func openAIInputMessage(input ModelInput) map[string]any {
	if !input.IsToolResult {
		return map[string]any{"role": "user", "content": input.UserMessage}
	}
	toolMessages := openAIToolResultMessages(input.ToolResults)
	if len(toolMessages) == 0 {
		return map[string]any{"role": "user", "content": ""}
	}
	return toolMessages[0]
}

func openAIMessageToolCalls(calls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for i, call := range calls {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("tool-%d", i)
		}
		args := string(call.Args)
		if args == "" {
			args = "{}"
		}
		out = append(out, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": args,
			},
		})
	}
	return out
}

func openAIToolResultMessages(results []ToolResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for i, result := range results {
		raw, _ := json.Marshal(result.Result)
		id := result.CallID
		if id == "" {
			id = fmt.Sprintf("tool-%d", i)
		}
		msg := map[string]any{
			"role":         "tool",
			"tool_call_id": id,
			"content":      string(raw),
		}
		if result.Name != "" {
			msg["name"] = result.Name
		}
		out = append(out, msg)
	}
	return out
}

func openAITools(tools []ToolSchema) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		var params any = map[string]any{"type": "object", "properties": map[string]any{}}
		if len(t.Parameters) > 0 {
			_ = json.Unmarshal(t.Parameters, &params)
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		})
	}
	return out
}

func openAIResponsesBody(req ModelRequest) map[string]any {
	body := map[string]any{
		"model":        req.Model,
		"stream":       true,
		"instructions": req.System,
		"input":        openAIResponsesInput(req),
		"tools":        openAIResponsesTools(req.Tools),
	}
	if effort := config.ModelReasoningEffort(); effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
	}
	if req.PreviousInteractionID != "" {
		body["previous_response_id"] = req.PreviousInteractionID
	}
	return body
}

func openAIResponsesInput(req ModelRequest) []map[string]any {
	if req.PreviousInteractionID != "" {
		return openAIResponsesCurrentInput(req.Input)
	}
	input := make([]map[string]any, 0, len(req.Transcript)+1)
	for _, msg := range req.Transcript {
		switch msg.Role {
		case "assistant":
			if strings.TrimSpace(msg.Content) != "" {
				input = append(input, map[string]any{"role": "assistant", "content": msg.Content})
			}
			input = append(input, openAIResponsesFunctionCalls(msg.ToolCalls)...)
		case "tool":
			output := strings.TrimSpace(msg.Content)
			if output == "" {
				output = "{}"
			}
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  output,
			})
		case "user":
			input = append(input, map[string]any{"role": "user", "content": msg.Content})
		default:
			if strings.TrimSpace(msg.Content) != "" {
				input = append(input, map[string]any{"role": "user", "content": msg.Content})
			}
		}
	}
	return append(input, openAIResponsesCurrentInput(req.Input)...)
}

func openAIResponsesCurrentInput(input ModelInput) []map[string]any {
	if input.IsToolResult {
		return openAIResponsesFunctionCallOutputs(input.ToolResults)
	}
	return []map[string]any{{"role": "user", "content": input.UserMessage}}
}

func openAIResponsesFunctionCalls(calls []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for i, call := range calls {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("tool-%d", i)
		}
		args := string(call.Args)
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		out = append(out, map[string]any{
			"type":      "function_call",
			"call_id":   id,
			"name":      call.Name,
			"arguments": args,
		})
	}
	return out
}

func openAIResponsesFunctionCallOutputs(results []ToolResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for i, result := range results {
		raw, _ := json.Marshal(result.Result)
		id := result.CallID
		if id == "" {
			id = fmt.Sprintf("tool-%d", i)
		}
		out = append(out, map[string]any{
			"type":    "function_call_output",
			"call_id": id,
			"output":  string(raw),
		})
	}
	return out
}

func openAIResponsesTools(tools []ToolSchema) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		var params any = map[string]any{"type": "object", "properties": map[string]any{}}
		if len(t.Parameters) > 0 {
			_ = json.Unmarshal(t.Parameters, &params)
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        t.Name,
			"description": t.Description,
			"parameters":  params,
		})
	}
	return out
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			// DeepSeek native streams the thinking trace as `reasoning_content`;
			// OpenRouter-style gateways normalize it to `reasoning`. Capture both.
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}

type openAIResponsesStreamEvent struct {
	Type        string                `json:"type"`
	Delta       string                `json:"delta"`
	ItemID      string                `json:"item_id"`
	OutputIndex int                   `json:"output_index"`
	Name        string                `json:"name"`
	Arguments   string                `json:"arguments"`
	Response    *openAIResponsesReply `json:"response"`
	Item        *openAIResponsesItem  `json:"item"`
	Error       *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type openAIResponsesReply struct {
	ID    string `json:"id"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Usage *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

type openAIResponsesItem struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Role      string `json:"role"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Content   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func ParseOpenAICompatibleSSE(r io.Reader, emit func(ModelEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	calls := map[int]*ToolCall{}
	argBuffers := map[int]string{}
	var interactionID string
	var usage ModelUsage
	debug := config.AgentVerboseLogs()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		if debug {
			fmt.Fprintf(os.Stderr, "[openai-compatible-sse] %s\n", data)
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("parse OpenAI-compatible SSE: %w", err)
		}
		if chunk.ID != "" {
			interactionID = chunk.ID
		}
		if chunk.Usage != nil {
			usage = ModelUsage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}
		for _, choice := range chunk.Choices {
			if reasoning := choice.Delta.ReasoningContent; reasoning != "" {
				if err := emit(ModelEvent{Type: ModelReasoningDelta, ReasoningText: reasoning}); err != nil {
					return err
				}
			} else if reasoning := choice.Delta.Reasoning; reasoning != "" {
				if err := emit(ModelEvent{Type: ModelReasoningDelta, ReasoningText: reasoning}); err != nil {
					return err
				}
			}
			if choice.Delta.Content != "" {
				if err := emit(ModelEvent{Type: ModelTextDelta, Text: choice.Delta.Content}); err != nil {
					return err
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				call := calls[tc.Index]
				if call == nil {
					call = &ToolCall{Args: json.RawMessage(`{}`)}
					calls[tc.Index] = call
				}
				if tc.ID != "" {
					call.ID = tc.ID
				}
				if tc.Function.Name != "" {
					call.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					argBuffers[tc.Index] += tc.Function.Arguments
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	keys := make([]int, 0, len(calls))
	for i := range calls {
		keys = append(keys, i)
	}
	sort.Ints(keys)
	for _, i := range keys {
		call := calls[i]
		if call == nil || call.Name == "" {
			continue
		}
		if buf := strings.TrimSpace(argBuffers[i]); buf != "" && json.Valid([]byte(buf)) {
			call.Args = json.RawMessage(buf)
		}
		if call.ID == "" {
			call.ID = fmt.Sprintf("tool-%d", i)
		}
		if err := emit(ModelEvent{Type: ModelToolCall, ToolCall: call}); err != nil {
			return err
		}
	}
	state, _ := json.Marshal(map[string]string{"interaction_id": interactionID})
	return emit(ModelEvent{Type: ModelDone, InteractionID: interactionID, ProviderState: state, Usage: usage})
}

type openAIResponsesCall struct {
	OutputIndex int
	ItemID      string
	CallID      string
	Name        string
	Args        string
}

func ParseOpenAIResponsesSSE(r io.Reader, emit func(ModelEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	calls := map[string]*openAIResponsesCall{}
	order := []string{}
	var interactionID string
	var usage ModelUsage
	var sawTextDelta bool
	debug := config.AgentVerboseLogs()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		if debug {
			fmt.Fprintf(os.Stderr, "[openai-responses-sse] %s\n", data)
		}
		var ev openAIResponsesStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("parse OpenAI Responses SSE: %w", err)
		}
		if ev.Response != nil {
			if ev.Response.ID != "" {
				interactionID = ev.Response.ID
			}
			if ev.Response.Error != nil {
				return fmt.Errorf("OpenAI Responses stream failed: %s", ev.Response.Error.Message)
			}
			if ev.Response.Usage != nil {
				usage = ModelUsage{
					InputTokens:  ev.Response.Usage.InputTokens,
					OutputTokens: ev.Response.Usage.OutputTokens,
					TotalTokens:  ev.Response.Usage.TotalTokens,
				}
			}
		}
		if ev.Error != nil {
			return fmt.Errorf("OpenAI Responses stream error: %s", ev.Error.Message)
		}
		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				sawTextDelta = true
				if err := emit(ModelEvent{Type: ModelTextDelta, Text: ev.Delta}); err != nil {
					return err
				}
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			if ev.Delta != "" {
				if err := emit(ModelEvent{Type: ModelReasoningDelta, ReasoningText: ev.Delta}); err != nil {
					return err
				}
			}
		case "response.function_call_arguments.delta":
			call := ensureOpenAIResponsesCall(calls, &order, ev.ItemID, ev.OutputIndex)
			call.Args += ev.Delta
		case "response.function_call_arguments.done":
			call := ensureOpenAIResponsesCall(calls, &order, ev.ItemID, ev.OutputIndex)
			call.Name = ev.Name
			if ev.Arguments != "" {
				call.Args = ev.Arguments
			}
		case "response.output_item.done":
			if ev.Item == nil {
				continue
			}
			if ev.Item.Type == "function_call" {
				call := ensureOpenAIResponsesCall(calls, &order, firstNonEmpty(ev.Item.ID, ev.ItemID), ev.OutputIndex)
				call.ItemID = firstNonEmpty(ev.Item.ID, call.ItemID)
				call.CallID = ev.Item.CallID
				call.Name = firstNonEmpty(ev.Item.Name, call.Name)
				if ev.Item.Arguments != "" {
					call.Args = ev.Item.Arguments
				}
			} else if ev.Item.Type == "message" && !sawTextDelta {
				for _, part := range ev.Item.Content {
					if part.Type == "output_text" && part.Text != "" {
						if err := emit(ModelEvent{Type: ModelTextDelta, Text: part.Text}); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	sort.SliceStable(order, func(i, j int) bool {
		return calls[order[i]].OutputIndex < calls[order[j]].OutputIndex
	})
	for i, key := range order {
		call := calls[key]
		if call == nil || call.Name == "" {
			continue
		}
		id := firstNonEmpty(call.CallID, call.ItemID)
		if id == "" {
			id = fmt.Sprintf("tool-%d", i)
		}
		args := strings.TrimSpace(call.Args)
		if args == "" || !json.Valid([]byte(args)) {
			args = "{}"
		}
		if err := emit(ModelEvent{Type: ModelToolCall, ToolCall: &ToolCall{
			ID:   id,
			Name: call.Name,
			Args: json.RawMessage(args),
		}}); err != nil {
			return err
		}
	}
	state, _ := json.Marshal(map[string]string{"interaction_id": interactionID})
	return emit(ModelEvent{Type: ModelDone, InteractionID: interactionID, ProviderState: state, Usage: usage})
}

func ensureOpenAIResponsesCall(calls map[string]*openAIResponsesCall, order *[]string, itemID string, outputIndex int) *openAIResponsesCall {
	key := itemID
	if key == "" {
		key = fmt.Sprintf("output-%d", outputIndex)
	}
	call := calls[key]
	if call == nil {
		call = &openAIResponsesCall{ItemID: itemID, OutputIndex: outputIndex}
		calls[key] = call
		*order = append(*order, key)
	}
	if itemID != "" {
		call.ItemID = itemID
	}
	return call
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
