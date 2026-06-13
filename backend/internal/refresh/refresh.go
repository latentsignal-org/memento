// Package refresh builds and maintains the materialized rollup tables
// (memento_*_report). Each Refresh* function runs inside a single transaction
// so readers never see a half-empty table.
package refresh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"memento/backend/internal/concept"
	"memento/backend/internal/newsletter"
	"memento/backend/internal/people"
	"memento/backend/internal/project"
	"memento/backend/internal/social"
)

const (
	peopleReportMessageBatchSize              = 5000
	peopleReportRecipientLookupBatchSize      = 950
	maxPeopleReportCorrespondentParticipants  = 50
	maxPeopleReportTopCorrespondentsPerPerson = 5
)

// slugifyPersonName matches src/lib/citation.tsx slugify: lowercase, keep
// [a-z0-9], replace whitespace/hyphen/underscore with a single hyphen, drop
// everything else.
func slugifyPersonName(name string) string {
	var sb strings.Builder
	lastWasDash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			lastWasDash = false
		} else if r == ' ' || r == '-' || r == '_' {
			if !lastWasDash {
				sb.WriteRune('-')
				lastWasDash = true
			}
		}
	}
	return strings.Trim(sb.String(), "-")
}

func uniqueSlug(base string, used map[string]int) string {
	if base == "" {
		base = "person"
	}
	n := used[base]
	used[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n+1)
}

