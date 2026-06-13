package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"memento/backend/internal/config"
	"memento/backend/internal/llm"
	"memento/backend/internal/msgvault"
	"memento/backend/internal/setup"
	"memento/backend/internal/setupenv"
	"memento/backend/internal/store"
)

type setupEnvRequest struct {
	OwnerName       *string `json:"ownerName"`
	OwnerEmail      *string `json:"ownerEmail"`
	ModelProvider   *string `json:"modelProvider"`
	Model           *string `json:"model"`
	ModelBaseURL    *string `json:"modelBaseUrl"`
	ModelAPIKey     *string `json:"modelApiKey"`
	MsgvaultDB      *string `json:"msgvaultDb"`
	ConfirmDBChange bool    `json:"confirmDbChange"`
}

type setupArchiveCheckRequest struct {
	MsgvaultDB *string `json:"msgvaultDb"`
}

type setupEmailPreview struct {
	ID      int64  `json:"id"`
	SentAt  string `json:"sentAt"`
	Sender  string `json:"sender"`
	Subject string `json:"subject"`
	Snippet string `json:"snippet"`
	Group   string `json:"group"`
}

func (s *Server) setupInitialized(ctx context.Context) bool {
	status, err := store.GetConfig(ctx, s.db, "onboarding_status")
	return err == nil && strings.TrimSpace(status) == "complete"
}

func (s *Server) requireSetupIncomplete(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.setupInitialized(r.Context()) {
			writeError(w, http.StatusConflict, errors.New("onboarding is already complete; use CLI commands for later reconfiguration"))
			return
		}
		next(w, r)
	}
}

func (s *Server) handleSetupInitialized(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"initialized": s.setupInitialized(r.Context())})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	path := s.reader.Path()
	info, statErr := os.Stat(path)
	var messageCount int64
	if statErr == nil {
		if stats, err := s.reader.Stats(r.Context()); err == nil {
			messageCount = stats.Messages
		}
	}
	ownerName, _ := store.GetConfig(r.Context(), s.db, "owner_name")
	draftOwnerName, _ := store.GetConfig(r.Context(), s.db, "setup_owner_name")
	draftOwnerEmail, _ := store.GetConfig(r.Context(), s.db, "setup_owner_email")
	checks := setup.RunWizardChecks(r.Context(), path)
	preview := s.setupArchivePreview(r.Context(), messageCount)
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized": s.setupInitialized(r.Context()),
		"msgvault": map[string]any{
			"path": path, "exists": statErr == nil && !info.IsDir(), "messageCount": messageCount,
		},
		"ownerConfigured":    strings.TrimSpace(ownerName) != "",
		"ownerName":          draftOwnerName,
		"ownerEmail":         draftOwnerEmail,
		"inferredOwnerEmail": setup.InferOwnerEmail(r.Context(), s.db),
		"provider": map[string]any{
			"name": config.ModelProvider(), "hasApiKey": config.ModelAPIKey() != "",
			"model": config.AgentModelFor("", config.ModelProvider()), "baseUrl": config.ModelBaseURL(),
		},
		"checks":           checks.Checks,
		"toolVersions":     checks.ToolVersions,
		"archiveChecks":    checks.ArchiveChecks,
		"archivePreview":   preview,
		"developerChecks":  checks.DeveloperChecks,
		"postflightChecks": checks.PostflightChecks,
	})
}

func (s *Server) setupArchivePreview(ctx context.Context, messageCount int64) []setupEmailPreview {
	if messageCount <= 0 {
		return nil
	}
	seen := map[int64]bool{}
	var preview []setupEmailPreview
	add := func(group string, rows []setupEmailPreview) {
		for _, row := range rows {
			if seen[row.ID] {
				continue
			}
			row.Group = group
			seen[row.ID] = true
			preview = append(preview, row)
		}
	}
	add("latest", s.querySetupPreview(ctx, "DESC", 0, 5))
	if messageCount > 10 {
		offset := messageCount/2 - 1
		if offset < 0 {
			offset = 0
		}
		add("middle", s.querySetupPreview(ctx, "DESC", offset, 3))
	}
	if messageCount > 5 {
		add("earliest", s.querySetupPreview(ctx, "ASC", 0, 5))
	}
	return preview
}

