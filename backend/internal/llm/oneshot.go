// Package llm provides small provider-neutral helpers for non-agent model calls.
package llm

import (
	"context"
	"fmt"
	"strings"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/config"
)

type OneShotRequest struct {
	Provider string
	Model    string
	System   string
	Prompt   string
}

type OneShotResponse struct {
	Text     string
	Provider string
	Model    string
}

type ResolvedConfig struct {
	Provider string
	Model    string
}

func ResolveConfig(req OneShotRequest) (ResolvedConfig, error) {
	providerName := strings.TrimSpace(req.Provider)
	if providerName == "" {
		providerName = config.ModelProvider()
	}
	if providerName == "" {
		return ResolvedConfig{}, fmt.Errorf("%s is not set", config.EnvModelProvider)
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = config.AgentModelFor("", providerName)
	}
	if model == "" {
		return ResolvedConfig{}, fmt.Errorf("model is not configured for provider %q", providerName)
	}
	return ResolvedConfig{Provider: providerName, Model: model}, nil
}

// OneShot sends a single prompt through the configured model provider and
// returns the accumulated text response. It is intended for concise generation
// tasks that do not need the durable agent loop or tool calls.
func OneShot(ctx context.Context, req OneShotRequest) (OneShotResponse, error) {
	config, err := ResolveConfig(req)
	if err != nil {
		return OneShotResponse{}, err
	}

	provider, err := providerForName(config.Provider)
	if err != nil {
		return OneShotResponse{}, err
	}

	var text strings.Builder
	streamReq := agentrunner.ModelRequest{
		Model:     config.Model,
		System:    req.System,
		Input:     agentrunner.ModelInput{UserMessage: req.Prompt},
		StepIndex: 1,
	}
	if err := provider.Stream(ctx, streamReq, func(ev agentrunner.ModelEvent) error {
		if ev.Type == agentrunner.ModelTextDelta {
			text.WriteString(ev.Text)
		}
		return nil
	}); err != nil {
		return OneShotResponse{}, err
	}

	out := strings.TrimSpace(text.String())
	if out == "" {
		return OneShotResponse{}, fmt.Errorf("model provider %q returned no text", config.Provider)
	}
	return OneShotResponse{Text: out, Provider: config.Provider, Model: config.Model}, nil
}

func providerForName(name string) (agentrunner.Provider, error) {
	switch name {
	case config.ProviderGemini:
		return &agentrunner.GeminiProvider{}, nil
	case config.ProviderOpenAICompatible:
		return &agentrunner.OpenAICompatibleProvider{}, nil
	case config.ProviderFake:
		return &agentrunner.FakeProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown model provider %q", name)
	}
}
