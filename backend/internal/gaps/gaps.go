// Package gaps implements deterministic gap detection over a message bundle.
// It never calls the LLM — the caller (an agent tool) decides what to do with
// the returned gaps.  Three modes:
//
//   - "chronological": finds time spans > defaultChronologicalGapDays between
//     consecutive bundle messages and suggests date-bounded search hints.
//   - "thematic": clusters messages by TF-IDF K-means and flags clusters with
//     fewer than thematicMinClusterSize messages as potentially under-evidenced.
//   - "participant": finds email addresses mentioned in message bodies that do
//     not appear as sender or recipient on any bundle message.
package gaps

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

const defaultChronologicalGapDays = 14
const thematicMinClusterSize = 3

// Gap describes a single detected hole in a message bundle.
type Gap struct {
	Kind             string   `json:"kind"`
	Description      string   `json:"description"`
	AnchorMessageIDs []int64  `json:"anchor_message_ids"`
	SearchHints      []string `json:"search_hints"`
	Severity         string   `json:"severity"` // "low", "medium", "high"
}

// Detect finds gaps in a message bundle according to the given mode.
// db must be the archive (msgvault) database; it is opened read-only from
// the caller's perspective.
// mode must be one of "chronological", "thematic", or "participant".
// Returns nil, nil when the bundle is too small to analyse.
func Detect(ctx context.Context, db *sql.DB, messageIDs []int64, mode string) ([]Gap, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	switch mode {
	case "chronological":
		return detectChronological(ctx, db, messageIDs)
	case "thematic":
		return detectThematic(ctx, db, messageIDs)
	case "participant":
		return detectParticipant(ctx, db, messageIDs)
	default:
		return nil, fmt.Errorf("unknown mode %q: must be chronological, thematic, or participant", mode)
	}
}

// ---------------------------------------------------------------------------
// Chronological mode
// ---------------------------------------------------------------------------

var dateFmts = []string{
	time.RFC3339,                // "2024-01-01T03:20:13Z" or "+00:00"
	"2006-01-02 15:04:05Z07:00", // "2024-01-01 03:20:13+00:00" (SQLite storage format)
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

func parseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}
	for _, f := range dateFmts {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date %q", s)
}

type msgRow struct {
	id      int64
	sentAt  time.Time
	subject string
}

func detectChronological(ctx context.Context, db *sql.DB, ids []int64) ([]Gap, error) {
	placeholders, args := buildInClause(ids)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, COALESCE(sent_at, ''), COALESCE(subject, '')
		FROM messages
		WHERE id IN (%s)
		ORDER BY sent_at ASC
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("chronological query: %w", err)
	}
	defer rows.Close()

	var msgs []msgRow
	for rows.Next() {
		var id int64
		var sentStr, subject string
		if err := rows.Scan(&id, &sentStr, &subject); err != nil {
			return nil, err
		}
		t, err := parseDate(sentStr)
		if err != nil || t.IsZero() {
			continue
		}
		msgs = append(msgs, msgRow{id: id, sentAt: t, subject: subject})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(msgs) < 2 {
		return nil, nil
	}

	var out []Gap
	for i := 1; i < len(msgs); i++ {
		spanDays := msgs[i].sentAt.Sub(msgs[i-1].sentAt).Hours() / 24
		if spanDays < defaultChronologicalGapDays {
			continue
		}
		days := int(math.Round(spanDays))
		severity := "medium"
		if days > 30 {
			severity = "high"
		}
		kind := chronologicalKind(msgs[i-1].subject, msgs[i].subject, days)
		hints := buildChronologicalHints(
			msgs[i-1].subject, msgs[i].subject,
			msgs[i-1].sentAt, msgs[i].sentAt,
		)
		out = append(out, Gap{
			Kind: kind,
			Description: fmt.Sprintf(
				"%d-day gap between [msg:%d] (%s) and [msg:%d] (%s)",
				days,
				msgs[i-1].id, msgs[i-1].sentAt.Format("2006-01-02"),
				msgs[i].id, msgs[i].sentAt.Format("2006-01-02"),
			),
			AnchorMessageIDs: []int64{msgs[i-1].id, msgs[i].id},
			SearchHints:      hints,
			Severity:         severity,
		})
	}
	return out, nil
}

