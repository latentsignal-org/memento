package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"memento/backend/internal/config"
	"memento/backend/internal/demoseed"
	"memento/backend/internal/msgvault"
	"memento/backend/internal/store"
)

func newSetupTestServer(t *testing.T) *Server {
	t.Helper()
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
	oldDir, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
	t.Setenv(config.EnvModelProvider, config.ProviderFake)
	t.Setenv(config.EnvAgentModel, "fake")

	dbPath := filepath.Join(root, "archive.sqlite")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := demoseed.CreateMsgvaultTables(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	reader, err := msgvault.OpenReader(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reader.Close() })
	return New(Options{DBPath: dbPath}, db, reader)
}

func TestSetupStatusNeverReturnsAPIKey(t *testing.T) {
	srv := newSetupTestServer(t)
	t.Setenv(config.EnvModelAPIKey, "secret-value")
	req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	recorder := httptest.NewRecorder()
	srv.mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-value") {
		t.Fatal("status response exposed API key")
	}
	var response struct {
		Provider struct {
			HasAPIKey bool `json:"hasApiKey"`
		} `json:"provider"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !response.Provider.HasAPIKey {
		t.Fatal("expected hasApiKey=true")
	}
}

func TestSetupStatusIncludesArchivePreviewAndSplitChecks(t *testing.T) {
	srv := newSetupTestServer(t)
	if err := demoseed.SeedE2E(context.Background(), srv.db); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	recorder := httptest.NewRecorder()
	srv.mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Checks         []setupStatusCheck  `json:"checks"`
		ArchiveChecks  []setupStatusCheck  `json:"archiveChecks"`
		ToolVersions   []setupStatusCheck  `json:"toolVersions"`
		ArchivePreview []setupEmailPreview `json:"archivePreview"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.ToolVersions) == 0 {
		t.Fatal("expected grouped tool versions")
	}
	if len(response.ArchivePreview) == 0 {
		t.Fatal("expected archive preview emails")
	}
	for _, check := range response.Checks {
		if check.Name == "Msgvault archive" || check.Name == "Keyword search" || check.Name == "Vector search" || check.Name == "Frontend-backend bridge" {
			t.Fatalf("non-preflight check leaked into preflight checks: %+v", response.Checks)
		}
	}
	var hasArchive, hasSearch, hasVector bool
	for _, check := range response.ArchiveChecks {
		hasArchive = hasArchive || check.Name == "Msgvault archive"
		hasSearch = hasSearch || check.Name == "Keyword search"
		hasVector = hasVector || check.Name == "Vector search"
	}
	if !hasArchive || !hasSearch || !hasVector {
		t.Fatalf("archive checks missing archive/search/vector rows: %+v", response.ArchiveChecks)
	}
}

type setupStatusCheck struct {
	Name string `json:"name"`
}

func TestSetupMutationsReturnConflictAfterInitialization(t *testing.T) {
	srv := newSetupTestServer(t)
	if err := store.SetConfig(context.Background(), srv.db, "onboarding_status", "complete"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/setup/env", bytes.NewBufferString(`{"modelProvider":"fake"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestSetupEnvDoesNotEchoAPIKey(t *testing.T) {
	srv := newSetupTestServer(t)
	body := `{"modelProvider":"fake","model":"fake","modelApiKey":"secret-value"}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/env", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "secret-value") {
		t.Fatal("env response exposed API key")
	}
}

func TestSetupEnvStagesOwnerWithoutMarkingInitialized(t *testing.T) {
	srv := newSetupTestServer(t)
	body := `{"ownerName":"Alex Morgan","ownerEmail":"alex@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/env", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if srv.setupInitialized(context.Background()) {
		t.Fatal("staged identity must not mark setup initialized")
	}
	name, err := store.GetConfig(context.Background(), srv.db, "setup_owner_name")
	if err != nil || name != "Alex Morgan" {
		t.Fatalf("staged name = %q, err = %v", name, err)
	}
}

func TestSetupEnvRejectsInvalidArchivePathBeforeWritingEnv(t *testing.T) {
	srv := newSetupTestServer(t)
	missingPath := filepath.Join(t.TempDir(), "missing", "archive.db")
	body := `{"msgvaultDb":` + strconv.Quote(missingPath) + `,"confirmDbChange":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/env", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "database is unavailable") {
		t.Fatalf("expected archive validation error, got: %s", recorder.Body.String())
	}
	if content, err := os.ReadFile(".env"); err == nil && strings.Contains(string(content), config.EnvMsgvaultDB) {
		t.Fatalf("invalid archive path was written to .env: %s", content)
	}
}

func TestSetupCheckArchiveReportsInvalidPathWithoutSaving(t *testing.T) {
	srv := newSetupTestServer(t)
	missingPath := filepath.Join(t.TempDir(), "missing", "archive.db")
	body := `{"msgvaultDb":` + strconv.Quote(missingPath) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/setup/check-archive", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.mux.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		OK     bool               `json:"ok"`
		Checks []setupStatusCheck `json:"checks"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.OK {
		t.Fatalf("invalid archive reported ok: %+v", response)
	}
	if len(response.Checks) == 0 || response.Checks[0].Name != "Msgvault archive" {
		t.Fatalf("expected archive checks, got: %+v", response.Checks)
	}
}
