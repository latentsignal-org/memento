// Package server — token-gated agent-tool endpoints for the general Memento dashboard router agent.
// (Phase 6). Exposes search and summary retrieval for people, projects, and concepts.
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ---- SEARCH STRUCTURES & ENDPOINTS ----

type searchRequest struct {
	Query string `json:"query"`
}

type personSearchItem struct {
	PersonID       int64  `json:"person_id"`
	CanonicalName  string `json:"canonical_name"`
	PrimaryEmail   string `json:"primary_email"`
	Domain         string `json:"domain"`
	TotalMessages  int64  `json:"total_messages"`
	Classification string `json:"classification"`
	Slug           string `json:"slug"`
}

func (s *Server) handleAgentSearchPersons(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		writeJSON(w, http.StatusOK, []personSearchItem{})
		return
	}

	likePat := "%" + q + "%"
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT person_id, canonical_name, primary_email, domain, total_messages, classification, slug
		FROM memento_people_report
		WHERE canonical_name LIKE ? OR primary_email LIKE ? OR EXISTS (
			SELECT 1 FROM memento_person_email pe
			WHERE pe.person_id = memento_people_report.person_id 
			  AND (pe.email_address LIKE ? OR pe.display_name LIKE ?)
		)
		ORDER BY total_messages DESC
		LIMIT 15
	`, likePat, likePat, likePat, likePat)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	items := []personSearchItem{}
	for rows.Next() {
		var item personSearchItem
		if err := rows.Scan(
			&item.PersonID, &item.CanonicalName, &item.PrimaryEmail, &item.Domain,
			&item.TotalMessages, &item.Classification, &item.Slug,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, items)
}

type projectSearchItem struct {
	ProjectID int64   `json:"project_id"`
	Slug      string  `json:"slug"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	StartedAt *string `json:"started_at"`
}

func (s *Server) handleAgentSearchProjects(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		writeJSON(w, http.StatusOK, []projectSearchItem{})
		return
	}

	likePat := "%" + q + "%"
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT project_id, slug, name, status, started_at
		FROM memento_projects_report
		WHERE name LIKE ? OR slug LIKE ?
		ORDER BY name ASC
		LIMIT 15
	`, likePat, likePat)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	items := []projectSearchItem{}
	for rows.Next() {
		var item projectSearchItem
		var startedAt sql.NullString
		if err := rows.Scan(&item.ProjectID, &item.Slug, &item.Name, &item.Status, &startedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if startedAt.Valid {
			item.StartedAt = new(startedAt.String)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, items)
}

type conceptSearchItem struct {
	ConceptID        int64  `json:"concept_id"`
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	ScopeDescription string `json:"scope_description"`
	MessageCount     int64  `json:"message_count"`
}

func (s *Server) handleAgentSearchConcepts(w http.ResponseWriter, r *http.Request) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		writeJSON(w, http.StatusOK, []conceptSearchItem{})
		return
	}

	likePat := "%" + q + "%"
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT concept_id, slug, name, status, scope_description, message_count
		FROM memento_concepts_report
		WHERE name LIKE ? OR slug LIKE ? OR scope_description LIKE ?
		ORDER BY name ASC
		LIMIT 15
	`, likePat, likePat, likePat)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	items := []conceptSearchItem{}
	for rows.Next() {
		var item conceptSearchItem
		if err := rows.Scan(
			&item.ConceptID, &item.Slug, &item.Name, &item.Status,
			&item.ScopeDescription, &item.MessageCount,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, items)
}

// ---- SUMMARY RETRIEVAL ENDPOINTS ----

type getPersonSummaryRequest struct {
	PersonID int64  `json:"person_id"`
	Slug     string `json:"slug"`
	Brief    *bool  `json:"brief"`
}

type personBriefAlias struct {
	EmailAddress    string `json:"email_address"`
	DisplayName     string `json:"display_name,omitempty"`
	LinkSource      string `json:"link_source,omitempty"`
	Locked          bool   `json:"locked,omitempty"`
	ArchiveMentions int64  `json:"archive_mentions,omitempty"`
}

