package setup

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"memento/backend/internal/config"
	"memento/backend/internal/msgvault"

	_ "modernc.org/sqlite"
)

type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint"`
}

type WizardChecks struct {
	Checks           []Check `json:"checks"`
	ToolVersions     []Check `json:"toolVersions,omitempty"`
	ArchiveChecks    []Check `json:"archiveChecks,omitempty"`
	DeveloperChecks  []Check `json:"developerChecks,omitempty"`
	PostflightChecks []Check `json:"postflightChecks,omitempty"`
}

var removedEnvNames = map[string]bool{
	"GO_BACKEND_URL":                  true,
	"INTERNAL_TOKEN":                  true,
	"AGENT_STEP_LIMIT":                true,
	"GEMINI_API_KEY":                  true,
	"MEMENTO_OPENAI_API_KEY":          true,
	"MEMENTO_OPENAI_BASE_URL":         true,
	"MEMENTO_OPENAI_REPLAY_REASONING": true,
	"MSGVAULT_HOME":                   true,
	"MEMENTO_AGENT_DEBUG_SSE":         true,
}

var versionPattern = regexp.MustCompile(`(\d+)(?:\.(\d+))?`)

func RunAllChecks(ctx context.Context, dbPath string) []Check {
	projectRoot := findProjectRoot()
	checks := []Check{CheckMsgvaultVersion(), CheckEnvFile(), CheckRuntimeEnv()}
	if projectRoot != "" {
		checks = append(checks, CheckProjectRoot())
		checks = append(checks, CheckDeveloperToolchain()...)
		checks = append(checks, CheckNodeDeps())
	}

	resolvedPath, resolveErr := resolveDBPath(dbPath)
	if resolveErr != nil {
		checks = append(checks,
			fail("Msgvault archive", resolveErr.Error(), "Run `msgvault stats` or pass `--db PATH`."),
			fail("Keyword search", "archive path could not be resolved", "Fix the archive path, then rerun `./memento doctor`."),
			warn("Vector search", "skipped because the archive path is unavailable", "Fix the archive path before checking vector search."),
			warn("Owner", "not checked because the archive path is unavailable", "Configure owner identity during onboarding."),
			warn("Onboarding", "not checked because the archive path is unavailable", "Fix the archive path, then rerun doctor."),
			warn("App data", "not checked because the archive path is unavailable", "Run `./memento init` or `./memento app --demo` after fixing the archive path."),
			warn("Memory indexes", "not checked because the archive path is unavailable", "Run onboarding after fixing the archive path."),
		)
	} else {
		archiveCheck := CheckMsgvault(ctx, resolvedPath)
		checks = append(checks, archiveCheck, CheckSearch(ctx, resolvedPath), CheckVectorSearch(ctx, resolvedPath))
		db, err := openReadOnly(resolvedPath)
		if err != nil {
			checks = append(checks,
				warn("Owner", "not checked because the database could not be opened", "Fix database access and rerun doctor."),
				warn("Onboarding", "not checked because the database could not be opened", "Fix database access and rerun doctor."),
				warn("App data", "not checked because the database could not be opened read-only", "Fix the archive path or create a demo archive with `./memento app --demo`."),
				warn("Memory indexes", "not checked because the database could not be opened", "Fix database access and rerun doctor."),
			)
		} else {
			checks = append(checks, CheckOwnerConfig(ctx, db), CheckOnboardingStatus(ctx, db), CheckSchema(ctx, db), CheckRollups(ctx, db))
			db.Close()
		}
	}

	checks = append(checks, CheckProvider())
	return checks
}

