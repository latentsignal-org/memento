// Command demo-author exports reviewed LLM-authored demo rows from a scratch
// database. Maintainers run the real enrichment/compile flows against that DB
// before invoking this tool; the runtime demo never calls it or an external LLM.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"memento/backend/internal/store"
)

type exportFile struct {
	Rows []exportRow `json:"rows"`
}

type exportRow struct {
	Table            string          `json:"table"`
	EntityID         int64           `json:"entity_id"`
	Section          string          `json:"section"`
	Content          string          `json:"content"`
	SourceMessageIDs json.RawMessage `json:"source_message_ids"`
	Confidence       *float64        `json:"confidence,omitempty"`
}

func main() {
	dbPath := flag.String("db", "", "scratch demo-author SQLite database")
	outPath := flag.String("out", "internal/demoseed/fixtures/baked-export.json", "export destination")
	flag.Parse()
	if *dbPath == "" {
		fmt.Fprintln(os.Stderr, "demo-author: --db is required")
		os.Exit(2)
	}
	if err := run(context.Background(), *dbPath, *outPath); err != nil {
		fmt.Fprintf(os.Stderr, "demo-author: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dbPath, outPath string) error {
	db, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	var rows []exportRow
	for _, table := range []struct {
		name     string
		idColumn string
	}{
		{"memento_person_narrative", "person_id"},
		{"memento_project_narrative", "project_id"},
		{"memento_concept_narrative", "concept_id"},
		{"memento_newsletter_narrative", "source_id"},
	} {
		query := fmt.Sprintf(`SELECT %s, section, content, source_message_ids FROM %s ORDER BY %s, section`, table.idColumn, table.name, table.idColumn)
		result, err := db.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		for result.Next() {
			var row exportRow
			var sourceIDs string
			row.Table = table.name
			if err := result.Scan(&row.EntityID, &row.Section, &row.Content, &sourceIDs); err != nil {
				result.Close()
				return err
			}
			row.SourceMessageIDs = normalizedJSON(sourceIDs)
			rows = append(rows, row)
		}
		if err := result.Close(); err != nil {
			return err
		}
	}

	facetRows, err := db.QueryContext(ctx, `SELECT person_id, facet_type, content, source_message_ids, confidence FROM memento_person_facet ORDER BY person_id, facet_type`)
	if err != nil {
		return err
	}
	for facetRows.Next() {
		var row exportRow
		var sourceIDs string
		row.Table = "memento_person_facet"
		if err := facetRows.Scan(&row.EntityID, &row.Section, &row.Content, &sourceIDs, &row.Confidence); err != nil {
			facetRows.Close()
			return err
		}
		row.SourceMessageIDs = normalizedJSON(sourceIDs)
		rows = append(rows, row)
	}
	if err := facetRows.Close(); err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no baked narrative rows found; run the authoring enrichment/compile flows first")
	}

	payload, err := json.MarshalIndent(exportFile{Rows: rows}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, payload, 0644); err != nil {
		return err
	}
	fmt.Printf("exported %d baked rows to %s\n", len(rows), outPath)
	return nil
}

func normalizedJSON(value string) json.RawMessage {
	raw := json.RawMessage(value)
	if json.Valid(raw) {
		return raw
	}
	encoded, _ := json.Marshal([]int64{})
	return encoded
}