type personAliasesSummary struct {
	Count                   int                `json:"count"`
	Primary                 string             `json:"primary"`
	HighVolume              []personBriefAlias `json:"high_volume"`
	LockedCount             int                `json:"locked_count"`
	ForwarderOrServiceCount int                `json:"forwarder_or_service_count"`
	Omitted                 int                `json:"omitted"`
}

type personExistingMemorySummary struct {
	FacetCount        int      `json:"facet_count"`
	AttributeCount    int      `json:"attribute_count"`
	NarrativeSections []string `json:"narrative_sections"`
}

type personSavedGroupSummary struct {
	GroupID     int64  `json:"group_id"`
	DisplayName string `json:"display_name,omitempty"`
	Label       string `json:"label,omitempty"`
	Size        int    `json:"size"`
	Note        string `json:"note,omitempty"`
	SavedAt     string `json:"saved_at,omitempty"`
}

type personSocialSummary struct {
	StructuralRole    string                    `json:"structural_role,omitempty"`
	Degree            int                       `json:"degree,omitempty"`
	DirectDegree      int                       `json:"direct_degree,omitempty"`
	CoRecipientDegree int                       `json:"co_recipient_degree,omitempty"`
	WeightedDegree    float64                   `json:"weighted_degree,omitempty"`
	DormancyDays      *int64                    `json:"dormancy_days,omitempty"`
	ClusterID         *int64                    `json:"cluster_id,omitempty"`
	SavedGroups       []personSavedGroupSummary `json:"saved_groups"`
}

