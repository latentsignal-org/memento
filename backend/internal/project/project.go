package project

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"memento/backend/internal/msgvaultapi"
	"memento/backend/internal/slugs"
)

func normalizeSearchMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "fts"
	}
	switch mode {
	case "fts", "hybrid":
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported search mode %q (expected fts or hybrid)", mode)
	}
}

type Project struct {
	ID        int64    `json:"project_id"`
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	Status    string   `json:"status"`
	StartedAt *string  `json:"started_at"`
	UpdatedAt string   `json:"updated_at"`
	Note      string   `json:"note"`
}

type ProjectMember struct {
	PersonID      int64  `json:"person_id"`
	CanonicalName string `json:"canonical_name"`
	PrimaryEmail  string `json:"primary_email"`
	Role          string `json:"role"`
	Slug          string `json:"slug"`
}

type MessageBundleItem struct {
	MessageID           int64  `json:"message_id"`
	Date                string `json:"date"`
	SenderCanonicalName string `json:"sender_canonical_name"`
	SenderPrimaryEmail  string `json:"sender_primary_email"`
	Subject             string `json:"subject"`
	Snippet             string `json:"snippet"`
	BodyText            string `json:"body_text"`
	Direction           string `json:"direction"` // from_account | to_account | other
}

func CreateProject(ctx context.Context, db *sql.DB, name, slug string, startedAt *string) (int64, error) {
	if err := slugs.ValidateEntitySlug(slug); err != nil {
		return 0, err
	}
	var aliasesStr = "[]"
	var startedAtVal sql.NullString
	if startedAt != nil && *startedAt != "" {
		startedAtVal = sql.NullString{String: *startedAt, Valid: true}
	}

	res, err := db.ExecContext(ctx, `
		INSERT INTO memento_project (name, slug, aliases, status, started_at)
		VALUES (?, ?, ?, 'active', ?)
	`, name, slug, aliasesStr, startedAtVal)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetProjectBySlug(ctx context.Context, db *sql.DB, slug string) (Project, error) {
	var p Project
	var startedAt sql.NullString
	var aliasesRaw string
	err := db.QueryRowContext(ctx, `
		SELECT id, slug, name, aliases, status, started_at, updated_at, note
		FROM memento_project
		WHERE slug = ?
	`, slug).Scan(&p.ID, &p.Slug, &p.Name, &aliasesRaw, &p.Status, &startedAt, &p.UpdatedAt, &p.Note)
	if err != nil {
		return p, err
	}
	if startedAt.Valid {
		p.StartedAt = &startedAt.String
	}
	if err := json.Unmarshal([]byte(aliasesRaw), &p.Aliases); err != nil {
		p.Aliases = []string{}
	}
	return p, nil
}

func AddPerson(ctx context.Context, db *sql.DB, projectSlug, email, role string) error {
	p, err := GetProjectBySlug(ctx, db, projectSlug)
	if err != nil {
		return fmt.Errorf("lookup project: %w", err)
	}

	// Lookup person by email in memento_person_email
	var personID int64
	err = db.QueryRowContext(ctx, `
		SELECT person_id FROM memento_person_email WHERE lower(email_address) = ?
	`, strings.ToLower(email)).Scan(&personID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Automatically create a new person and email link for this email address.
			parts := strings.Split(email, "@")
			canonicalName := parts[0]
			if len(canonicalName) > 0 {
				canonicalName = strings.ToUpper(canonicalName[:1]) + canonicalName[1:]
			} else {
				canonicalName = email
			}

			res, err := db.ExecContext(ctx, `
				INSERT INTO memento_person (canonical_name, primary_email)
				VALUES (?, ?)
			`, canonicalName, email)
			if err != nil {
				return fmt.Errorf("create person for %s: %w", email, err)
			}

			personID, err = res.LastInsertId()
			if err != nil {
				return err
			}

			_, err = db.ExecContext(ctx, `
				INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence, locked)
				VALUES (?, ?, ?, 'manual', 1.0, 1)
			`, strings.ToLower(email), personID, canonicalName)
			if err != nil {
				return fmt.Errorf("create person email for %s: %w", email, err)
			}
		} else {
			return err
		}
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO memento_project_member (project_id, person_id, role)
		VALUES (?, ?, ?)
		ON CONFLICT(project_id, person_id) DO UPDATE SET role = excluded.role
	`, p.ID, personID, role)
	return err
}

func AddMessageExplicit(ctx context.Context, db *sql.DB, projectSlug string, messageID int64, includedBy string) error {
	p, err := GetProjectBySlug(ctx, db, projectSlug)
	if err != nil {
		return fmt.Errorf("lookup project: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO memento_project_message (project_id, message_id, included_by)
		VALUES (?, ?, ?)
		ON CONFLICT(project_id, message_id) DO NOTHING
	`, p.ID, messageID, includedBy)
	return err
}