var chronologicalImpactTerms = []string{
	"cancel", "cancellation", "reschedule", "delay", "late", "blocked",
	"declined", "failed", "refund", "charge", "billing", "invoice", "payment",
	"error", "issue", "problem", "complaint", "escalat", "urgent", "outage",
	"termination", "terminate", "depart", "departure", "switch", "transfer",
	"migration", "pause", "hold", "dispute", "compromise", "fraud",
}

func chronologicalKind(beforeSubj, afterSubj string, days int) string {
	if days >= 60 {
		return "chronological_impact"
	}
	subj := strings.ToLower(beforeSubj + " " + afterSubj)
	for _, term := range chronologicalImpactTerms {
		if strings.Contains(subj, term) {
			return "chronological_impact"
		}
	}
	return "chronological_continuity"
}

func buildChronologicalHints(beforeSubj, afterSubj string, start, end time.Time) []string {
	dateRange := fmt.Sprintf("after:%s before:%s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	var hints []string
	if terms := keyTerms(beforeSubj, 3); len(terms) > 0 {
		hints = append(hints, strings.Join(terms, " ")+" "+dateRange)
	}
	if terms := keyTerms(afterSubj, 3); len(terms) > 0 {
		h := strings.Join(terms, " ") + " " + dateRange
		if len(hints) == 0 || hints[0] != h {
			hints = append(hints, h)
		}
	}
	if len(hints) == 0 {
		hints = []string{dateRange}
	}
	if len(hints) > 3 {
		hints = hints[:3]
	}
	return hints
}

var subjectStopwords = map[string]bool{
	"re": true, "fwd": true, "fw": true, "the": true, "and": true, "for": true,
	"is": true, "in": true, "a": true, "an": true, "of": true, "to": true, "on": true,
	"with": true, "from": true, "your": true, "our": true, "we": true, "as": true,
	"it": true, "at": true, "be": true, "by": true, "has": true, "have": true,
	"will": true, "are": true, "was": true, "not": true, "that": true, "this": true,
}

// keyTerms returns up to n significant words from a subject line.
func keyTerms(subject string, n int) []string {
	words := strings.Fields(strings.ToLower(subject))
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		w = strings.Trim(w, ".,!?:;\"'()[]{}")
		if len(w) < 3 || subjectStopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == n {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Thematic mode (TF-IDF K-means)
// ---------------------------------------------------------------------------

type thematicDoc struct {
	id     int64
	text   string
	vector map[string]float64
}

type thematicCluster struct {
	label      string
	messageIDs []int64
	topTerms   []string
}

func detectThematic(ctx context.Context, db *sql.DB, ids []int64) ([]Gap, error) {
	docs, err := loadThematicDocs(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	// Need at least thematicMinClusterSize docs to form a meaningful analysis.
	if len(docs) < thematicMinClusterSize {
		return nil, nil
	}
	clusters := kMeansClusters(docs)

	var out []Gap
	for _, c := range clusters {
		if len(c.messageIDs) >= thematicMinClusterSize {
			continue
		}
		severity := "medium"
		if len(c.messageIDs) == 1 {
			severity = "high"
		}
		hints := c.topTerms
		if len(hints) > 3 {
			hints = hints[:3]
		}
		out = append(out, Gap{
			Kind: "thematic",
			Description: fmt.Sprintf(
				"thin cluster %q has only %d message(s); may be missing related content",
				c.label, len(c.messageIDs),
			),
			AnchorMessageIDs: c.messageIDs,
			SearchHints:      hints,
			Severity:         severity,
		})
	}
	return out, nil
}

func loadThematicDocs(ctx context.Context, db *sql.DB, ids []int64) ([]thematicDoc, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders, args := buildInClause(ids)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id, COALESCE(m.subject, ''), COALESCE(m.snippet, ''), COALESCE(mb.body_text, '')
		FROM messages m
		LEFT JOIN message_bodies mb ON mb.message_id = m.id
		WHERE m.id IN (%s)
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("thematic docs query: %w", err)
	}
	defer rows.Close()

	var docs []thematicDoc
	for rows.Next() {
		var id int64
		var subject, snippet, body string
		if err := rows.Scan(&id, &subject, &snippet, &body); err != nil {
			return nil, err
		}
		text := strings.TrimSpace(subject + " " + snippet + " " + body)
		if len(text) > 4000 {
			text = text[:4000]
		}
		docs = append(docs, thematicDoc{id: id, text: text})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].id < docs[j].id })
	return docs, nil
}

var tokenRe = regexp.MustCompile(`[a-z0-9][a-z0-9_\-]{2,}`)

var thematicStopwords = map[string]bool{
	"about": true, "after": true, "also": true, "and": true, "are": true, "because": true,
	"been": true, "but": true, "can": true, "com": true, "from": true, "has": true,
	"have": true, "her": true, "his": true, "http": true, "https": true, "into": true,
	"not": true, "our": true, "out": true, "the": true, "their": true, "them": true,
	"then": true, "there": true, "this": true, "that": true, "was": true, "were": true,
	"with": true, "you": true, "your": true,
}

func tokenize(text string) []string {
	raw := tokenRe.FindAllString(strings.ToLower(text), -1)
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if !thematicStopwords[t] {
			out = append(out, t)
		}
	}
	return out
}

