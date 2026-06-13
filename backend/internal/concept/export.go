package concept

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type ConceptPageReport struct {
	ConceptID    int64                `json:"concept_id"`
	Slug         string               `json:"slug"`
	Name         string               `json:"name"`
	Scope        string               `json:"scope_description"`
	Status       string               `json:"status"`
	SeedKeywords []string             `json:"seed_keywords"`
	MessageCount int                  `json:"message_count"`
	DateRange    ConceptDateRange     `json:"date_range"`
	SourceMap    ConceptSourceMap     `json:"source_map"`
	Timeline     []ExportTimelineItem `json:"timeline"`
	Narrative    ExportNarrative      `json:"narrative"`
}

type ConceptDateRange struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

type ConceptSourceMap struct {
	People      []PersonContribution     `json:"people"`
	Newsletters []NewsletterContribution `json:"newsletters"`
}

type PersonContribution struct {
	PersonID      int64  `json:"person_id"`
	CanonicalName string `json:"canonical_name"`
	PrimaryEmail  string `json:"primary_email"`
	Slug          string `json:"slug"`
	ProfileSlug   string `json:"profile_slug,omitempty"`
	HasProfile    bool   `json:"has_profile"`
	Contributions int    `json:"contributions"`
}

type NewsletterContribution struct {
	Slug          string `json:"slug"`
	DisplayName   string `json:"display_name"`
	SenderEmail   string `json:"sender_email"`
	Contributions int    `json:"contributions"`
}

type ExportTimelineItem struct {
	MessageID         int64  `json:"message_id"`
	Date              string `json:"date"`
	Subject           string `json:"subject"`
	FromCanonicalName string `json:"from_canonical_name"`
	Snippet           string `json:"snippet"`
	IsNewsletter      bool   `json:"is_newsletter"`
	NewsletterSlug    string `json:"newsletter_slug,omitempty"`
}

type ExportNarrative struct {
	ScopeSummary          string       `json:"scope_summary"`
	DistilledInsights     []LLMInsight `json:"distilled_insights"`
	EvolvingUnderstanding string       `json:"evolving_understanding"`
}

