package server

import "testing"

func TestTranscriptFromStoredLines(t *testing.T) {
	got := transcriptFromStoredLines(`[
		{"role":"user","content":"first"},
		{"role":"assistant","content":"second"},
		{"role":"tool","content":"ignored"},
		{"role":"user","content":"   "}
	]`)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[0].Content != "first" || got[1].Role != "assistant" || got[1].Content != "second" {
		t.Fatalf("unexpected transcript: %+v", got)
	}
}

func TestTranscriptFromMetadata(t *testing.T) {
	got := transcriptFromMetadata([]any{
		map[string]any{"role": "user", "content": "hello"},
		map[string]any{"role": "assistant", "content": "hi"},
		map[string]any{"role": "system", "content": "ignored"},
	})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[1].Role != "assistant" {
		t.Fatalf("unexpected roles: %+v", got)
	}
}

func TestAgentMaxParallelToolsDefaultDependsOnMsgvaultAPI(t *testing.T) {
	t.Setenv("MEMENTO_MSGVAULT_API_URL", "")
	if got := agentMaxParallelTools(); got != 4 {
		t.Fatalf("agentMaxParallelTools without API = %d, want 4", got)
	}

	t.Setenv("MEMENTO_MSGVAULT_API_URL", "http://127.0.0.1:8080")
	if got := agentMaxParallelTools(); got != 8 {
		t.Fatalf("agentMaxParallelTools with API = %d, want 8", got)
	}
}
