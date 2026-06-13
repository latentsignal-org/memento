package people

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"memento/backend/internal/msgvault"
	"memento/backend/internal/person"
)

type CandidateOptions struct {
	Limit           int
	IncludeExcluded bool
	// Full classifies and returns the entire non-account identity universe
	// (every resolved person), persisting `excluded` rows alongside the
	// meaningful classes. This makes memento_people_candidates the canonical
	// classified ledger that the Excluded tab and the social-graph bot filter
	// depend on. When set, Limit and IncludeExcluded are ignored (treated as
	// unbounded / include). The CLI display path leaves it off and uses Limit.
	Full bool
}

type CandidateReport struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Database    string      `json:"database"`
	InboundOnly bool        `json:"inbound_only"`
	Candidates  []Candidate `json:"candidates"`
}

type Candidate struct {
	PersonID           int64   `json:"person_id"`
	CanonicalName      string  `json:"canonical_name"`
	PrimaryEmail       string  `json:"primary_email"`
	Domain             string  `json:"domain"`
	EmailCount         int64   `json:"email_count"`
	TotalMessages      int64   `json:"total_messages"`
	FromContactCount   int64   `json:"from_contact_count"`
	ToContactCount     int64   `json:"to_contact_count"`
	FirstMessageAt     *string `json:"first_message_at,omitempty"`
	LastMessageAt      *string `json:"last_message_at,omitempty"`
	BidirectionalScore float64 `json:"bidirectional_score"`
	Classification     string  `json:"classification"`
	ExclusionReason    string  `json:"exclusion_reason,omitempty"`
	SampleMessageIDs   []int64 `json:"sample_message_ids"`
}

// ErrResolverNotRun is returned when the candidate report is requested but the
// memento_person_email table is empty — i.e. `person-resolve --persist` has
// not been run yet. The candidate query joins on canonical persons, so
// without that table the report would be silently empty.
var ErrResolverNotRun = errors.New("memento_person_email is empty — run `memento person-resolve --persist` first")

