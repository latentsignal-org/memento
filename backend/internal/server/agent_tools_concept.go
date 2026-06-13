// Package server — token-gated agent-tool endpoints for the concept agent
// (Phase 4). These mirror the project-agent tools, but concept narratives are
// thematic: bundle loading, deterministic subject clustering, and section
// writes to memento_concept_narrative.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"memento/backend/internal/concept"
	"memento/backend/internal/refresh"
)

type getConceptBundleRequest struct {
	ConceptID int64  `json:"concept_id"`
	Detail    string `json:"detail"` // "full" | "index"
}

func (s *Server) handleAgentGetConceptBundle(w http.ResponseWriter, r *http.Request) {
	var req getConceptBundleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ConceptID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("concept_id is required"))
		return
	}
	if req.Detail == "" {
		req.Detail = "full"
	}
	bundle, err := concept.GetConceptBundle(r.Context(), s.db, req.ConceptID, req.Detail)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if bundle == nil {
		bundle = []concept.MessageBundleItem{}
	}
	writeJSON(w, http.StatusOK, bundle)
}

type writeConceptSectionRequest struct {
	ConceptID        int64   `json:"concept_id"`
	Section          string  `json:"section"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

var allowedConceptSections = map[string]bool{
	"scope_summary":          true,
	"distilled_insights":     true,
	"evolving_understanding": true,
}

func (s *Server) handleAgentWriteConceptSection(w http.ResponseWriter, r *http.Request) {
	var req writeConceptSectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.ConceptID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("concept_id is required"))
		return
	}
	if !allowedConceptSections[req.Section] {
		writeError(w, http.StatusBadRequest,
			fmt.Errorf("section %q not allowed (want one of scope_summary|distilled_insights|evolving_understanding)", req.Section))
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("content is required"))
		return
	}
	if len(req.SourceMessageIDs) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("source_message_ids must contain at least one message id"))
		return
	}
	if req.Section == "distilled_insights" {
		var insights []concept.LLMInsight
		if err := json.Unmarshal([]byte(req.Content), &insights); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("distilled_insights content must be a JSON array: %w", err))
			return
		}
	}

	result, err := saveConceptSection(r.Context(), s.db, req.ConceptID, req.Section, req.Content, req.SourceMessageIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func saveConceptSection(
	ctx context.Context, db *sql.DB,
	conceptID int64, section, content string, msgIDs []int64,
) (saveSectionResult, error) {
	var editedBy string
	err := db.QueryRowContext(ctx,
		`SELECT edited_by FROM memento_concept_narrative WHERE concept_id = ? AND section = ?`,
		conceptID, section,
	).Scan(&editedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return saveSectionResult{}, err
	}
	if editedBy == "user" {
		return saveSectionResult{
			OK:         true,
			Skipped:    true,
			SkipReason: "section is user-edited; agent writes preserved as draft only",
		}, nil
	}

	ids, _ := json.Marshal(msgIDs)
	_, err = db.ExecContext(ctx, `
		INSERT INTO memento_concept_narrative (concept_id, section, content, source_message_ids, generated_at, edited_by)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, 'llm')
		ON CONFLICT(concept_id, section) DO UPDATE SET
		    content = excluded.content,
		    source_message_ids = excluded.source_message_ids,
		    generated_at = CURRENT_TIMESTAMP,
		    edited_by = 'llm'`,
		conceptID, section, content, string(ids),
	)
	if err != nil {
		return saveSectionResult{}, err
	}
	return saveSectionResult{OK: true, CitationCnt: len(msgIDs)}, nil
}

type clusterMessagesRequest struct {
	MessageIDs []int64 `json:"message_ids"`
	K          int     `json:"k"`
}

type messageCluster struct {
	Label      string   `json:"label"`
	MessageIDs []int64  `json:"message_ids"`
	TopTerms   []string `json:"top_terms"`
}

type clusterDoc struct {
	ID     int64
	Text   string
	Vector map[string]float64
}

func (s *Server) handleAgentClusterMessages(w http.ResponseWriter, r *http.Request) {
	var req clusterMessagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	docs, err := loadClusterDocs(r.Context(), s.db, req.MessageIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	clusters := clusterDocs(docs, req.K)
	writeJSON(w, http.StatusOK, clusters)
}

func loadClusterDocs(ctx context.Context, db *sql.DB, ids []int64) ([]clusterDoc, error) {
	if len(ids) == 0 {
		return []clusterDoc{}, nil
	}
	seen := map[int64]bool{}
	var unique []int64
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return []clusterDoc{}, nil
	}

	placeholders := make([]string, len(unique))
	args := make([]any, len(unique))
	for i, id := range unique {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id, COALESCE(m.subject, ''), COALESCE(m.snippet, ''), COALESCE(mb.body_text, '')
		FROM messages m
		LEFT JOIN message_bodies mb ON mb.message_id = m.id
		WHERE m.id IN (%s)
	`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []clusterDoc
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
		docs = append(docs, clusterDoc{ID: id, Text: text})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	return docs, nil
}

