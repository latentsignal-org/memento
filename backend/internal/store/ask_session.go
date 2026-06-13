// Package store — ask_session.go: CRUD for the user-facing Ask Session
// dimension (memento_ask_session / memento_ask_turn / memento_ask_context_ref).
// Ask Sessions are product artifacts: saved investigations with final answers
// and context references. They link to raw memento_agent_session debug runs
// via a nullable run_id and must survive debug purges.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"memento/backend/internal/slugs"
)

type AskSession struct {
	ID         int64   `json:"id"`
	Slug       string  `json:"slug"`
	Title      string  `json:"title"`
	Summary    string  `json:"summary"`
	Status     string  `json:"status"`
	Pinned     bool    `json:"pinned"`
	ArchivedAt *string `json:"archived_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	TurnCount  int     `json:"turn_count,omitempty"`
}

type AskTurn struct {
	ID                  int64  `json:"id"`
	AskSessionID        int64  `json:"ask_session_id"`
	RunID               *int64 `json:"run_id,omitempty"`
	TurnIndex           int    `json:"turn_index"`
	UserMessage         string `json:"user_message"`
	AssistantAnswer     string `json:"assistant_answer"`
	AnswerSummary       string `json:"answer_summary"`
	Status              string `json:"status"`
	CitedMessageIDsJSON string `json:"cited_message_ids_json"`
	ToolSummaryJSON     string `json:"tool_summary_json"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type AskContextRef struct {
	ID          int64  `json:"id"`
	AskTurnID   int64  `json:"ask_turn_id"`
	RefKind     string `json:"ref_kind"`
	RefID       string `json:"ref_id"`
	Label       string `json:"label"`
	PayloadJSON string `json:"payload_json"`
	CreatedAt   string `json:"created_at"`
}

var askSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// askSessionSlugBase derives a slug fragment from the session title (itself
// derived from the first user query). Falls back to "ask" for empty input.
func askSessionSlugBase(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = askSlugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = s[:64]
		s = strings.Trim(s, "-")
	}
	if s == "" || slugs.ReservedPathSegment(s) {
		return "ask"
	}
	return s
}

// AskSessionTitleFromQuery derives a deterministic session title from the
// first user message: whitespace-collapsed and truncated on a word boundary.
func AskSessionTitleFromQuery(query string) string {
	clean := strings.Join(strings.Fields(query), " ")
	const maxLen = 80
	if len(clean) <= maxLen {
		return clean
	}
	cut := clean[:maxLen]
	if idx := strings.LastIndex(cut, " "); idx > maxLen/2 {
		cut = cut[:idx]
	}
	return cut + "…"
}

