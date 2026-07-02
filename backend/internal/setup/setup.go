package setup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"memento/backend/internal/config"
	"memento/backend/internal/msgvault"
	"memento/backend/internal/newsletter"
	"memento/backend/internal/people"
	"memento/backend/internal/person"
	"memento/backend/internal/refresh"
	"memento/backend/internal/setupenv"
	"memento/backend/internal/social"
	"memento/backend/internal/store"
)

type InitParams struct {
	OwnerName      string `json:"ownerName"`
	OwnerEmail     string `json:"ownerEmail"`
	ModelProvider  string `json:"modelProvider"`
	Model          string `json:"model"`
	ModelBaseURL   string `json:"modelBaseUrl"`
	ModelAPIKey    string `json:"modelApiKey,omitempty"`
	MsgvaultDBPath string `json:"msgvaultDb"`
}

type Progress func(step string, done, total int, detail string)

type Summary struct {
	Persons     int     `json:"persons"`
	Humans      int     `json:"humans"`
	Excluded    int     `json:"excluded"`
	Newsletters int     `json:"newsletters"`
	Duplicates  int     `json:"duplicates"`
	Warnings    []Check `json:"warnings,omitempty"`
}

func InferOwnerEmail(ctx context.Context, db *sql.DB) string {
	var email string
	_ = db.QueryRowContext(ctx, `SELECT identifier FROM sources WHERE identifier LIKE '%@%' LIMIT 1`).Scan(&email)
	return email
}

const initStepTotal = 14

func ValidateInitParams(p InitParams) error {
	if strings.TrimSpace(p.OwnerName) == "" {
		return fmt.Errorf("owner name is required")
	}
	if strings.TrimSpace(p.OwnerEmail) == "" || !strings.Contains(p.OwnerEmail, "@") {
		return fmt.Errorf("a valid owner email is required")
	}
	switch p.ModelProvider {
	case config.ProviderGemini, config.ProviderOpenAICompatible, config.ProviderFake:
	default:
		return fmt.Errorf("unsupported model provider %q", p.ModelProvider)
	}
	if strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if p.ModelProvider == config.ProviderOpenAICompatible && strings.TrimSpace(p.ModelBaseURL) == "" {
		return fmt.Errorf("%s is required when provider is %s", config.EnvModelBaseURL, config.ProviderOpenAICompatible)
	}
	if strings.TrimSpace(p.MsgvaultDBPath) == "" {
		return fmt.Errorf("msgvault database path is required")
	}
	return nil
}