var clusterTokenRe = regexp.MustCompile(`[a-z0-9][a-z0-9_\-]{2,}`)

var clusterStopwords = map[string]bool{
	"about": true, "after": true, "also": true, "and": true, "are": true, "because": true,
	"been": true, "but": true, "can": true, "com": true, "from": true, "has": true,
	"have": true, "her": true, "his": true, "http": true, "https": true, "into": true,
	"not": true, "our": true, "out": true, "the": true, "their": true, "them": true,
	"then": true, "there": true, "this": true, "that": true, "was": true, "were": true,
	"with": true, "you": true, "your": true,
}

func tokenizeClusterText(text string) []string {
	raw := clusterTokenRe.FindAllString(strings.ToLower(text), -1)
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		if clusterStopwords[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

func clusterDocs(docs []clusterDoc, k int) []messageCluster {
	if len(docs) == 0 {
		return []messageCluster{}
	}
	if k <= 0 {
		k = int(math.Round(math.Sqrt(float64(len(docs)))))
	}
	if k < 1 {
		k = 1
	}
	if k > 6 {
		k = 6
	}
	if k > len(docs) {
		k = len(docs)
	}

	df := map[string]int{}
	docTerms := make([]map[string]int, len(docs))
	for i, d := range docs {
		counts := map[string]int{}
		for _, t := range tokenizeClusterText(d.Text) {
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
		docs[i].Vector = normalizeVector(vec)
	}

	centroids := make([]map[string]float64, k)
	for i := 0; i < k; i++ {
		centroids[i] = copyVector(docs[i].Vector)
	}
	assignments := make([]int, len(docs))
	for iter := 0; iter < 8; iter++ {
		for i, d := range docs {
			bestIdx := 0
			bestScore := -1.0
			for c, centroid := range centroids {
				score := dot(d.Vector, centroid)
				if score > bestScore {
					bestScore = score
					bestIdx = c
				}
			}
			assignments[i] = bestIdx
		}
		centroids = recomputeCentroids(docs, assignments, k)
	}

	grouped := make([][]clusterDoc, k)
	for i, d := range docs {
		grouped[assignments[i]] = append(grouped[assignments[i]], d)
	}

	var out []messageCluster
	for _, group := range grouped {
		if len(group) == 0 {
			continue
		}
		var ids []int64
		weights := map[string]float64{}
		for _, d := range group {
			ids = append(ids, d.ID)
			for term, weight := range d.Vector {
				weights[term] += weight
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		terms := topTerms(weights, 4)
		label := "Unlabeled theme"
		if len(terms) > 0 {
			label = strings.Title(strings.Join(terms[:minInt(len(terms), 3)], " "))
		}
		out = append(out, messageCluster{Label: label, MessageIDs: ids, TopTerms: terms})
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i].MessageIDs) > len(out[j].MessageIDs) })
	return out
}

func normalizeVector(vec map[string]float64) map[string]float64 {
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		return vec
	}
	norm = math.Sqrt(norm)
	for k, v := range vec {
		vec[k] = v / norm
	}
	return vec
}

func copyVector(vec map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(vec))
	for k, v := range vec {
		out[k] = v
	}
	return out
}

func dot(a, b map[string]float64) float64 {
	if len(a) > len(b) {
		a, b = b, a
	}
	var score float64
	for k, av := range a {
		score += av * b[k]
	}
	return score
}

func recomputeCentroids(docs []clusterDoc, assignments []int, k int) []map[string]float64 {
	next := make([]map[string]float64, k)
	counts := make([]int, k)
	for i := 0; i < k; i++ {
		next[i] = map[string]float64{}
	}
	for i, d := range docs {
		c := assignments[i]
		counts[c]++
		for term, weight := range d.Vector {
			next[c][term] += weight
		}
	}
	for c := 0; c < k; c++ {
		if counts[c] == 0 && c < len(docs) {
			next[c] = copyVector(docs[c].Vector)
			continue
		}
		for term, weight := range next[c] {
			next[c][term] = weight / float64(counts[c])
		}
		next[c] = normalizeVector(next[c])
	}
	return next
}

func topTerms(weights map[string]float64, limit int) []string {
	type pair struct {
		Term   string
		Weight float64
	}
	var pairs []pair
	for term, weight := range weights {
		pairs = append(pairs, pair{Term: term, Weight: weight})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Weight == pairs[j].Weight {
			return pairs[i].Term < pairs[j].Term
		}
		return pairs[i].Weight > pairs[j].Weight
	})
	if limit > len(pairs) {
		limit = len(pairs)
	}
	out := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, pairs[i].Term)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (s *Server) handleAgentRefreshConcepts(w http.ResponseWriter, r *http.Request) {
	if _, err := refresh.RefreshConceptsReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
