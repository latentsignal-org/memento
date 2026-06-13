package agentrunner

import (
	"context"
	"strings"
	"testing"
)

func TestDemoFakeProviderReplaysCitedAnswer(t *testing.T) {
	provider := &FakeProvider{Demo: true}
	var text string
	err := provider.Stream(context.Background(), ModelRequest{
		Input: ModelInput{UserMessage: "What is my relationship with Maya?"},
	}, func(event ModelEvent) error {
		if event.Type == ModelTextDelta {
			text += event.Text
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Maya Chen") || !strings.Contains(text, "[msg:2001]") {
		t.Fatalf("demo replay = %q, want Maya answer with citation", text)
	}
}
