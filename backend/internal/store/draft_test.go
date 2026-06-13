package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func newDraftTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestCreateDraft_AppendsInitialRevision(t *testing.T) {
	db := newDraftTestDB(t)
	ctx := context.Background()

	draftID, err := CreateDraft(ctx, db, "project", "Test draft")
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_draft_revision WHERE draft_id = ?`, draftID).Scan(&count); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if count != 1 {
		t.Fatalf("revision count = %d, want 1", count)
	}
}

func TestUpdateDraftEntities_AppendsRevisionSnapshot(t *testing.T) {
	db := newDraftTestDB(t)
	ctx := context.Background()

	draftID, err := CreateDraft(ctx, db, "concept", "Concept draft")
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if err := UpdateDraftState(ctx, db, draftID, "interaction_1", `[{"role":"user","text":"first prompt"}]`); err != nil {
		t.Fatalf("UpdateDraftState: %v", err)
	}
	if err := UpdateDraftEntities(ctx, db, draftID, `{"name":"Bundle A"}`); err != nil {
		t.Fatalf("UpdateDraftEntities: %v", err)
	}

	var kind, transcriptJSON, entitiesJSON string
	if err := db.QueryRowContext(ctx, `
		SELECT revision_kind, transcript_json, entities_json
		FROM memento_draft_revision
		WHERE draft_id = ?
		ORDER BY id DESC
		LIMIT 1`, draftID,
	).Scan(&kind, &transcriptJSON, &entitiesJSON); err != nil {
		t.Fatalf("load latest revision: %v", err)
	}
	if kind != "entities_update" {
		t.Fatalf("revision_kind = %q, want entities_update", kind)
	}
	if transcriptJSON != `[{"role":"user","text":"first prompt"}]` {
		t.Fatalf("transcript_json = %q", transcriptJSON)
	}
	if entitiesJSON != `{"name":"Bundle A"}` {
		t.Fatalf("entities_json = %q", entitiesJSON)
	}
}

func TestUpdateDraftStateAndCommit_AppendLifecycleRevisions(t *testing.T) {
	db := newDraftTestDB(t)
	ctx := context.Background()

	draftID, err := CreateDraft(ctx, db, "project", "Lifecycle draft")
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if err := UpdateDraftEntities(ctx, db, draftID, `{"name":"Bundle B","messages":[{"message_id":1}]}`); err != nil {
		t.Fatalf("UpdateDraftEntities: %v", err)
	}
	if err := UpdateDraftState(ctx, db, draftID, "interaction_2", `[{"role":"user","text":"refine this bundle"}]`); err != nil {
		t.Fatalf("UpdateDraftState: %v", err)
	}
	if err := MarkDraftCommitted(ctx, db, draftID, 99); err != nil {
		t.Fatalf("MarkDraftCommitted: %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT revision_kind
		FROM memento_draft_revision
		WHERE draft_id = ?
		ORDER BY id ASC`, draftID,
	)
	if err != nil {
		t.Fatalf("query revisions: %v", err)
	}
	defer rows.Close()

	var kinds []string
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatalf("scan revision kind: %v", err)
		}
		kinds = append(kinds, kind)
	}
	want := []string{"draft_created", "entities_update", "transcript_update", "committed_snapshot"}
	if len(kinds) != len(want) {
		t.Fatalf("revision kinds len = %d, want %d (%v)", len(kinds), len(want), kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("revision kind[%d] = %q, want %q", i, kinds[i], want[i])
		}
	}
}

func TestSetCommittedEntityDraftOrigin_StampsProjectAndConcept(t *testing.T) {
	db := newDraftTestDB(t)
	ctx := context.Background()

	projectRes, err := db.ExecContext(ctx, `
		INSERT INTO memento_project (slug, name, aliases, status, note)
		VALUES ('proj-a', 'Project A', '[]', 'active', '')`)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	projectID, _ := projectRes.LastInsertId()

	conceptRes, err := db.ExecContext(ctx, `
		INSERT INTO memento_concept (slug, name, scope_description, seed_keywords, status, note)
		VALUES ('concept-a', 'Concept A', '', '[]', 'active', '')`)
	if err != nil {
		t.Fatalf("insert concept: %v", err)
	}
	conceptID, _ := conceptRes.LastInsertId()

	if err := SetCommittedEntityDraftOrigin(ctx, db, "project", projectID, 41); err != nil {
		t.Fatalf("SetCommittedEntityDraftOrigin(project): %v", err)
	}
	if err := SetCommittedEntityDraftOrigin(ctx, db, "concept", conceptID, 42); err != nil {
		t.Fatalf("SetCommittedEntityDraftOrigin(concept): %v", err)
	}

	var projectDraftID, conceptDraftID int64
	if err := db.QueryRowContext(ctx, `SELECT origin_draft_id FROM memento_project WHERE id = ?`, projectID).Scan(&projectDraftID); err != nil {
		t.Fatalf("query project origin draft: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT origin_draft_id FROM memento_concept WHERE id = ?`, conceptID).Scan(&conceptDraftID); err != nil {
		t.Fatalf("query concept origin draft: %v", err)
	}
	if projectDraftID != 41 {
		t.Fatalf("project origin_draft_id = %d, want 41", projectDraftID)
	}
	if conceptDraftID != 42 {
		t.Fatalf("concept origin_draft_id = %d, want 42", conceptDraftID)
	}
}
