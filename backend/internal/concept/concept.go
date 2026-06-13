package concept

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

type Concept struct {
	ID               int64    `json:"concept_id"`
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	ScopeDescription string   `json:"scope_description"`
	Status           string   `json:"status"`
	SeedKeywords     []string `json:"seed_keywords"`
	Note             string   `json:"note"`
}

type MessageBundleItem struct {
	MessageID           int64  `json:"message_id"`
	Date                string `json:"date"`
	SenderCanonicalName string `json:"sender_canonical_name"`
	SenderPrimaryEmail  string `json:"sender_primary_email"`
	Subject             string `json:"subject"`
	Snippet             string `json:"snippet"`
	BodyText            string `json:"body_text"`
	IsNewsletter        bool   `json:"is_newsletter"`
	NewsletterSlug      string `json:"newsletter_slug"`
	QueryTerm           string `json:"query_term"`
}

func CreateConcept(ctx context.Context, db *sql.DB, name, slug, scope string, keywords []string) (int64, error) {
	if err := slugs.ValidateEntitySlug(slug); err != nil {
		return 0, err
	}
	keywordsJSON, _ := json.Marshal(keywords)
	res, err := db.ExecContext(ctx, `
		INSERT INTO memento_concept (name, slug, scope_description, seed_keywords, status)
		VALUES (?, ?, ?, ?, 'active')
	`, name, slug, scope, string(keywordsJSON))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetConceptBySlug(ctx context.Context, db *sql.DB, slug string) (Concept, error) {
	var c Concept
	var seedRaw string
	err := db.QueryRowContext(ctx, `
		SELECT id, slug, name, scope_description, status, seed_keywords, note
		FROM memento_concept
		WHERE slug = ?
	`, slug).Scan(&c.ID, &c.Slug, &c.Name, &c.ScopeDescription, &c.Status, &seedRaw, &c.Note)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(seedRaw), &c.SeedKeywords); err != nil {
		c.SeedKeywords = []string{}
	}
	return c, nil
}

