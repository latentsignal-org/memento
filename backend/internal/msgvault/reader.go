package msgvault

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type Reader struct {
	path string
	db   *sql.DB
}

func OpenReader(path string) (*Reader, error) {
	dsn := "file:" + url.PathEscape(path) + "?mode=ro&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Reader{path: path, db: db}, nil
}

func (r *Reader) Close() error {
	return r.db.Close()
}

func (r *Reader) DB() *sql.DB {
	return r.db
}

func (r *Reader) Path() string {
	return r.path
}

func (r *Reader) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	counts := []struct {
		table string
		dst   *int64
	}{
		{"messages", &stats.Messages},
		{"conversations", &stats.Conversations},
		{"participants", &stats.Participants},
		{"message_recipients", &stats.MessageRecipients},
		{"labels", &stats.Labels},
		{"attachments", &stats.Attachments},
		{"sources", &stats.Sources},
	}

	for _, count := range counts {
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s", count.table)
		if err := r.db.QueryRowContext(ctx, query).Scan(count.dst); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func (r *Reader) HasOutboundMessages(ctx context.Context) (bool, error) {
	var count int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM messages m
		JOIN participants p ON p.id = m.sender_id
		JOIN sources s ON lower(s.identifier) = lower(p.email_address)
		WHERE s.identifier LIKE '%@%'
	`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Reader) Schema(ctx context.Context) ([]Table, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table'
		  AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, tableName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var tables []Table
	for _, tableName := range tableNames {
		table := Table{Name: tableName}
		columns, err := r.columns(ctx, table.Name)
		if err != nil {
			return nil, err
		}
		table.Columns = columns
		tables = append(tables, table)
	}
	return tables, nil
}

func (r *Reader) ListMessages(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			id,
			conversation_id,
			source_id,
			source_message_id,
			message_type,
			sent_at,
			sender_id,
			COALESCE(is_from_me, 0),
			subject,
			snippet,
			COALESCE(has_attachments, 0),
			COALESCE(attachment_count, 0)
		FROM messages
		ORDER BY sent_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var message Message
		var sourceMessageID, sentAt, subject, snippet sql.NullString
		var senderID sql.NullInt64
		var isFromMe, hasAttachments int64
		if err := rows.Scan(
			&message.ID,
			&message.ConversationID,
			&message.SourceID,
			&sourceMessageID,
			&message.MessageType,
			&sentAt,
			&senderID,
			&isFromMe,
			&subject,
			&snippet,
			&hasAttachments,
			&message.AttachmentCount,
		); err != nil {
			return nil, err
		}
		message.SourceMessageID = nullStringPtr(sourceMessageID)
		message.SentAt = nullStringPtr(sentAt)
		message.SenderID = nullInt64Ptr(senderID)
		message.IsFromMe = isFromMe != 0
		message.Subject = nullStringPtr(subject)
		message.Snippet = nullStringPtr(snippet)
		message.HasAttachments = hasAttachments != 0
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (r *Reader) ListParticipants(ctx context.Context, limit int) ([]Participant, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, email_address, phone_number, display_name, domain, canonical_id
		FROM participants
		ORDER BY id
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var participants []Participant
	for rows.Next() {
		var participant Participant
		var email, phone, displayName, domain, canonicalID sql.NullString
		if err := rows.Scan(
			&participant.ID,
			&email,
			&phone,
			&displayName,
			&domain,
			&canonicalID,
		); err != nil {
			return nil, err
		}
		participant.EmailAddress = nullStringPtr(email)
		participant.PhoneNumber = nullStringPtr(phone)
		participant.DisplayName = nullStringPtr(displayName)
		participant.Domain = nullStringPtr(domain)
		participant.CanonicalID = nullStringPtr(canonicalID)
		participants = append(participants, participant)
	}
	return participants, rows.Err()
}

func (r *Reader) ListRecipients(ctx context.Context, limit int) ([]Recipient, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, message_id, participant_id, recipient_type, display_name
		FROM message_recipients
		ORDER BY message_id DESC, id
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recipients []Recipient
	for rows.Next() {
		var recipient Recipient
		var displayName sql.NullString
		if err := rows.Scan(
			&recipient.ID,
			&recipient.MessageID,
			&recipient.ParticipantID,
			&recipient.RecipientType,
			&displayName,
		); err != nil {
			return nil, err
		}
		recipient.DisplayName = nullStringPtr(displayName)
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}

func (r *Reader) ListConversations(ctx context.Context, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			id,
			source_id,
			source_conversation_id,
			conversation_type,
			title,
			COALESCE(participant_count, 0),
			COALESCE(message_count, 0),
			last_message_at
		FROM conversations
		ORDER BY last_message_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []Conversation
	for rows.Next() {
		var conversation Conversation
		var sourceConversationID, title, lastMessageAt sql.NullString
		if err := rows.Scan(
			&conversation.ID,
			&conversation.SourceID,
			&sourceConversationID,
			&conversation.ConversationType,
			&title,
			&conversation.ParticipantCount,
			&conversation.MessageCount,
			&lastMessageAt,
		); err != nil {
			return nil, err
		}
		conversation.SourceIDExternal = nullStringPtr(sourceConversationID)
		conversation.Title = nullStringPtr(title)
		conversation.LastMessageAt = nullStringPtr(lastMessageAt)
		conversations = append(conversations, conversation)
	}
	return conversations, rows.Err()
}

func (r *Reader) ListLabels(ctx context.Context, limit int) ([]Label, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, source_id, source_label_id, name, label_type, color
		FROM labels
		ORDER BY name
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []Label
	for rows.Next() {
		var label Label
		var sourceID sql.NullInt64
		var sourceLabelID, labelType, color sql.NullString
		if err := rows.Scan(
			&label.ID,
			&sourceID,
			&sourceLabelID,
			&label.Name,
			&labelType,
			&color,
		); err != nil {
			return nil, err
		}
		label.SourceID = nullInt64Ptr(sourceID)
		label.SourceLabelID = nullStringPtr(sourceLabelID)
		label.LabelType = nullStringPtr(labelType)
		label.Color = nullStringPtr(color)
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (r *Reader) ListAttachments(ctx context.Context, limit int) ([]Attachment, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, message_id, filename, mime_type, size, content_hash, storage_path
		FROM attachments
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attachments []Attachment
	for rows.Next() {
		var attachment Attachment
		var filename, mimeType, contentHash sql.NullString
		var size sql.NullInt64
		if err := rows.Scan(
			&attachment.ID,
			&attachment.MessageID,
			&filename,
			&mimeType,
			&size,
			&contentHash,
			&attachment.StoragePath,
		); err != nil {
			return nil, err
		}
		attachment.Filename = nullStringPtr(filename)
		attachment.MimeType = nullStringPtr(mimeType)
		attachment.Size = nullInt64Ptr(size)
		attachment.ContentHash = nullStringPtr(contentHash)
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func (r *Reader) columns(ctx context.Context, table string) ([]Column, error) {
	rows, err := r.db.QueryContext(ctx, "PRAGMA table_info("+quoteIdent(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []Column
	for rows.Next() {
		var cid int64
		var col Column
		var notNull int64
		var defaultValue sql.NullString
		var pk int64
		if err := rows.Scan(&cid, &col.Name, &col.Type, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		col.Nullable = notNull == 0
		col.PK = pk != 0
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

// PeopleCandidateRows returns one row per memento_person, aggregating
// involvement counts across every email mapped to that person. Requires the
// resolver to have been run — participants that are not yet mapped to any
// person are silently excluded (the alternative would be to dump them into
// synthetic person ids, which would defeat the canonicalization).
func (r *Reader) PeopleCandidateRows(ctx context.Context, limit int, includeExcluded bool) ([]PeopleCandidateRow, error) {
	if limit <= 0 {
		limit = 25
	}

	rows, err := r.db.QueryContext(ctx, `
		WITH account_emails AS (
			SELECT lower(identifier) AS email
			FROM sources
			WHERE identifier LIKE '%@%'
		),
		account_participants AS (
			SELECT p.id
			FROM participants p
			JOIN account_emails ae ON ae.email = lower(p.email_address)
		),
		participant_to_person AS (
			SELECT p.id AS participant_id, pe.person_id
			FROM participants p
			JOIN memento_person_email pe ON pe.email_address = lower(p.email_address)
		),
		involvement AS (
			SELECT
				ptp.person_id,
				m.id AS message_id,
				m.sent_at,
				'from_contact' AS role
			FROM messages m
			JOIN participant_to_person ptp ON ptp.participant_id = m.sender_id
			WHERE m.sender_id IS NOT NULL
			  AND m.sender_id NOT IN (SELECT id FROM account_participants)

			UNION ALL

			SELECT
				ptp.person_id,
				m.id AS message_id,
				m.sent_at,
				'to_contact' AS role
			FROM message_recipients mr
			JOIN messages m ON m.id = mr.message_id
			JOIN participant_to_person ptp ON ptp.participant_id = mr.participant_id
			WHERE m.sender_id IN (SELECT id FROM account_participants)
			  AND mr.recipient_type IN ('to', 'cc', 'bcc', 'mention')
		),
		rolled AS (
			SELECT
				i.person_id,
				COUNT(DISTINCT i.message_id) AS total_messages,
				SUM(CASE WHEN i.role = 'from_contact' THEN 1 ELSE 0 END) AS from_contact_count,
				SUM(CASE WHEN i.role = 'to_contact' THEN 1 ELSE 0 END) AS to_contact_count,
				MIN(i.sent_at) AS first_message_at,
				MAX(i.sent_at) AS last_message_at
			FROM involvement i
			GROUP BY i.person_id
		),
		email_counts AS (
			SELECT person_id, COUNT(*) AS n FROM memento_person_email GROUP BY person_id
		)
		SELECT
			mp.id AS person_id,
			mp.canonical_name,
			mp.primary_email,
			-- domain comes from the primary email's domain (best-effort)
			COALESCE(
				CASE WHEN instr(mp.primary_email, '@') > 0
					 THEN substr(mp.primary_email, instr(mp.primary_email, '@') + 1)
					 ELSE '' END, '') AS domain,
			COALESCE(ec.n, 0) AS email_count,
			r.total_messages,
			r.from_contact_count,
			r.to_contact_count,
			r.first_message_at,
			r.last_message_at,
			(
				SELECT group_concat(message_id)
				FROM (
					SELECT i2.message_id
					FROM involvement i2
					WHERE i2.person_id = mp.id
					GROUP BY i2.message_id
					ORDER BY MAX(i2.sent_at) DESC
					LIMIT 3
				)
			) AS sample_message_ids
		FROM rolled r
		JOIN memento_person mp ON mp.id = r.person_id
		LEFT JOIN email_counts ec ON ec.person_id = mp.id
		WHERE NOT EXISTS (
			SELECT 1 FROM memento_person_email mpe
			JOIN account_emails ae ON ae.email = lower(mpe.email_address)
			WHERE mpe.person_id = mp.id
		)
		ORDER BY r.total_messages DESC, r.last_message_at DESC
		LIMIT ?
	`, candidateFetchLimit(limit, includeExcluded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []PeopleCandidateRow
	for rows.Next() {
		var row PeopleCandidateRow
		var first, last, samples sql.NullString
		if err := rows.Scan(
			&row.PersonID,
			&row.CanonicalName,
			&row.PrimaryEmail,
			&row.Domain,
			&row.EmailCount,
			&row.TotalMessages,
			&row.FromContactCount,
			&row.ToContactCount,
			&first,
			&last,
			&samples,
		); err != nil {
			return nil, err
		}
		row.FirstMessageAt = nullStringPtr(first)
		row.LastMessageAt = nullStringPtr(last)
		row.SampleMessageIDsRaw = nullStringPtr(samples)
		row.SampleMessageIDs = parseMessageIDs(samples.String)
		candidates = append(candidates, row)
	}
	return candidates, rows.Err()
}

// candidateFetchLimit over-fetches from SQL so the Go-side classifier can drop
// excluded rows and still return `limit` real candidates. Phase-0 shortcut:
// fine for a 25k-message demo vault, not fine at the 500k-message target —
// at that scale we need server-side exclusion or pagination, not a 10k cap.
// ListParticipantsForResolution returns every participant with a non-empty
// email address along with a rough involvement count (as sender + as
// recipient). The count is used as a tie-break / primary-email signal during
// person resolution; it is not the same as the candidate-report's signed
// from/to balance.
func (r *Reader) ListParticipantsForResolution(ctx context.Context) ([]ParticipantForResolution, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH sender_counts AS (
			SELECT sender_id AS participant_id, COUNT(*) AS n
			FROM messages
			WHERE sender_id IS NOT NULL
			GROUP BY sender_id
		),
		recipient_counts AS (
			SELECT participant_id, COUNT(*) AS n
			FROM message_recipients
			GROUP BY participant_id
		)
		SELECT
			p.id,
			lower(p.email_address) AS email_address,
			COALESCE(NULLIF(trim(p.display_name), ''), '') AS display_name,
			COALESCE(p.domain, '') AS domain,
			COALESCE(sc.n, 0) + COALESCE(rc.n, 0) AS message_count
		FROM participants p
		LEFT JOIN sender_counts sc ON sc.participant_id = p.id
		LEFT JOIN recipient_counts rc ON rc.participant_id = p.id
		WHERE p.email_address IS NOT NULL AND p.email_address <> ''
		ORDER BY message_count DESC, p.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ParticipantForResolution
	for rows.Next() {
		var p ParticipantForResolution
		if err := rows.Scan(&p.ID, &p.EmailAddress, &p.DisplayName, &p.Domain, &p.MessageCount); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func candidateFetchLimit(limit int, includeExcluded bool) int {
	if includeExcluded {
		return limit
	}
	if limit < 200 {
		return 10000
	}
	return limit * 50
}

func parseMessageIDs(value string) []int64 {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