func parseDBTime(s string) string {
	if s == "" {
		return ""
	}
	layouts := []string{
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05+00:00",
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return s
}

func placeholders(n int) string {
	p := make([]string, n)
	for i := range p {
		p[i] = "?"
	}
	return strings.Join(p, ",")
}

func int64Args(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

func upsertMeta(ctx context.Context, tx *sql.Tx, dimension string, rowCount int, generatedAt string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO memento_report_meta (dimension, generated_at, row_count)
		VALUES (?, ?, ?)
		ON CONFLICT(dimension) DO UPDATE SET
			generated_at = excluded.generated_at,
			row_count = excluded.row_count
	`, dimension, generatedAt, rowCount)
	return err
}

// RefreshPeopleReport rebuilds memento_people_report from memento_people_candidates
// using batched queries for aliases and timelines, and per-person queries for
// correspondents. Runs inside a single transaction.
func RefreshPeopleReport(ctx context.Context, db *sql.DB) (int, error) {
	type personRow struct {
		people.PagePerson
		slug string
	}
	traceStart := time.Now()
	traceLast := traceStart
	trace := func(stage string) {
		if os.Getenv("MEMENTO_REFRESH_TRACE") == "" {
			return
		}
		now := time.Now()
		fmt.Fprintf(os.Stderr, "[refresh people] %-18s +%s total=%s\n",
			stage, now.Sub(traceLast).Round(time.Millisecond), now.Sub(traceStart).Round(time.Millisecond))
		traceLast = now
	}

	// 1. Load report-worthy candidate rows.
	//
	// memento_people_candidates is the full classified ledger (~5,900 rows,
	// including ~2,600 `excluded` and a long tail of one-message `weak_signal`
	// strangers). The report — and its expensive per-person timeline /
	// correspondent enrichment below — must stay scoped to people a user would
	// actually want in their directory, so:
	//   • keep every meaningful human (candidate / candidate_inbound_only),
	//     regardless of volume; and
	//   • keep weak_signal only above a minimal recurring-contact bar, which
	//     drops the single-message long tail (the bulk of weak_signal) without
	//     re-capping classification.
	// This is a relevance floor, not a count cap: the ledger stays complete,
	// and the Excluded tab is unaffected.
	rows, err := db.QueryContext(ctx, `
		SELECT mpc.person_id, mpc.canonical_name, mpc.primary_email, mpc.domain, mpc.email_count,
		       mpc.total_messages, mpc.from_contact_count, mpc.to_contact_count,
		       mpc.bidirectional_score, mpc.classification,
		       COALESCE(mpc.first_message_at, ''), COALESCE(mpc.last_message_at, '')
		FROM memento_people_candidates mpc
		JOIN memento_person mp ON mp.id = mpc.person_id
		WHERE mp.dismissed_at IS NULL
		  AND (
		        mpc.classification IN ('candidate', 'candidate_inbound_only')
		     OR (mpc.classification = 'weak_signal' AND mpc.total_messages >= 3)
		  )
		ORDER BY mpc.total_messages DESC, mpc.last_message_at DESC
	`)
	if err != nil {
		return 0, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var persons []personRow
	var personIDs []int64
	usedSlugs := map[string]int{}

	for rows.Next() {
		var p personRow
		var first, last string
		if err := rows.Scan(
			&p.PersonID, &p.CanonicalName, &p.PrimaryEmail, &p.Domain,
			&p.EmailCount, &p.TotalMessages, &p.FromContactCount, &p.ToContactCount,
			&p.BidirectionalScore, &p.Classification, &first, &last,
		); err != nil {
			return 0, fmt.Errorf("scan candidate: %w", err)
		}
		p.FirstMessageAt = parseDBTime(first)
		p.LastMessageAt = parseDBTime(last)
		p.slug = uniqueSlug(slugifyPersonName(p.CanonicalName), usedSlugs)
		persons = append(persons, p)
		personIDs = append(personIDs, p.PersonID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	if len(persons) == 0 {
		return 0, nil
	}
	trace(fmt.Sprintf("candidates:%d", len(persons)))

	ph := placeholders(len(personIDs))
	args := int64Args(personIDs)

	// 2. Batch load aliases.
	aliasesByPerson := map[int64][]people.Alias{}
	aliasRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT person_id, email_address, display_name, link_source, locked
		FROM memento_person_email
		WHERE person_id IN (%s)
		ORDER BY person_id, locked DESC, email_address ASC
	`, ph), args...)
	if err != nil {
		return 0, fmt.Errorf("batch aliases: %w", err)
	}
	for aliasRows.Next() {
		var personID int64
		var a people.Alias
		var lockedInt int
		if err := aliasRows.Scan(&personID, &a.EmailAddress, &a.DisplayName, &a.LinkSource, &lockedInt); err != nil {
			aliasRows.Close()
			return 0, fmt.Errorf("scan alias: %w", err)
		}
		a.Locked = lockedInt != 0
		aliasesByPerson[personID] = append(aliasesByPerson[personID], a)
	}
	if err := aliasRows.Close(); err != nil {
		return 0, err
	}
	trace("aliases")

	// 3. Recent timelines for every person. This is an envelope scan instead
	// of a SQL ROW_NUMBER window over millions of participation rows; keep only
	// the 20 newest entries each report person needs.
	timelinesByPerson, correspondentsByPerson, err := buildPeopleReportEnvelopeSummaries(ctx, db, personIDs)
	if err != nil {
		return 0, fmt.Errorf("batch envelope summaries: %w", err)
	}
	trace("envelopes")

	// 4. Top correspondents for every person in a single set-based pass.
	//
	// This used to be one large SQL self-join over conversation involvement. On
	// imported corpora with broad list/all-hands threads, SQLite materialized a
	// huge temporary pair table and could stall init for many minutes. The Go
	// accumulator scans envelopes once, drops oversized conversations, and only
	// keeps the top few pairs each person needs for the directory card.
	// Computed in the shared envelope scan above.

	// 5. Transactional swap: delete old rows, insert new rows, update meta.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_people_report`); err != nil {
		return 0, fmt.Errorf("delete old people report: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, first_message_at, last_message_at,
			slug, aliases_json, timeline_json, top_correspondents_json, generated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, p := range persons {
		aliases := aliasesByPerson[p.PersonID]
		if aliases == nil {
			aliases = []people.Alias{}
		}
		aliasesJSON, _ := json.Marshal(aliases)

		timeline := timelinesByPerson[p.PersonID]
		if timeline == nil {
			timeline = []people.TimelineEntry{}
		}
		timelineJSON, _ := json.Marshal(timeline)

		correspondents := correspondentsByPerson[p.PersonID]
		if correspondents == nil {
			correspondents = []people.Correspondent{}
		}
		correspondentsJSON, _ := json.Marshal(correspondents)

		var firstMsg, lastMsg any
		if p.FirstMessageAt != "" {
			firstMsg = p.FirstMessageAt
		}
		if p.LastMessageAt != "" {
			lastMsg = p.LastMessageAt
		}

		if _, err := stmt.ExecContext(ctx,
			p.PersonID, p.CanonicalName, p.PrimaryEmail, p.Domain, p.EmailCount,
			p.TotalMessages, p.FromContactCount, p.ToContactCount,
			p.BidirectionalScore, p.Classification, firstMsg, lastMsg,
			p.slug, string(aliasesJSON), string(timelineJSON), string(correspondentsJSON), now,
		); err != nil {
			return 0, fmt.Errorf("insert person %d: %w", p.PersonID, err)
		}
	}

	if err := upsertMeta(ctx, tx, "people", len(persons), now); err != nil {
		return 0, fmt.Errorf("update meta: %w", err)
	}
	trace("persist")
	return len(persons), tx.Commit()
}

type peopleReportRecipientRow struct {
	messageID     int64
	participantID int64
	recipientType string
}

type peopleReportMessageRow struct {
	id             int64
	senderID       int64
	sentAt         string
	conversationID int64
	subject        string
	snippet        string
}

type timelineCandidate struct {
	entry people.TimelineEntry
	date  string
}

type peopleReportParticipantInfo struct {
	personID  int64
	email     string
	isAccount bool
}

func buildPeopleReportEnvelopeSummaries(ctx context.Context, db *sql.DB, targetIDs []int64) (map[int64][]people.TimelineEntry, map[int64][]people.Correspondent, error) {
	timelines := map[int64][]people.TimelineEntry{}
	correspondents := map[int64][]people.Correspondent{}
	if len(targetIDs) == 0 {
		return timelines, correspondents, nil
	}

	targets := make(map[int64]bool, len(targetIDs))
	for _, id := range targetIDs {
		targets[id] = true
	}

	timelineParticipants, err := loadPeopleReportParticipantInfo(ctx, db, targets)
	if err != nil {
		return nil, nil, fmt.Errorf("load timeline participants: %w", err)
	}
	correspondentParticipants, err := loadPeopleReportParticipantMap(ctx, db)
	if err != nil {
		return nil, nil, fmt.Errorf("load correspondent participants: %w", err)
	}

	timelineCandidates := map[int64][]timelineCandidate{}
	conversationPeople := map[int64]map[int64]bool{}
	var minID int64
	for {
		msgs, err := loadPeopleReportMessageBatch(ctx, db, minID)
		if err != nil {
			return nil, nil, err
		}
		if len(msgs) == 0 {
			break
		}
		minID = msgs[len(msgs)-1].id

		msgIDs := make([]int64, len(msgs))
		for i, msg := range msgs {
			msgIDs[i] = msg.id
		}
		recipientsByMsg, err := loadPeopleReportRecipientsBatch(ctx, db, msgIDs)
		if err != nil {
			return nil, nil, err
		}

		for _, msg := range msgs {
			recipients := recipientsByMsg[msg.id]

			sender := timelineParticipants[msg.senderID]
			if sender.personID != 0 && !sender.isAccount && targets[sender.personID] {
				addTimelineCandidate(timelineCandidates, sender.personID, timelineCandidate{
					date: msg.sentAt,
					entry: people.TimelineEntry{
						Date:      msg.sentAt,
						Subject:   msg.subject,
						Snippet:   msg.snippet,
						MessageID: msg.id,
						Direction: "from_contact",
						ViaEmail:  sender.email,
					},
				})
			}
			if sender.isAccount {
				seenRecipientPersons := map[int64]bool{}
				for _, recipientRow := range recipients {
					if !isPeopleReportTimelineRecipientType(recipientRow.recipientType) {
						continue
					}
					recipient := timelineParticipants[recipientRow.participantID]
					if recipient.personID == 0 || !targets[recipient.personID] || seenRecipientPersons[recipient.personID] {
						continue
					}
					seenRecipientPersons[recipient.personID] = true
					addTimelineCandidate(timelineCandidates, recipient.personID, timelineCandidate{
						date: msg.sentAt,
						entry: people.TimelineEntry{
							Date:      msg.sentAt,
							Subject:   msg.subject,
							Snippet:   msg.snippet,
							MessageID: msg.id,
							Direction: "to_contact",
							ViaEmail:  recipient.email,
						},
					})
				}
			}

			if msg.conversationID == 0 {
				continue
			}
			set := conversationPeople[msg.conversationID]
			if set == nil {
				set = map[int64]bool{}
				conversationPeople[msg.conversationID] = set
			}
			if len(set) <= maxPeopleReportCorrespondentParticipants {
				if pid, ok := correspondentParticipants[msg.senderID]; ok {
					set[pid] = true
				}
				for _, recipientRow := range recipients {
					if pid, ok := correspondentParticipants[recipientRow.participantID]; ok {
						set[pid] = true
					}
				}
			}
		}

		if len(msgs) < peopleReportMessageBatchSize {
			break
		}
	}

	for personID, entries := range timelineCandidates {
		sortTimelineCandidates(entries)
		for _, c := range entries {
			c.entry.Date = parseDBTime(c.entry.Date)
			timelines[personID] = append(timelines[personID], c.entry)
		}
	}

	correspondents, err = topCorrespondentsFromConversations(ctx, db, targets, conversationPeople)
	if err != nil {
		return nil, nil, err
	}
	return timelines, correspondents, nil
}

func addTimelineCandidate(candidates map[int64][]timelineCandidate, personID int64, candidate timelineCandidate) {
	entries := candidates[personID]
	entries = append(entries, candidate)
	sortTimelineCandidates(entries)
	if len(entries) > 20 {
		entries = entries[:20]
	}
	candidates[personID] = entries
}

func sortTimelineCandidates(entries []timelineCandidate) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].date != entries[j].date {
			return entries[i].date > entries[j].date
		}
		return entries[i].entry.MessageID > entries[j].entry.MessageID
	})
}

