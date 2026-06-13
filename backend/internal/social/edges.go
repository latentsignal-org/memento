package social

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"
)

// edgeKey is the canonical (min,max) pair key for an undirected edge.
type edgeKey struct{ A, B int64 }

// canonicalPair ensures A < B always.
func canonicalPair(a, b int64) edgeKey {
	if a < b {
		return edgeKey{a, b}
	}
	return edgeKey{b, a}
}

// edgeAccum is the in-memory accumulator for one undirected edge during scan.
type edgeAccum struct {
	DirectCount      int
	ToCount          int
	CcCount          int
	BccCount         int
	CoRecipientCount int
	AToB             int // from A to B (A < B always)
	BToA             int // from B to A
	firstTs          time.Time
	lastTs           time.Time
	hasTs            bool
	threadSet        map[int64]bool
}

func (e *edgeAccum) addTs(t time.Time) {
	if t.IsZero() {
		return
	}
	if !e.hasTs || t.Before(e.firstTs) {
		e.firstTs = t
		e.hasTs = true
	}
	if t.After(e.lastTs) {
		e.lastTs = t
	}
}

// edgeRow is the final, flattened edge ready for insertion.
type edgeRow struct {
	PersonAID        int64
	PersonBID        int64
	DirectCount      int
	ToCount          int
	CcCount          int
	BccCount         int
	CoRecipientCount int
	AToB             int
	BToA             int
	ThreadCount      int
	FirstTs          string
	LastTs           string
	Weight           float64
}

const batchSize = 5000
const recipientLookupBatchSize = 950
const maxRecipientsForCoEdge = 30

// buildEdges scans msgvault messages and returns the accumulated social edges
// after applying the noise floor. All reads run on db (outside any transaction).
func buildEdges(ctx context.Context, db *sql.DB) ([]edgeRow, error) {
	participantToPerson, err := loadParticipantPersonMap(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load participant-person map: %w", err)
	}

	excludedPersons, err := loadExcludedPersons(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("load excluded persons: %w", err)
	}

	accum := map[edgeKey]*edgeAccum{}
	now := time.Now().UTC()

	var minID int64
	for {
		type msgRow struct {
			id             int64
			senderID       int64
			sentAt         string
			conversationID int64
		}

		msgRows, err := db.QueryContext(ctx, `
			SELECT id, COALESCE(sender_id, 0), COALESCE(sent_at, ''), COALESCE(conversation_id, 0)
			FROM messages WHERE id > ? ORDER BY id LIMIT ?
		`, minID, batchSize)
		if err != nil {
			return nil, fmt.Errorf("query messages: %w", err)
		}

		var msgs []msgRow
		for msgRows.Next() {
			var m msgRow
			if err := msgRows.Scan(&m.id, &m.senderID, &m.sentAt, &m.conversationID); err != nil {
				msgRows.Close()
				return nil, fmt.Errorf("scan message: %w", err)
			}
			msgs = append(msgs, m)
		}
		if err := msgRows.Close(); err != nil {
			return nil, err
		}
		if len(msgs) == 0 {
			break
		}
		minID = msgs[len(msgs)-1].id

		// Load all recipients for this batch in one query.
		msgIDs := make([]int64, len(msgs))
		for i, m := range msgs {
			msgIDs[i] = m.id
		}
		recipientsByMsg, err := loadRecipientsBatch(ctx, db, msgIDs)
		if err != nil {
			return nil, fmt.Errorf("load recipients: %w", err)
		}

		for _, msg := range msgs {
			if msg.senderID == 0 {
				continue
			}
			senderPersonID, ok := participantToPerson[msg.senderID]
			if !ok {
				continue
			}
			if excludedPersons[senderPersonID] {
				continue
			}
			ts := parseTimestamp(msg.sentAt)

			recipients := recipientsByMsg[msg.id]
			// Resolve recipient participant IDs to canonical person IDs. One person
			// can appear multiple times through aliases on the same envelope; count
			// them once per message so aliases do not inflate weights or self-link.
			type rcpt struct {
				personID      int64
				recipientType string
			}
			recipientTypeByPerson := map[int64]string{}
			var resolved []rcpt
			for _, r := range recipients {
				if r.recipientType == "mention" {
					continue
				}
				pid, ok := participantToPerson[r.participantID]
				if !ok {
					continue
				}
				if excludedPersons[pid] || pid == senderPersonID {
					continue
				}
				if existing, seen := recipientTypeByPerson[pid]; !seen || recipientTypeRank(r.recipientType) > recipientTypeRank(existing) {
					recipientTypeByPerson[pid] = r.recipientType
				}
			}
			for pid, recipientType := range recipientTypeByPerson {
				resolved = append(resolved, rcpt{pid, recipientType})
			}

			// Emit direct edges (sender -> each recipient).
			for _, r := range resolved {
				k := canonicalPair(senderPersonID, r.personID)
				e := getOrCreate(accum, k)
				e.DirectCount++
				switch r.recipientType {
				case "to":
					e.ToCount++
				case "cc":
					e.CcCount++
				case "bcc":
					e.BccCount++
				}
				if senderPersonID < r.personID {
					e.AToB++
				} else {
					e.BToA++
				}
				e.addTs(ts)
				if e.threadSet == nil {
					e.threadSet = map[int64]bool{}
				}
				if msg.conversationID != 0 {
					e.threadSet[msg.conversationID] = true
				}
			}

			// Emit co-recipient edges (explosion guard: skip if >30 recipients).
			if len(resolved) > maxRecipientsForCoEdge {
				continue
			}
			for i := 0; i < len(resolved); i++ {
				for j := i + 1; j < len(resolved); j++ {
					if resolved[i].personID == resolved[j].personID {
						continue
					}
					k := canonicalPair(resolved[i].personID, resolved[j].personID)
					e := getOrCreate(accum, k)
					e.CoRecipientCount++
					e.addTs(ts)
					if e.threadSet == nil {
						e.threadSet = map[int64]bool{}
					}
					if msg.conversationID != 0 {
						e.threadSet[msg.conversationID] = true
					}
				}
			}
		}

		if len(msgs) < batchSize {
			break
		}
	}

	return applyNoiseFloorAndWeights(accum, now), nil
}