func (s *Server) querySetupPreview(ctx context.Context, direction string, offset int64, limit int) []setupEmailPreview {
	if direction != "ASC" {
		direction = "DESC"
	}
	query := fmt.Sprintf(`
		SELECT m.id,
		       COALESCE(m.sent_at, ''),
		       COALESCE(NULLIF(p.display_name, ''), NULLIF(p.email_address, ''), 'Unknown sender'),
		       COALESCE(NULLIF(m.subject, ''), '(no subject)'),
		       COALESCE(NULLIF(m.snippet, ''), substr(COALESCE(mb.body_text, ''), 1, 160))
		FROM messages m
		LEFT JOIN participants p ON p.id = m.sender_id
		LEFT JOIN message_bodies mb ON mb.message_id = m.id
		ORDER BY m.sent_at %s, m.id %s
		LIMIT ? OFFSET ?
	`, direction, direction)
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []setupEmailPreview
	for rows.Next() {
		var row setupEmailPreview
		if err := rows.Scan(&row.ID, &row.SentAt, &row.Sender, &row.Subject, &row.Snippet); err != nil {
			return out
		}
		out = append(out, row)
	}
	return out
}

func setupArchiveChecks(ctx context.Context, path string) ([]setup.Check, int64) {
	checks := []setup.Check{
		setup.CheckMsgvault(ctx, path),
		setup.CheckSearch(ctx, path),
		setup.CheckVectorSearch(ctx, path),
	}
	var messageCount int64
	if checks[0].Status != setup.StatusFail {
		if reader, err := msgvault.OpenReader(path); err == nil {
			if stats, err := reader.Stats(ctx); err == nil {
				messageCount = stats.Messages
			}
			reader.Close()
		}
	}
	return checks, messageCount
}

func setupArchiveChecksOK(checks []setup.Check) bool {
	for _, check := range checks {
		if check.Status == setup.StatusFail {
			return false
		}
	}
	return true
}

func setupArchiveCheckError(checks []setup.Check) error {
	for _, check := range checks {
		if check.Status == setup.StatusFail {
			if check.Hint != "" {
				return fmt.Errorf("%s: %s %s", check.Name, check.Detail, check.Hint)
			}
			return fmt.Errorf("%s: %s", check.Name, check.Detail)
		}
	}
	return nil
}

func (s *Server) handleSetupCheckArchive(w http.ResponseWriter, r *http.Request) {
	var req setupArchiveCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode archive check: %w", err))
		return
	}
	if req.MsgvaultDB == nil || strings.TrimSpace(*req.MsgvaultDB) == "" {
		writeError(w, http.StatusBadRequest, errors.New("msgvault database path cannot be empty"))
		return
	}
	path := strings.TrimSpace(*req.MsgvaultDB)
	checks, messageCount := setupArchiveChecks(r.Context(), path)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           setupArchiveChecksOK(checks),
		"checks":       checks,
		"messageCount": messageCount,
	})
}