func ListConcepts(ctx context.Context, db *sql.DB) ([]Concept, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, slug, name, scope_description, status, seed_keywords, note
		FROM memento_concept
		WHERE dismissed_at IS NULL
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Concept
	for rows.Next() {
		var c Concept
		var seedRaw string
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.ScopeDescription, &c.Status, &seedRaw, &c.Note); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(seedRaw), &c.SeedKeywords); err != nil {
			c.SeedKeywords = []string{}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func AddMessageExplicit(ctx context.Context, db *sql.DB, conceptSlug string, messageID int64, addedBy, queryTerm string, score float64) error {
	c, err := GetConceptBySlug(ctx, db, conceptSlug)
	if err != nil {
		return fmt.Errorf("lookup concept: %w", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO memento_concept_message (concept_id, message_id, added_by, query_term, relevance_score)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(concept_id, message_id) DO UPDATE SET
			query_term = excluded.query_term,
			relevance_score = MAX(memento_concept_message.relevance_score, excluded.relevance_score)
	`, c.ID, messageID, addedBy, queryTerm, score)
	return err
}

func AddMessagesByThread(ctx context.Context, db *sql.DB, conceptSlug string, threadID int64, addedBy string) (int, error) {
	c, err := GetConceptBySlug(ctx, db, conceptSlug)
	if err != nil {
		return 0, fmt.Errorf("lookup concept: %w", err)
	}

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

	if strings.TrimSpace(addedBy) == "" {
		addedBy = "thread"
	}
	added := 0
	for _, msgID := range msgIDs {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO memento_concept_message (concept_id, message_id, added_by, query_term, relevance_score)
			VALUES (?, ?, ?, 'thread', 1.0)
			ON CONFLICT(concept_id, message_id) DO NOTHING
		`, c.ID, msgID, addedBy); err == nil {
			added++
		}
	}
	return added, nil
}

func RemoveMessage(ctx context.Context, db *sql.DB, conceptSlug string, messageID int64) error {
	c, err := GetConceptBySlug(ctx, db, conceptSlug)
	if err != nil {
		return fmt.Errorf("lookup concept: %w", err)
	}
	_, err = db.ExecContext(ctx, `
		DELETE FROM memento_concept_message
		WHERE concept_id = ? AND message_id = ?
	`, c.ID, messageID)
	return err
}

// msgvaultSearchResult mirrors project package — keep behaviour identical so
// the same hybrid/FTS fallback works.
type msgvaultSearchResult struct {
	Results []struct {
		ID int64 `json:"id"`
	} `json:"results"`
}

func runMsgvaultSearch(ctx context.Context, query string, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 200
	}
	if client, ok := msgvaultapi.FromEnv(); ok {
		if !msgvaultapi.RequiresFTSMode(query) {
			if ids, err := client.SearchIDs(ctx, query, "hybrid", limit); err == nil {
				return ids, nil
			}
		}
		if ids, err := client.SearchIDs(ctx, query, "fts", limit); err == nil {
			return ids, nil
		}
	}

	mode := "hybrid"
	if msgvaultapi.RequiresFTSMode(query) {
		mode = "fts"
	}
	args := []string{"search", query, "--mode", mode, "--json", "--limit", fmt.Sprintf("%d", limit)}
	cmd := exec.CommandContext(ctx, "msgvault", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Fallback to FTS mode.
		if mode == "fts" {
			return nil, fmt.Errorf("msgvault search %q failed: %w (stderr: %s)", query, err, stderr.String())
		}
		cmd = exec.CommandContext(ctx, "msgvault", "search", query, "--mode", "fts", "--json", "--limit", fmt.Sprintf("%d", limit))
		stdout.Reset()
		stderr.Reset()
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("msgvault search %q failed: %w (stderr: %s)", query, err, stderr.String())
		}
	}
	raw := stdout.Bytes()
	if idx := bytes.IndexAny(raw, "{["); idx > 0 {
		raw = raw[idx:]
	}
	var hybrid msgvaultSearchResult
	if err := json.Unmarshal(raw, &hybrid); err == nil && len(hybrid.Results) > 0 {
		ids := make([]int64, 0, len(hybrid.Results))
		for _, r := range hybrid.Results {
			ids = append(ids, r.ID)
		}
		return ids, nil
	}
	var fts []struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &fts); err == nil {
		ids := make([]int64, 0, len(fts))
		for _, r := range fts {
			ids = append(ids, r.ID)
		}
		return ids, nil
	}
	return nil, fmt.Errorf("unparseable msgvault search output: %s", strings.TrimSpace(stdout.String()))
}

// AddMessagesBySearch runs a msgvault FTS/hybrid search for each query using
// the HTTP API when configured, with CLI fallback, and inserts the result ids
// into memento_concept_message. Multiple queries are supported so a concept can
// be backed by a small set of OR'd terms.
func AddMessagesBySearch(ctx context.Context, db *sql.DB, conceptSlug string, queries []string, limit int) (int, error) {
	if len(queries) == 0 {
		return 0, fmt.Errorf("no search queries supplied")
	}
	added := 0
	for _, q := range queries {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		ids, err := runMsgvaultSearch(ctx, q, limit)
		if err != nil {
			return added, fmt.Errorf("search %q: %w", q, err)
		}
		for _, id := range ids {
			if err := AddMessageExplicit(ctx, db, conceptSlug, id, "search", q, 1.0); err == nil {
				added++
			}
		}
	}
	return added, nil
}

