package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"memento/backend/internal/config"
	"memento/backend/internal/demoseed"
	"memento/backend/internal/msgvault"
	"memento/backend/internal/newsletter"
	"memento/backend/internal/people"
	"memento/backend/internal/refresh"
	"memento/backend/internal/setupenv"
	"memento/backend/internal/store"
)

type demoModeOptions struct {
	DBPath      string
	Port        int
	PrepareOnly bool
	Onboarding  bool
}

func runDemoMode(ctx context.Context, opts demoModeOptions) error {
	if opts.Port < 1 || opts.Port > 65535 {
		return fmt.Errorf("invalid port %d", opts.Port)
	}
	if opts.DBPath == "" {
		name := "memento-demo.db"
		if opts.Onboarding {
			name = "onboard-demo.db"
		}
		opts.DBPath = filepath.Join(setupenv.FindProjectRoot(), "data", name)
	}

	resolvedDB, err := filepath.Abs(opts.DBPath)
	if err != nil {
		return fmt.Errorf("resolve demo database path: %w", err)
	}
	if !opts.PrepareOnly {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
		if err != nil {
			if errors.Is(err, syscall.EADDRINUSE) {
				return fmt.Errorf("port %d is already in use - another `memento serve` may be running; stop it or pass `--port`", opts.Port)
			}
			return fmt.Errorf("probe port %d: %w", opts.Port, err)
		}
		listener.Close()
	}
	if err := guardDemoTarget(ctx, resolvedDB); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(resolvedDB), 0755); err != nil {
		return fmt.Errorf("create demo data directory: %w", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(resolvedDB + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove previous demo database: %w", err)
		}
	}

	db, err := store.Open(resolvedDB)
	if err != nil {
		return fmt.Errorf("open demo database: %w", err)
	}
	if opts.Onboarding {
		err = prepareOnboardDemoDatabase(ctx, db, resolvedDB)
	} else {
		err = prepareDemoDatabase(ctx, db, resolvedDB)
	}
	if err != nil {
		db.Close()
		return err
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close prepared demo database: %w", err)
	}
	if err := prepareDemoEnv(opts.Port); err != nil {
		return err
	}

	if opts.Onboarding {
		fmt.Printf("Onboarding demo database: %s\n", displayProjectPath(resolvedDB))
	} else {
		fmt.Printf("Demo database: %s\n", displayProjectPath(resolvedDB))
	}
	if opts.PrepareOnly {
		if opts.Onboarding {
			fmt.Println("Onboarding demo database prepared; API server not started (--prepare-only).")
		} else {
			fmt.Println("Demo database prepared; API server not started (--prepare-only).")
		}
		return nil
	}
	fmt.Printf("API server:    http://127.0.0.1:%d\n\n", opts.Port)
	fmt.Println("# in a second terminal:")
	fmt.Println("pnpm run dev")
	if opts.Onboarding {
		fmt.Println("# then open http://localhost:3000/onboard")
	} else {
		fmt.Println("# then open http://localhost:3000")
	}
	fmt.Println()

	return runServe(ctx, []string{"--db", resolvedDB, "--port", fmt.Sprint(opts.Port)})
}

func prepareDemoDatabase(ctx context.Context, db *sql.DB, dbPath string) error {
	if _, err := store.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate demo database: %w", err)
	}
	if err := demoseed.CreateMsgvaultTables(ctx, db); err != nil {
		return fmt.Errorf("create demo archive tables: %w", err)
	}
	if err := demoseed.SeedDemo(ctx, db); err != nil {
		return fmt.Errorf("seed demo corpus: %w", err)
	}

	reader, err := msgvault.OpenReader(dbPath)
	if err != nil {
		return fmt.Errorf("open demo archive reader: %w", err)
	}
	defer reader.Close()

	classify := func() error {
		report, err := people.BuildCandidateReport(ctx, reader, people.CandidateOptions{Full: true})
		if err != nil {
			return fmt.Errorf("classify demo people: %w", err)
		}
		if err := people.PersistCandidateReport(ctx, db, report); err != nil {
			return fmt.Errorf("persist demo people: %w", err)
		}
		return nil
	}
	if err := classify(); err != nil {
		return err
	}
	nlReport, err := newsletter.DetectSources(ctx, db, 20)
	if err != nil {
		return fmt.Errorf("detect demo newsletters: %w", err)
	}
	if err := newsletter.PersistSources(ctx, db, nlReport); err != nil {
		return fmt.Errorf("persist demo newsletters: %w", err)
	}
	if err := classify(); err != nil {
		return err
	}
	if err := demoseed.SeedDemoNewsletterNarratives(ctx, db); err != nil {
		return fmt.Errorf("seed demo newsletter narratives: %w", err)
	}
	if err := refresh.RefreshAll(ctx, db); err != nil {
		return fmt.Errorf("refresh demo reports: %w", err)
	}
	return nil
}

