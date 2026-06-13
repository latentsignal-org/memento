package setup

import (
	"testing"

	"memento/backend/internal/config"
)

func TestValidateInitParams(t *testing.T) {
	valid := InitParams{
		OwnerName: "Alex", OwnerEmail: "alex@example.com", ModelProvider: config.ProviderFake,
		Model: "fake", MsgvaultDBPath: "/tmp/archive.sqlite",
	}
	if err := ValidateInitParams(valid); err != nil {
		t.Fatalf("valid params: %v", err)
	}

	missingBase := valid
	missingBase.ModelProvider = config.ProviderOpenAICompatible
	if err := ValidateInitParams(missingBase); err == nil {
		t.Fatal("expected missing base URL error")
	}

	badEmail := valid
	badEmail.OwnerEmail = "not-an-email"
	if err := ValidateInitParams(badEmail); err == nil {
		t.Fatal("expected invalid email error")
	}
}