func (s *Server) handleAgentGetPersonSummary(w http.ResponseWriter, r *http.Request) {
	var req getPersonSummaryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	var personID int64 = req.PersonID
	if personID <= 0 && req.Slug != "" {
		err := s.db.QueryRowContext(r.Context(), `
			SELECT person_id FROM memento_people_report WHERE slug = ?
		`, req.Slug).Scan(&personID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, fmt.Errorf("person slug %q not found", req.Slug))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if personID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("either person_id or slug is required"))
		return
	}

	result, err := s.getPersonSummaryForAgent(r.Context(), getPersonSummaryRequest{
		PersonID: personID,
		Brief:    req.Brief,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("person id %d not found", personID))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type getProjectSummaryRequest struct {
	ProjectID int64  `json:"project_id"`
	Slug      string `json:"slug"`
	Brief     bool   `json:"brief"`
}

func (s *Server) handleAgentGetProjectSummary(w http.ResponseWriter, r *http.Request) {
	var req getProjectSummaryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	var projectID int64 = req.ProjectID
	if projectID <= 0 && req.Slug != "" {
		err := s.db.QueryRowContext(r.Context(), `
			SELECT project_id FROM memento_projects_report WHERE slug = ?
		`, req.Slug).Scan(&projectID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, fmt.Errorf("project slug %q not found", req.Slug))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if projectID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("either project_id or slug is required"))
		return
	}

	// Load project basic details
	var p struct {
		ProjectID int64   `json:"project_id"`
		Slug      string  `json:"slug"`
		Name      string  `json:"name"`
		Status    string  `json:"status"`
		StartedAt *string `json:"started_at"`
	}
	var startedAt sql.NullString
	err := s.db.QueryRowContext(r.Context(), `
		SELECT id, slug, name, status, started_at
		FROM memento_project
		WHERE id = ?
	`, projectID).Scan(&p.ProjectID, &p.Slug, &p.Name, &p.Status, &startedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("project id %d not found", projectID))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if startedAt.Valid {
		p.StartedAt = new(startedAt.String)
	}

	// Load members
	type memberItem struct {
		PersonID      int64  `json:"person_id"`
		CanonicalName string `json:"canonical_name"`
		PrimaryEmail  string `json:"primary_email"`
		Role          string `json:"role"`
		Slug          string `json:"slug"`
	}
	members := []memberItem{}
	if !req.Brief {
		membersRows, err := s.db.QueryContext(r.Context(), `
			SELECT pm.person_id, mp.canonical_name, mp.primary_email, pm.role, COALESCE(pr.slug, '')
			FROM memento_project_member pm
			JOIN memento_person mp ON mp.id = pm.person_id
			LEFT JOIN memento_people_report pr ON pr.person_id = pm.person_id
			WHERE pm.project_id = ?
		`, projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		defer membersRows.Close()

		for membersRows.Next() {
			var m memberItem
			if err := membersRows.Scan(&m.PersonID, &m.CanonicalName, &m.PrimaryEmail, &m.Role, &m.Slug); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			members = append(members, m)
		}
	}

	// Load narrative sections
	narrative := make(map[string]any)
	if !req.Brief {
		narrativeRows, err := s.db.QueryContext(r.Context(), `
			SELECT section, content, source_message_ids, edited_by, COALESCE(generated_at, '')
			FROM memento_project_narrative
			WHERE project_id = ?
		`, projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		defer narrativeRows.Close()

		for narrativeRows.Next() {
			var section string
			var ns struct {
				Content          string  `json:"content"`
				SourceMessageIDs []int64 `json:"source_message_ids"`
				EditedBy         string  `json:"edited_by,omitempty"`
				GeneratedAt      string  `json:"generated_at,omitempty"`
			}
			var idsJSON string
			if err := narrativeRows.Scan(&section, &ns.Content, &idsJSON, &ns.EditedBy, &ns.GeneratedAt); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			_ = json.Unmarshal([]byte(idsJSON), &ns.SourceMessageIDs)
			if ns.SourceMessageIDs == nil {
				ns.SourceMessageIDs = []int64{}
			}
			narrative[section] = ns
		}
	}

	// Load message count
	var msgCount int64
	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM memento_project_message WHERE project_id = ?
	`, projectID).Scan(&msgCount)

	result := map[string]any{
		"project_id":    p.ProjectID,
		"slug":          p.Slug,
		"name":          p.Name,
		"status":        p.Status,
		"started_at":    p.StartedAt,
		"message_count": msgCount,
	}
	if !req.Brief {
		result["members"] = members
		result["narrative"] = narrative
	}

	writeJSON(w, http.StatusOK, result)
}

type getConceptSummaryRequest struct {
	ConceptID int64  `json:"concept_id"`
	Slug      string `json:"slug"`
	Brief     bool   `json:"brief"`
}

func (s *Server) handleAgentGetConceptSummary(w http.ResponseWriter, r *http.Request) {
	var req getConceptSummaryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	var conceptID int64 = req.ConceptID
	if conceptID <= 0 && req.Slug != "" {
		err := s.db.QueryRowContext(r.Context(), `
			SELECT concept_id FROM memento_concepts_report WHERE slug = ?
		`, req.Slug).Scan(&conceptID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, fmt.Errorf("concept slug %q not found", req.Slug))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if conceptID <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("either concept_id or slug is required"))
		return
	}

	// Load concept details
	var c struct {
		ConceptID        int64    `json:"concept_id"`
		Slug             string   `json:"slug"`
		Name             string   `json:"name"`
		ScopeDescription string   `json:"scope_description"`
		Status           string   `json:"status"`
		SeedKeywords     []string `json:"seed_keywords"`
	}
	var seedRaw string
	err := s.db.QueryRowContext(r.Context(), `
		SELECT id, slug, name, scope_description, status, seed_keywords
		FROM memento_concept
		WHERE id = ?
	`, conceptID).Scan(&c.ConceptID, &c.Slug, &c.Name, &c.ScopeDescription, &c.Status, &seedRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("concept id %d not found", conceptID))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_ = json.Unmarshal([]byte(seedRaw), &c.SeedKeywords)
	if c.SeedKeywords == nil {
		c.SeedKeywords = []string{}
	}

	// Load narrative sections
	narrative := make(map[string]any)
	if !req.Brief {
		narrativeRows, err := s.db.QueryContext(r.Context(), `
			SELECT section, content, source_message_ids, edited_by, COALESCE(generated_at, '')
			FROM memento_concept_narrative
			WHERE concept_id = ?
		`, conceptID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		defer narrativeRows.Close()

		for narrativeRows.Next() {
			var section string
			var ns struct {
				Content          string  `json:"content"`
				SourceMessageIDs []int64 `json:"source_message_ids"`
				EditedBy         string  `json:"edited_by,omitempty"`
				GeneratedAt      string  `json:"generated_at,omitempty"`
			}
			var idsJSON string
			if err := narrativeRows.Scan(&section, &ns.Content, &idsJSON, &ns.EditedBy, &ns.GeneratedAt); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			_ = json.Unmarshal([]byte(idsJSON), &ns.SourceMessageIDs)
			if ns.SourceMessageIDs == nil {
				ns.SourceMessageIDs = []int64{}
			}
			narrative[section] = ns
		}
	}

	// Load message count
	var msgCount int64
	_ = s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM memento_concept_message WHERE concept_id = ?
	`, conceptID).Scan(&msgCount)

	result := map[string]any{
		"concept_id":        c.ConceptID,
		"slug":              c.Slug,
		"name":              c.Name,
		"scope_description": c.ScopeDescription,
		"status":            c.Status,
		"seed_keywords":     c.SeedKeywords,
		"message_count":     msgCount,
	}
	if !req.Brief {
		result["narrative"] = narrative
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) searchPersonsForAgent(ctx context.Context, query string) ([]personSearchItem, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []personSearchItem{}, nil
	}
	likePat := "%" + q + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT person_id, canonical_name, primary_email, domain, total_messages, classification, slug
		FROM memento_people_report
		WHERE canonical_name LIKE ? OR primary_email LIKE ? OR EXISTS (
			SELECT 1 FROM memento_person_email pe
			WHERE pe.person_id = memento_people_report.person_id 
			  AND (pe.email_address LIKE ? OR pe.display_name LIKE ?)
		)
		ORDER BY total_messages DESC
		LIMIT 15`, likePat, likePat, likePat, likePat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []personSearchItem{}
	for rows.Next() {
		var item personSearchItem
		if err := rows.Scan(&item.PersonID, &item.CanonicalName, &item.PrimaryEmail, &item.Domain, &item.TotalMessages, &item.Classification, &item.Slug); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) searchProjectsForAgent(ctx context.Context, query string) ([]projectSearchItem, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []projectSearchItem{}, nil
	}
	likePat := "%" + q + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, slug, name, status, started_at
		FROM memento_projects_report
		WHERE name LIKE ? OR slug LIKE ?
		ORDER BY name ASC
		LIMIT 15`, likePat, likePat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []projectSearchItem{}
	for rows.Next() {
		var item projectSearchItem
		var startedAt sql.NullString
		if err := rows.Scan(&item.ProjectID, &item.Slug, &item.Name, &item.Status, &startedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			item.StartedAt = new(startedAt.String)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) searchConceptsForAgent(ctx context.Context, query string) ([]conceptSearchItem, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []conceptSearchItem{}, nil
	}
	likePat := "%" + q + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT concept_id, slug, name, status, scope_description, message_count
		FROM memento_concepts_report
		WHERE name LIKE ? OR slug LIKE ? OR scope_description LIKE ?
		ORDER BY name ASC
		LIMIT 15`, likePat, likePat, likePat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []conceptSearchItem{}
	for rows.Next() {
		var item conceptSearchItem
		if err := rows.Scan(&item.ConceptID, &item.Slug, &item.Name, &item.Status, &item.ScopeDescription, &item.MessageCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) getPersonSummaryForAgent(ctx context.Context, req getPersonSummaryRequest) (map[string]any, error) {
	personID := req.PersonID
	if personID <= 0 && req.Slug != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT person_id FROM memento_people_report WHERE slug = ?`, req.Slug).Scan(&personID); err != nil {
			return nil, normalizeToolNoRows(err, fmt.Sprintf("person slug %q", req.Slug))
		}
	}
	if personID <= 0 {
		return nil, fmt.Errorf("either person_id or slug is required")
	}
	if req.Brief == nil || *req.Brief {
		return s.loadPersonBriefForAgent(ctx, personID)
	}
	p, err := loadPersonForAgent(ctx, s.db, personID)
	if err != nil {
		return nil, normalizeToolNoRows(err, fmt.Sprintf("person id %d", personID))
	}
	facets, err := loadPersonFacets(ctx, s.db, personID)
	if err != nil {
		return nil, err
	}
	narrative, err := loadPersonNarrative(ctx, s.db, personID)
	if err != nil {
		return nil, err
	}
	attrs, err := loadPersonAttributes(ctx, s.db, personID)
	if err != nil {
		return nil, err
	}
	p["facets"] = facets
	p["narrative"] = narrative
	p["attributes"] = attrs
	return p, nil
}

