package person

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const LinkSourceGraph = "graph"

type MergeSuggestionRow struct {
	ID             int64    `json:"id"`
	PersonAID      int64    `json:"person_a_id"`
	PersonBID      int64    `json:"person_b_id"`
	Sources        []string `json:"sources"`
	NameSimilarity float64  `json:"name_similarity"`
	TokenOverlap   float64  `json:"token_overlap"`
	SignatureScore float64  `json:"signature_score"`
	CombinedScore  float64  `json:"combined_score"`
	ScoresStale    bool     `json:"scores_stale"`
	Status         string   `json:"status"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
	ResolvedAt     string   `json:"resolved_at,omitempty"`
}

type MergeSuggestionInput struct {
	PersonAID      int64
	PersonBID      int64
	Sources        []string
	NameSimilarity float64
	TokenOverlap   float64
	SignatureScore float64
	CombinedScore  float64
	ScoresStale    bool
}

type ListMergeSuggestionOptions struct {
	Status string
	Sort   string
	Limit  int
}

func PersistResolveSuggestions(ctx context.Context, db *sql.DB, suggestions []ResolveSuggestion, clusterPersonIDs map[int]int64) (int, error) {
	var n int
	for _, suggestion := range suggestions {
		a := clusterPersonIDs[suggestion.ClusterA]
		b := clusterPersonIDs[suggestion.ClusterB]
		if a == 0 || b == 0 || a == b {
			continue
		}
		combined := suggestion.NameSimilarity
		if suggestion.TokenOverlap > combined {
			combined = suggestion.TokenOverlap
		}
		if err := UpsertMergeSuggestion(ctx, db, MergeSuggestionInput{
			PersonAID:      a,
			PersonBID:      b,
			Sources:        suggestion.Sources,
			NameSimilarity: suggestion.NameSimilarity,
			TokenOverlap:   suggestion.TokenOverlap,
			CombinedScore:  combined,
			ScoresStale:    true,
		}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func GenerateAndPersistGraphSuggestions(ctx context.Context, db *sql.DB, opts FindMergeOptions) ([]MergeCandidate, error) {
	candidates, err := FindMergeCandidates(ctx, db, opts)
	if err != nil {
		return nil, err
	}
	if err := PersistGraphMergeCandidates(ctx, db, candidates); err != nil {
		return nil, err
	}
	return candidates, nil
}

func PersistGraphMergeCandidates(ctx context.Context, db *sql.DB, candidates []MergeCandidate) error {
	for _, candidate := range candidates {
		if err := UpsertMergeSuggestion(ctx, db, MergeSuggestionInput{
			PersonAID:      candidate.FromID,
			PersonBID:      candidate.IntoID,
			Sources:        []string{LinkSourceGraph},
			NameSimilarity: candidate.NameScore,
			SignatureScore: candidate.SignatureScore,
			CombinedScore:  candidate.CombinedScore,
			ScoresStale:    false,
		}); err != nil {
			return err
		}
	}
	return nil
}

func UpsertMergeSuggestion(ctx context.Context, db *sql.DB, input MergeSuggestionInput) error {
	a, b := orderedPair(input.PersonAID, input.PersonBID)
	if a == 0 || b == 0 || a == b {
		return nil
	}
	input.PersonAID, input.PersonBID = a, b
	input.Sources = normalizeSources(input.Sources)
	if len(input.Sources) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	sourcesJSON, err := json.Marshal(input.Sources)
	if err != nil {
		return err
	}
	res, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO memento_merge_suggestion (
			person_a_id, person_b_id, sources, name_similarity, token_overlap,
			signature_score, combined_score, scores_stale, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)
	`, input.PersonAID, input.PersonBID, string(sourcesJSON), input.NameSimilarity, input.TokenOverlap,
		input.SignatureScore, input.CombinedScore, boolInt(input.ScoresStale), now, now)
	if err != nil {
		return err
	}
	if inserted, _ := res.RowsAffected(); inserted > 0 {
		return nil
	}

	existing, err := GetMergeSuggestionByPair(ctx, db, input.PersonAID, input.PersonBID)
	if err != nil {
		return err
	}
	if existing.Status != "pending" {
		return nil
	}
	mergedSources := mergeSourceLists(existing.Sources, input.Sources)
	mergedJSON, err := json.Marshal(mergedSources)
	if err != nil {
		return err
	}
	nameSimilarity := maxFloat(existing.NameSimilarity, input.NameSimilarity)
	tokenOverlap := maxFloat(existing.TokenOverlap, input.TokenOverlap)
	signatureScore := maxFloat(existing.SignatureScore, input.SignatureScore)
	combinedScore := maxFloat(existing.CombinedScore, input.CombinedScore)
	scoresStale := existing.ScoresStale && input.ScoresStale
	if input.SignatureScore > 0 || containsString(input.Sources, LinkSourceGraph) {
		scoresStale = false
	}
	_, err = db.ExecContext(ctx, `
		UPDATE memento_merge_suggestion
		SET sources = ?, name_similarity = ?, token_overlap = ?, signature_score = ?,
		    combined_score = ?, scores_stale = ?, updated_at = ?
		WHERE id = ? AND status = 'pending'
	`, string(mergedJSON), nameSimilarity, tokenOverlap, signatureScore, combinedScore, boolInt(scoresStale), now, existing.ID)
	return err
}