func isPeopleReportTimelineRecipientType(recipientType string) bool {
	switch recipientType {
	case "to", "cc", "bcc", "mention":
		return true
	default:
		return false
	}
}

func loadPeopleReportParticipantInfo(ctx context.Context, db *sql.DB, targetIDs map[int64]bool) (map[int64]peopleReportParticipantInfo, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id, COALESCE(pe.person_id, 0), p.email_address,
		       lower(p.email_address) IN (
		         SELECT lower(identifier) FROM sources WHERE identifier LIKE '%@%'
		       ) AS is_account
		FROM participants p
		LEFT JOIN memento_person_email pe ON pe.email_address = lower(p.email_address)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]peopleReportParticipantInfo{}
	for rows.Next() {
		var participantID, personID int64
		var email string
		var isAccount bool
		if err := rows.Scan(&participantID, &personID, &email, &isAccount); err != nil {
			return nil, err
		}
		if isAccount || (personID != 0 && targetIDs[personID]) {
			out[participantID] = peopleReportParticipantInfo{
				personID:  personID,
				email:     email,
				isAccount: isAccount,
			}
		}
	}
	return out, rows.Err()
}

func topCorrespondentsFromConversations(
	ctx context.Context,
	db *sql.DB,
	targets map[int64]bool,
	conversationPeople map[int64]map[int64]bool,
) (map[int64][]people.Correspondent, error) {
	out := map[int64][]people.Correspondent{}
	pairCounts := map[int64]map[int64]int{}
	for _, set := range conversationPeople {
		if len(set) < 2 || len(set) > maxPeopleReportCorrespondentParticipants {
			continue
		}
		members := make([]int64, 0, len(set))
		for pid := range set {
			members = append(members, pid)
		}
		for _, targetID := range members {
			if !targets[targetID] {
				continue
			}
			counts := pairCounts[targetID]
			if counts == nil {
				counts = map[int64]int{}
				pairCounts[targetID] = counts
			}
			for _, corrID := range members {
				if corrID != targetID {
					counts[corrID]++
				}
			}
		}
	}

	topCorrIDs := map[int64]bool{}
	type scoredCorrespondent struct {
		personID int64
		count    int
	}
	topByTarget := map[int64][]scoredCorrespondent{}
	for targetID, counts := range pairCounts {
		scored := make([]scoredCorrespondent, 0, len(counts))
		for corrID, count := range counts {
			scored = append(scored, scoredCorrespondent{personID: corrID, count: count})
		}
		sort.Slice(scored, func(i, j int) bool {
			if scored[i].count != scored[j].count {
				return scored[i].count > scored[j].count
			}
			return scored[i].personID < scored[j].personID
		})
		if len(scored) > maxPeopleReportTopCorrespondentsPerPerson {
			scored = scored[:maxPeopleReportTopCorrespondentsPerPerson]
		}
		topByTarget[targetID] = scored
		for _, s := range scored {
			topCorrIDs[s.personID] = true
		}
	}

	names, err := loadPeopleReportPersonBasics(ctx, db, topCorrIDs)
	if err != nil {
		return nil, err
	}
	for targetID, scored := range topByTarget {
		for _, s := range scored {
			c := names[s.personID]
			c.PersonID = s.personID
			c.SharedCount = int64(s.count)
			out[targetID] = append(out[targetID], c)
		}
	}
	return out, nil
}