func (s *Server) loadPersonBriefForAgent(ctx context.Context, personID int64) (map[string]any, error) {
	notes, errNotes := loadNotes(ctx, s.db, "person", personID)
	if errNotes != nil && !isNotSetUp(errNotes) {
		return nil, errNotes
	}
	if notes == nil {
		notes = []noteRow{}
	}
	authoritativeNotes := make([]string, 0, len(notes))
	for _, note := range notes {
		if content := strings.TrimSpace(note.Content); content != "" {
			authoritativeNotes = append(authoritativeNotes, content)
		}
	}

	var row struct {
		PersonID           int64
		CanonicalName      string
		PrimaryEmail       string
		Domain             string
		EmailCount         int64
		TotalMessages      int64
		FromContactCount   int64
		ToContactCount     int64
		BidirectionalScore float64
		Classification     string
		FirstMessageAt     string
		LastMessageAt      string
		Slug               string
		TopCorrespondents  string
	}
	err := s.db.QueryRowContext(ctx, `
		SELECT person_id, canonical_name, primary_email, domain, email_count,
		       total_messages, from_contact_count, to_contact_count,
		       bidirectional_score, classification,
		       COALESCE(first_message_at, ''), COALESCE(last_message_at, ''),
		       slug, top_correspondents_json
		FROM memento_people_report
		WHERE person_id = ?
	`, personID).Scan(
		&row.PersonID, &row.CanonicalName, &row.PrimaryEmail, &row.Domain, &row.EmailCount,
		&row.TotalMessages, &row.FromContactCount, &row.ToContactCount,
		&row.BidirectionalScore, &row.Classification,
		&row.FirstMessageAt, &row.LastMessageAt, &row.Slug, &row.TopCorrespondents,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if fallbackErr := s.db.QueryRowContext(ctx, `
				SELECT id, canonical_name, primary_email
				FROM memento_person
				WHERE id = ?
			`, personID).Scan(&row.PersonID, &row.CanonicalName, &row.PrimaryEmail); fallbackErr != nil {
				return nil, err
			}
			row.Classification = "candidate"
		} else {
			return nil, err
		}
	}

	var topCorrespondents []map[string]any
	if row.TopCorrespondents != "" {
		_ = json.Unmarshal([]byte(row.TopCorrespondents), &topCorrespondents)
	}
	if topCorrespondents == nil {
		topCorrespondents = []map[string]any{}
	}

	aliasesSummary, err := s.loadPersonAliasesSummary(ctx, personID, row.PrimaryEmail)
	if err != nil {
		return nil, err
	}
	existingMemory, err := s.loadPersonExistingMemorySummary(ctx, personID)
	if err != nil {
		return nil, err
	}
	socialSummary, err := s.loadPersonSocialSummary(ctx, personID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"person_id":           row.PersonID,
		"canonical_name":      row.CanonicalName,
		"primary_email":       row.PrimaryEmail,
		"domain":              row.Domain,
		"slug":                row.Slug,
		"classification":      row.Classification,
		"authoritative_notes": authoritativeNotes,
		"relationship": map[string]any{
			"total_messages":      row.TotalMessages,
			"from_contact_count":  row.FromContactCount,
			"to_contact_count":    row.ToContactCount,
			"bidirectional_score": row.BidirectionalScore,
			"first_message_at":    row.FirstMessageAt,
			"last_message_at":     row.LastMessageAt,
		},
		"aliases_summary":    aliasesSummary,
		"top_correspondents": topCorrespondents,
		"existing_memory":    existingMemory,
		"social_graph":       socialSummary,
		"omitted": map[string]any{
			"aliases":                     aliasesSummary.Omitted,
			"recent_timeline_sample":      "use list_person_messages with fields=\"compact\" when individual messages are needed",
			"facets_narrative_attributes": "call get_person_summary with brief=false or inspect the person_enrich bootstrap when full generated memory is needed",
		},
	}, nil
}