func GetMergeSuggestion(ctx context.Context, db *sql.DB, id int64) (MergeSuggestionRow, error) {
	return scanMergeSuggestion(db.QueryRowContext(ctx, `
		SELECT id, person_a_id, person_b_id, sources, name_similarity, token_overlap,
		       signature_score, combined_score, scores_stale, status, created_at, updated_at,
		       COALESCE(resolved_at, '')
		FROM memento_merge_suggestion
		WHERE id = ?
	`, id))
}

func GetMergeSuggestionByPair(ctx context.Context, db *sql.DB, personAID, personBID int64) (MergeSuggestionRow, error) {
	a, b := orderedPair(personAID, personBID)
	return scanMergeSuggestion(db.QueryRowContext(ctx, `
		SELECT id, person_a_id, person_b_id, sources, name_similarity, token_overlap,
		       signature_score, combined_score, scores_stale, status, created_at, updated_at,
		       COALESCE(resolved_at, '')
		FROM memento_merge_suggestion
		WHERE person_a_id = ? AND person_b_id = ?
	`, a, b))
}

func ListMergeSuggestions(ctx context.Context, db *sql.DB, opts ListMergeSuggestionOptions) ([]MergeSuggestionRow, error) {
	status := strings.TrimSpace(opts.Status)
	if status == "" {
		status = "pending"
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	orderBy := mergeSuggestionOrderBy(opts.Sort)
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, person_a_id, person_b_id, sources, name_similarity, token_overlap,
		       signature_score, combined_score, scores_stale, status, created_at, updated_at,
		       COALESCE(resolved_at, '')
		FROM memento_merge_suggestion
		WHERE status = ?
		ORDER BY %s
		LIMIT ?
	`, orderBy), status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MergeSuggestionRow
	for rows.Next() {
		row, err := scanMergeSuggestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func MarkMergeSuggestionResolved(ctx context.Context, db *sql.DB, id int64, status string) (MergeSuggestionRow, error) {
	if status != "accepted" && status != "rejected" {
		return MergeSuggestionRow{}, fmt.Errorf("invalid merge suggestion status %q", status)
	}
	res, err := db.ExecContext(ctx, `
		UPDATE memento_merge_suggestion
		SET status = ?, resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'
	`, status, id)
	if err != nil {
		return MergeSuggestionRow{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		existing, getErr := GetMergeSuggestion(ctx, db, id)
		if getErr != nil {
			return MergeSuggestionRow{}, getErr
		}
		return existing, fmt.Errorf("merge suggestion %d is already %s", id, existing.Status)
	}
	return GetMergeSuggestion(ctx, db, id)
}

func RejectPendingMergeSuggestionsForPerson(ctx context.Context, db *sql.DB, personID int64) error {
	_, err := db.ExecContext(ctx, `
		UPDATE memento_merge_suggestion
		SET status = 'rejected', resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE status = 'pending' AND (person_a_id = ? OR person_b_id = ?)
	`, personID, personID)
	return err
}

func scanMergeSuggestion(scanner interface {
	Scan(dest ...any) error
}) (MergeSuggestionRow, error) {
	var row MergeSuggestionRow
	var sourcesJSON string
	var scoresStale int
	err := scanner.Scan(
		&row.ID, &row.PersonAID, &row.PersonBID, &sourcesJSON, &row.NameSimilarity, &row.TokenOverlap,
		&row.SignatureScore, &row.CombinedScore, &scoresStale, &row.Status, &row.CreatedAt, &row.UpdatedAt,
		&row.ResolvedAt,
	)
	if err != nil {
		return row, err
	}
	_ = json.Unmarshal([]byte(sourcesJSON), &row.Sources)
	row.Sources = normalizeSources(row.Sources)
	row.ScoresStale = scoresStale != 0
	return row, nil
}

func mergeSuggestionOrderBy(sortKey string) string {
	switch sortKey {
	case "name_similarity":
		return "name_similarity DESC, combined_score DESC, updated_at DESC, id DESC"
	case "token_overlap":
		return "token_overlap DESC, combined_score DESC, updated_at DESC, id DESC"
	case "signature":
		return "signature_score DESC, combined_score DESC, updated_at DESC, id DESC"
	default:
		return "combined_score DESC, updated_at DESC, id DESC"
	}
}

func orderedPair(a, b int64) (int64, int64) {
	if a > b {
		return b, a
	}
	return a, b
}

func normalizeSources(sources []string) []string {
	set := map[string]bool{}
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		set[source] = true
	}
	out := make([]string, 0, len(set))
	for source := range set {
		out = append(out, source)
	}
	sort.Strings(out)
	return out
}

func mergeSourceLists(a, b []string) []string {
	return normalizeSources(append(append([]string{}, a...), b...))
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}