func kMeansClusters(docs []thematicDoc) []thematicCluster {
	if len(docs) == 0 {
		return nil
	}
	k := int(math.Round(math.Sqrt(float64(len(docs)))))
	if k < 1 {
		k = 1
	}
	if k > 6 {
		k = 6
	}
	if k > len(docs) {
		k = len(docs)
	}

	// Build TF-IDF vectors.
	df := map[string]int{}
	docTerms := make([]map[string]int, len(docs))
	for i, d := range docs {
		counts := map[string]int{}
		for _, t := range tokenize(d.text) {
			counts[t]++
		}
		docTerms[i] = counts
		for t := range counts {
			df[t]++
		}
	}
	for i := range docs {
		vec := map[string]float64{}
		for t, count := range docTerms[i] {
			idf := math.Log(1 + float64(len(docs))/(1+float64(df[t])))
			vec[t] = float64(count) * idf
		}
		docs[i].vector = normalizeVec(vec)
	}

	// Initialize centroids from the first k documents (sorted by id).
	centroids := make([]map[string]float64, k)
	for i := 0; i < k; i++ {
		centroids[i] = copyVec(docs[i].vector)
	}

	assignments := make([]int, len(docs))
	for iter := 0; iter < 8; iter++ {
		for i, d := range docs {
			best, bestScore := 0, -1.0
			for c, centroid := range centroids {
				score := dotVec(d.vector, centroid)
				if score > bestScore {
					bestScore = score
					best = c
				}
			}
			assignments[i] = best
		}
		centroids = recomputeCentroids(docs, assignments, k)
	}

	grouped := make([][]thematicDoc, k)
	for i, d := range docs {
		grouped[assignments[i]] = append(grouped[assignments[i]], d)
	}

	var out []thematicCluster
	for _, group := range grouped {
		if len(group) == 0 {
			continue
		}
		var msgIDs []int64
		weights := map[string]float64{}
		for _, d := range group {
			msgIDs = append(msgIDs, d.id)
			for term, w := range d.vector {
				weights[term] += w
			}
		}
		sort.Slice(msgIDs, func(i, j int) bool { return msgIDs[i] < msgIDs[j] })
		terms := topTerms(weights, 4)
		label := "unlabeled"
		if len(terms) > 0 {
			n := 3
			if len(terms) < n {
				n = len(terms)
			}
			label = strings.Join(terms[:n], " ")
		}
		out = append(out, thematicCluster{label: label, messageIDs: msgIDs, topTerms: terms})
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i].messageIDs) > len(out[j].messageIDs) })
	return out
}

func normalizeVec(v map[string]float64) map[string]float64 {
	var norm float64
	for _, w := range v {
		norm += w * w
	}
	if norm == 0 {
		return v
	}
	norm = math.Sqrt(norm)
	for k, w := range v {
		v[k] = w / norm
	}
	return v
}

func copyVec(v map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(v))
	for k, w := range v {
		out[k] = w
	}
	return out
}

func dotVec(a, b map[string]float64) float64 {
	if len(a) > len(b) {
		a, b = b, a
	}
	var score float64
	for k, av := range a {
		score += av * b[k]
	}
	return score
}

