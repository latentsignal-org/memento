package newsletter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Source struct {
	ID                   int64   `json:"id"`
	SenderEmail          string  `json:"sender_email"`
	DisplayName          string  `json:"display_name"`
	Domain               string  `json:"domain"`
	Slug                 string  `json:"slug"`
	FirstSeen            *string `json:"first_seen,omitempty"`
	LastSeen             *string `json:"last_seen,omitempty"`
	MessageCount         int64   `json:"message_count"`
	UnsubscribeCount     int64   `json:"unsubscribe_count"`
	ClassificationReason string  `json:"classification_reason"`
}

type DetectReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	Sources     []Source  `json:"sources"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	SentAt    string `json:"sent_at"`
	Subject   string `json:"subject"`
	Snippet   string `json:"snippet"`
	BodyText  string `json:"body_text,omitempty"`
}

type Theme struct {
	Theme            string  `json:"theme"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

type NotableRecent struct {
	Headline         string  `json:"headline"`
	Date             string  `json:"date"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

type Narrative struct {
	CoverageSummary string          `json:"coverage_summary"`
	RecurringThemes []Theme         `json:"recurring_themes"`
	NotableRecent   []NotableRecent `json:"notable_recent"`
}

type PageReport struct {
	GeneratedAt string    `json:"generated_at"`
	Source      Source    `json:"source"`
	Narrative   Narrative `json:"narrative"`
	Timeline    []Message `json:"timeline"`
}

type IndexReport struct {
	GeneratedAt string        `json:"generated_at"`
	Sources     []IndexSource `json:"sources"`
}

type IndexSource struct {
	ID                   int64    `json:"id"`
	Slug                 string   `json:"slug"`
	DisplayName          string   `json:"display_name"`
	SenderEmail          string   `json:"sender_email"`
	Domain               string   `json:"domain"`
	MessageCount         int64    `json:"message_count"`
	UnsubscribeCount     int64    `json:"unsubscribe_count"`
	FirstSeen            *string  `json:"first_seen,omitempty"`
	LastSeen             *string  `json:"last_seen,omitempty"`
	ClassificationReason string   `json:"classification_reason"`
	RecentSubjects       []string `json:"recent_subjects"`
}

func DetectSources(ctx context.Context, db *sql.DB, minMessages int) (DetectReport, error) {
	if minMessages <= 0 {
		minMessages = 20
	}
	rows, err := db.QueryContext(ctx, `
		WITH account_emails AS (
			-- Account-owned addresses (the user themselves). The user's outbound
			-- mail often quotes "unsubscribe" footers from forwarded newsletters,
			-- which would otherwise self-classify them as a newsletter source.
			SELECT lower(identifier) AS email
			FROM sources
			WHERE identifier LIKE '%@%'
		),
		human_emails AS (
			-- Any address that resolves to a Person classified as a meaningful
			-- human contact by the People classifier. Real humans (e.g. someone
			-- who forwards newsletters) can otherwise slip through the body-text
			-- "unsubscribe" heuristic. A sender is either a person or a
			-- newsletter, not both.
			SELECT lower(pe.email_address) AS email
			FROM memento_person_email pe
			JOIN memento_people_candidates pc ON pc.person_id = pe.person_id
			WHERE pc.classification IN ('candidate', 'candidate_inbound_only')
		),
		sender_stats AS (
			SELECT
				lower(p.email_address) AS sender_email,
				COALESCE(MAX(NULLIF(p.display_name, '')), '') AS display_name,
				COALESCE(MAX(NULLIF(p.domain, '')), '') AS domain,
				COUNT(*) AS message_count,
				SUM(CASE WHEN lower(COALESCE(b.body_text, '')) LIKE '%unsubscribe%' THEN 1 ELSE 0 END) AS unsubscribe_count,
				MIN(m.sent_at) AS first_seen,
				MAX(m.sent_at) AS last_seen
			FROM messages m
			JOIN participants p ON p.id = m.sender_id
			LEFT JOIN message_bodies b ON b.message_id = m.id
			WHERE p.email_address IS NOT NULL
			  AND lower(p.email_address) NOT IN (SELECT email FROM account_emails)
			  AND lower(p.email_address) NOT IN (SELECT email FROM human_emails)
			GROUP BY lower(p.email_address)
		)
		SELECT sender_email, display_name, domain, message_count, unsubscribe_count, first_seen, last_seen
		FROM sender_stats
		WHERE message_count >= ?
		ORDER BY unsubscribe_count DESC, message_count DESC
	`, minMessages)
	if err != nil {
		return DetectReport{}, err
	}
	defer rows.Close()

	var sources []Source
	usedSlugs := map[string]int{}
	for rows.Next() {
		var source Source
		var first, last sql.NullString
		if err := rows.Scan(
			&source.SenderEmail,
			&source.DisplayName,
			&source.Domain,
			&source.MessageCount,
			&source.UnsubscribeCount,
			&first,
			&last,
		); err != nil {
			return DetectReport{}, err
		}
		reason, ok := classifySource(source, int64(minMessages))
		if !ok {
			continue
		}
		if source.DisplayName == "" {
			source.DisplayName = displayNameFromEmail(source.SenderEmail)
		}
		source.ClassificationReason = reason
		source.FirstSeen = nullStringPtr(first)
		source.LastSeen = nullStringPtr(last)
		source.Slug = uniqueSlug(slugify(source.DisplayName), source.SenderEmail, usedSlugs)
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return DetectReport{}, err
	}
	return DetectReport{GeneratedAt: time.Now().UTC(), Sources: sources}, nil
}

func PersistSources(ctx context.Context, db *sql.DB, report DetectReport) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Sweep stale rows: anything currently in memento_newsletter_source that
	// the new detection no longer classifies as a newsletter (e.g. the user's
	// own address now correctly excluded) is removed. Narratives referencing
	// removed sources cascade via the FK. This mirrors the candidate-report
	// snapshot semantics.
	keep := make(map[string]bool, len(report.Sources))
	for _, s := range report.Sources {
		keep[s.SenderEmail] = true
	}
	// Only sweep undismissed rows — dismissed rows are user decisions and
	// survive re-detection indefinitely.
	staleRows, err := tx.QueryContext(ctx, `SELECT sender_email FROM memento_newsletter_source WHERE dismissed_at IS NULL`)
	if err != nil {
		return err
	}
	var staleEmails []string
	for staleRows.Next() {
		var email string
		if err := staleRows.Scan(&email); err != nil {
			staleRows.Close()
			return err
		}
		if !keep[email] {
			staleEmails = append(staleEmails, email)
		}
	}
	staleRows.Close()
	for _, email := range staleEmails {
		if _, err := tx.ExecContext(ctx, `DELETE FROM memento_newsletter_source WHERE sender_email = ?`, email); err != nil {
			return err
		}
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_newsletter_source (
			sender_email, display_name, domain, slug, first_seen, last_seen,
			message_count, unsubscribe_count, classification_reason, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(sender_email) DO UPDATE SET
			display_name = excluded.display_name,
			domain = excluded.domain,
			first_seen = excluded.first_seen,
			last_seen = excluded.last_seen,
			message_count = excluded.message_count,
			unsubscribe_count = excluded.unsubscribe_count,
			classification_reason = excluded.classification_reason,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range report.Sources {
		if _, err := stmt.ExecContext(ctx,
			s.SenderEmail,
			s.DisplayName,
			s.Domain,
			s.Slug,
			stringOrNil(s.FirstSeen),
			stringOrNil(s.LastSeen),
			s.MessageCount,
			s.UnsubscribeCount,
			s.ClassificationReason,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func SourceBySlug(ctx context.Context, db *sql.DB, slug string) (Source, error) {
	var s Source
	var first, last sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, sender_email, display_name, domain, slug, first_seen, last_seen,
		       message_count, unsubscribe_count, classification_reason
		FROM memento_newsletter_source
		WHERE slug = ?
	`, slug).Scan(
		&s.ID,
		&s.SenderEmail,
		&s.DisplayName,
		&s.Domain,
		&s.Slug,
		&first,
		&last,
		&s.MessageCount,
		&s.UnsubscribeCount,
		&s.ClassificationReason,
	)
	if err != nil {
		return s, err
	}
	s.FirstSeen = nullStringPtr(first)
	s.LastSeen = nullStringPtr(last)
	return s, nil
}

