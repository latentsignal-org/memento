package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"memento/backend/internal/demoseed"
	"memento/backend/internal/store"
)

const defaultDBPath = "/tmp/memento-e2e.sqlite"

func main() {
	dbPath := flag.String("db", defaultDBPath, "SQLite fixture database path")
	flag.Parse()

	if err := run(context.Background(), *dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "e2e fixture: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("seeded E2E fixture DB at %s\n", *dbPath)
}

func run(ctx context.Context, dbPath string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := store.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := demoseed.CreateMsgvaultTables(ctx, db); err != nil {
		return fmt.Errorf("create msgvault fixture tables: %w", err)
	}
	if err := demoseed.SeedE2E(ctx, db); err != nil {
		return fmt.Errorf("seed fixture: %w", err)
	}
	return nil
}
