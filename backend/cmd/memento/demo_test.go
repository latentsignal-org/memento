package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"memento/backend/internal/config"
	"memento/backend/internal/person"
	"memento/backend/internal/store"
)

func TestGuardDemoTargetRefusesUnmarkedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	err = guardDemoTarget(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "not marked") {
		t.Fatalf("guardDemoTarget error = %v, want unmarked refusal", err)
	}
}

func TestGuardDemoTargetAllowsMarkedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO memento_config (key, value) VALUES ('demo_mode', 'true')`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	if err := guardDemoTarget(context.Background(), path); err != nil {
		t.Fatalf("guardDemoTarget: %v", err)
	}
}

func TestGuardDemoTargetRefusesConfiguredArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.sqlite")
	t.Setenv(config.EnvMsgvaultDB, path)
	if err := guardDemoTarget(context.Background(), path); err == nil || !strings.Contains(err.Error(), "configured msgvault archive") {
		t.Fatalf("guardDemoTarget error = %v, want configured archive refusal", err)
	}
}

func TestPrepareDemoEnvBacksUpAndPreservesProviderConfig(t *testing.T) {
	temp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(temp); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)

	original := strings.Join([]string{
		config.EnvModelProvider + "=" + config.ProviderOpenAICompatible,
		config.EnvAgentModel + "=local-model",
		config.EnvModelAPIKey + "=secret",
		config.EnvModelBaseURL + "=http://127.0.0.1:9999",
		config.EnvMsgvaultDB + "=/real/archive.sqlite",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(temp, ".env"), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvModelProvider, config.ProviderOpenAICompatible)
	for _, key := range []string{
		config.EnvInternalToken,
		config.EnvBackendURL,
		config.EnvAgentStepLimit,
		config.EnvAgentSimulation,
		config.EnvPublicAgentSimulation,
	} {
		t.Setenv(key, "")
	}

	if err := prepareDemoEnv(8788); err != nil {
		t.Fatal(err)
	}
	if config.InternalToken() == "" {
		t.Fatal("generated internal token was not loaded into the current process")
	}
	data, err := os.ReadFile(filepath.Join(temp, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, preserved := range []string{
		config.EnvModelProvider + "=" + config.ProviderOpenAICompatible,
		config.EnvAgentModel + "=local-model",
		config.EnvModelAPIKey + "=secret",
		config.EnvModelBaseURL + "=http://127.0.0.1:9999",
		config.EnvMsgvaultDB + "=/real/archive.sqlite",
	} {
		if !strings.Contains(text, preserved) {
			t.Fatalf("updated .env did not preserve %q:\n%s", preserved, text)
		}
	}
	if !strings.Contains(text, config.EnvBackendURL+"=http://127.0.0.1:8788") {
		t.Fatalf("updated .env missing custom backend URL:\n%s", text)
	}
	backups, err := filepath.Glob(filepath.Join(temp, ".env.backup-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, err = %v, want one", backups, err)
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != original {
		t.Fatalf("backup changed: got %q want %q", backup, original)
	}
}

func TestPrepareDemoDatabaseBuildsActivePeopleAndSocialContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.sqlite")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := prepareDemoDatabase(ctx, db, path); err != nil {
		t.Fatal(err)
	}

	for _, check := range []struct {
		name  string
		query string
		min   int
	}{
		{"active people", `SELECT COUNT(*) FROM memento_people_candidates WHERE classification = 'candidate'`, 6},
		{"social edges", `SELECT COUNT(*) FROM memento_social_edge WHERE co_recipient_count > 0`, 6},
		{"social groups", `SELECT COUNT(*) FROM memento_social_group`, 1},
	} {
		var got int
		if err := db.QueryRowContext(ctx, check.query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got < check.min {
			t.Fatalf("%s = %d, want at least %d", check.name, got, check.min)
		}
	}

	var missingCitations int
	err = db.QueryRowContext(ctx, `
		WITH refs AS (
			SELECT value AS message_id FROM memento_person_narrative, json_each(source_message_ids)
			UNION ALL SELECT value FROM memento_person_facet, json_each(source_message_ids)
			UNION ALL SELECT value FROM memento_project_narrative, json_each(source_message_ids)
			UNION ALL SELECT value FROM memento_newsletter_narrative, json_each(source_message_ids)
			UNION ALL SELECT value FROM memento_concept_narrative, json_each(source_message_ids)
		)
		SELECT COUNT(*) FROM refs LEFT JOIN messages ON messages.id = refs.message_id WHERE messages.id IS NULL
	`).Scan(&missingCitations)
	if err != nil {
		t.Fatal(err)
	}
	if missingCitations != 0 {
		t.Fatalf("missing narrative citations = %d, want 0", missingCitations)
	}

	candidates, err := person.FindMergeCandidates(ctx, db, person.DefaultMergeOptions())
	if err != nil {
		t.Fatal(err)
	}
	wantPairs := map[[2]int64]bool{
		{1, 11}: false,
		{3, 12}: false,
	}
	for _, candidate := range candidates {
		pair := [2]int64{candidate.FromID, candidate.IntoID}
		if pair[0] > pair[1] {
			pair[0], pair[1] = pair[1], pair[0]
		}
		if _, ok := wantPairs[pair]; ok {
			wantPairs[pair] = true
		}
	}
	for pair, found := range wantPairs {
		if !found {
			t.Fatalf("merge candidate %v not found in %+v", pair, candidates)
		}
	}
}

func TestPrepareOnboardDemoDatabaseLeavesRichArchiveUninitialized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "onboard-demo.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := prepareOnboardDemoDatabase(ctx, db, path); err != nil {
		t.Fatal(err)
	}

	var messages int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages`).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if messages < 10 {
		t.Fatalf("messages = %d, want rich setup archive", messages)
	}

	var marker string
	if err := db.QueryRowContext(ctx, `SELECT value FROM memento_config WHERE key = 'demo_mode'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != "true" {
		t.Fatalf("demo marker = %q, want true", marker)
	}

	var ownerRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_config WHERE key = 'owner_name'`).Scan(&ownerRows); err != nil {
		t.Fatal(err)
	}
	if ownerRows != 0 {
		t.Fatalf("owner_name rows = %d, want onboarding to be incomplete", ownerRows)
	}
	var onboardingRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_config WHERE key = 'onboarding_status'`).Scan(&onboardingRows); err != nil {
		t.Fatal(err)
	}
	if onboardingRows != 0 {
		t.Fatalf("onboarding_status rows = %d, want onboarding to be incomplete", onboardingRows)
	}

	var reportTables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'memento_people_report'`).Scan(&reportTables); err != nil {
		t.Fatal(err)
	}
	if reportTables != 0 {
		t.Fatalf("memento_people_report exists in onboarding demo")
	}

	if err := guardDemoTarget(ctx, path); err != nil {
		t.Fatalf("guardDemoTarget should allow rerunning onboarding demo: %v", err)
	}
}