func ListSources(ctx context.Context, db *sql.DB) ([]Source, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, sender_email, display_name, domain, slug, first_seen, last_seen,
		       message_count, unsubscribe_count, classification_reason
		FROM memento_newsletter_source
		WHERE dismissed_at IS NULL
		ORDER BY message_count DESC, display_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Source
	for rows.Next() {
		var s Source
		var first, last sql.NullString
		if err := rows.Scan(
			&s.ID,
			&s.SenderEmail,
			&s.DisplayName,
			&s.Domain,
			&s.Slug,
			&first,
			&last,
			&s.MessageCount,
			&s.UnsubscribeCount,
			&s.ClassificationReason,
		); err != nil {
			return nil, err
		}
		s.FirstSeen = nullStringPtr(first)
		s.LastSeen = nullStringPtr(last)
		out = append(out, s)
	}
	return out, rows.Err()
}

// DismissSource sets dismissed_at on the newsletter source with the given slug.
// Returns sql.ErrNoRows if the slug doesn't exist.
func DismissSource(ctx context.Context, db *sql.DB, slug string) error {
	res, err := db.ExecContext(ctx, `
		UPDATE memento_newsletter_source SET dismissed_at = CURRENT_TIMESTAMP WHERE slug = ?
	`, slug)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func RecentMessages(ctx context.Context, db *sql.DB, source Source, limit int, bodyLimit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
		SELECT m.id, COALESCE(m.sent_at, ''), COALESCE(m.subject, ''), COALESCE(m.snippet, ''),
		       COALESCE(b.body_text, '')
		FROM messages m
		JOIN participants p ON p.id = m.sender_id
		LEFT JOIN message_bodies b ON b.message_id = m.id
		WHERE lower(p.email_address) = lower(?)
		ORDER BY m.sent_at DESC, m.id DESC
		LIMIT ?
	`, source.SenderEmail, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.MessageID, &m.SentAt, &m.Subject, &m.Snippet, &m.BodyText); err != nil {
			return nil, err
		}
		m.SentAt = parseDBTime(m.SentAt)
		m.Subject = cleanText(m.Subject)
		m.Snippet = cleanText(m.Snippet)
		m.BodyText = truncate(cleanText(m.BodyText), bodyLimit)
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

func SaveNarrative(ctx context.Context, db *sql.DB, sourceID int64, narrative Narrative) error {
	if err := saveSection(ctx, db, sourceID, "coverage_summary", narrative.CoverageSummary, extractCitations(narrative.CoverageSummary)); err != nil {
		return err
	}
	themesJSON, _ := json.Marshal(narrative.RecurringThemes)
	var themeIDs []int64
	for _, theme := range narrative.RecurringThemes {
		themeIDs = append(themeIDs, theme.SourceMessageIDs...)
	}
	if err := saveSection(ctx, db, sourceID, "recurring_themes", string(themesJSON), themeIDs); err != nil {
		return err
	}
	recentJSON, _ := json.Marshal(narrative.NotableRecent)
	var recentIDs []int64
	for _, item := range narrative.NotableRecent {
		recentIDs = append(recentIDs, item.SourceMessageIDs...)
	}
	return saveSection(ctx, db, sourceID, "notable_recent", string(recentJSON), recentIDs)
}

func LoadNarrative(ctx context.Context, db *sql.DB, sourceID int64) (Narrative, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT section, content
		FROM memento_newsletter_narrative
		WHERE source_id = ?
	`, sourceID)
	if err != nil {
		return Narrative{}, err
	}
	defer rows.Close()

	var narrative Narrative
	for rows.Next() {
		var section, content string
		if err := rows.Scan(&section, &content); err != nil {
			return Narrative{}, err
		}
		switch section {
		case "coverage_summary":
			narrative.CoverageSummary = content
		case "recurring_themes":
			_ = json.Unmarshal([]byte(content), &narrative.RecurringThemes)
		case "notable_recent":
			_ = json.Unmarshal([]byte(content), &narrative.NotableRecent)
		}
	}
	return narrative, rows.Err()
}

