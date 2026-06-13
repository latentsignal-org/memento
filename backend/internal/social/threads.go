package social

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Top-threads + cadence snapshots backing the Group card UI.
//
// A "co-thread message" is one where at least two non-excluded group members
// appear in the participants (sender ∪ recipients). The card needs:
//   - The most-recent co-thread messages with subject/from/timestamp for
//     "Top recent threads".
//   - A 12-month bar series of co-thread message counts for the cadence
//     sparkline.
//
// Computed at group save/refresh time and cached on
// memento_social_group.top_threads_json / cadence_json so the directory render
// doesn't re-walk msgvault on every load.

const topThreadsLimit = 12

// ComputeTopThreads returns the most recent co-thread messages (subject, sender
// display, internal timestamp) where ≥2 non-excluded members of the group
// co-participated.
func ComputeTopThreads(ctx context.Context, db *sql.DB, groupID int64) ([]GroupThread, error) {
	participantIDs, err := groupParticipantIDs(ctx, db, groupID)
	if err != nil {
		return nil, err
	}
	if len(participantIDs) < 2 {
		return []GroupThread{}, nil
	}
	ph := placeholders(len(participantIDs))
	args := make([]any, 0, len(participantIDs))
	for _, id := range participantIDs {
		args = append(args, id)
	}

	// Collapse to distinct threads: for each thread, pick the most-recent
	// shared message and show that. Falls back to msg:<id> when a message
	// has no conversation_id so we still produce a stable key.
	q := fmt.Sprintf(`
		WITH msg_ppl AS (
			SELECT m.id AS msg_id, m.sender_id AS participant_id
			FROM messages m
			WHERE m.sender_id IN (%s)
			UNION
			SELECT mr.message_id AS msg_id, mr.participant_id
			FROM message_recipients mr
			WHERE mr.participant_id IN (%s)
		),
		shared AS (
			SELECT msg_id
			FROM msg_ppl
			GROUP BY msg_id
			HAVING COUNT(DISTINCT participant_id) >= 2
		),
		shared_msgs AS (
			SELECT m.id AS msg_id,
			       COALESCE(NULLIF(m.conversation_id, ''), 'msg:' || m.id) AS thread_key,
			       m.sent_at
			FROM shared s
			JOIN messages m ON m.id = s.msg_id
			WHERE m.sent_at IS NOT NULL
		),
		latest_per_thread AS (
			SELECT thread_key, MAX(sent_at) AS sent_at
			FROM shared_msgs
			GROUP BY thread_key
		),
		picked AS (
			SELECT MIN(sm.msg_id) AS msg_id, sm.thread_key
			FROM shared_msgs sm
			JOIN latest_per_thread lpt
			  ON lpt.thread_key = sm.thread_key AND lpt.sent_at = sm.sent_at
			GROUP BY sm.thread_key
		)
		SELECT m.id,
		       p.thread_key,
		       COALESCE(m.source_message_id, ''),
		       COALESCE(m.subject, ''),
		       COALESCE(sp.display_name, ''),
		       COALESCE(sp.email_address, ''),
		       COALESCE(m.sent_at, '')
		FROM picked p
		JOIN messages m ON m.id = p.msg_id
		LEFT JOIN participants sp ON sp.id = m.sender_id
		ORDER BY m.sent_at DESC
		LIMIT ?
	`, ph, ph)

	queryArgs := append(append([]any{}, args...), args...)
	queryArgs = append(queryArgs, topThreadsLimit)

	rows, err := db.QueryContext(ctx, q, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("top threads query: %w", err)
	}
	defer rows.Close()

	out := []GroupThread{}
	seen := map[string]bool{}
	for rows.Next() {
		var t GroupThread
		var msgID int64
		var threadKey, sourceMsgID, sentAt string
		if err := rows.Scan(&msgID, &threadKey, &sourceMsgID, &t.Subject, &t.FromName, &t.FromEmail, &sentAt); err != nil {
			return nil, err
		}
		// Defensive: drop the unlikely event of a duplicate thread_key surviving
		// the query (e.g. on legacy data) so the React keyspace stays clean.
		if seen[threadKey] {
			continue
		}
		seen[threadKey] = true
		t.ThreadID = threadKey
		t.MessageID = sourceMsgID
		t.InternalMsgID = msgID
		t.InternalTS = parseInternalTS(sentAt)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ComputeCadence returns a 12-bucket all-time series (oldest → newest) of
// co-thread message counts. Always returns exactly 12 entries, padded with
// zeros. This backs the compact activity chart; it is intentionally all-time
// so dormant groups with old top threads still show their historical shape.
func ComputeCadence(ctx context.Context, db *sql.DB, groupID int64) ([]int, error) {
	participantIDs, err := groupParticipantIDs(ctx, db, groupID)
	if err != nil {
		return nil, err
	}
	out := make([]int, 12)
	if len(participantIDs) < 2 {
		return out, nil
	}
	ph := placeholders(len(participantIDs))
	args := make([]any, 0, len(participantIDs))
	for _, id := range participantIDs {
		args = append(args, id)
	}

	q := fmt.Sprintf(`
		WITH msg_ppl AS (
			SELECT m.id AS msg_id, m.sender_id AS participant_id, m.sent_at
			FROM messages m
			WHERE m.sender_id IN (%s)
			UNION
			SELECT mr.message_id AS msg_id, mr.participant_id, m2.sent_at
			FROM message_recipients mr
			JOIN messages m2 ON m2.id = mr.message_id
			WHERE mr.participant_id IN (%s)
		),
		shared AS (
			SELECT msg_id, MAX(sent_at) AS sent_at
			FROM msg_ppl
			GROUP BY msg_id
			HAVING COUNT(DISTINCT participant_id) >= 2
		)
		SELECT COALESCE(sent_at, '')
		FROM shared
		WHERE sent_at IS NOT NULL AND sent_at <> ''
		ORDER BY sent_at ASC
	`, ph, ph)

	queryArgs := append(append([]any{}, args...), args...)
	rows, err := db.QueryContext(ctx, q, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("cadence query: %w", err)
	}
	defer rows.Close()

	var times []int64
	for rows.Next() {
		var sentAt string
		if err := rows.Scan(&sentAt); err != nil {
			return nil, err
		}
		if ts := parseInternalTS(sentAt); ts > 0 {
			times = append(times, ts)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(times) == 0 {
		return out, nil
	}
	minTS := times[0]
	maxTS := times[len(times)-1]
	if minTS == maxTS {
		out[11] = len(times)
		return out, nil
	}
	span := maxTS - minTS + 1
	for _, ts := range times {
		idx := int(((ts - minTS) * 12) / span)
		if idx < 0 {
			idx = 0
		}
		if idx > 11 {
			idx = 11
		}
		out[idx]++
	}
	return out, nil
}

// ComputeActivityStats returns the all-time count of co-thread messages and
// the unix-seconds timestamp of the most recent one. Distinct from cadence,
// which only covers the trailing 12 months — these power the card's honest
// "N messages · last activity X ago" summary for dormant groups too.
func ComputeActivityStats(ctx context.Context, db *sql.DB, groupID int64) (count int, lastTS int64, err error) {
	participantIDs, err := groupParticipantIDs(ctx, db, groupID)
	if err != nil {
		return 0, 0, err
	}
	if len(participantIDs) < 2 {
		return 0, 0, nil
	}
	ph := placeholders(len(participantIDs))
	args := make([]any, 0, len(participantIDs)*2)
	for _, id := range participantIDs {
		args = append(args, id)
	}
	for _, id := range participantIDs {
		args = append(args, id)
	}

	q := fmt.Sprintf(`
		WITH msg_ppl AS (
			SELECT m.id AS msg_id, m.sender_id AS participant_id, m.sent_at
			FROM messages m
			WHERE m.sender_id IN (%s)
			UNION
			SELECT mr.message_id AS msg_id, mr.participant_id, m2.sent_at
			FROM message_recipients mr
			JOIN messages m2 ON m2.id = mr.message_id
			WHERE mr.participant_id IN (%s)
		),
		shared AS (
			SELECT msg_id, MAX(sent_at) AS sent_at
			FROM msg_ppl
			GROUP BY msg_id
			HAVING COUNT(DISTINCT participant_id) >= 2
		)
		SELECT COUNT(*) AS n, COALESCE(MAX(sent_at), '') AS newest
		FROM shared
	`, ph, ph)

	var newest string
	if err := db.QueryRowContext(ctx, q, args...).Scan(&count, &newest); err != nil {
		return 0, 0, fmt.Errorf("activity stats query: %w", err)
	}
	return count, parseInternalTS(newest), nil
}

// RefreshGroupSnapshots recomputes top_threads_json, cadence_json, message_count
// and last_activity_ts for the given group and writes them back. Used by save,
// by member-edit endpoints, and by `memento refresh`.
func RefreshGroupSnapshots(ctx context.Context, db *sql.DB, groupID int64) error {
	threads, err := ComputeTopThreads(ctx, db, groupID)
	if err != nil {
		return err
	}
	cadence, err := ComputeCadence(ctx, db, groupID)
	if err != nil {
		return err
	}
	msgCount, lastTS, err := ComputeActivityStats(ctx, db, groupID)
	if err != nil {
		return err
	}
	threadsJSON, _ := json.Marshal(threads)
	cadenceJSON, _ := json.Marshal(cadence)
	_, err = db.ExecContext(ctx, `
		UPDATE memento_social_group
		SET top_threads_json = ?, cadence_json = ?, message_count = ?, last_activity_ts = ?
		WHERE group_id = ?
	`, string(threadsJSON), string(cadenceJSON), msgCount, lastTS, groupID)
	return err
}

// RefreshAllGroupSnapshots recomputes snapshots for visible groups. Suppressed
// auto-groups can be enormous on imported corpora (for example, Enron-wide
// communities with thousands of members); hydrating their thread/cadence cards
// means repeating broad archive scans for groups the default UI hides anyway.
// Saved groups are still refreshed even if their current topology would be
// suppressed, because the user explicitly promoted them.
func RefreshAllGroupSnapshots(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT group_id
		FROM memento_social_group
		WHERE dismissed_at IS NULL
		  AND (is_actionable = 1 OR saved_at IS NOT NULL)
	`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := RefreshGroupSnapshots(ctx, db, id); err != nil {
			return fmt.Errorf("group %d: %w", id, err)
		}
	}
	return nil
}

// groupParticipantIDs returns msgvault participant.id values for the non-
// excluded members of the group, joining via memento_person_email.
func groupParticipantIDs(ctx context.Context, db *sql.DB, groupID int64) ([]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT p.id
		FROM memento_social_group_member gm
		JOIN memento_person_email pe ON pe.person_id = gm.person_id
		JOIN participants p ON lower(p.email_address) = lower(pe.email_address)
		WHERE gm.group_id = ? AND gm.excluded_at IS NULL
	`, groupID)
	if err != nil {
		return nil, fmt.Errorf("load participant ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// parseInternalTS turns a msgvault sent_at string into unix seconds.
// msgvault stores timestamps as "2006-01-02 15:04:05+00:00" (space separator
// with a numeric offset), but we also accept RFC3339 and a few common
// variants for safety. Returns 0 on parse failure.
func parseInternalTS(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05-07:00", // msgvault native: space sep + numeric offset
		"2006-01-02 15:04:05Z07:00", // same, tolerating a literal Z
		time.RFC3339,                // "2006-01-02T15:04:05Z07:00"
		"2006-01-02T15:04:05",       // naive ISO, no zone
		"2006-01-02 15:04:05",       // naive, space sep, no zone
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}