type LLMInsight struct {
	Title            string  `json:"title"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

type ConceptIndexEntry struct {
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Scope        string `json:"scope_description"`
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	HasNarrative bool   `json:"has_narrative"`
}

func formatDateOnly(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// BuildConceptReport assembles the full ConceptPageReport without writing to
// disk. Shared by the file-exporter and the HTTP read endpoint.
func BuildConceptReport(ctx context.Context, db *sql.DB, slug string) (ConceptPageReport, error) {
	c, err := GetConceptBySlug(ctx, db, slug)
	if err != nil {
		return ConceptPageReport{}, fmt.Errorf("lookup concept: %w", err)
	}

	bundle, err := GetConceptBundle(ctx, db, c.ID, "full")
	if err != nil {
		return ConceptPageReport{}, fmt.Errorf("get bundle: %w", err)
	}

	// Source map: people contributions
	peopleRows, err := db.QueryContext(ctx, `
		SELECT mp.id, mp.canonical_name, mp.primary_email, COALESCE(pr.slug, '') AS profile_slug, COUNT(*) AS n
		FROM memento_concept_message mcm
		JOIN messages m ON m.id = mcm.message_id
		JOIN participants p ON p.id = m.sender_id
		JOIN memento_person_email mpe ON mpe.email_address = lower(p.email_address)
		JOIN memento_person mp ON mp.id = mpe.person_id
		LEFT JOIN memento_people_report pr ON pr.person_id = mp.id
		WHERE mcm.concept_id = ?
		  AND NOT EXISTS (
		    SELECT 1 FROM memento_newsletter_source mns
		    WHERE lower(mns.sender_email) = lower(p.email_address)
		  )
		GROUP BY mp.id, mp.canonical_name, mp.primary_email, pr.slug
		ORDER BY n DESC
		LIMIT 10
	`, c.ID)
	if err != nil {
		return ConceptPageReport{}, fmt.Errorf("query people contributions: %w", err)
	}
	defer peopleRows.Close()
	var people []PersonContribution
	for peopleRows.Next() {
		var pc PersonContribution
		if err := peopleRows.Scan(&pc.PersonID, &pc.CanonicalName, &pc.PrimaryEmail, &pc.ProfileSlug, &pc.Contributions); err != nil {
			return ConceptPageReport{}, err
		}
		pc.HasProfile = pc.ProfileSlug != ""
		pc.Slug = pc.ProfileSlug
		people = append(people, pc)
	}

	// Source map: newsletter contributions
	nlRows, err := db.QueryContext(ctx, `
		SELECT mns.slug, mns.display_name, mns.sender_email, COUNT(*) AS n
		FROM memento_concept_message mcm
		JOIN messages m ON m.id = mcm.message_id
		JOIN participants p ON p.id = m.sender_id
		JOIN memento_newsletter_source mns ON lower(mns.sender_email) = lower(p.email_address)
		WHERE mcm.concept_id = ?
		GROUP BY mns.slug, mns.display_name, mns.sender_email
		ORDER BY n DESC
		LIMIT 12
	`, c.ID)
	if err != nil {
		return ConceptPageReport{}, fmt.Errorf("query newsletter contributions: %w", err)
	}
	defer nlRows.Close()
	var newsletters []NewsletterContribution
	for nlRows.Next() {
		var nc NewsletterContribution
		if err := nlRows.Scan(&nc.Slug, &nc.DisplayName, &nc.SenderEmail, &nc.Contributions); err != nil {
			return ConceptPageReport{}, err
		}
		newsletters = append(newsletters, nc)
	}

	// Timeline (truncated to most-recent 50 for the JSON; bundle could be larger)
	var timeline []ExportTimelineItem
	var firstDate, lastDate string
	for _, b := range bundle {
		d := formatDateOnly(b.Date)
		if firstDate == "" || (d != "" && d < firstDate) {
			firstDate = d
		}
		if lastDate == "" || (d != "" && d > lastDate) {
			lastDate = d
		}
		timeline = append(timeline, ExportTimelineItem{
			MessageID:         b.MessageID,
			Date:              d,
			Subject:           b.Subject,
			FromCanonicalName: b.SenderCanonicalName,
			Snippet:           b.Snippet,
			IsNewsletter:      b.IsNewsletter,
			NewsletterSlug:    b.NewsletterSlug,
		})
	}
	// Reverse timeline → most recent first; cap at 50.
	sort.Slice(timeline, func(i, j int) bool { return timeline[i].Date > timeline[j].Date })
	if len(timeline) > 50 {
		timeline = timeline[:50]
	}

	// Narrative sections
	var narrative ExportNarrative
	nrows, err := db.QueryContext(ctx, `
		SELECT section, content FROM memento_concept_narrative WHERE concept_id = ?
	`, c.ID)
	if err != nil {
		return ConceptPageReport{}, fmt.Errorf("query narrative: %w", err)
	}
	defer nrows.Close()
	for nrows.Next() {
		var section, content string
		if err := nrows.Scan(&section, &content); err != nil {
			return ConceptPageReport{}, err
		}
		switch section {
		case "scope_summary":
			narrative.ScopeSummary = content
		case "evolving_understanding":
			narrative.EvolvingUnderstanding = content
		case "distilled_insights":
			_ = json.Unmarshal([]byte(content), &narrative.DistilledInsights)
		}
	}

	return ConceptPageReport{
		ConceptID:    c.ID,
		Slug:         c.Slug,
		Name:         c.Name,
		Scope:        c.ScopeDescription,
		Status:       c.Status,
		SeedKeywords: c.SeedKeywords,
		MessageCount: len(bundle),
		DateRange:    ConceptDateRange{First: firstDate, Last: lastDate},
		SourceMap:    ConceptSourceMap{People: people, Newsletters: newsletters},
		Timeline:     timeline,
		Narrative:    narrative,
	}, nil
}

// BuildConceptIndex returns the list of all declared concepts.
func BuildConceptIndex(ctx context.Context, db *sql.DB) ([]ConceptIndexEntry, error) {
	concepts, err := ListConcepts(ctx, db)
	if err != nil {
		return nil, err
	}
	entries := make([]ConceptIndexEntry, 0, len(concepts))
	for _, c := range concepts {
		var msgCount int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_concept_message WHERE concept_id = ?`, c.ID).Scan(&msgCount)
		var narrCount int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_concept_narrative WHERE concept_id = ?`, c.ID).Scan(&narrCount)
		entries = append(entries, ConceptIndexEntry{
			Slug:         c.Slug,
			Name:         c.Name,
			Scope:        c.ScopeDescription,
			Status:       c.Status,
			MessageCount: msgCount,
			HasNarrative: narrCount > 0,
		})
	}
	return entries, nil
}

func ExportConceptPage(ctx context.Context, db *sql.DB, slug string, outPath string) error {
	report, err := BuildConceptReport(ctx, db, slug)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	fmt.Printf("Exported concept %s -> %s\n", slug, outPath)
	return nil
}

// ExportConceptIndex writes a small index.json listing every concept and
// whether its narrative has been generated. The Next.js /concepts page
// consumes this — though the HTTP API uses BuildConceptIndex directly.
func ExportConceptIndex(ctx context.Context, db *sql.DB, outDir string) error {
	entries, err := BuildConceptIndex(ctx, db)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(outDir, "index.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Concepts []ConceptIndexEntry `json:"concepts"`
	}{entries})
}