func RemoveMessage(ctx context.Context, db *sql.DB, projectSlug string, messageID int64) error {
	p, err := GetProjectBySlug(ctx, db, projectSlug)
	if err != nil {
		return fmt.Errorf("lookup project: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		DELETE FROM memento_project_message
		WHERE project_id = ? AND message_id = ?
	`, p.ID, messageID)
	return err
}

func ClearMessages(ctx context.Context, db *sql.DB, projectSlug string) (int64, error) {
	p, err := GetProjectBySlug(ctx, db, projectSlug)
	if err != nil {
		return 0, fmt.Errorf("lookup project: %w", err)
	}

	res, err := db.ExecContext(ctx, `
		DELETE FROM memento_project_message
		WHERE project_id = ?
	`, p.ID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// msgvaultSearchResult represents the parsed result from the msgvault CLI
type msgvaultSearchResult struct {
	Results []struct {
		ID int64 `json:"id"`
	} `json:"results"`
}

func runMsgvaultSearch(ctx context.Context, query, mode string) ([]int64, error) {
	mode, err := normalizeSearchMode(mode)
	if err != nil {
		return nil, err
	}
	if mode == "hybrid" && msgvaultapi.RequiresFTSMode(query) {
		mode = "fts"
	}
	if client, ok := msgvaultapi.FromEnv(); ok {
		if ids, err := client.SearchIDs(ctx, query, mode, 500); err == nil {
			return ids, nil
		}
		if mode == "hybrid" {
			if ids, err := client.SearchIDs(ctx, query, "fts", 500); err == nil {
				return ids, nil
			}
		}
	}

	cmd := exec.CommandContext(ctx, "msgvault", "search", query, "--mode", mode, "--json", "--limit", "500")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		// Fallback to FTS only if hybrid fails.
		if mode != "hybrid" {
			return nil, fmt.Errorf("msgvault search command failed: %w (stderr: %s)", err, stderr.String())
		}
		cmd = exec.CommandContext(ctx, "msgvault", "search", query, "--mode", "fts", "--json", "--limit", "500")
		stdout.Reset()
		stderr.Reset()
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		if err != nil {
			return nil, fmt.Errorf("msgvault search command failed: %w (stderr: %s)", err, stderr.String())
		}
	}

	jsonData := stdout.Bytes()
	idx := bytes.IndexAny(jsonData, "{[")
	if idx >= 0 {
		jsonData = jsonData[idx:]
	}

	// Try hybrid format first: {"results": [...]}
	var hybridRes msgvaultSearchResult
	if err := json.Unmarshal(jsonData, &hybridRes); err == nil && len(hybridRes.Results) > 0 {
		var ids []int64
		for _, r := range hybridRes.Results {
			ids = append(ids, r.ID)
		}
		return ids, nil
	}

	// Try FTS format: [...]
	var ftsRes []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(jsonData, &ftsRes); err == nil {
		var ids []int64
		for _, r := range ftsRes {
			ids = append(ids, r.ID)
		}
		return ids, nil
	}

	return nil, fmt.Errorf("unmarshal search results failed (output: %s)", stdout.String())
}

func AddMessagesBySearch(ctx context.Context, db *sql.DB, projectSlug, query, mode string) (int, error) {
	mode, err := normalizeSearchMode(mode)
	if err != nil {
		return 0, err
	}

	ids, err := runMsgvaultSearch(ctx, query, mode)
	if err != nil {
		return 0, err
	}

	added := 0
	for _, id := range ids {
		err := AddMessageExplicit(ctx, db, projectSlug, id, "search:"+mode)
		if err == nil {
			added++
		}
	}
	return added, nil
}

func AddMessagesByLabel(ctx context.Context, db *sql.DB, projectSlug, labelName string) (int, error) {
	p, err := GetProjectBySlug(ctx, db, projectSlug)
	if err != nil {
		return 0, fmt.Errorf("lookup project: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT ml.message_id
		FROM message_labels ml
		JOIN labels l ON l.id = ml.label_id
		WHERE lower(l.name) = ?
	`, strings.ToLower(labelName))
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var msgIDs []int64
	for rows.Next() {
		var msgID int64
		if err := rows.Scan(&msgID); err != nil {
			return 0, err
		}
		msgIDs = append(msgIDs, msgID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	added := 0
	for _, msgID := range msgIDs {
		_, err = db.ExecContext(ctx, `
			INSERT INTO memento_project_message (project_id, message_id, included_by)
			VALUES (?, ?, 'label')
			ON CONFLICT(project_id, message_id) DO NOTHING
		`, p.ID, msgID)
		if err == nil {
			added++
		}
	}
	return added, nil
}

func AddMessagesByThread(ctx context.Context, db *sql.DB, projectSlug string, threadID int64) (int, error) {
	p, err := GetProjectBySlug(ctx, db, projectSlug)
	if err != nil {
		return 0, fmt.Errorf("lookup project: %w", err)
	}

	// Join on conversations (msgvault has conversation_id, which represents the thread)
	rows, err := db.QueryContext(ctx, `
		SELECT id
		FROM messages
		WHERE conversation_id = ?
	`, threadID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var msgIDs []int64
	for rows.Next() {
		var msgID int64
		if err := rows.Scan(&msgID); err != nil {
			return 0, err
		}
		msgIDs = append(msgIDs, msgID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	added := 0
	for _, msgID := range msgIDs {
		_, err = db.ExecContext(ctx, `
			INSERT INTO memento_project_message (project_id, message_id, included_by)
			VALUES (?, ?, 'thread')
			ON CONFLICT(project_id, message_id) DO NOTHING
		`, p.ID, msgID)
		if err == nil {
			added++
		}
	}
	return added, nil
}

func GetProjectBundle(ctx context.Context, db *sql.DB, projectID int64, detail string) ([]MessageBundleItem, error) {
	bodySelect := "COALESCE(mb.body_text, '') AS body_text"
	bodyJoin := "LEFT JOIN message_bodies mb ON mb.message_id = m.id"
	if detail == "index" {
		bodySelect = "'' AS body_text"
		bodyJoin = ""
	}

	query := fmt.Sprintf(`
		WITH account_emails AS (
			SELECT lower(identifier) AS email
			FROM sources
			WHERE identifier LIKE '%%@%%'
		),
		account_participants AS (
			SELECT id FROM participants WHERE lower(email_address) IN (SELECT email FROM account_emails)
		)
		SELECT
			m.id AS message_id,
			COALESCE(m.sent_at, '') AS date,
			COALESCE(mp.canonical_name, p.display_name, p.email_address, '') AS sender_canonical_name,
			COALESCE(p.email_address, '') AS sender_primary_email,
			COALESCE(m.subject, '') AS subject,
			COALESCE(m.snippet, '') AS snippet,
			%s,
			CASE
				WHEN m.sender_id IN (SELECT id FROM account_participants) THEN 'from_account'
				WHEN EXISTS (
					SELECT 1 FROM message_recipients mr
					WHERE mr.message_id = m.id
					  AND mr.participant_id IN (SELECT id FROM account_participants)
				) THEN 'to_account'
				ELSE 'other'
			END AS direction
		FROM memento_project_message mpm
		JOIN messages m ON m.id = mpm.message_id
		%s
		LEFT JOIN participants p ON p.id = m.sender_id
		LEFT JOIN memento_person_email mpe ON mpe.email_address = lower(p.email_address)
		LEFT JOIN memento_person mp ON mp.id = mpe.person_id
		WHERE mpm.project_id = ?
		ORDER BY m.sent_at ASC, m.id ASC`, bodySelect, bodyJoin)

	rows, err := db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bundle []MessageBundleItem
	for rows.Next() {
		var item MessageBundleItem
		if err := rows.Scan(
			&item.MessageID,
			&item.Date,
			&item.SenderCanonicalName,
			&item.SenderPrimaryEmail,
			&item.Subject,
			&item.Snippet,
			&item.BodyText,
			&item.Direction,
		); err != nil {
			return nil, err
		}

		// Truncate to first 2000 characters
		if len(item.BodyText) > 2000 {
			item.BodyText = item.BodyText[:2000] + " [...]"
		}
		bundle = append(bundle, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Budget limit: 150K tokens cap (~600K characters)
	// If the bundle is too big, drop the longest bodies first.
	const maxChars = 150000 * 4
	for {
		totalChars := 0
		longestIdx := -1
		longestLen := 0
		for i, item := range bundle {
			// Estimate char count for this message
			itemChars := len(item.BodyText) + len(item.Snippet) + len(item.Subject) + len(item.SenderCanonicalName) + 100
			totalChars += itemChars
			if len(item.BodyText) > longestLen {
				longestLen = len(item.BodyText)
				longestIdx = i
			}
		}

		if totalChars <= maxChars || longestIdx == -1 || longestLen <= 0 {
			break
		}

		// Cut the longest body text in half or to empty
		if longestLen > 500 {
			bundle[longestIdx].BodyText = bundle[longestIdx].BodyText[:longestLen/2] + " [...]"
		} else {
			bundle[longestIdx].BodyText = ""
		}
	}

	return bundle, nil
}

func PersonSlug(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			sb.WriteRune('-')
		}
	}
	res := sb.String()
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	return strings.Trim(res, "-")
}

func ShowProject(ctx context.Context, db *sql.DB, slug string) error {
	p, err := GetProjectBySlug(ctx, db, slug)
	if err != nil {
		return err
	}

	// Counts
	var messageCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM memento_project_message WHERE project_id = ?
	`, p.ID).Scan(&messageCount)
	if err != nil {
		return err
	}

	// Members
	rows, err := db.QueryContext(ctx, `
		SELECT pm.person_id, mp.canonical_name, mp.primary_email, pm.role
		FROM memento_project_member pm
		JOIN memento_person mp ON mp.id = pm.person_id
		WHERE pm.project_id = ?
	`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var members []ProjectMember
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.PersonID, &m.CanonicalName, &m.PrimaryEmail, &m.Role); err != nil {
			return err
		}
		m.Slug = PersonSlug(m.CanonicalName)
		members = append(members, m)
	}

	fmt.Printf("Project: %s (slug: %s)\n", p.Name, p.Slug)
	fmt.Printf("Status:  %s\n", p.Status)
	if p.StartedAt != nil {
		fmt.Printf("Started: %s\n", *p.StartedAt)
	}
	fmt.Printf("Message Count: %d\n", messageCount)
	fmt.Printf("Members:\n")
	for _, m := range members {
		fmt.Printf("  - %s (%s) as %q [id: %d]\n", m.CanonicalName, m.PrimaryEmail, m.Role, m.PersonID)
	}

	return nil
}