func (s *Server) loadPersonAliasesSummary(ctx context.Context, personID int64, primaryEmail string) (personAliasesSummary, error) {
	summary := personAliasesSummary{
		Primary:    primaryEmail,
		HighVolume: []personBriefAlias{},
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH alias_counts AS (
			SELECT
				mpe.email_address,
				mpe.display_name,
				mpe.link_source,
				mpe.locked,
				(
					SELECT COUNT(*)
					FROM participants p
					JOIN messages m ON m.sender_id = p.id
					WHERE lower(p.email_address) = lower(mpe.email_address)
				) + (
					SELECT COUNT(*)
					FROM participants p
					JOIN message_recipients mr ON mr.participant_id = p.id
					WHERE lower(p.email_address) = lower(mpe.email_address)
				) AS archive_mentions
			FROM memento_person_email mpe
			WHERE mpe.person_id = ?
		)
		SELECT email_address, display_name, link_source, locked, archive_mentions
		FROM alias_counts
		ORDER BY archive_mentions DESC, locked DESC, email_address ASC
	`, personID)
	if err != nil {
		if isNotSetUp(err) {
			return s.loadPersonAliasesSummaryWithoutCounts(ctx, personID, primaryEmail)
		}
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias personBriefAlias
		var locked int
		if err := rows.Scan(&alias.EmailAddress, &alias.DisplayName, &alias.LinkSource, &locked, &alias.ArchiveMentions); err != nil {
			return summary, err
		}
		alias.Locked = locked != 0
		summary.Count++
		if alias.Locked {
			summary.LockedCount++
		}
		if isServiceAlias(alias.EmailAddress, alias.LinkSource) {
			summary.ForwarderOrServiceCount++
		}
		if len(summary.HighVolume) < 5 {
			summary.HighVolume = append(summary.HighVolume, alias)
		}
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}
	if summary.Count > len(summary.HighVolume) {
		summary.Omitted = summary.Count - len(summary.HighVolume)
	}
	return summary, nil
}

func (s *Server) loadPersonAliasesSummaryWithoutCounts(ctx context.Context, personID int64, primaryEmail string) (personAliasesSummary, error) {
	summary := personAliasesSummary{
		Primary:    primaryEmail,
		HighVolume: []personBriefAlias{},
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT email_address, display_name, link_source, locked
		FROM memento_person_email
		WHERE person_id = ?
		ORDER BY locked DESC, email_address ASC
	`, personID)
	if err != nil {
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var alias personBriefAlias
		var locked int
		if err := rows.Scan(&alias.EmailAddress, &alias.DisplayName, &alias.LinkSource, &locked); err != nil {
			return summary, err
		}
		alias.Locked = locked != 0
		summary.Count++
		if alias.Locked {
			summary.LockedCount++
		}
		if isServiceAlias(alias.EmailAddress, alias.LinkSource) {
			summary.ForwarderOrServiceCount++
		}
		if len(summary.HighVolume) < 5 {
			summary.HighVolume = append(summary.HighVolume, alias)
		}
	}
	if err := rows.Err(); err != nil {
		return summary, err
	}
	if summary.Count > len(summary.HighVolume) {
		summary.Omitted = summary.Count - len(summary.HighVolume)
	}
	return summary, nil
}