func loadPeopleReportParticipantMap(ctx context.Context, db *sql.DB) (map[int64]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id AS participant_id, pe.person_id
		FROM participants p
		JOIN memento_person_email pe ON pe.email_address = lower(p.email_address)
		WHERE p.id NOT IN (
			SELECT p2.id
			FROM participants p2
			WHERE lower(p2.email_address) IN (
				SELECT lower(identifier) FROM sources WHERE identifier LIKE '%@%'
			)
		)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]int64{}
	for rows.Next() {
		var participantID, personID int64
		if err := rows.Scan(&participantID, &personID); err != nil {
			return nil, err
		}
		out[participantID] = personID
	}
	return out, rows.Err()
}

func loadPeopleReportMessageBatch(ctx context.Context, db *sql.DB, minID int64) ([]peopleReportMessageRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, COALESCE(sender_id, 0), COALESCE(sent_at, ''),
		       COALESCE(conversation_id, 0), COALESCE(subject, ''), COALESCE(snippet, '')
		FROM messages
		WHERE id > ?
		ORDER BY id
		LIMIT ?
	`, minID, peopleReportMessageBatchSize)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var out []peopleReportMessageRow
	for rows.Next() {
		var msg peopleReportMessageRow
		if err := rows.Scan(&msg.id, &msg.senderID, &msg.sentAt, &msg.conversationID, &msg.subject, &msg.snippet); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func loadPeopleReportRecipientsBatch(ctx context.Context, db *sql.DB, msgIDs []int64) (map[int64][]peopleReportRecipientRow, error) {
	out := map[int64][]peopleReportRecipientRow{}
	for start := 0; start < len(msgIDs); start += peopleReportRecipientLookupBatchSize {
		end := start + peopleReportRecipientLookupBatchSize
		if end > len(msgIDs) {
			end = len(msgIDs)
		}
		chunk := msgIDs[start:end]
		rows, err := db.QueryContext(ctx,
			fmt.Sprintf(`SELECT message_id, participant_id, recipient_type FROM message_recipients WHERE message_id IN (%s)`, placeholders(len(chunk))),
			int64Args(chunk)...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row peopleReportRecipientRow
			if err := rows.Scan(&row.messageID, &row.participantID, &row.recipientType); err != nil {
				rows.Close()
				return nil, err
			}
			out[row.messageID] = append(out[row.messageID], row)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func loadPeopleReportPersonBasics(ctx context.Context, db *sql.DB, personIDs map[int64]bool) (map[int64]people.Correspondent, error) {
	out := map[int64]people.Correspondent{}
	if len(personIDs) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(personIDs))
	for id := range personIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for start := 0; start < len(ids); start += peopleReportRecipientLookupBatchSize {
		end := start + peopleReportRecipientLookupBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		rows, err := db.QueryContext(ctx,
			fmt.Sprintf(`SELECT id, canonical_name, primary_email FROM memento_person WHERE id IN (%s)`, placeholders(len(chunk))),
			int64Args(chunk)...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c people.Correspondent
			if err := rows.Scan(&c.PersonID, &c.CanonicalName, &c.PrimaryEmail); err != nil {
				rows.Close()
				return nil, err
			}
			out[c.PersonID] = c
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// RefreshNewslettersReport rebuilds memento_newsletters_report from
// memento_newsletter_source, fetching recent subjects in one batched query.
func RefreshNewslettersReport(ctx context.Context, db *sql.DB) (int, error) {
	sources, err := newsletter.ListSources(ctx, db)
	if err != nil {
		return 0, fmt.Errorf("list sources: %w", err)
	}
	if len(sources) == 0 {
		return 0, nil
	}

	// Batch recent subjects: top 3 per sender, ordered by sent_at DESC.
	emails := make([]any, len(sources))
	senderEmails := make([]string, len(sources))
	for i, s := range sources {
		emails[i] = strings.ToLower(s.SenderEmail)
		senderEmails[i] = strings.ToLower(s.SenderEmail)
	}
	ph := placeholders(len(emails))

	subjectsByEmail := map[string][]string{}
	subjectRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		WITH ranked AS (
			SELECT lower(p.email_address) AS sender_email, m.subject,
			       ROW_NUMBER() OVER (
			         PARTITION BY lower(p.email_address)
			         ORDER BY m.sent_at DESC, m.id DESC
			       ) AS rn
			FROM messages m
			JOIN participants p ON p.id = m.sender_id
			WHERE lower(p.email_address) IN (%s)
			  AND m.subject IS NOT NULL AND m.subject != ''
		)
		SELECT sender_email, subject FROM ranked WHERE rn <= 3
	`, ph), emails...)
	if err != nil {
		return 0, fmt.Errorf("batch subjects: %w", err)
	}
	for subjectRows.Next() {
		var email, subject string
		if err := subjectRows.Scan(&email, &subject); err != nil {
			subjectRows.Close()
			return 0, err
		}
		subjectsByEmail[email] = append(subjectsByEmail[email], subject)
	}
	if err := subjectRows.Close(); err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_newsletters_report`); err != nil {
		return 0, fmt.Errorf("delete old newsletters report: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_newsletters_report (
			source_id, slug, display_name, sender_email, domain,
			message_count, unsubscribe_count, first_seen, last_seen,
			classification_reason, recent_subjects_json, generated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range sources {
		subjects := subjectsByEmail[strings.ToLower(s.SenderEmail)]
		if subjects == nil {
			subjects = []string{}
		}
		subjectsJSON, _ := json.Marshal(subjects)

		if _, err := stmt.ExecContext(ctx,
			s.ID, s.Slug, s.DisplayName, s.SenderEmail, s.Domain,
			s.MessageCount, s.UnsubscribeCount,
			stringOrNil(s.FirstSeen), stringOrNil(s.LastSeen),
			s.ClassificationReason, string(subjectsJSON), now,
		); err != nil {
			return 0, fmt.Errorf("insert newsletter source %d: %w", s.ID, err)
		}
	}

	if err := upsertMeta(ctx, tx, "newsletters", len(sources), now); err != nil {
		return 0, fmt.Errorf("update meta: %w", err)
	}
	return len(sources), tx.Commit()
}

// RefreshProjectsReport rebuilds memento_projects_report from memento_project.
func RefreshProjectsReport(ctx context.Context, db *sql.DB) (int, error) {
	summaries, err := project.ListProjectsSummary(ctx, db)
	if err != nil {
		return 0, fmt.Errorf("list projects: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_projects_report`); err != nil {
		return 0, fmt.Errorf("delete old projects report: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_projects_report (
			project_id, slug, name, status, started_at, summary_json, members_json, generated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range summaries {
		summaryJSON, _ := json.Marshal(s)
		var startedAt any
		if s.StartedAt != "" {
			startedAt = s.StartedAt
		}
		if _, err := stmt.ExecContext(ctx,
			s.ProjectID, s.Slug, s.Name, s.Status, startedAt,
			string(summaryJSON), "[]", now,
		); err != nil {
			return 0, fmt.Errorf("insert project %d: %w", s.ProjectID, err)
		}
	}

	if err := upsertMeta(ctx, tx, "projects", len(summaries), now); err != nil {
		return 0, fmt.Errorf("update meta: %w", err)
	}
	return len(summaries), tx.Commit()
}

// RefreshConceptsReport rebuilds memento_concepts_report from memento_concept.
func RefreshConceptsReport(ctx context.Context, db *sql.DB) (int, error) {
	entries, err := concept.BuildConceptIndex(ctx, db)
	if err != nil {
		return 0, fmt.Errorf("build concept index: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memento_concepts_report`); err != nil {
		return 0, fmt.Errorf("delete old concepts report: %w", err)
	}

	// Load concept IDs by slug for FK insertion (use tx to avoid deadlock on
	// SetMaxOpenConns(1) — the transaction already holds the only connection).
	type conceptIDRow struct {
		id   int64
		slug string
	}
	idRows, err := tx.QueryContext(ctx, `SELECT id, slug FROM memento_concept`)
	if err != nil {
		return 0, fmt.Errorf("load concept ids: %w", err)
	}
	idBySlug := map[string]int64{}
	for idRows.Next() {
		var r conceptIDRow
		if err := idRows.Scan(&r.id, &r.slug); err != nil {
			idRows.Close()
			return 0, err
		}
		idBySlug[r.slug] = r.id
	}
	idRows.Close()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_concepts_report (
			concept_id, slug, name, status, scope_description, message_count, payload_json, generated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		id, ok := idBySlug[e.Slug]
		if !ok {
			continue
		}
		payloadJSON, _ := json.Marshal(e)
		if _, err := stmt.ExecContext(ctx,
			id, e.Slug, e.Name, e.Status, e.Scope, e.MessageCount,
			string(payloadJSON), now,
		); err != nil {
			return 0, fmt.Errorf("insert concept %s: %w", e.Slug, err)
		}
	}

	if err := upsertMeta(ctx, tx, "concepts", len(entries), now); err != nil {
		return 0, fmt.Errorf("update meta: %w", err)
	}
	return len(entries), tx.Commit()
}

// RefreshPeopleReportForPerson updates the report table for a single person.
func RefreshPeopleReportForPerson(ctx context.Context, db *sql.DB, personID int64) error {
	type PagePerson struct {
		PersonID           int64   `json:"person_id"`
		CanonicalName      string  `json:"canonical_name"`
		PrimaryEmail       string  `json:"primary_email"`
		Domain             string  `json:"domain"`
		EmailCount         int     `json:"email_count"`
		TotalMessages      int     `json:"total_messages"`
		FromContactCount   int     `json:"from_contact_count"`
		ToContactCount     int     `json:"to_contact_count"`
		BidirectionalScore float64 `json:"bidirectional_score"`
		Classification     string  `json:"classification"`
		FirstMessageAt     string  `json:"first_message_at"`
		LastMessageAt      string  `json:"last_message_at"`
	}

	var p PagePerson
	var first, last string
	err := db.QueryRowContext(ctx, `
		SELECT mpc.person_id, mpc.canonical_name, mpc.primary_email, mpc.domain, mpc.email_count,
		       mpc.total_messages, mpc.from_contact_count, mpc.to_contact_count,
		       mpc.bidirectional_score, mpc.classification,
		       COALESCE(mpc.first_message_at, ''), COALESCE(mpc.last_message_at, '')
		FROM memento_people_candidates mpc
		JOIN memento_person mp ON mp.id = mpc.person_id
		WHERE mp.id = ? AND mp.dismissed_at IS NULL
	`, personID).Scan(
		&p.PersonID, &p.CanonicalName, &p.PrimaryEmail, &p.Domain,
		&p.EmailCount, &p.TotalMessages, &p.FromContactCount, &p.ToContactCount,
		&p.BidirectionalScore, &p.Classification, &first, &last,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Person is either dismissed or not a candidate anymore. We should delete them from the report.
			_, err = db.ExecContext(ctx, `DELETE FROM memento_people_report WHERE person_id = ?`, personID)
			return err
		}
		return fmt.Errorf("scan candidate: %w", err)
	}
	p.FirstMessageAt = parseDBTime(first)
	p.LastMessageAt = parseDBTime(last)

	// Determine slug
	var slug string
	err = db.QueryRowContext(ctx, `SELECT slug FROM memento_people_report WHERE person_id = ?`, personID).Scan(&slug)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if slug == "" {
		baseSlug := slugifyPersonName(p.CanonicalName)
		// Check for uniqueness
		var count int
		err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_people_report WHERE slug = ? AND person_id <> ?`, baseSlug, personID).Scan(&count)
		if err != nil {
			return err
		}
		if count == 0 {
			slug = baseSlug
		} else {
			// Find a unique slug suffix
			for suffix := 2; ; suffix++ {
				cand := fmt.Sprintf("%s-%d", baseSlug, suffix)
				err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_people_report WHERE slug = ? AND person_id <> ?`, cand, personID).Scan(&count)
				if err != nil {
					return err
				}
				if count == 0 {
					slug = cand
					break
				}
			}
		}
	}

	// 2. Load aliases
	var aliases []people.Alias
	aliasRows, err := db.QueryContext(ctx, `
		SELECT email_address, display_name, link_source, locked
		FROM memento_person_email
		WHERE person_id = ?
		ORDER BY locked DESC, email_address ASC
	`, personID)
	if err != nil {
		return fmt.Errorf("aliases: %w", err)
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var a people.Alias
		var lockedInt int
		if err := aliasRows.Scan(&a.EmailAddress, &a.DisplayName, &a.LinkSource, &lockedInt); err != nil {
			return fmt.Errorf("scan alias: %w", err)
		}
		a.Locked = lockedInt != 0
		aliases = append(aliases, a)
	}
	aliasRows.Close()

	// 3. Load timeline
	var timeline []people.TimelineEntry
	timelineRows, err := db.QueryContext(ctx, `
		WITH account_emails AS (
			SELECT lower(identifier) AS email FROM sources WHERE identifier LIKE '%@%'
		),
		account_participants AS (
			SELECT p.id FROM participants p
			JOIN account_emails ae ON ae.email = lower(p.email_address)
		),
		person_emails AS (
			SELECT lower(email_address) AS email, person_id
			FROM memento_person_email
			WHERE person_id = ?
		),
		person_participants AS (
			SELECT p.id, pe.person_id, p.email_address
			FROM participants p
			JOIN person_emails pe ON pe.email = lower(p.email_address)
		),
		involvement AS (
			SELECT pp.person_id, m.id AS message_id, m.sent_at,
			       'from_contact' AS direction, pp.email_address AS via_email
			FROM messages m
			JOIN person_participants pp ON pp.id = m.sender_id
			WHERE m.sender_id NOT IN (SELECT id FROM account_participants)
			UNION ALL
			SELECT pp.person_id, m.id AS message_id, m.sent_at,
			       'to_contact' AS direction, pp.email_address AS via_email
			FROM message_recipients mr
			JOIN messages m ON m.id = mr.message_id
			JOIN person_participants pp ON pp.id = mr.participant_id
			WHERE m.sender_id IN (SELECT id FROM account_participants)
			  AND mr.recipient_type IN ('to', 'cc', 'bcc', 'mention')
		),
		ranked AS (
			SELECT i.person_id, i.message_id,
			       COALESCE(i.sent_at, '') AS sent_at,
			       i.direction, i.via_email,
			       COALESCE(m.subject, '') AS subject,
			       COALESCE(m.snippet, '') AS snippet
			FROM involvement i
			JOIN messages m ON m.id = i.message_id
		)
		SELECT message_id, sent_at, direction, via_email, subject, snippet
		FROM ranked
		ORDER BY sent_at DESC, message_id DESC
		LIMIT 20
	`, personID)
	if err != nil {
		return fmt.Errorf("timeline: %w", err)
	}
	defer timelineRows.Close()
	for timelineRows.Next() {
		var entry people.TimelineEntry
		var date string
		if err := timelineRows.Scan(
			&entry.MessageID, &date, &entry.Direction, &entry.ViaEmail, &entry.Subject, &entry.Snippet,
		); err != nil {
			return fmt.Errorf("scan timeline: %w", err)
		}
		entry.Date = parseDBTime(date)
		timeline = append(timeline, entry)
	}
	timelineRows.Close()

	// 4. Load correspondents
	var correspondents []people.Correspondent
	corrRows, err := db.QueryContext(ctx, `
		WITH target_emails AS (
			SELECT lower(email_address) AS email FROM memento_person_email WHERE person_id = ?
		),
		target_participants AS (
			SELECT id FROM participants WHERE lower(email_address) IN (SELECT email FROM target_emails)
		),
		target_conversations AS (
			SELECT DISTINCT m.conversation_id
			FROM messages m
			LEFT JOIN message_recipients mr ON mr.message_id = m.id
			WHERE m.sender_id IN (SELECT id FROM target_participants)
			   OR mr.participant_id IN (SELECT id FROM target_participants)
		),
		account_emails AS (
			SELECT lower(identifier) AS email FROM sources WHERE identifier LIKE '%@%'
		),
		account_participants AS (
			SELECT id FROM participants WHERE lower(email_address) IN (SELECT email FROM account_emails)
		),
		other_involvements AS (
			SELECT m.conversation_id, pe.person_id
			FROM messages m
			LEFT JOIN message_recipients mr ON mr.message_id = m.id
			JOIN participants p ON p.id = m.sender_id OR p.id = mr.participant_id
			JOIN memento_person_email pe ON pe.email_address = lower(p.email_address)
			WHERE m.conversation_id IN (SELECT conversation_id FROM target_conversations)
			  AND pe.person_id <> ?
			  AND p.id NOT IN (SELECT id FROM account_participants)
		)
		SELECT oi.person_id, mp.canonical_name, mp.primary_email,
		       COUNT(DISTINCT oi.conversation_id) AS shared_count
		FROM other_involvements oi
		JOIN memento_person mp ON mp.id = oi.person_id
		GROUP BY oi.person_id
		ORDER BY shared_count DESC
		LIMIT 5
	`, personID, personID)
	if err != nil {
		return fmt.Errorf("correspondents: %w", err)
	}
	defer corrRows.Close()
	for corrRows.Next() {
		var c people.Correspondent
		if err := corrRows.Scan(&c.PersonID, &c.CanonicalName, &c.PrimaryEmail, &c.SharedCount); err != nil {
			return fmt.Errorf("scan correspondent: %w", err)
		}
		correspondents = append(correspondents, c)
	}
	corrRows.Close()

	// 5. Transactional insert or replace
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	aliasesJSON, _ := json.Marshal(aliases)
	timelineJSON, _ := json.Marshal(timeline)
	correspondentsJSON, _ := json.Marshal(correspondents)
	now := time.Now().UTC().Format(time.RFC3339)

	var firstMsg, lastMsg any
	if p.FirstMessageAt != "" {
		firstMsg = p.FirstMessageAt
	}
	if p.LastMessageAt != "" {
		lastMsg = p.LastMessageAt
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, first_message_at, last_message_at,
			slug, aliases_json, timeline_json, top_correspondents_json, generated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(person_id) DO UPDATE SET
			canonical_name = excluded.canonical_name,
			primary_email = excluded.primary_email,
			domain = excluded.domain,
			email_count = excluded.email_count,
			total_messages = excluded.total_messages,
			from_contact_count = excluded.from_contact_count,
			to_contact_count = excluded.to_contact_count,
			bidirectional_score = excluded.bidirectional_score,
			classification = excluded.classification,
			first_message_at = excluded.first_message_at,
			last_message_at = excluded.last_message_at,
			slug = excluded.slug,
			aliases_json = excluded.aliases_json,
			timeline_json = excluded.timeline_json,
			top_correspondents_json = excluded.top_correspondents_json,
			generated_at = excluded.generated_at
	`,
		p.PersonID, p.CanonicalName, p.PrimaryEmail, p.Domain, p.EmailCount,
		p.TotalMessages, p.FromContactCount, p.ToContactCount,
		p.BidirectionalScore, p.Classification, firstMsg, lastMsg,
		slug, string(aliasesJSON), string(timelineJSON), string(correspondentsJSON), now,
	)
	if err != nil {
		return fmt.Errorf("upsert person %d: %w", p.PersonID, err)
	}

	return tx.Commit()
}

// RefreshAll refreshes all rollup tables in sequence. Social runs last so it
// can rely on resolved/dismissed person state from RefreshPeopleReport.
func RefreshAll(ctx context.Context, db *sql.DB) error {
	if _, err := RefreshPeopleReport(ctx, db); err != nil {
		return fmt.Errorf("people: %w", err)
	}
	if _, err := RefreshNewslettersReport(ctx, db); err != nil {
		return fmt.Errorf("newsletters: %w", err)
	}
	if _, err := RefreshProjectsReport(ctx, db); err != nil {
		return fmt.Errorf("projects: %w", err)
	}
	if _, err := RefreshConceptsReport(ctx, db); err != nil {
		return fmt.Errorf("concepts: %w", err)
	}
	if _, err := social.BuildSocialGraph(ctx, db); err != nil {
		return fmt.Errorf("social: %w", err)
	}
	return nil
}

func stringOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