// CreateAskSession inserts a new session, generating a unique slug from the
// title. Concurrent-safe at this app's write concurrency (single connection).
func CreateAskSession(ctx context.Context, db *sql.DB, title string) (AskSession, error) {
	base := askSessionSlugBase(title)
	slug := base
	for attempt := 2; ; attempt++ {
		var exists int
		err := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM memento_ask_session WHERE slug = ?`, slug,
		).Scan(&exists)
		if err != nil {
			return AskSession{}, err
		}
		if exists == 0 {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, attempt)
	}
	res, err := db.ExecContext(ctx, `
		INSERT INTO memento_ask_session (slug, title) VALUES (?, ?)`,
		slug, title,
	)
	if err != nil {
		return AskSession{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return AskSession{}, err
	}
	return GetAskSessionByID(ctx, db, id)
}

const askSessionColumns = `id, slug, title, summary, status, pinned, archived_at, created_at, updated_at`

func scanAskSession(row *sql.Row) (AskSession, error) {
	var s AskSession
	var pinned int
	var archived sql.NullString
	err := row.Scan(&s.ID, &s.Slug, &s.Title, &s.Summary, &s.Status, &pinned, &archived, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	s.Pinned = pinned != 0
	if archived.Valid {
		s.ArchivedAt = &archived.String
	}
	return s, nil
}

// GetAskSessionByID fetches one session. Returns sql.ErrNoRows if absent.
func GetAskSessionByID(ctx context.Context, db *sql.DB, id int64) (AskSession, error) {
	return scanAskSession(db.QueryRowContext(ctx,
		`SELECT `+askSessionColumns+` FROM memento_ask_session WHERE id = ?`, id))
}

// GetAskSessionBySlug fetches one session. Returns sql.ErrNoRows if absent.
func GetAskSessionBySlug(ctx context.Context, db *sql.DB, slug string) (AskSession, error) {
	return scanAskSession(db.QueryRowContext(ctx,
		`SELECT `+askSessionColumns+` FROM memento_ask_session WHERE slug = ?`, slug))
}

// ListAskSessions returns sessions for the index page: unarchived first,
// pinned before unpinned, most recently updated first. Includes turn counts.
func ListAskSessions(ctx context.Context, db *sql.DB, includeArchived bool) ([]AskSession, error) {
	where := ""
	if !includeArchived {
		where = "WHERE s.archived_at IS NULL"
	}
	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.slug, s.title, s.summary, s.status, s.pinned, s.archived_at,
		       s.created_at, s.updated_at,
		       (SELECT COUNT(*) FROM memento_ask_turn t WHERE t.ask_session_id = s.id) AS turn_count
		FROM memento_ask_session s
		`+where+`
		ORDER BY s.archived_at IS NOT NULL, s.pinned DESC, s.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []AskSession{}
	for rows.Next() {
		var s AskSession
		var pinned int
		var archived sql.NullString
		if err := rows.Scan(&s.ID, &s.Slug, &s.Title, &s.Summary, &s.Status, &pinned, &archived,
			&s.CreatedAt, &s.UpdatedAt, &s.TurnCount); err != nil {
			return nil, err
		}
		s.Pinned = pinned != 0
		if archived.Valid {
			s.ArchivedAt = &archived.String
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// UpdateAskSession applies user-facing metadata changes. Nil fields are left
// untouched; archived=true stamps archived_at, archived=false clears it.
func UpdateAskSession(ctx context.Context, db *sql.DB, id int64, title *string, pinned *bool, archived *bool) (AskSession, error) {
	if title != nil {
		clean := strings.Join(strings.Fields(*title), " ")
		if clean == "" {
			return AskSession{}, fmt.Errorf("title cannot be empty")
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE memento_ask_session SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			clean, id,
		); err != nil {
			return AskSession{}, err
		}
	}
	if pinned != nil {
		pin := 0
		if *pinned {
			pin = 1
		}
		if _, err := db.ExecContext(ctx,
			`UPDATE memento_ask_session SET pinned = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			pin, id,
		); err != nil {
			return AskSession{}, err
		}
	}
	if archived != nil {
		var err error
		if *archived {
			_, err = db.ExecContext(ctx,
				`UPDATE memento_ask_session SET archived_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				id,
			)
		} else {
			_, err = db.ExecContext(ctx,
				`UPDATE memento_ask_session SET archived_at = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				id,
			)
		}
		if err != nil {
			return AskSession{}, err
		}
	}
	return GetAskSessionByID(ctx, db, id)
}