func isServiceAlias(emailAddress, linkSource string) bool {
	addr := strings.ToLower(emailAddress)
	source := strings.ToLower(linkSource)
	if strings.Contains(source, "forwarder") {
		return true
	}
	serviceHints := []string{"noreply", "no-reply", "do-not-reply", "notification", "comments", "shares", "support@"}
	for _, hint := range serviceHints {
		if strings.Contains(addr, hint) {
			return true
		}
	}
	return false
}

func (s *Server) loadPersonExistingMemorySummary(ctx context.Context, personID int64) (personExistingMemorySummary, error) {
	summary := personExistingMemorySummary{NarrativeSections: []string{}}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM memento_person_facet
		WHERE person_id = ?
	`, personID).Scan(&summary.FacetCount); err != nil {
		if !isNotSetUp(err) {
			return summary, err
		}
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM memento_person_attribute
		WHERE person_id = ?
	`, personID).Scan(&summary.AttributeCount); err != nil {
		if !isNotSetUp(err) {
			return summary, err
		}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT section
		FROM memento_person_narrative
		WHERE person_id = ? AND content <> ''
		ORDER BY
			CASE section
				WHEN 'summary' THEN 1
				WHEN 'relationship_arc' THEN 2
				WHEN 'current_status' THEN 3
				ELSE 4
			END
	`, personID)
	if err != nil {
		if isNotSetUp(err) {
			return summary, nil
		}
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var section string
		if err := rows.Scan(&section); err != nil {
			return summary, err
		}
		summary.NarrativeSections = append(summary.NarrativeSections, section)
	}
	return summary, rows.Err()
}

func (s *Server) loadPersonSocialSummary(ctx context.Context, personID int64) (personSocialSummary, error) {
	summary := personSocialSummary{SavedGroups: []personSavedGroupSummary{}}
	var clusterID sql.NullInt64
	var dormancyDays sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT structural_role, degree, direct_degree, co_recipient_degree,
		       weighted_degree, dormancy_days, cluster_id
		FROM memento_social_metric
		WHERE person_id = ?
	`, personID).Scan(
		&summary.StructuralRole, &summary.Degree, &summary.DirectDegree,
		&summary.CoRecipientDegree, &summary.WeightedDegree, &dormancyDays, &clusterID,
	)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && !isNotSetUp(err) {
			return summary, err
		}
	} else {
		if dormancyDays.Valid {
			summary.DormancyDays = &dormancyDays.Int64
		}
		if clusterID.Valid {
			summary.ClusterID = &clusterID.Int64
		}
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT g.group_id,
		       COALESCE(NULLIF(g.display_name, ''), ''),
		       COALESCE(NULLIF(g.label, ''), ''),
		       g.size,
		       COALESCE(g.note, ''),
		       COALESCE(g.saved_at, '')
		FROM memento_social_group_member gm
		JOIN memento_social_group g ON g.group_id = gm.group_id
		WHERE gm.person_id = ?
		  AND gm.excluded_at IS NULL
		  AND g.saved_at IS NOT NULL
		ORDER BY g.saved_at DESC, g.group_id ASC
		LIMIT 3
	`, personID)
	if err != nil {
		if isNotSetUp(err) {
			return summary, nil
		}
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var group personSavedGroupSummary
		if err := rows.Scan(&group.GroupID, &group.DisplayName, &group.Label, &group.Size, &group.Note, &group.SavedAt); err != nil {
			return summary, err
		}
		summary.SavedGroups = append(summary.SavedGroups, group)
	}
	return summary, rows.Err()
}

func (s *Server) getProjectSummaryForAgent(ctx context.Context, req getProjectSummaryRequest) (map[string]any, error) {
	projectID := req.ProjectID
	if projectID <= 0 && req.Slug != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT project_id FROM memento_projects_report WHERE slug = ?`, req.Slug).Scan(&projectID); err != nil {
			return nil, normalizeToolNoRows(err, fmt.Sprintf("project slug %q", req.Slug))
		}
	}
	if projectID <= 0 {
		return nil, fmt.Errorf("either project_id or slug is required")
	}
	var p struct {
		ProjectID int64   `json:"project_id"`
		Slug      string  `json:"slug"`
		Name      string  `json:"name"`
		Status    string  `json:"status"`
		StartedAt *string `json:"started_at"`
	}
	var startedAt sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, slug, name, status, started_at
		FROM memento_project
		WHERE id = ?`, projectID).Scan(&p.ProjectID, &p.Slug, &p.Name, &p.Status, &startedAt); err != nil {
		return nil, normalizeToolNoRows(err, fmt.Sprintf("project id %d", projectID))
	}
	if startedAt.Valid {
		p.StartedAt = new(startedAt.String)
	}
	var msgCount int64
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_project_message WHERE project_id = ?`, projectID).Scan(&msgCount)

	result := map[string]any{
		"project_id":    p.ProjectID,
		"slug":          p.Slug,
		"name":          p.Name,
		"status":        p.Status,
		"started_at":    p.StartedAt,
		"message_count": msgCount,
	}

	if !req.Brief {
		members, err := s.projectMembersForAgent(ctx, projectID)
		if err != nil {
			return nil, err
		}
		narrative, err := loadNarrativeMap(ctx, s.db, "memento_project_narrative", "project_id", projectID)
		if err != nil {
			return nil, err
		}
		result["members"] = members
		result["narrative"] = narrative
	}

	return result, nil
}