func recipientTypeRank(recipientType string) int {
	switch recipientType {
	case "to":
		return 3
	case "cc":
		return 2
	case "bcc":
		return 1
	default:
		return 0
	}
}

func getOrCreate(accum map[edgeKey]*edgeAccum, k edgeKey) *edgeAccum {
	if e, ok := accum[k]; ok {
		return e
	}
	e := &edgeAccum{}
	accum[k] = e
	return e
}

// applyNoiseFloorAndWeights drops weak pairs and computes the final weight.
func applyNoiseFloorAndWeights(accum map[edgeKey]*edgeAccum, now time.Time) []edgeRow {
	out := make([]edgeRow, 0, len(accum))
	for k, e := range accum {
		if k.A == k.B {
			continue
		}
		threadCount := len(e.threadSet)
		// Noise floor: drop if no direct contact AND weak co-recipient signal.
		if e.DirectCount == 0 && e.CoRecipientCount < 2 && threadCount < 2 {
			continue
		}
		row := edgeRow{
			PersonAID:        k.A,
			PersonBID:        k.B,
			DirectCount:      e.DirectCount,
			ToCount:          e.ToCount,
			CcCount:          e.CcCount,
			BccCount:         e.BccCount,
			CoRecipientCount: e.CoRecipientCount,
			AToB:             e.AToB,
			BToA:             e.BToA,
			ThreadCount:      threadCount,
		}
		if e.hasTs {
			row.FirstTs = e.firstTs.UTC().Format(time.RFC3339)
			row.LastTs = e.lastTs.UTC().Format(time.RFC3339)
		}
		row.Weight = computeWeight(e.DirectCount, e.CoRecipientCount, e.lastTs, now)
		out = append(out, row)
	}
	return out
}

func computeWeight(direct, coRecip int, lastTs, now time.Time) float64 {
	w := math.Log1p(float64(direct))*10.0 + math.Log1p(float64(coRecip))*2.0
	if !lastTs.IsZero() {
		days := now.Sub(lastTs).Hours() / 24.0
		w += math.Max(0, 5.0*math.Exp(-days/180.0))
	}
	return w
}

// loadParticipantPersonMap returns participant_id -> person_id for all participants
// whose person is in memento_person and not dismissed.
func loadParticipantPersonMap(ctx context.Context, db *sql.DB) (map[int64]int64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id AS participant_id, pe.person_id
		FROM participants p
		JOIN memento_person_email pe ON pe.email_address = lower(p.email_address)
		JOIN memento_person mp ON mp.id = pe.person_id
		WHERE mp.dismissed_at IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int64]int64{}
	for rows.Next() {
		var participantID, personID int64
		if err := rows.Scan(&participantID, &personID); err != nil {
			return nil, err
		}
		result[participantID] = personID
	}
	return result, rows.Err()
}

// loadExcludedPersons returns person IDs classified as 'excluded' in candidates.
func loadExcludedPersons(ctx context.Context, db *sql.DB) (map[int64]bool, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT person_id FROM memento_people_candidates WHERE classification = 'excluded'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

type recipientRow struct {
	participantID int64
	recipientType string
}

// loadRecipientsBatch loads all message_recipients rows for the given message IDs.
func loadRecipientsBatch(ctx context.Context, db *sql.DB, msgIDs []int64) (map[int64][]recipientRow, error) {
	if len(msgIDs) == 0 {
		return map[int64][]recipientRow{}, nil
	}
	result := map[int64][]recipientRow{}
	for start := 0; start < len(msgIDs); start += recipientLookupBatchSize {
		end := start + recipientLookupBatchSize
		if end > len(msgIDs) {
			end = len(msgIDs)
		}
		chunk := msgIDs[start:end]
		ph := makePlaceholders(len(chunk))
		args := int64Args(chunk)
		rows, err := db.QueryContext(ctx,
			fmt.Sprintf(`SELECT message_id, participant_id, recipient_type FROM message_recipients WHERE message_id IN (%s)`, ph),
			args...,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var msgID, participantID int64
			var recipientType string
			if err := rows.Scan(&msgID, &participantID, &recipientType); err != nil {
				rows.Close()
				return nil, err
			}
			result[msgID] = append(result[msgID], recipientRow{participantID, recipientType})
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// parseTimestamp tries common SQLite datetime formats. Returns zero time on failure.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05+00:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
