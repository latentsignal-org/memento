package setupenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memento/backend/internal/config"
)

func TestWriteKeyBacksUpOnceAndReloadsConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "go.mod"), []byte("module test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("MEMENTO_MODEL_PROVIDER=fake\n"), 0600); err != nil {
		t.Fatal(err)
	}
	oldDir, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	t.Setenv("MEMENTO_MODEL_PROVIDER", "fake")

	if err := WriteKey(config.EnvModelProvider, config.ProviderGemini); err != nil {
		t.Fatal(err)
	}
	if err := WriteKey(config.EnvAgentModel, "test-model"); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(envPath + ".backup-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("backups = %v, err = %v", matches, err)
	}
	backup, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "MEMENTO_MODEL_PROVIDER=fake\n" {
		t.Fatalf("backup = %q", backup)
	}

	config.LoadDotEnv()
	if got := config.ModelProvider(); got != config.ProviderGemini {
		t.Fatalf("provider = %q", got)
	}
	if got := config.EnvString(config.EnvAgentModel); got != "test-model" {
		t.Fatalf("model = %q", got)
	}
	data, _ := os.ReadFile(envPath)
	if strings.Contains(string(data), " #") {
		t.Fatalf("env contains inline comment: %s", data)
	}
}