func BuildCandidateReport(ctx context.Context, reader *msgvault.Reader, opts CandidateOptions) (CandidateReport, error) {
	if opts.Limit <= 0 {
		opts.Limit = 25
	}
	// Full mode classifies the entire universe and keeps excluded rows.
	includeExcluded := opts.IncludeExcluded || opts.Full

	var personEmailCount int64
	if err := reader.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_person_email`).Scan(&personEmailCount); err != nil {
		return CandidateReport{}, err
	}
	if personEmailCount == 0 {
		return CandidateReport{}, ErrResolverNotRun
	}

	// Deterministic bot signal (no-reply / role / newsletter senders). Computed
	// once and applied during classification so bots land as `excluded` with a
	// specific reason rather than leaking into the human candidate set.
	botReasons, err := person.BotPersonIDs(ctx, reader.DB())
	if err != nil {
		return CandidateReport{}, fmt.Errorf("bot detection: %w", err)
	}

	overrides, err := LoadClassificationOverrides(ctx, reader.DB())
	if err != nil {
		return CandidateReport{}, fmt.Errorf("classification overrides: %w", err)
	}

	// In Full mode fetch the whole universe; otherwise honor the requested limit.
	fetchLimit := opts.Limit
	if opts.Full {
		fetchLimit = 1 << 30
	}
	rows, err := reader.PeopleCandidateRows(ctx, fetchLimit, includeExcluded)
	if err != nil {
		return CandidateReport{}, err
	}
	hasOutbound, err := reader.HasOutboundMessages(ctx)
	if err != nil {
		return CandidateReport{}, err
	}

	report := CandidateReport{
		GeneratedAt: time.Now().UTC(),
		Database:    reader.Path(),
		InboundOnly: !hasOutbound,
	}
	for _, row := range rows {
		candidate := classify(row, report.InboundOnly, botReasons, overrides)
		if !includeExcluded && candidate.Classification == "excluded" {
			continue
		}
		report.Candidates = append(report.Candidates, candidate)
		if !opts.Full && len(report.Candidates) >= opts.Limit {
			break
		}
	}
	return report, nil
}

// PersistCandidateReport replaces the full contents of memento_people_candidates
// with the supplied report. Treat the table as a snapshot of the latest run, not
// a history log — if you need history, version it explicitly.
func PersistCandidateReport(ctx context.Context, db *sql.DB, report CandidateReport) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM memento_people_candidates"); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO memento_people_candidates (
			person_id,
			canonical_name,
			primary_email,
			domain,
			email_count,
			total_messages,
			from_contact_count,
			to_contact_count,
			first_message_at,
			last_message_at,
			bidirectional_score,
			classification,
			exclusion_reason,
			sample_message_ids,
			generated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, candidate := range report.Candidates {
		samples, err := json.Marshal(candidate.SampleMessageIDs)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			candidate.PersonID,
			candidate.CanonicalName,
			candidate.PrimaryEmail,
			candidate.Domain,
			candidate.EmailCount,
			candidate.TotalMessages,
			candidate.FromContactCount,
			candidate.ToContactCount,
			stringOrNil(candidate.FirstMessageAt),
			stringOrNil(candidate.LastMessageAt),
			candidate.BidirectionalScore,
			candidate.Classification,
			candidate.ExclusionReason,
			string(samples),
			report.GeneratedAt.Format(time.RFC3339),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func bidirectionalScore(a, b int64) float64 {
	if a == 0 && b == 0 {
		return 0
	}
	if a > b {
		return float64(b) / float64(a)
	}
	return float64(a) / float64(b)
}

// classify(row, inboundOnly) decides whether `row` is a meaningful-contact
// candidate. Inbound-only archives (no outbound messages observable) get a
// softer threshold because we can't measure bidirectional ratio.
//
// TODO: extend with the remaining heuristics from spec §2.2 — median body
// length, time-of-day variance, reply latency distribution.
func classify(row msgvault.PeopleCandidateRow, inboundOnly bool, botReasons map[int64]string, overrides map[int64]string) Candidate {
	score := bidirectionalScore(row.FromContactCount, row.ToContactCount)
	candidate := Candidate{
		PersonID:           row.PersonID,
		CanonicalName:      row.CanonicalName,
		PrimaryEmail:       row.PrimaryEmail,
		Domain:             row.Domain,
		EmailCount:         row.EmailCount,
		TotalMessages:      row.TotalMessages,
		FromContactCount:   row.FromContactCount,
		ToContactCount:     row.ToContactCount,
		FirstMessageAt:     row.FirstMessageAt,
		LastMessageAt:      row.LastMessageAt,
		BidirectionalScore: score,
		SampleMessageIDs:   row.SampleMessageIDs,
	}

	// Check manual user overrides first.
	if val, ok := overrides[row.PersonID]; ok {
		if val == "excluded" {
			candidate.Classification = "excluded"
			candidate.ExclusionReason = "manually excluded by user"
			return candidate
		} else if val == "human" {
			// Skip bot and automated exclusion checks and evaluate human classification.
			if inboundOnly && row.TotalMessages >= 10 {
				candidate.Classification = "candidate_inbound_only"
				return candidate
			}
			if row.TotalMessages >= 10 && score >= 0.10 {
				candidate.Classification = "candidate"
				return candidate
			}
			candidate.Classification = "weak_signal"
			candidate.ExclusionReason = "below meaningful-contact thresholds"
			return candidate
		}
	}

	// Deterministic bot signal takes precedence — its reason is more specific
	// than the classifier's own heuristics.
	if reason, ok := botReasons[row.PersonID]; ok {
		candidate.Classification = "excluded"
		candidate.ExclusionReason = reason
		return candidate
	}

	reason := exclusionReason(row, inboundOnly)
	if reason != "" {
		candidate.Classification = "excluded"
		candidate.ExclusionReason = reason
		return candidate
	}
	if inboundOnly && row.TotalMessages >= 10 {
		candidate.Classification = "candidate_inbound_only"
		candidate.ExclusionReason = "archive subset has no outbound messages"
		return candidate
	}
	if row.TotalMessages >= 10 && score >= 0.10 {
		candidate.Classification = "candidate"
		return candidate
	}
	candidate.Classification = "weak_signal"
	candidate.ExclusionReason = "below meaningful-contact thresholds"
	return candidate
}

func exclusionReason(row msgvault.PeopleCandidateRow, inboundOnly bool) string {
	email := strings.ToLower(row.PrimaryEmail)
	domain := strings.ToLower(row.Domain)
	name := strings.ToLower(row.CanonicalName)
	localPart := strings.Split(email, "@")[0]

	if email == "" {
		return "missing email address"
	}
	for _, marker := range []string{"noreply", "no-reply", "do-not-reply", "donotreply", "auto-reply", "autoresponse"} {
		if strings.Contains(localPart, marker) {
			return "system or no-reply address"
		}
	}
	if systemAddressPattern.MatchString(email) {
		return "system or no-reply address"
	}
	if newsletterDomainPattern.MatchString(domain) {
		return "newsletter or broadcast domain"
	}
	for _, marker := range []string{"newsletter", "notification", "weekly", "digest", "status", "focus", "amazon news"} {
		if strings.Contains(name, marker) {
			return "broadcast sender display name"
		}
	}
	// Generic role addresses (ask@, permits@, experience@, production@, programs@).
	// Real humans almost never sit on these mailboxes — they're shared role inboxes.
	if roleLocalPartPattern.MatchString(localPart) {
		return "generic role address"
	}
	if strings.HasPrefix(localPart, "list") || strings.HasPrefix(localPart, "admin") || localPart == "news" {
		return "broadcast sender display name"
	}
	// Plus-tagged broadcast/transactional addresses (messages+xyz@, alerts+xyz@).
	if plusTaggedBroadcastPattern.MatchString(localPart) {
		return "plus-tagged broadcast address"
	}
	if !inboundOnly && row.FromContactCount >= 10 && row.ToContactCount == 0 {
		return "unidirectional sender"
	}
	return ""
}

// LoadClassificationOverrides loads user classification overrides from the database.
func LoadClassificationOverrides(ctx context.Context, db *sql.DB) (map[int64]string, error) {
	out := map[int64]string{}
	rows, err := db.QueryContext(ctx, `SELECT person_id, classification_override FROM memento_classification_override`)
	if err != nil {
		// Table might not exist yet (e.g. before migration is run)
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var personID int64
		var val string
		if err := rows.Scan(&personID, &val); err != nil {
			return nil, err
		}
		out[personID] = val
	}
	return out, rows.Err()
}

func stringOrNil(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

var systemAddressPattern = regexp.MustCompile(`(?i)(^|[._+-])(no-?reply|donotreply|notification|notifications|alerts?|updates?|support|help|hello|team|info|billing|receipts?|newsletter|digest|weekly|marketing|noreply)([._+-]|@)`)
var newsletterDomainPattern = regexp.MustCompile(`(?i)(substack\.com|ghost\.io|morningbrew\.com|manning\.com|readwise\.io|monarch\.com|github\.com|ui\.dev|aihero\.dev|kevinpowell\.co|builtformars\.com|pycoders\.com|ben-evans\.com|smol\.ai|cooperpress\.com|thisweekinreact\.com|frontendmasters\.com|iximiuz\.com|turbotax\.intuit\.com)`)

// roleLocalPartPattern matches generic role mailboxes that almost never sit
// behind a real human (ask@, permits@, programs@, etc.). Anchored at start/end
// so we don't accidentally exclude `alexpermits@…`.
var roleLocalPartPattern = regexp.MustCompile(`^(ask|permits?|experience|production|programs?|orders?|sales|press|careers?|jobs|hr|legal|privacy|abuse|postmaster|webmaster|reservations|tickets|booking|bookings|enquiries|inquiries|enquiry|inquiry|contact|contactus|hello|hi|hey|info|members|membership|community|events?|service)([._+-].*)?$`)

// plusTaggedBroadcastPattern matches plus-tagged transactional addresses where
// the prefix itself is broadcast-y (messages+xyz@, alerts+xyz@, updates+xyz@).
var plusTaggedBroadcastPattern = regexp.MustCompile(`^(messages?|alerts?|notifications?|updates?|reminders?|digest|newsletter)\+`)
