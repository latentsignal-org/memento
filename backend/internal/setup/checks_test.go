package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memento/backend/internal/config"
	"memento/backend/internal/demoseed"
	"memento/backend/internal/store"
)

func TestCheckMsgvaultDoesNotCreateMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	check := CheckMsgvault(context.Background(), path)
	if check.Status != StatusFail {
		t.Fatalf("status = %q, want fail: %+v", check.Status, check)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing database was created: %v", err)
	}
}

func TestFreshSQLiteReportsUninitializedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sqlite")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	check := CheckSchema(context.Background(), db)
	if check.Status != StatusWarn || !strings.Contains(check.Detail, "not initialized") {
		t.Fatalf("schema check = %+v", check)
	}
}

func TestDemoDatabaseChecksPass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.Migrate(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := demoseed.CreateMsgvaultTables(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := demoseed.SeedE2E(ctx, db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memento_config (key, value) VALUES ('demo_mode', 'true')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	for _, check := range []Check{
		CheckMsgvault(ctx, path),
		CheckSearch(ctx, path),
	} {
		if check.Status != StatusOK {
			t.Fatalf("check = %+v, want ok", check)
		}
	}
	vector := CheckVectorSearch(ctx, path)
	if vector.Status == StatusOK || strings.Contains(vector.Detail, "demo semantic") {
		t.Fatalf("vector check should not claim demo embeddings are ready: %+v", vector)
	}
	ro, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for _, check := range []Check{CheckSchema(ctx, ro), CheckOwnerConfig(ctx, ro), CheckRollups(ctx, ro)} {
		if check.Status != StatusOK {
			t.Fatalf("check = %+v, want ok", check)
		}
	}
}

func TestCheckEnvFileRejectsRemovedNamesAndWarnsOnFormatting(t *testing.T) {
	root := makeProjectRoot(t)
	withWorkingDirectory(t, root)
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("GO_BACKEND_URL=http://127.0.0.1:8787\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check := CheckEnvFile()
	if check.Status != StatusFail || !strings.Contains(check.Detail, "GO_BACKEND_URL") {
		t.Fatalf("removed-name check = %+v", check)
	}

	content := "MEMENTO_BACKEND_URL=http://127.0.0.1:8787 # local\nMEMENTO_BACKEND_URL=http://127.0.0.1:8787\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	check = CheckEnvFile()
	if check.Status != StatusWarn || !strings.Contains(check.Detail, "duplicate") || !strings.Contains(check.Detail, "inline") {
		t.Fatalf("formatting check = %+v", check)
	}
}

func TestCheckProviderSeverity(t *testing.T) {
	t.Setenv(config.EnvModelProvider, config.ProviderFake)
	if check := CheckProvider(); check.Status != StatusOK {
		t.Fatalf("fake provider = %+v", check)
	}
	t.Setenv(config.EnvModelProvider, config.ProviderOpenAICompatible)
	t.Setenv(config.EnvModelBaseURL, "")
	if check := CheckProvider(); check.Status != StatusFail {
		t.Fatalf("missing compatible base URL = %+v", check)
	}
	t.Setenv(config.EnvModelProvider, config.ProviderGemini)
	t.Setenv(config.EnvModelAPIKey, "")
	if check := CheckProvider(); check.Status != StatusWarn {
		t.Fatalf("missing Gemini key = %+v", check)
	}
}

func TestRunWizardChecksUsesFocusedPrimaryChecks(t *testing.T) {
	checks := RunWizardChecks(context.Background(), filepath.Join(t.TempDir(), "missing.sqlite"))
	primary := map[string]bool{}
	for _, check := range checks.Checks {
		primary[check.Name] = true
	}
	for _, name := range []string{"Project root", "Environment"} {
		if !primary[name] {
			t.Fatalf("primary wizard checks missing %q: %+v", name, checks.Checks)
		}
	}
	for _, hidden := range []string{"Go", "Node.js", "pnpm", "Frontend-backend bridge", "Msgvault archive", "Keyword search", "Vector search", "Owner", "Model provider"} {
		if primary[hidden] {
			t.Fatalf("primary wizard checks included %q: %+v", hidden, checks.Checks)
		}
	}
	archive := map[string]bool{}
	for _, check := range checks.ArchiveChecks {
		archive[check.Name] = true
	}
	for _, name := range []string{"Msgvault archive", "Keyword search", "Vector search"} {
		if !archive[name] {
			t.Fatalf("archive wizard checks missing %q: %+v", name, checks.ArchiveChecks)
		}
	}
	toolVersions := map[string]bool{}
	for _, check := range checks.ToolVersions {
		toolVersions[check.Name] = true
	}
	for _, name := range []string{"msgvault", "Go", "Node.js", "pnpm"} {
		if !toolVersions[name] {
			t.Fatalf("tool versions missing %q: %+v", name, checks.ToolVersions)
		}
	}
	var vector Check
	for _, check := range checks.ArchiveChecks {
		if check.Name == "Vector search" {
			vector = check
		}
	}
	if !strings.Contains(vector.Detail, "Vector embeddings") {
		t.Fatalf("vector detail is not user-friendly: %+v", vector)
	}
}

func TestRuntimeEnvDoesNotFailWithoutLegacyBridgeVariables(t *testing.T) {
	root := makeProjectRoot(t)
	withWorkingDirectory(t, root)
	t.Setenv(config.EnvInternalToken, "")
	t.Setenv(config.EnvBackendURL, "")

	check := CheckRuntimeEnv()
	if check.Status != StatusWarn {
		t.Fatalf("runtime env check = %+v, want warn", check)
	}
	if strings.Contains(check.Detail, config.EnvBackendURL) {
		t.Fatalf("runtime env check should not require dev backend URL: %+v", check)
	}
}

func TestRuntimeChecksOmitDeveloperToolingOutsideSourceCheckout(t *testing.T) {
	withWorkingDirectory(t, t.TempDir())

	checks := RunAllChecks(context.Background(), filepath.Join(t.TempDir(), "missing.sqlite"))
	all := map[string]bool{}
	for _, check := range checks {
		all[check.Name] = true
	}
	for _, name := range []string{"Project root", "Go", "Node.js", "pnpm", "Node dependencies"} {
		if all[name] {
			t.Fatalf("runtime checks included developer-only check %q: %+v", name, checks)
		}
	}

	wizard := RunWizardChecks(context.Background(), filepath.Join(t.TempDir(), "missing.sqlite"))
	wizardNames := map[string]bool{}
	for _, group := range [][]Check{wizard.Checks, wizard.ToolVersions, wizard.DeveloperChecks} {
		for _, check := range group {
			wizardNames[check.Name] = true
		}
	}
	for _, name := range []string{"Project root", "Go", "Node.js", "pnpm", "Node dependencies"} {
		if wizardNames[name] {
			t.Fatalf("wizard checks included developer-only check %q: %+v", name, wizard)
		}
	}
	if !wizardNames["msgvault"] {
		t.Fatalf("wizard checks should still include msgvault prerequisite: %+v", wizard)
	}
}

func makeProjectRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func withWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
