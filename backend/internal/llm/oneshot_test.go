package llm

import (
	"context"
	"strings"
	"testing"
)

func TestOneShotUsesConfiguredProvider(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_PROVIDER", "fake")
	t.Setenv("MEMENTO_AGENT_MODEL", "")

	resp, err := OneShot(context.Background(), OneShotRequest{Prompt: "Name this group"})
	if err != nil {
		t.Fatalf("OneShot returned error: %v", err)
	}
	if resp.Provider != "fake" {
		t.Fatalf("provider = %q, want fake", resp.Provider)
	}
	if resp.Model != "fake" {
		t.Fatalf("model = %q, want fake", resp.Model)
	}
	if !strings.Contains(resp.Text, "Fake agent run complete") {
		t.Fatalf("text = %q, want fake provider text", resp.Text)
	}
}

func TestResolveConfigDefaultsToGemini(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_PROVIDER", "")

	cfg, err := ResolveConfig(OneShotRequest{Prompt: "Name this group"})
	if err != nil {
		t.Fatalf("ResolveConfig returned error: %v", err)
	}
	if cfg.Provider != "gemini" {
		t.Fatalf("provider = %q, want gemini", cfg.Provider)
	}
	if cfg.Model != "gemini-3.5-flash" {
		t.Fatalf("model = %q, want gemini-3.5-flash", cfg.Model)
	}
}

func TestOneShotUsesSharedAgentModel(t *testing.T) {
	t.Setenv("MEMENTO_MODEL_PROVIDER", "fake")
	t.Setenv("MEMENTO_AGENT_MODEL", "agent-model")

	resp, err := OneShot(context.Background(), OneShotRequest{Prompt: "Name this group"})
	if err != nil {
		t.Fatalf("OneShot returned error: %v", err)
	}
	if resp.Model != "agent-model" {
		t.Fatalf("model = %q, want agent-model", resp.Model)
	}
}
