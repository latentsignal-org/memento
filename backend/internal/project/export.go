package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ProjectPageReport struct {
	ProjectID    int64                `json:"project_id"`
	Slug         string               `json:"slug"`
	Name         string               `json:"name"`
	Aliases      []string             `json:"aliases"`
	Status       string               `json:"status"`
	StartedAt    *string              `json:"started_at"`
	UpdatedAt    string               `json:"updated_at"`
	Members      []ProjectMember      `json:"members"`
	MessageCount int                  `json:"message_count"`
	DateRange    ProjectDateRange     `json:"date_range"`
	Timeline     []ExportTimelineItem `json:"timeline"`
	Narrative    ExportNarrative      `json:"narrative"`
}

// ProjectSummary is the compact row used by the /api/projects index endpoint.
type ProjectSummary struct {
	ProjectID      int64    `json:"project_id"`
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Aliases        []string `json:"aliases"`
	Status         string   `json:"status"`
	StartedAt      string   `json:"started_at,omitempty"`
	MessageCount   int64    `json:"message_count"`
	SummaryExcerpt string   `json:"summary_excerpt,omitempty"`
}

type ProjectDateRange struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

type ExportTimelineItem struct {
	MessageID         int64  `json:"message_id"`
	Date              string `json:"date"`
	Subject           string `json:"subject"`
	FromCanonicalName string `json:"from_canonical_name"`
	FromEmail         string `json:"from_email"`
	Direction         string `json:"direction"`
	Snippet           string `json:"snippet"`
	BodyText          string `json:"body_text"`
}

// NarrativePhase / NarrativeFrictionPoint are the JSON shapes the agent
// emits as the `content` of the corresponding write_section calls. Kept
// here (rather than in llm.go which was deleted in Phase 3) because the
// frontend's project page reads these directly.
type NarrativePhase struct {
	Title            string  `json:"title"`
	DateRange        string  `json:"date_range"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

type NarrativeFrictionPoint struct {
	Text             string  `json:"text"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
}

type ExportNarrative struct {
	Summary              string                   `json:"summary"`
	Phases               []NarrativePhase         `json:"phases"`
	FrictionPoints       []NarrativeFrictionPoint `json:"friction_points"`
	CurrentUnderstanding string                   `json:"current_understanding"`
}

func formatDateOnly(datetime string) string {
	if len(datetime) >= 10 {
		return datetime[:10]
	}
	return datetime
}

// BuildProjectReport assembles a ProjectPageReport for one project slug —
// joins members, message bundle, timeline, and narrative sections — without
// writing to disk. Shared by the file-exporter and the HTTP read endpoint.
func BuildProjectReport(ctx context.Context, db *sql.DB, slug string) (ProjectPageReport, error) {
	p, err := GetProjectBySlug(ctx, db, slug)
	if err != nil {
		return ProjectPageReport{}, fmt.Errorf("lookup project: %w", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT pm.person_id, mp.canonical_name, mp.primary_email, pm.role
		FROM memento_project_member pm
		JOIN memento_person mp ON mp.id = pm.person_id
		WHERE pm.project_id = ?
	`, p.ID)
	if err != nil {
		return ProjectPageReport{}, fmt.Errorf("query members: %w", err)
	}
	defer rows.Close()

	var members []ProjectMember
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.PersonID, &m.CanonicalName, &m.PrimaryEmail, &m.Role); err != nil {
			return ProjectPageReport{}, err
		}
		m.Slug = PersonSlug(m.CanonicalName)
		members = append(members, m)
	}

	bundle, err := GetProjectBundle(ctx, db, p.ID, "full")
	if err != nil {
		return ProjectPageReport{}, fmt.Errorf("get message bundle: %w", err)
	}

	timeline := []ExportTimelineItem{}
	var firstDate, lastDate string
	for _, bMsg := range bundle {
		dateStr := formatDateOnly(bMsg.Date)
		if firstDate == "" || (dateStr != "" && dateStr < firstDate) {
			firstDate = dateStr
		}
		if lastDate == "" || (dateStr != "" && dateStr > lastDate) {
			lastDate = dateStr
		}

		timeline = append(timeline, ExportTimelineItem{
			MessageID:         bMsg.MessageID,
			Date:              dateStr,
			Subject:           bMsg.Subject,
			FromCanonicalName: bMsg.SenderCanonicalName,
			FromEmail:         bMsg.SenderPrimaryEmail,
			Direction:         bMsg.Direction,
			Snippet:           bMsg.Snippet,
			BodyText:          bMsg.BodyText,
		})
	}

	var narrative ExportNarrative
	narrativeRows, err := db.QueryContext(ctx, `
		SELECT section, content FROM memento_project_narrative
		WHERE project_id = ?
	`, p.ID)
	if err != nil {
		return ProjectPageReport{}, fmt.Errorf("query narrative: %w", err)
	}
	defer narrativeRows.Close()

	for narrativeRows.Next() {
		var section, content string
		if err := narrativeRows.Scan(&section, &content); err != nil {
			return ProjectPageReport{}, err
		}
		switch section {
		case "summary":
			narrative.Summary = content
		case "current_understanding":
			narrative.CurrentUnderstanding = content
		case "phases":
			_ = json.Unmarshal([]byte(content), &narrative.Phases)
		case "friction_points":
			_ = json.Unmarshal([]byte(content), &narrative.FrictionPoints)
		}
	}

	return ProjectPageReport{
		ProjectID:    p.ID,
		Slug:         p.Slug,
		Name:         p.Name,
		Aliases:      p.Aliases,
		Status:       p.Status,
		StartedAt:    p.StartedAt,
		UpdatedAt:    p.UpdatedAt,
		Members:      members,
		MessageCount: len(bundle),
		DateRange: ProjectDateRange{
			First: firstDate,
			Last:  lastDate,
		},
		Timeline:  timeline,
		Narrative: narrative,
	}, nil
}

// ListProjectsSummary returns a compact list view for the projects index page.
func ListProjectsSummary(ctx context.Context, db *sql.DB) ([]ProjectSummary, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.name, p.aliases, p.status, p.started_at,
		       (SELECT COUNT(*) FROM memento_project_message m WHERE m.project_id = p.id) AS msg_count,
		       (SELECT MAX(content) FROM memento_project_narrative n WHERE n.project_id = p.id AND n.section = 'summary') AS summary_excerpt
		FROM memento_project p
		WHERE p.dismissed_at IS NULL
		ORDER BY p.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectSummary
	for rows.Next() {
		var s ProjectSummary
		var startedAt, aliases, summary sql.NullString
		if err := rows.Scan(&s.ProjectID, &s.Slug, &s.Name, &aliases, &s.Status, &startedAt, &s.MessageCount, &summary); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			s.StartedAt = startedAt.String
		}
		if aliases.Valid {
			_ = json.Unmarshal([]byte(aliases.String), &s.Aliases)
		}
		if summary.Valid {
			// Trim to a single-paragraph excerpt for index display.
			s.SummaryExcerpt = summary.String
			if len(s.SummaryExcerpt) > 280 {
				s.SummaryExcerpt = s.SummaryExcerpt[:280] + "..."
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func ExportProjectPage(ctx context.Context, db *sql.DB, slug string, outPath string) error {
	report, err := BuildProjectReport(ctx, db, slug)
	if err != nil {
		return err
	}
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("serialize JSON: %w", err)
	}
	fmt.Printf("Successfully exported project page report to %s\n", outPath)
	return nil
}