func prepareOnboardDemoDatabase(ctx context.Context, db *sql.DB, dbPath string) error {
	if err := prepareDemoDatabase(ctx, db, dbPath); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys for onboarding demo cleanup: %w", err)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table'
		  AND name LIKE 'memento_%'
		  AND name <> 'memento_config'
		ORDER BY name
	`)
	if err != nil {
		return fmt.Errorf("discover onboarding demo tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			rows.Close()
			return fmt.Errorf("scan onboarding demo table: %w", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close onboarding demo table scan: %w", err)
	}
	for _, table := range tables {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			return fmt.Errorf("drop onboarding demo table %s: %w", table, err)
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM memento_config WHERE key <> 'demo_mode'`); err != nil {
		return fmt.Errorf("clear onboarding demo Memento config: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_config (key, value)
		VALUES ('demo_mode', 'true')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`); err != nil {
		return fmt.Errorf("mark onboarding demo database: %w", err)
	}
	_, _ = db.ExecContext(ctx, "PRAGMA foreign_keys = ON")
	return nil
}

func guardDemoTarget(ctx context.Context, target string) error {
	target = canonicalPath(target)
	if archivePath, err := config.ResolveMsgvaultDBPath(); err == nil && canonicalPath(archivePath) == target {
		return fmt.Errorf("refusing to use the configured msgvault archive as a demo database: %s", target)
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect demo database target: %w", err)
	}

	dsn := "file:" + url.PathEscape(target) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("refusing to overwrite existing unmarked file %s", target)
	}
	defer db.Close()
	var marker string
	if err := db.QueryRowContext(ctx, `SELECT value FROM memento_config WHERE key = 'demo_mode'`).Scan(&marker); err != nil || marker != "true" {
		return fmt.Errorf("refusing to overwrite existing database %s because it is not marked as a Memento demo DB", target)
	}
	return nil
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

func prepareDemoEnv(port int) error {
	envPath := filepath.Join(setupenv.FindProjectRoot(), ".env")
	existing := setupenv.ReadEnvKeys(envPath)
	needed := []string{
		config.EnvInternalToken,
		config.EnvBackendURL,
		config.EnvAgentStepLimit,
		config.EnvAgentModel,
		config.EnvAgentSimulation,
		config.EnvPublicAgentSimulation,
	}
	if _, ok := existing[config.EnvModelProvider]; !ok {
		needed = append(needed, config.EnvModelProvider)
		if _, modelOK := existing[config.EnvAgentModel]; !modelOK {
			needed = append(needed, config.EnvAgentModel)
		}
	}
	willWrite := false
	for _, key := range needed {
		if _, ok := existing[key]; !ok {
			willWrite = true
			break
		}
	}
	if willWrite {
		backup, err := setupenv.BackupOnce(envPath)
		if err != nil {
			return fmt.Errorf("back up .env: %w", err)
		}
		if backup != "" {
			fmt.Printf("Backed up .env to %s\n", filepath.Base(backup))
		}
	}

	if _, ok := existing[config.EnvBackendURL]; !ok && port != config.DefaultBackendPort {
		value := fmt.Sprintf("http://127.0.0.1:%d", port)
		if err := setupenv.WriteKey(config.EnvBackendURL, value); err != nil {
			return err
		}
		os.Setenv(config.EnvBackendURL, value)
	}
	if _, err := setupenv.EnsureAgentEnv(); err != nil {
		return fmt.Errorf("prepare demo runtime env: %w", err)
	}
	for _, key := range []string{config.EnvAgentSimulation, config.EnvPublicAgentSimulation} {
		if _, ok := existing[key]; ok {
			continue
		}
		if err := setupenv.WriteKey(key, "1"); err != nil {
			return err
		}
		os.Setenv(key, "1")
	}
	if _, ok := existing[config.EnvModelProvider]; !ok {
		if err := setupenv.WriteKey(config.EnvModelProvider, config.ProviderFake); err != nil {
			return err
		}
		os.Setenv(config.EnvModelProvider, config.ProviderFake)
		if _, modelOK := existing[config.EnvAgentModel]; !modelOK {
			if err := setupenv.WriteKey(config.EnvAgentModel, config.DefaultModelForProvider(config.ProviderFake)); err != nil {
				return err
			}
			os.Setenv(config.EnvAgentModel, config.DefaultModelForProvider(config.ProviderFake))
		}
	} else if provider := config.ModelProvider(); provider != config.ProviderFake {
		fmt.Printf("Notice: demo pages use baked content; new generation will use configured provider %q.\n", provider)
	}
	// The CLI loaded .env before dispatch. Reload once so the server started by
	// this same process sees keys that were just created.
	config.LoadDotEnv()
	return nil
}

func displayProjectPath(path string) string {
	root := canonicalPath(setupenv.FindProjectRoot())
	path = canonicalPath(path)
	if relative, err := filepath.Rel(root, path); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return relative
	}
	return path
}