// BuildPage assembles a single newsletter PageReport (source + narrative +
// timeline) without writing to disk. Shared by ExportPages and the HTTP
// /api/newsletters/:slug handler.
func BuildPage(ctx context.Context, db *sql.DB, slug string, timelineLimit int) (PageReport, error) {
	source, err := SourceBySlug(ctx, db, slug)
	if err != nil {
		return PageReport{}, err
	}
	timeline, err := RecentMessages(ctx, db, source, timelineLimit, 0)
	if err != nil {
		return PageReport{}, fmt.Errorf("timeline for %s: %w", source.Slug, err)
	}
	narrative, err := LoadNarrative(ctx, db, source.ID)
	if err != nil {
		return PageReport{}, fmt.Errorf("narrative for %s: %w", source.Slug, err)
	}
	return PageReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      source,
		Narrative:   narrative,
		Timeline:    timeline,
	}, nil
}

// BuildIndex assembles the newsletter index report (one IndexSource per
// detected source). Shared by ExportPages and the HTTP /api/newsletters
// handler. timelineLimit controls how many recent subjects per source.
func BuildIndex(ctx context.Context, db *sql.DB, timelineLimit int) (IndexReport, error) {
	sources, err := ListSources(ctx, db)
	if err != nil {
		return IndexReport{}, err
	}
	index := IndexReport{GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, source := range sources {
		timeline, err := RecentMessages(ctx, db, source, timelineLimit, 0)
		if err != nil {
			return IndexReport{}, fmt.Errorf("timeline for %s: %w", source.Slug, err)
		}
		var subjects []string
		for _, msg := range timeline {
			if msg.Subject != "" {
				subjects = append(subjects, msg.Subject)
			}
			if len(subjects) >= 3 {
				break
			}
		}
		index.Sources = append(index.Sources, IndexSource{
			ID:                   source.ID,
			Slug:                 source.Slug,
			DisplayName:          source.DisplayName,
			SenderEmail:          source.SenderEmail,
			Domain:               source.Domain,
			MessageCount:         source.MessageCount,
			UnsubscribeCount:     source.UnsubscribeCount,
			FirstSeen:            source.FirstSeen,
			LastSeen:             source.LastSeen,
			ClassificationReason: source.ClassificationReason,
			RecentSubjects:       subjects,
		})
	}
	return index, nil
}

func ExportPages(ctx context.Context, db *sql.DB, outDir string, timelineLimit int) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	index, err := BuildIndex(ctx, db, timelineLimit)
	if err != nil {
		return err
	}
	for _, src := range index.Sources {
		page, err := BuildPage(ctx, db, src.Slug, timelineLimit)
		if err != nil {
			return err
		}
		if err := writeJSON(filepath.Join(outDir, src.Slug+".json"), page); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(outDir, "index.json"), index)
}