func RunInit(ctx context.Context, db *sql.DB, p InitParams, progress Progress) (Summary, error) {
	var summary Summary
	if progress == nil {
		progress = func(string, int, int, string) {}
	}
	if err := ValidateInitParams(p); err != nil {
		return summary, err
	}

	progress("preflight", 1, initStepTotal, "Checking archive and keyword search")
	for _, check := range []Check{CheckMsgvault(ctx, p.MsgvaultDBPath), CheckSearch(ctx, p.MsgvaultDBPath)} {
		if check.Status == StatusFail {
			return summary, fmt.Errorf("%s: %s", check.Name, check.Detail)
		}
	}

	progress("migrate_schema", 2, initStepTotal, "Migrating Memento-owned tables")
	if _, err := store.Migrate(ctx, db); err != nil {
		return summary, fmt.Errorf("migrate: %w", err)
	}

	progress("save_config", 3, initStepTotal, "Saving owner and model configuration")
	if err := persistInitConfig(ctx, db, p); err != nil {
		return summary, err
	}

	reader, err := msgvault.OpenReader(p.MsgvaultDBPath)
	if err != nil {
		return summary, fmt.Errorf("open msgvault reader: %w", err)
	}
	defer reader.Close()

	progress("resolve_persons", 4, initStepTotal, "Resolving canonical people")
	resolveReport, err := person.ResolveAndPersist(ctx, reader, db, person.DefaultResolveOptions())
	if err != nil {
		return summary, fmt.Errorf("resolve and persist persons: %w", err)
	}
	summary.Persons = resolveReport.PersonsCreated

	classifyAll := func() (int, int, error) {
		report, err := people.BuildCandidateReport(ctx, reader, people.CandidateOptions{Full: true})
		if err != nil {
			return 0, 0, fmt.Errorf("build candidates: %w", err)
		}
		if err := people.PersistCandidateReport(ctx, db, report); err != nil {
			return 0, 0, fmt.Errorf("persist candidates: %w", err)
		}
		var humans, excluded int
		for _, candidate := range report.Candidates {
			switch candidate.Classification {
			case "candidate", "candidate_inbound_only":
				humans++
			case "excluded":
				excluded++
			}
		}
		return humans, excluded, nil
	}

	progress("classify_people", 5, initStepTotal, "Classifying people and automated senders")
	summary.Humans, summary.Excluded, err = classifyAll()
	if err != nil {
		return summary, err
	}

	progress("detect_newsletters", 6, initStepTotal, "Detecting recurring newsletter sources")
	nlReport, err := newsletter.DetectSources(ctx, db, 20)
	if err != nil {
		return summary, fmt.Errorf("detect newsletters: %w", err)
	}
	if err := newsletter.PersistSources(ctx, db, nlReport); err != nil {
		return summary, fmt.Errorf("persist newsletters: %w", err)
	}
	summary.Newsletters = len(nlReport.Sources)

	progress("reclassify_people", 7, initStepTotal, "Excluding detected newsletter senders")
	if summary.Newsletters > 0 {
		summary.Humans, summary.Excluded, err = classifyAll()
		if err != nil {
			return summary, err
		}
	}

	progress("refresh_people", 8, initStepTotal, "Refreshing People report")
	if _, err := refresh.RefreshPeopleReport(ctx, db); err != nil {
		return summary, fmt.Errorf("refresh people: %w", err)
	}
	progress("refresh_newsletters", 9, initStepTotal, "Refreshing Newsletters report")
	if _, err := refresh.RefreshNewslettersReport(ctx, db); err != nil {
		return summary, fmt.Errorf("refresh newsletters: %w", err)
	}
	progress("refresh_projects", 10, initStepTotal, "Refreshing Projects report")
	if _, err := refresh.RefreshProjectsReport(ctx, db); err != nil {
		return summary, fmt.Errorf("refresh projects: %w", err)
	}
	progress("refresh_concepts", 11, initStepTotal, "Refreshing Concepts report")
	if _, err := refresh.RefreshConceptsReport(ctx, db); err != nil {
		return summary, fmt.Errorf("refresh concepts: %w", err)
	}
	progress("build_social_graph", 12, initStepTotal, "Building social graph and groups")
	if _, err := social.BuildSocialGraph(ctx, db); err != nil {
		return summary, fmt.Errorf("build social graph: %w", err)
	}

	progress("scan_duplicates", 13, initStepTotal, "Scanning for likely duplicate people")
	mergeCandidates, err := person.FindMergeCandidates(ctx, db, person.DefaultMergeOptions())
	if err == nil {
		summary.Duplicates = len(mergeCandidates)
	} else {
		summary.Warnings = append(summary.Warnings, warn("Duplicate scan", err.Error(), "Review duplicates later from People merge review."))
	}

	progress("postflight", 14, initStepTotal, "Checking initialized reports and configuration")
	for _, check := range []Check{CheckSchema(ctx, db), CheckOwnerConfig(ctx, db), CheckRollups(ctx, db), CheckProvider()} {
		if check.Status == StatusFail {
			return summary, fmt.Errorf("postflight %s: %s", check.Name, check.Detail)
		}
		if check.Status == StatusWarn {
			summary.Warnings = append(summary.Warnings, check)
		}
	}
	if err := store.SetConfig(ctx, db, "onboarding_status", "complete"); err != nil {
		return summary, fmt.Errorf("save onboarding status: %w", err)
	}
	if err := store.SetConfig(ctx, db, "onboarding_completed_at", time.Now().UTC().Format(time.RFC3339)); err != nil {
		return summary, fmt.Errorf("save onboarding completion time: %w", err)
	}
	return summary, nil
}

func persistInitConfig(ctx context.Context, db *sql.DB, p InitParams) error {
	if err := store.SetConfig(ctx, db, "owner_name", strings.TrimSpace(p.OwnerName)); err != nil {
		return fmt.Errorf("save owner_name: %w", err)
	}
	if err := store.SetConfig(ctx, db, "owner_email", strings.TrimSpace(p.OwnerEmail)); err != nil {
		return fmt.Errorf("save owner_email: %w", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM memento_config WHERE key IN ('setup_owner_name', 'setup_owner_email')`); err != nil {
		return fmt.Errorf("clear staged owner config: %w", err)
	}
	values := []struct{ key, value string }{
		{config.EnvModelProvider, p.ModelProvider},
		{config.EnvAgentModel, p.Model},
		{config.EnvModelBaseURL, p.ModelBaseURL},
	}
	if p.ModelAPIKey != "" {
		values = append(values, struct{ key, value string }{config.EnvModelAPIKey, p.ModelAPIKey})
	}
	for _, item := range values {
		if err := setupenv.WriteKey(item.key, item.value); err != nil {
			return fmt.Errorf("write %s: %w", item.key, err)
		}
	}
	if _, err := setupenv.EnsureAgentEnv(); err != nil {
		return fmt.Errorf("seed agent environment: %w", err)
	}
	config.LoadDotEnv()
	return nil
}