func RunWizardChecks(ctx context.Context, dbPath string) WizardChecks {
	var result WizardChecks
	result.ToolVersions = CheckSetupToolVersions()
	if projectRoot := findProjectRoot(); projectRoot != "" {
		result.DeveloperChecks = append(result.DeveloperChecks, CheckNodeDeps())
		result.Checks = append(result.Checks, CheckProjectRoot())
	}
	result.Checks = append(result.Checks,
		relabelCheck(CheckEnvFile(), "Environment", ".env file is valid"),
	)

	resolvedPath, resolveErr := resolveDBPath(dbPath)
	if resolveErr != nil {
		result.ArchiveChecks = append(result.ArchiveChecks,
			fail("Msgvault archive", resolveErr.Error(), "Run `msgvault stats` or pass the correct archive path when starting Memento."),
			fail("Keyword search", "archive path could not be resolved", "Fix the archive path, then rerun setup checks."),
			warn("Vector search", vectorRequiredDetail(), "Fix the archive path, then configure embeddings in msgvault for this archive."),
		)
		return result
	}

	result.ArchiveChecks = append(result.ArchiveChecks, CheckMsgvault(ctx, resolvedPath), CheckSearch(ctx, resolvedPath), CheckVectorSearch(ctx, resolvedPath))

	db, err := openReadOnly(resolvedPath)
	if err != nil {
		result.PostflightChecks = append(result.PostflightChecks,
			warn("App data", "not checked because the database could not be opened read-only", "Fix the archive path or create it through msgvault/demo setup."),
			warn("Memory indexes", "not checked because the database could not be opened", "Run onboarding after fixing database access."),
		)
		return result
	}
	defer db.Close()
	result.PostflightChecks = append(result.PostflightChecks, CheckSchema(ctx, db), CheckRollups(ctx, db))
	return result
}

func CheckSetupToolVersions() []Check {
	checks := []Check{CheckMsgvaultVersion()}
	if findProjectRoot() != "" {
		checks = append(checks, CheckDeveloperToolchain()...)
	}
	return checks
}

func CheckToolchain() []Check {
	checks := CheckDeveloperToolchain()
	checks = append(checks, CheckMsgvaultVersion())
	return checks
}

func CheckDeveloperToolchain() []Check {
	return []Check{
		checkCommandVersion("Go", "go", []string{"version"}, 1, 26, "Install Go 1.26 or newer from https://go.dev/dl/"),
		checkCommandVersion("Node.js", "node", []string{"--version"}, 22, 0, "Install Node.js 22 or newer."),
		checkCommandVersion("pnpm", "pnpm", []string{"--version"}, 0, 0, "Install pnpm, then run `pnpm install`."),
	}
}

func CheckMsgvaultVersion() Check {
	path, err := exec.LookPath("msgvault")
	if err != nil {
		return fail("msgvault", "msgvault is not installed or not on PATH", "Install msgvault and sync your archive before running setup.")
	}
	for _, args := range [][]string{{"--version"}, {"version"}} {
		out, err := exec.Command(path, args...).CombinedOutput()
		detail := strings.TrimSpace(string(out))
		if err == nil && detail != "" {
			return okCheck("msgvault", detail)
		}
	}
	return warn("msgvault", "installed, but version could not be read", "Confirm msgvault is recent enough to support search and embeddings.")
}

func checkCommandVersion(name, command string, args []string, minimumMajor, minimumMinor int, hint string) Check {
	path, err := exec.LookPath(command)
	if err != nil {
		return fail(name, fmt.Sprintf("%s is not installed or not on PATH", command), hint)
	}
	out, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		return fail(name, fmt.Sprintf("could not run %s: %v", command, err), hint)
	}
	detail := strings.TrimSpace(string(out))
	major, minor, ok := parseVersion(detail)
	if !ok {
		return warn(name, detail, "Version could not be parsed; confirm the installed tool is supported.")
	}
	if minimumMajor > 0 && (major < minimumMajor || (major == minimumMajor && minor < minimumMinor)) {
		return warn(name, detail, hint)
	}
	return okCheck(name, detail)
}

func parseVersion(value string) (int, int, bool) {
	match := versionPattern.FindStringSubmatch(value)
	if len(match) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, 0, false
	}
	minor := 0
	if len(match) > 2 && match[2] != "" {
		minor, _ = strconv.Atoi(match[2])
	}
	return major, minor, true
}