func saveSection(ctx context.Context, db *sql.DB, sourceID int64, section string, content string, msgIDs []int64) error {
	var editedBy string
	err := db.QueryRowContext(ctx, `
		SELECT edited_by
		FROM memento_newsletter_narrative
		WHERE source_id = ? AND section = ?
	`, sourceID, section).Scan(&editedBy)
	if err == nil && editedBy == "user" {
		fmt.Printf("Skipping newsletter section %q because it has been edited by a user.\n", section)
		return nil
	}

	sourceMsgIDs, _ := json.Marshal(uniqueInt64s(msgIDs))
	_, err = db.ExecContext(ctx, `
		INSERT INTO memento_newsletter_narrative (source_id, section, content, source_message_ids, generated_at, edited_by)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')
		ON CONFLICT(source_id, section) DO UPDATE SET
			content = excluded.content,
			source_message_ids = excluded.source_message_ids,
			generated_at = CURRENT_TIMESTAMP,
			edited_by = 'llm'
	`, sourceID, section, content, string(sourceMsgIDs))
	return err
}

func writeJSON(path string, v any) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

func classifySource(s Source, minMessages int64) (string, bool) {
	email := strings.ToLower(s.SenderEmail)
	name := strings.ToLower(s.DisplayName)
	domain := strings.ToLower(s.Domain)
	local := strings.Split(email, "@")[0]

	switch {
	case knownNewsletterDomain(domain):
		return "known newsletter/broadcast domain", true
	case strings.Contains(local, "newsletter") || strings.Contains(local, "digest") || strings.Contains(local, "weekly"):
		return "newsletter-like sender address", true
	case strings.Contains(name, "newsletter") || strings.Contains(name, "digest") || strings.Contains(name, "weekly"):
		return "newsletter-like display name", true
	case s.UnsubscribeCount >= minMessages:
		return "unsubscribe link in message body", true
	case s.UnsubscribeCount >= 10 && s.MessageCount >= 30:
		return "recurring sender with unsubscribe links", true
	default:
		return "", false
	}
}

func knownNewsletterDomain(domain string) bool {
	domains := []string{
		"substack.com", "ghost.io", "buttondown.email", "beehiiv.com", "convertkit.com",
		"mailchimp.com", "cooperpress.com", "morningbrew.com", "medium.com", "realpython.com",
		"pycoders.com", "smashingmagazine.com", "frontendmasters.com", "thisweekinreact.com",
		"ben-evans.com", "thegeneralist.com", "aisecret.us", "smol.ai", "sidebar.io",
	}
	for _, known := range domains {
		if domain == known || strings.HasSuffix(domain, "."+known) {
			return true
		}
	}
	return false
}

func displayNameFromEmail(email string) string {
	local := strings.Split(email, "@")[0]
	local = strings.ReplaceAll(local, ".", " ")
	local = strings.ReplaceAll(local, "-", " ")
	local = strings.ReplaceAll(local, "_", " ")
	words := strings.Fields(local)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	if len(words) == 0 {
		return email
	}
	return strings.Join(words, " ")
}

func uniqueSlug(base string, email string, used map[string]int) string {
	if base == "" {
		base = slugify(displayNameFromEmail(email))
	}
	if base == "" {
		base = "newsletter"
	}
	n := used[base]
	used[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n+1)
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func cleanText(value string) string {
	value = tagRE.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.ToValidUTF8(value, "")
	return strings.Join(strings.Fields(value), " ")
}

func truncate(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return strings.TrimSpace(value[:max]) + "..."
}

func parseDBTime(value string) string {
	if value == "" {
		return ""
	}
	layouts := []string{
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05+00:00",
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return value
}

func uniqueInt64s(values []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func stringOrNil(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

var tagRE = regexp.MustCompile(`<[^>]+>`)