// DeleteAskSession removes the product session and its turns/context refs. Raw
// debug runs are intentionally left alone unless the caller purges them first.
func DeleteAskSession(ctx context.Context, db *sql.DB, id int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM memento_ask_context_ref
		WHERE ask_turn_id IN (SELECT id FROM memento_ask_turn WHERE ask_session_id = ?)`, id,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_ask_turn WHERE ask_session_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM memento_ask_session WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// AppendAskTurn adds the next turn to a session in 'running' state and bumps
// the session's updated_at. The turn index is assigned inside a transaction.
func AppendAskTurn(ctx context.Context, db *sql.DB, sessionID int64, userMessage string) (AskTurn, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return AskTurn{}, err
	}
	defer tx.Rollback()

	var next int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(turn_index), -1) + 1 FROM memento_ask_turn WHERE ask_session_id = ?`,
		sessionID,
	).Scan(&next); err != nil {
		return AskTurn{}, err
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO memento_ask_turn (ask_session_id, turn_index, user_message)
		VALUES (?, ?, ?)`,
		sessionID, next, userMessage,
	)
	if err != nil {
		return AskTurn{}, err
	}
	turnID, err := res.LastInsertId()
	if err != nil {
		return AskTurn{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE memento_ask_session SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		sessionID,
	); err != nil {
		return AskTurn{}, err
	}
	if err := tx.Commit(); err != nil {
		return AskTurn{}, err
	}
	return GetAskTurn(ctx, db, turnID)
}

const askTurnColumns = `id, ask_session_id, run_id, turn_index, user_message, assistant_answer,
	answer_summary, status, cited_message_ids_json, tool_summary_json, created_at, updated_at`

func scanAskTurn(scan func(dest ...any) error) (AskTurn, error) {
	var t AskTurn
	var runID sql.NullInt64
	err := scan(&t.ID, &t.AskSessionID, &runID, &t.TurnIndex, &t.UserMessage, &t.AssistantAnswer,
		&t.AnswerSummary, &t.Status, &t.CitedMessageIDsJSON, &t.ToolSummaryJSON, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return t, err
	}
	if runID.Valid {
		t.RunID = new(runID.Int64)
	}
	return t, nil
}

// GetAskTurn fetches one turn by id. Returns sql.ErrNoRows if absent.
func GetAskTurn(ctx context.Context, db *sql.DB, id int64) (AskTurn, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+askTurnColumns+` FROM memento_ask_turn WHERE id = ?`, id)
	return scanAskTurn(row.Scan)
}

// ListAskTurns returns a session's turns in conversation order.
func ListAskTurns(ctx context.Context, db *sql.DB, sessionID int64) ([]AskTurn, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+askTurnColumns+` FROM memento_ask_turn WHERE ask_session_id = ? ORDER BY turn_index`,
		sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns := []AskTurn{}
	for rows.Next() {
		t, err := scanAskTurn(rows.Scan)
		if err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

// LinkAskTurnRun records the debug run backing a turn once the run has started.
func LinkAskTurnRun(ctx context.Context, db *sql.DB, turnID, runID int64) error {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_ask_turn
		SET run_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, runID, turnID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CompleteAskTurn persists the final answer for a turn and refreshes the
// session: updated_at always, and summary from the answer summary when the
// session summary is still empty (i.e. the first completed turn).
func CompleteAskTurn(ctx context.Context, db *sql.DB, turnID int64, answer, answerSummary, citedMessageIDsJSON, toolSummaryJSON string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var sessionID int64
	if err := tx.QueryRowContext(ctx,
		`SELECT ask_session_id FROM memento_ask_turn WHERE id = ?`, turnID,
	).Scan(&sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE memento_ask_turn
		SET assistant_answer = ?, answer_summary = ?, status = 'complete',
		    cited_message_ids_json = ?, tool_summary_json = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		answer, answerSummary, citedMessageIDsJSON, toolSummaryJSON, turnID,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE memento_ask_session
		SET summary = CASE WHEN summary = '' THEN ? ELSE summary END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		answerSummary, sessionID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// MarkAskTurnFailed flags a turn whose backing run failed or was cancelled.
func MarkAskTurnFailed(ctx context.Context, db *sql.DB, turnID int64) error {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_ask_turn
		SET status = 'failed', updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, turnID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AddAskContextRefs persists validated context references for a turn.
func AddAskContextRefs(ctx context.Context, db *sql.DB, turnID int64, refs []AskContextRef) error {
	for _, ref := range refs {
		payload := ref.PayloadJSON
		if payload == "" {
			payload = "{}"
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO memento_ask_context_ref (ask_turn_id, ref_kind, ref_id, label, payload_json)
			VALUES (?, ?, ?, ?, ?)`,
			turnID, ref.RefKind, ref.RefID, ref.Label, payload,
		); err != nil {
			return err
		}
	}
	return nil
}

// ListAskContextRefs returns the refs recorded for a set of turns, keyed by turn id.
func ListAskContextRefs(ctx context.Context, db *sql.DB, turnIDs []int64) (map[int64][]AskContextRef, error) {
	out := map[int64][]AskContextRef{}
	if len(turnIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(turnIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(turnIDs))
	for i, id := range turnIDs {
		args[i] = id
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, ask_turn_id, ref_kind, ref_id, label, payload_json, created_at
		FROM memento_ask_context_ref
		WHERE ask_turn_id IN (`+placeholders+`)
		ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref AskContextRef
		if err := rows.Scan(&ref.ID, &ref.AskTurnID, &ref.RefKind, &ref.RefID, &ref.Label, &ref.PayloadJSON, &ref.CreatedAt); err != nil {
			return nil, err
		}
		out[ref.AskTurnID] = append(out[ref.AskTurnID], ref)
	}
	return out, rows.Err()
}