func CheckProjectRoot() Check {
	root := findProjectRoot()
	if root == "" {
		return warn("Project root", "current directory is not under an obvious Memento checkout", "Run this command from the Memento repository.")
	}
	return okCheck("Project root", root)
}

func CheckNodeDeps() Check {
	root := findProjectRoot()
	if root == "" {
		return warn("Node dependencies", "project root is unknown", "Run `pnpm install` from the Memento repository.")
	}
	if info, err := os.Stat(filepath.Join(root, "node_modules")); err != nil || !info.IsDir() {
		return warn("Node dependencies", "node_modules is missing", "Run `pnpm install`.")
	}
	return okCheck("Node dependencies", "installed")
}

func CheckEnvFile() Check {
	root := findProjectRoot()
	if root == "" {
		root = "."
	}
	path := filepath.Join(root, ".env")
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return warn("Environment file", ".env does not exist", "Run `./memento app --demo` or complete onboarding to create it.")
	}
	if err != nil {
		return fail("Environment file", fmt.Sprintf("cannot read .env: %v", err), "Fix .env file permissions.")
	}
	defer file.Close()

	seen := map[string]int{}
	var duplicates, inlineComments, removed []string
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return fail("Environment file", fmt.Sprintf("invalid assignment on line %d", lineNumber), "Use one `KEY=value` assignment per line.")
		}
		key := strings.TrimSpace(parts[0])
		seen[key]++
		if seen[key] == 2 {
			duplicates = append(duplicates, key)
		}
		if removedEnvNames[key] {
			removed = append(removed, key)
		}
		if hasInlineComment(parts[1]) {
			inlineComments = append(inlineComments, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return fail("Environment file", fmt.Sprintf("cannot parse .env: %v", err), "Fix the file and rerun doctor.")
	}
	if len(removed) > 0 {
		sort.Strings(removed)
		return fail("Environment file", "removed environment names are present: "+strings.Join(removed, ", "), "Replace them with the canonical MEMENTO_* names from `.env.sample`.")
	}
	var warnings []string
	if len(duplicates) > 0 {
		sort.Strings(duplicates)
		warnings = append(warnings, "duplicate keys: "+strings.Join(duplicates, ", "))
	}
	if len(inlineComments) > 0 {
		sort.Strings(inlineComments)
		warnings = append(warnings, "inline comments after values: "+strings.Join(inlineComments, ", "))
	}
	if len(warnings) > 0 {
		return warn("Environment file", strings.Join(warnings, "; "), "Put comments on their own lines and keep one assignment per key.")
	}
	return okCheck("Environment file", ".env is valid")
}

func hasInlineComment(value string) bool {
	value = strings.TrimSpace(value)
	quote := rune(0)
	for _, char := range value {
		if quote == 0 && (char == '\'' || char == '"') {
			quote = char
			continue
		}
		if quote != 0 && char == quote {
			quote = 0
			continue
		}
		if quote == 0 && char == '#' {
			return true
		}
	}
	return false
}

func CheckRuntimeEnv() Check {
	if config.InternalToken() == "" && findProjectRoot() != "" {
		return warn(
			"Runtime defaults",
			config.EnvInternalToken+" is not configured",
			"Normal single-binary app use is fine; onboarding/demo will create it before internal tooling needs it.",
		)
	}
	return okCheck("Runtime defaults", "single-binary runtime defaults are usable")
}