func (s *Server) getConceptSummaryForAgent(ctx context.Context, req getConceptSummaryRequest) (map[string]any, error) {
	conceptID := req.ConceptID
	if conceptID <= 0 && req.Slug != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT concept_id FROM memento_concepts_report WHERE slug = ?`, req.Slug).Scan(&conceptID); err != nil {
			return nil, normalizeToolNoRows(err, fmt.Sprintf("concept slug %q", req.Slug))
		}
	}
	if conceptID <= 0 {
		return nil, fmt.Errorf("either concept_id or slug is required")
	}
	var c struct {
		ConceptID        int64    `json:"concept_id"`
		Slug             string   `json:"slug"`
		Name             string   `json:"name"`
		ScopeDescription string   `json:"scope_description"`
		Status           string   `json:"status"`
		SeedKeywords     []string `json:"seed_keywords"`
	}
	var seedRaw string
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, slug, name, scope_description, status, seed_keywords
		FROM memento_concept
		WHERE id = ?`, conceptID).Scan(&c.ConceptID, &c.Slug, &c.Name, &c.ScopeDescription, &c.Status, &seedRaw); err != nil {
		return nil, normalizeToolNoRows(err, fmt.Sprintf("concept id %d", conceptID))
	}
	_ = json.Unmarshal([]byte(seedRaw), &c.SeedKeywords)
	if c.SeedKeywords == nil {
		c.SeedKeywords = []string{}
	}
	var msgCount int64
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memento_concept_message WHERE concept_id = ?`, conceptID).Scan(&msgCount)

	result := map[string]any{
		"concept_id":        c.ConceptID,
		"slug":              c.Slug,
		"name":              c.Name,
		"scope_description": c.ScopeDescription,
		"status":            c.Status,
		"seed_keywords":     c.SeedKeywords,
		"message_count":     msgCount,
	}

	if !req.Brief {
		narrative, err := loadNarrativeMap(ctx, s.db, "memento_concept_narrative", "concept_id", conceptID)
		if err != nil {
			return nil, err
		}
		result["narrative"] = narrative
	}

	return result, nil
}

func (s *Server) projectMembersForAgent(ctx context.Context, projectID int64) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pm.person_id, mp.canonical_name, mp.primary_email, pm.role, COALESCE(pr.slug, '')
		FROM memento_project_member pm
		JOIN memento_person mp ON mp.id = pm.person_id
		LEFT JOIN memento_people_report pr ON pr.person_id = pm.person_id
		WHERE pm.project_id = ?`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []map[string]any{}
	for rows.Next() {
		var personID int64
		var name, email, role, slug string
		if err := rows.Scan(&personID, &name, &email, &role, &slug); err != nil {
			return nil, err
		}
		members = append(members, map[string]any{
			"person_id":      personID,
			"canonical_name": name,
			"primary_email":  email,
			"role":           role,
			"slug":           slug,
		})
	}
	return members, rows.Err()
}

