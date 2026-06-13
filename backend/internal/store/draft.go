// Package store — draft.go: CRUD for memento_draft rows. Drafts are the
// staging area for project/concept creation: a chat transcript, the agent's
// proposed EntityBundle, and provider interaction state used to continue
// conversation history server-side.
package store

import (
	"context"
	"database/sql"
	"fmt"
)

type Draft struct {
	ID                int64  `json:"id"`
	Kind              string `json:"kind"` // 'project' | 'concept'
	NameHint          string `json:"name_hint"`
	TranscriptJSON    string `json:"transcript_json"`
	EntitiesJSON      string `json:"entities_json"`
	InteractionID     string `json:"interaction_id"`
	Status            string `json:"status"`
	CommittedEntityID *int64 `json:"committed_entity_id,omitempty"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type DraftRevision struct {
	ID             int64  `json:"id"`
	DraftID        int64  `json:"draft_id"`
	RevisionKind   string `json:"revision_kind"`
	TranscriptJSON string `json:"transcript_json"`
	EntitiesJSON   string `json:"entities_json"`
	CreatedAt      string `json:"created_at"`
}

// CreateDraft inserts a new draft in 'collecting' state and returns its id.
func CreateDraft(ctx context.Context, db *sql.DB, kind, nameHint string) (int64, error) {
	if kind != "project" && kind != "concept" {
		return 0, fmt.Errorf("invalid draft kind %q (want project|concept)", kind)
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO memento_draft (kind, name_hint, transcript_json, entities_json, status)
		VALUES (?, ?, '[]', '{}', 'collecting')`,
		kind, nameHint,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := appendDraftRevision(ctx, db, id, "draft_created", "[]", "{}"); err != nil {
		return 0, err
	}
	return id, nil
}

// GetDraft fetches a single draft by id. Returns sql.ErrNoRows if absent.
func GetDraft(ctx context.Context, db *sql.DB, id int64) (Draft, error) {
	var d Draft
	var committed sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT id, kind, name_hint, transcript_json, entities_json,
		       interaction_id, status, committed_entity_id, created_at, updated_at
		FROM memento_draft WHERE id = ?`, id,
	).Scan(
		&d.ID, &d.Kind, &d.NameHint, &d.TranscriptJSON, &d.EntitiesJSON,
		&d.InteractionID, &d.Status, &committed, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return d, err
	}
	if committed.Valid {
		d.CommittedEntityID = new(committed.Int64)
	}
	return d, nil
}

// UpdateDraftEntities replaces entities_json wholesale and bumps updated_at.
// Used by the curation UI (browser) and by the agent's propose_bundle tool.
func UpdateDraftEntities(ctx context.Context, db *sql.DB, id int64, entitiesJSON string) error {
	var transcriptJSON string
	err := db.QueryRowContext(ctx, `
		SELECT transcript_json
		FROM memento_draft
		WHERE id = ?`, id,
	).Scan(&transcriptJSON)
	if err != nil {
		return err
	}

	res, err := db.ExecContext(ctx, `
		UPDATE memento_draft
		SET entities_json = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, entitiesJSON, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return appendDraftRevision(ctx, db, id, "entities_update", transcriptJSON, entitiesJSON)
}

// UpdateDraftState writes the latest interaction_id and replaces the
// user-visible transcript. Called by Next.js after each agent turn finishes.
func UpdateDraftState(ctx context.Context, db *sql.DB, id int64, interactionID, transcriptJSON string) error {
	var entitiesJSON string
	err := db.QueryRowContext(ctx, `
		SELECT entities_json
		FROM memento_draft
		WHERE id = ?`, id,
	).Scan(&entitiesJSON)
	if err != nil {
		return err
	}

	res, err := db.ExecContext(ctx, `
		UPDATE memento_draft
		SET interaction_id = ?, transcript_json = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, interactionID, transcriptJSON, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return appendDraftRevision(ctx, db, id, "transcript_update", transcriptJSON, entitiesJSON)
}

// MarkDraftCommitted records the promoted project/concept id and locks the draft.
func MarkDraftCommitted(ctx context.Context, db *sql.DB, id, committedEntityID int64) error {
	var transcriptJSON, entitiesJSON string
	err := db.QueryRowContext(ctx, `
		SELECT transcript_json, entities_json
		FROM memento_draft
		WHERE id = ?`, id,
	).Scan(&transcriptJSON, &entitiesJSON)
	if err != nil {
		return err
	}

	res, err := db.ExecContext(ctx, `
		UPDATE memento_draft
		SET status = 'committed', committed_entity_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, committedEntityID, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return appendDraftRevision(ctx, db, id, "committed_snapshot", transcriptJSON, entitiesJSON)
}

// MarkDraftAbandoned soft-deletes a draft.
func MarkDraftAbandoned(ctx context.Context, db *sql.DB, id int64) error {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_draft
		SET status = 'abandoned', updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func SetCommittedEntityDraftOrigin(ctx context.Context, db *sql.DB, kind string, entityID, draftID int64) error {
	var query string
	switch kind {
	case "project":
		query = `UPDATE memento_project SET origin_draft_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	case "concept":
		query = `UPDATE memento_concept SET origin_draft_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	default:
		return fmt.Errorf("unsupported entity kind %q", kind)
	}
	res, err := db.ExecContext(ctx, query, draftID, entityID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func appendDraftRevision(ctx context.Context, db *sql.DB, draftID int64, revisionKind, transcriptJSON, entitiesJSON string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO memento_draft_revision (draft_id, revision_kind, transcript_json, entities_json)
		VALUES (?, ?, ?, ?)`,
		draftID, revisionKind, transcriptJSON, entitiesJSON,
	)
	return err
}