func CheckMsgvault(ctx context.Context, dbPath string) Check {
	info, err := os.Stat(dbPath)
	if err != nil {
		return fail("Msgvault archive", fmt.Sprintf("database is unavailable at %s: %v", dbPath, err), "Run `msgvault stats` or pass the correct `--db PATH`.")
	}
	if info.IsDir() {
		return fail("Msgvault archive", dbPath+" is a directory", "Pass the path to the SQLite database file.")
	}
	reader, err := msgvault.OpenReader(dbPath)
	if err != nil {
		return fail("Msgvault archive", fmt.Sprintf("database cannot be opened read-only: %v", err), "Check SQLite file permissions and integrity.")
	}
	defer reader.Close()
	required := map[string]bool{"sources": false, "participants": false, "messages": false, "message_bodies": false, "message_recipients": false}
	tables, err := reader.Schema(ctx)
	if err != nil {
		return fail("Msgvault archive", fmt.Sprintf("schema cannot be read: %v", err), "Check the archive with `msgvault stats`.")
	}
	for _, table := range tables {
		if _, ok := required[table.Name]; ok {
			required[table.Name] = true
		}
	}
	var missing []string
	for name, present := range required {
		if !present {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fail("Msgvault archive", "required tables are missing: "+strings.Join(missing, ", "), "Use a valid msgvault archive or run `./memento app --demo`.")
	}
	stats, err := reader.Stats(ctx)
	if err != nil {
		return fail("Msgvault archive", fmt.Sprintf("archive counts cannot be read: %v", err), "Check the archive with `msgvault stats`.")
	}
	detail := fmt.Sprintf("%d messages found", stats.Messages)
	if stats.Messages == 0 {
		return warn("Msgvault archive", detail, "Sync messages with msgvault or use `./memento app --demo`.")
	}
	return okCheck("Msgvault archive", detail)
}

func CheckSearch(ctx context.Context, dbPath string) Check {
	if _, err := os.Stat(dbPath); err != nil {
		return fail("Keyword search", "archive database is unavailable", "Fix the archive path before checking keyword search.")
	}
	db, err := openReadOnly(dbPath)
	if err != nil {
		return fail("Keyword search", fmt.Sprintf("database cannot be opened: %v", err), "Fix archive access and rerun doctor.")
	}
	defer db.Close()
	var sample string
	err = db.QueryRowContext(ctx, `SELECT trim(COALESCE(body_text, '')) FROM message_bodies WHERE length(trim(COALESCE(body_text, ''))) > 0 LIMIT 1`).Scan(&sample)
	if err == sql.ErrNoRows {
		return fail("Keyword search", "archive has no message body text to search", "Sync message bodies before running agents.")
	}
	if err != nil {
		return fail("Keyword search", fmt.Sprintf("message bodies are unreadable: %v", err), "Check the msgvault schema and database integrity.")
	}
	term := firstSearchTerm(sample)
	if term == "" {
		return fail("Keyword search", "could not derive a search term from message bodies", "Verify the archive contains searchable text.")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM message_bodies WHERE lower(body_text) LIKE '%' || lower(?) || '%'`, term).Scan(&count); err != nil {
		return fail("Keyword search", fmt.Sprintf("local keyword query failed: %v", err), "Check the message_bodies table and SQLite integrity.")
	}
	if count == 0 {
		return fail("Keyword search", "local keyword query returned no match for archive text", "Rebuild or repair the archive search data.")
	}
	return okCheck("Keyword search", fmt.Sprintf("local text search found %d match(es)", count))
}

func firstSearchTerm(text string) string {
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, ".,:;!?()[]{}\"'")
		if len(field) >= 4 {
			return field
		}
	}
	return ""
}

func CheckVectorSearch(ctx context.Context, dbPath string) Check {
	path, err := exec.LookPath("msgvault")
	if err != nil {
		return warn("Vector search", vectorRequiredDetail(), "Install msgvault, configure embeddings in ~/.msgvault/config.toml, start the embedding provider if it is local, then rerun setup checks.")
	}
	configured, resolveErr := config.ResolveMsgvaultDBPath()
	if resolveErr != nil || canonicalPath(configured) != canonicalPath(dbPath) {
		return warn("Vector search", vectorRequiredDetail(), vectorRequiredHint())
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, path, "search", "memory", "--mode", "vector", "--json", "--limit", "1", "--local", "--no-log-file")
	_, err = cmd.CombinedOutput()
	if err != nil {
		return warn("Vector search", vectorRequiredDetail(), vectorRequiredHint())
	}
	return okCheck("Vector search", "msgvault semantic search responded")
}

func vectorRequiredDetail() string {
	return "Vector embeddings are not ready. High-quality memory generation depends on semantic retrieval from msgvault."
}

func vectorRequiredHint() string {
	return "Configure embeddings for this archive in msgvault's config file (~/.msgvault/config.toml), start the embedding provider if it is local, then rerun setup checks."
}

func relabelCheck(check Check, name, okDetail string) Check {
	check.Name = name
	if check.Status == StatusOK {
		check.Detail = okDetail
	}
	return check
}

func CheckSchema(ctx context.Context, db *sql.DB) Check {
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'memento_config'`).Scan(&name)
	if err == sql.ErrNoRows {
		return warn("App data", "not initialized yet", "Run `./memento init` or `./memento app --demo`.")
	}
	if err != nil {
		return fail("App data", fmt.Sprintf("schema metadata is unreadable: %v", err), "Check SQLite integrity before running migrations.")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_config`).Scan(&count); err != nil {
		return fail("App data", fmt.Sprintf("memento_config is unreadable: %v", err), "Repair the Memento schema or restore from backup.")
	}
	return okCheck("App data", "Memento-owned tables are initialized")
}

func CheckOwnerConfig(ctx context.Context, db *sql.DB) Check {
	if !tableExists(ctx, db, "memento_config") {
		return warn("Owner", "Memento is not initialized", "Run setup to configure owner name and email.")
	}
	values := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM memento_config WHERE key IN ('owner_name', 'owner_email')`)
	if err != nil {
		return warn("Owner", fmt.Sprintf("owner configuration cannot be read: %v", err), "Rerun setup to configure owner identity.")
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return warn("Owner", fmt.Sprintf("owner configuration cannot be read: %v", err), "Rerun setup to configure owner identity.")
		}
		values[key] = strings.TrimSpace(value)
	}
	var missing []string
	for _, key := range []string{"owner_name", "owner_email"} {
		if values[key] == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return warn("Owner", "missing "+strings.Join(missing, " and "), "Rerun setup to configure owner identity.")
	}
	return okCheck("Owner", fmt.Sprintf("%s <%s>", values["owner_name"], values["owner_email"]))
}