func loadNarrativeMap(ctx context.Context, db *sql.DB, table, idCol string, entityID int64) (map[string]any, error) {
	if table != "memento_project_narrative" && table != "memento_concept_narrative" {
		return nil, fmt.Errorf("unsupported narrative table %s", table)
	}
	if idCol != "project_id" && idCol != "concept_id" {
		return nil, fmt.Errorf("unsupported narrative id column %s", idCol)
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
		SELECT section, content, source_message_ids, edited_by, COALESCE(generated_at, '')
		FROM %s
		WHERE %s = ?`, table, idCol), entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	narrative := make(map[string]any)
	for rows.Next() {
		var section string
		var ns struct {
			Content          string  `json:"content"`
			SourceMessageIDs []int64 `json:"source_message_ids"`
			EditedBy         string  `json:"edited_by,omitempty"`
			GeneratedAt      string  `json:"generated_at,omitempty"`
		}
		var idsJSON string
		if err := rows.Scan(&section, &ns.Content, &idsJSON, &ns.EditedBy, &ns.GeneratedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(idsJSON), &ns.SourceMessageIDs)
		if ns.SourceMessageIDs == nil {
			ns.SourceMessageIDs = []int64{}
		}
		narrative[section] = ns
	}
	return narrative, rows.Err()
}