// GetConceptBundle returns the chronologically-sorted message bundle for a
// concept. Mirrors project.GetProjectBundle but joins from
// memento_concept_message and additionally surfaces newsletter source info
// (so source-map summaries can group by newsletter slug).
func GetConceptBundle(ctx context.Context, db *sql.DB, conceptID int64, detail string) ([]MessageBundleItem, error) {
	bodySelect := "COALESCE(mb.body_text, '') AS body_text"
	bodyJoin := "LEFT JOIN message_bodies mb ON mb.message_id = m.id"
	if detail == "index" {
		bodySelect = "'' AS body_text"
		bodyJoin = ""
	}

	query := fmt.Sprintf(`
		SELECT
			m.id AS message_id,
			COALESCE(m.sent_at, '') AS date,
			COALESCE(mp.canonical_name, p.display_name, p.email_address, '') AS sender_canonical_name,
			COALESCE(p.email_address, '') AS sender_primary_email,
			COALESCE(m.subject, '') AS subject,
			COALESCE(m.snippet, '') AS snippet,
			%s,
			CASE WHEN mns.id IS NOT NULL THEN 1 ELSE 0 END AS is_newsletter,
			COALESCE(mns.slug, '') AS newsletter_slug,
			COALESCE(mcm.query_term, '') AS query_term
		FROM memento_concept_message mcm
		JOIN messages m ON m.id = mcm.message_id
		%s
		LEFT JOIN participants p ON p.id = m.sender_id
		LEFT JOIN memento_person_email mpe ON mpe.email_address = lower(p.email_address)
		LEFT JOIN memento_person mp ON mp.id = mpe.person_id
		LEFT JOIN memento_newsletter_source mns ON lower(mns.sender_email) = lower(p.email_address)
		WHERE mcm.concept_id = ?
		ORDER BY m.sent_at ASC, m.id ASC`, bodySelect, bodyJoin)

	rows, err := db.QueryContext(ctx, query, conceptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bundle []MessageBundleItem
	for rows.Next() {
		var item MessageBundleItem
		var isNewsletter int
		if err := rows.Scan(
			&item.MessageID,
			&item.Date,
			&item.SenderCanonicalName,
			&item.SenderPrimaryEmail,
			&item.Subject,
			&item.Snippet,
			&item.BodyText,
			&isNewsletter,
			&item.NewsletterSlug,
			&item.QueryTerm,
		); err != nil {
			return nil, err
		}
		item.IsNewsletter = isNewsletter != 0
		if len(item.BodyText) > 2000 {
			item.BodyText = item.BodyText[:2000] + " [...]"
		}
		bundle = append(bundle, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return truncateBundleToBudget(bundle), nil
}

// truncateBundleToBudget keeps the same 150K-token budget approach the
// project package uses — drop the longest bodies first when oversized.
func truncateBundleToBudget(bundle []MessageBundleItem) []MessageBundleItem {
	const maxChars = 150000 * 4
	for {
		total := 0
		longestIdx := -1
		longestLen := 0
		for i, item := range bundle {
			total += len(item.BodyText) + len(item.Snippet) + len(item.Subject) + len(item.SenderCanonicalName) + 100
			if len(item.BodyText) > longestLen {
				longestLen = len(item.BodyText)
				longestIdx = i
			}
		}
		if total <= maxChars || longestIdx == -1 || longestLen <= 0 {
			return bundle
		}
		if longestLen > 500 {
			bundle[longestIdx].BodyText = bundle[longestIdx].BodyText[:longestLen/2] + " [...]"
		} else {
			bundle[longestIdx].BodyText = ""
		}
	}
}

func ShowConcept(ctx context.Context, db *sql.DB, slug string) error {
	c, err := GetConceptBySlug(ctx, db, slug)
	if err != nil {
		return err
	}
	var msgCount int64
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_concept_message WHERE concept_id = ?`, c.ID).Scan(&msgCount)
	fmt.Printf("Concept #%d: %s\n", c.ID, c.Name)
	fmt.Printf("  Slug:        %s\n", c.Slug)
	fmt.Printf("  Status:      %s\n", c.Status)
	fmt.Printf("  Scope:       %s\n", c.ScopeDescription)
	if len(c.SeedKeywords) > 0 {
		fmt.Printf("  Seed terms:  %s\n", strings.Join(c.SeedKeywords, ", "))
	}
	fmt.Printf("  Messages:    %d\n", msgCount)
	return nil
}