func recomputeCentroids(docs []thematicDoc, assignments []int, k int) []map[string]float64 {
	next := make([]map[string]float64, k)
	counts := make([]int, k)
	for i := 0; i < k; i++ {
		next[i] = map[string]float64{}
	}
	for i, d := range docs {
		c := assignments[i]
		counts[c]++
		for term, w := range d.vector {
			next[c][term] += w
		}
	}
	for c := 0; c < k; c++ {
		if counts[c] == 0 && c < len(docs) {
			next[c] = copyVec(docs[c].vector)
			continue
		}
		for term, w := range next[c] {
			next[c][term] = w / float64(counts[c])
		}
	}
	return next
}

func topTerms(weights map[string]float64, n int) []string {
	type kv struct {
		k string
		v float64
	}
	pairs := make([]kv, 0, len(weights))
	for k, v := range weights {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].v > pairs[j].v })
	out := make([]string, 0, n)
	for _, p := range pairs {
		out = append(out, p.k)
		if len(out) == n {
			break
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Participant mode
// ---------------------------------------------------------------------------

// emailRe matches most common email address patterns in plain text.
var emailRe = regexp.MustCompile(`\b[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}\b`)

func detectParticipant(ctx context.Context, db *sql.DB, ids []int64) ([]Gap, error) {
	placeholders, args := buildInClause(ids)

	// Step 1: collect all participant emails that already have messages in bundle.
	bundleEmails := map[string]bool{}

	senderRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT LOWER(COALESCE(p.email_address, ''))
		FROM messages m
		LEFT JOIN participants p ON p.id = m.sender_id
		WHERE m.id IN (%s) AND COALESCE(p.email_address, '') <> ''
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("participant sender query: %w", err)
	}
	defer senderRows.Close()
	for senderRows.Next() {
		var e string
		if err := senderRows.Scan(&e); err != nil {
			return nil, err
		}
		bundleEmails[e] = true
	}
	if err := senderRows.Err(); err != nil {
		return nil, err
	}

	recipRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT LOWER(COALESCE(p.email_address, ''))
		FROM message_recipients mr
		LEFT JOIN participants p ON p.id = mr.participant_id
		WHERE mr.message_id IN (%s) AND COALESCE(p.email_address, '') <> ''
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("participant recipient query: %w", err)
	}
	defer recipRows.Close()
	for recipRows.Next() {
		var e string
		if err := recipRows.Scan(&e); err != nil {
			return nil, err
		}
		bundleEmails[e] = true
	}
	if err := recipRows.Err(); err != nil {
		return nil, err
	}

	// Step 2: extract email addresses mentioned in message bodies.
	bodyRows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id, COALESCE(mb.body_text, '')
		FROM messages m
		LEFT JOIN message_bodies mb ON mb.message_id = m.id
		WHERE m.id IN (%s) AND COALESCE(mb.body_text, '') <> ''
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("participant body query: %w", err)
	}
	defer bodyRows.Close()

	// referenced maps email → list of message IDs where it was mentioned.
	referenced := map[string][]int64{}
	for bodyRows.Next() {
		var msgID int64
		var body string
		if err := bodyRows.Scan(&msgID, &body); err != nil {
			return nil, err
		}
		for _, match := range emailRe.FindAllString(body, -1) {
			lower := strings.ToLower(match)
			if !bundleEmails[lower] {
				referenced[lower] = appendUniq(referenced[lower], msgID)
			}
		}
	}
	if err := bodyRows.Err(); err != nil {
		return nil, err
	}

	// Step 3: emit a gap for each email referenced but absent from bundle.
	type emailAnchor struct {
		email     string
		anchorIDs []int64
	}
	ordered := make([]emailAnchor, 0, len(referenced))
	for email, anchorIDs := range referenced {
		ordered = append(ordered, emailAnchor{email, anchorIDs})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].email < ordered[j].email })

	var out []Gap
	for _, ea := range ordered {
		out = append(out, Gap{
			Kind: "participant",
			Description: fmt.Sprintf(
				"%q is referenced in message bodies but has no direct messages in the bundle",
				ea.email,
			),
			AnchorMessageIDs: ea.anchorIDs,
			SearchHints:      []string{fmt.Sprintf("from:%s", ea.email), fmt.Sprintf("to:%s", ea.email)},
			Severity:         "medium",
		})
	}
	return out, nil
}

func appendUniq(ids []int64, id int64) []int64 {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func buildInClause(ids []int64) (string, []any) {
	parts := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		parts[i] = "?"
		args[i] = id
	}
	return strings.Join(parts, ","), args
}