func (s *Server) handleSetupEnv(w http.ResponseWriter, r *http.Request) {
	var req setupEnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode setup environment: %w", err))
		return
	}
	if req.OwnerName != nil && strings.TrimSpace(*req.OwnerName) == "" {
		writeError(w, http.StatusBadRequest, errors.New("owner name cannot be empty"))
		return
	}
	if req.OwnerEmail != nil && (!strings.Contains(*req.OwnerEmail, "@") || strings.ContainsAny(*req.OwnerEmail, "\r\n")) {
		writeError(w, http.StatusBadRequest, errors.New("owner email must be a valid single-line email address"))
		return
	}
	if req.OwnerName != nil {
		if err := store.SetConfig(r.Context(), s.db, "setup_owner_name", strings.TrimSpace(*req.OwnerName)); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if req.OwnerEmail != nil {
		if err := store.SetConfig(r.Context(), s.db, "setup_owner_email", strings.TrimSpace(*req.OwnerEmail)); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	restartRequired := false
	if req.MsgvaultDB != nil {
		path := strings.TrimSpace(*req.MsgvaultDB)
		if path == "" {
			writeError(w, http.StatusBadRequest, errors.New("msgvault database path cannot be empty"))
			return
		}
		if path != s.reader.Path() {
			if !req.ConfirmDBChange {
				writeError(w, http.StatusBadRequest, errors.New("confirmDbChange is required before changing the archive path"))
				return
			}
			checks, _ := setupArchiveChecks(r.Context(), path)
			if err := setupArchiveCheckError(checks); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			if err := setupenv.WriteKey(config.EnvMsgvaultDB, path); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			restartRequired = true
		}
	}

	provider := config.ModelProvider()
	if req.ModelProvider != nil {
		provider = strings.TrimSpace(*req.ModelProvider)
		switch provider {
		case config.ProviderGemini, config.ProviderOpenAICompatible, config.ProviderFake:
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported model provider %q", provider))
			return
		}
	}
	baseURL := config.ModelBaseURL()
	if req.ModelBaseURL != nil {
		baseURL = strings.TrimSpace(*req.ModelBaseURL)
	}
	if provider == config.ProviderOpenAICompatible && baseURL == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s is required for openai_compatible", config.EnvModelBaseURL))
		return
	}
	for _, item := range []struct {
		key   string
		value *string
	}{
		{config.EnvModelProvider, req.ModelProvider},
		{config.EnvAgentModel, req.Model},
		{config.EnvModelBaseURL, req.ModelBaseURL},
		{config.EnvModelAPIKey, req.ModelAPIKey},
	} {
		if item.value == nil {
			continue
		}
		if item.key == config.EnvAgentModel && strings.TrimSpace(*item.value) == "" {
			writeError(w, http.StatusBadRequest, errors.New("model cannot be empty"))
			return
		}
		if err := setupenv.WriteKey(item.key, strings.TrimSpace(*item.value)); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if _, err := setupenv.EnsureAgentEnv(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	config.LoadDotEnv()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restartRequired": restartRequired})
}

func (s *Server) handleSetupTestProvider(w http.ResponseWriter, r *http.Request) {
	if config.ModelProvider() == config.ProviderFake {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	response, err := llm.OneShot(ctx, llm.OneShotRequest{
		Provider: config.ModelProvider(), Model: config.AgentModelFor("", config.ModelProvider()),
		System: "Return only the word ok.", Prompt: "Reply ok.",
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": strings.TrimSpace(response.Text) != ""})
}

func (s *Server) handleSetupInit(w http.ResponseWriter, r *http.Request) {
	var params setup.InitParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode init parameters: %w", err))
		return
	}
	if params.MsgvaultDBPath == "" {
		params.MsgvaultDBPath = s.reader.Path()
	}
	if params.MsgvaultDBPath != s.reader.Path() {
		writeError(w, http.StatusConflict, errors.New("archive path changed; restart `./memento serve` before initializing"))
		return
	}
	if err := setup.ValidateInitParams(params); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	job := s.jobs.Create("setup-init")
	go s.runSetupInit(job.ID, params)
	writeJSON(w, http.StatusAccepted, map[string]string{"jobId": job.ID})
}

func (s *Server) runSetupInit(jobID string, params setup.InitParams) {
	ctx, cancel := jobCtx()
	defer cancel()
	summary, err := setup.RunInit(ctx, s.db, params, func(step string, done, total int, detail string) {
		s.jobs.AppendProgress(jobID, step, done, total, detail)
	})
	if err == nil {
		s.jobs.SetResult(jobID, summary)
	}
	s.jobs.Finish(jobID, err)
}