func CheckOnboardingStatus(ctx context.Context, db *sql.DB) Check {
	if !tableExists(ctx, db, "memento_config") {
		return warn("Onboarding", "Memento is not initialized", "Run the onboarding wizard, or `./memento onboard mark-complete` if this archive is already populated.")
	}
	var status, completedAt string
	_ = db.QueryRowContext(ctx, `SELECT value FROM memento_config WHERE key = 'onboarding_status'`).Scan(&status)
	status = strings.TrimSpace(status)
	if status != "complete" {
		detail := "onboarding_status is not set"
		if status != "" {
			detail = fmt.Sprintf("onboarding_status=%q", status)
		}
		return warn("Onboarding", detail, "Finish the wizard at /onboard, or run `./memento onboard mark-complete` if this archive is already populated.")
	}
	_ = db.QueryRowContext(ctx, `SELECT value FROM memento_config WHERE key = 'onboarding_completed_at'`).Scan(&completedAt)
	completedAt = strings.TrimSpace(completedAt)
	detail := "complete"
	if completedAt != "" {
		detail = fmt.Sprintf("complete (at %s)", completedAt)
	}
	return okCheck("Onboarding", detail)
}

func CheckRollups(ctx context.Context, db *sql.DB) Check {
	tables := []string{"memento_people_report", "memento_projects_report", "memento_newsletters_report", "memento_concepts_report", "memento_social_edge", "memento_social_group"}
	var missing []string
	counts := map[string]int{}
	for _, table := range tables {
		if !tableExists(ctx, db, table) {
			missing = append(missing, table)
			continue
		}
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
		var count int
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fail("Memory indexes", fmt.Sprintf("%s is unreadable: %v", table, err), "Repair the schema or rerun onboarding from a valid archive.")
		}
		counts[table] = count
	}
	if len(missing) > 0 {
		return warn("Memory indexes", "missing tables: "+strings.Join(missing, ", "), "Run `./memento init` or `./memento refresh`.")
	}
	detail := fmt.Sprintf("people=%d, projects=%d, newsletters=%d, concepts=%d, social_edges=%d, groups=%d",
		counts["memento_people_report"], counts["memento_projects_report"], counts["memento_newsletters_report"], counts["memento_concepts_report"], counts["memento_social_edge"], counts["memento_social_group"])
	if counts["memento_people_report"]+counts["memento_projects_report"]+counts["memento_newsletters_report"]+counts["memento_concepts_report"] == 0 {
		return warn("Memory indexes", detail, "Run `./memento refresh` after resolving and classifying the archive.")
	}
	return okCheck("Memory indexes", detail)
}

