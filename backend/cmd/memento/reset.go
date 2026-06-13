package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"strings"

	"memento/backend/internal/config"
	"memento/backend/internal/store"
)

// discoverMementoTables returns every `memento_*` table currently present
// in the SQLite file. Computed dynamically so a new migration that adds a
// table is automatically swept up by `./memento reset` without needing a
// matching edit here — the previous hard-coded list silently rotted as
// the schema grew (missing memento_social_*, memento_*_report,
// memento_draft, memento_note, memento_person_facet/narrative, and the
// agent-session tables).
func discoverMementoTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name LIKE 'memento_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func runReset(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path (overrides config)")
	force := fs.Bool("force", false, "skip confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedPath := *dbPath
	if resolvedPath == "" {
		var err error
		resolvedPath, err = config.ResolveMsgvaultDBPath()
		if err != nil {
			return fmt.Errorf("cannot resolve database path: %w", err)
		}
	}

	db, err := store.Open(resolvedPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	tables, err := discoverMementoTables(ctx, db)
	if err != nil {
		return fmt.Errorf("discover memento tables: %w", err)
	}

	fmt.Printf("Database: %s\n\n", resolvedPath)
	if len(tables) == 0 {
		fmt.Println("No memento_* tables found — nothing to reset.")
		return nil
	}
	fmt.Printf("This will permanently drop %d Memento-owned table(s):\n", len(tables))
	for _, t := range tables {
		fmt.Printf("  %s\n", t)
	}
	fmt.Println()
	fmt.Println("The msgvault archive data (messages, threads, etc.) is untouched.")
	fmt.Println()

	if !*force {
		fmt.Print("Type \"reset\" to confirm: ")
		s := getInitScanner()
		s.Scan()
		if strings.TrimSpace(s.Text()) != "reset" {
			fmt.Println("Aborted.")
			return nil
		}
		fmt.Println()
	}

	// Disable FK enforcement for the duration of the drop. SQLite enforces
	// foreign keys per connection; combined with SetMaxOpenConns(1) in
	// store.Open, this PRAGMA only needs to be set once and stays in effect
	// for every following statement on this handle. With FKs off, drop
	// order is immaterial and we cannot leave orphan parent tables behind
	// when a child happens to alphabetize after its parent.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return fmt.Errorf("disable foreign keys: %w", err)
	}

	dropped := 0
	for _, table := range tables {
		// Each table name came straight out of sqlite_master, so it is a
		// known identifier — no quoting concerns. Still using IF EXISTS in
		// case a concurrent process raced us (paranoid; unlikely on a
		// local-first SQLite file).
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
		dropped++
		fmt.Printf("  dropped %s\n", table)
	}

	// Re-enable FKs so the connection is in a sane state if anything else
	// reuses it before close. Harmless if it fails — the handle is about
	// to be closed by the deferred db.Close anyway.
	_, _ = db.ExecContext(ctx, "PRAGMA foreign_keys = ON")

	fmt.Printf("\nReset complete. %d table(s) removed.\n", dropped)
	fmt.Println("Run `memento init` to re-index from scratch.")
	return nil
}