func CheckProvider() Check {
	provider := config.ModelProvider()
	model := config.EnvString(config.EnvAgentModel)
	baseURL := config.EnvString(config.EnvModelBaseURL)
	switch provider {
	case config.ProviderFake:
		return okCheck("AI Provider", providerDetail("fake", model, baseURL))
	case config.ProviderGemini:
		if config.ModelAPIKey() == "" {
			return warn("AI Provider", providerDetail("gemini", model, baseURL)+" — API key missing", "Set MEMENTO_MODEL_API_KEY or choose the fake provider for demo use.")
		}
		return okCheck("AI Provider", providerDetail("gemini", model, baseURL))
	case config.ProviderOpenAICompatible:
		if baseURL == "" {
			return fail("AI Provider", "openai_compatible requires MEMENTO_MODEL_BASE_URL", "Set MEMENTO_MODEL_BASE_URL to the compatible endpoint.")
		}
		if config.ModelAPIKey() == "" {
			return warn("AI Provider", providerDetail("openai_compatible", model, baseURL)+" — API key missing", "Set MEMENTO_MODEL_API_KEY if the endpoint requires authentication.")
		}
		return okCheck("AI Provider", providerDetail("openai_compatible", model, baseURL))
	default:
		return fail("AI Provider", fmt.Sprintf("unsupported provider %q", provider), "Use gemini, openai_compatible, or fake.")
	}
}

func providerDetail(provider, model, baseURL string) string {
	parts := []string{provider}
	if model != "" {
		parts = append(parts, "model="+model)
	}
	if baseURL != "" {
		parts = append(parts, "url="+baseURL)
	}
	return strings.Join(parts, " · ")
}

func resolveDBPath(dbPath string) (string, error) {
	if strings.TrimSpace(dbPath) == "" {
		var err error
		dbPath, err = config.ResolveMsgvaultDBPath()
		if err != nil {
			return "", err
		}
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func openReadOnly(path string) (*sql.DB, error) {
	dsn := "file:" + url.PathEscape(path) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) bool {
	var count int
	return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count) == nil && count == 1
}

func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		if fileExists(filepath.Join(dir, "package.json")) && fileExists(filepath.Join(dir, "backend", "go.mod")) {
			return dir
		}
		if filepath.Base(dir) == "backend" && fileExists(filepath.Join(dir, "go.mod")) && fileExists(filepath.Join(filepath.Dir(dir), "package.json")) {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

func okCheck(name, detail string) Check { return Check{Name: name, Status: StatusOK, Detail: detail} }
func warn(name, detail, hint string) Check {
	return Check{Name: name, Status: StatusWarn, Detail: detail, Hint: hint}
}
func fail(name, detail, hint string) Check {
	return Check{Name: name, Status: StatusFail, Detail: detail, Hint: hint}
}
