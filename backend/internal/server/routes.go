package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"memento/backend/internal/avatar"
	"memento/backend/internal/concept"
	"memento/backend/internal/msgvaultapi"
	"memento/backend/internal/newsletter"
	"memento/backend/internal/people"
	"memento/backend/internal/person"
	"memento/backend/internal/project"
	"memento/backend/internal/refresh"
	"memento/backend/internal/store"
	"memento/backend/internal/webui"
)

const defaultTimelineLimit = 50

func (s *Server) registerRoutes() {
	// Browser-facing routes for the statically exported web UI (served by
	// this binary; see internal/webui). Registered first for clarity — the
	// patterns do not overlap with the API routes below.
	s.registerBrowserRoutes()

	// Embedded web UI: everything that is not /api/... is a page or asset.
	s.mux.Handle("/", webui.Handler(func(r *http.Request) bool {
		return s.setupInitialized(r.Context())
	}))

	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/setup/initialized", s.handleSetupInitialized)
	s.mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/setup/check-archive", s.requireSetupIncomplete(s.handleSetupCheckArchive))
	s.mux.HandleFunc("POST /api/setup/env", s.requireSetupIncomplete(s.handleSetupEnv))
	s.mux.HandleFunc("POST /api/setup/init", s.requireSetupIncomplete(s.handleSetupInit))
	s.mux.HandleFunc("POST /api/setup/test-provider", s.requireSetupIncomplete(s.handleSetupTestProvider))
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("GET /api/avatar/{hash}", s.handleGetAvatar)
	s.mux.HandleFunc("POST /api/config", s.handlePostConfig)
	s.mux.HandleFunc("GET /api/messages/{id}", s.handleGetMessageDetail)
	s.mux.HandleFunc("GET /api/context-search", s.handleContextSearch)

	s.mux.HandleFunc("GET /api/people", s.handleGetPeople)
	s.mux.HandleFunc("GET /api/people/merge-suggestions", s.handleGetPeopleMergeSuggestions)
	s.mux.HandleFunc("POST /api/people/merge-decision", s.handlePostPeopleMergeDecision)
	s.mux.HandleFunc("POST /api/people/merge-apply", s.handlePostPeopleMergeApply)
	s.mux.HandleFunc("GET /api/people/{slug}/network", s.handleGetPersonNetwork)
	s.mux.HandleFunc("GET /api/people/{slug}", s.handleGetPersonBySlug)
	s.mux.HandleFunc("POST /api/people/{slug}/override-classification", s.handleOverrideClassification)
	s.mux.HandleFunc("PATCH /api/people/{slug}/memory", s.handlePatchPersonMemory)
	s.mux.HandleFunc("DELETE /api/people/{slug}", s.handleDismissPerson)
	s.mux.HandleFunc("GET /api/social/clusters", s.handleGetClusters)
	s.mux.HandleFunc("GET /api/social/groups", s.handleGetGroups)
	s.mux.HandleFunc("GET /api/social/groups/{id}", s.handleGetGroup)
	s.mux.HandleFunc("POST /api/social/groups/{id}/save", s.handleSaveGroup)
	s.mux.HandleFunc("DELETE /api/social/groups/{id}/save", s.handleUnsaveGroup)
	s.mux.HandleFunc("POST /api/social/groups/{id}/dismiss", s.handleDismissGroup)
	s.mux.HandleFunc("DELETE /api/social/groups/{id}/dismiss", s.handleRestoreGroup)
	s.mux.HandleFunc("PATCH /api/social/groups/{id}", s.handlePatchGroup)
	s.mux.HandleFunc("POST /api/social/groups/{id}/members", s.handleAddGroupMember)
	s.mux.HandleFunc("POST /api/social/groups/{id}/members/{personId}/exclude", s.handleExcludeGroupMember)
	s.mux.HandleFunc("DELETE /api/social/groups/{id}/members/{personId}/exclude", s.handleRestoreGroupMember)
	s.mux.HandleFunc("GET /api/social/leaderboard", s.handleGetLeaderboard)
	s.mux.HandleFunc("GET /api/projects", s.handleGetProjects)
	s.mux.HandleFunc("GET /api/projects/{slug}", s.handleGetProject)
	s.mux.HandleFunc("GET /api/projects/{slug}/provenance", s.handleGetProjectProvenance)
	s.mux.HandleFunc("DELETE /api/projects/{slug}", s.handleDismissProject)
	s.mux.HandleFunc("PATCH /api/projects/{slug}", s.handleRenameProject)
	s.mux.HandleFunc("GET /api/newsletters", s.handleGetNewsletters)
	s.mux.HandleFunc("GET /api/newsletters/{slug}", s.handleGetNewsletter)
	s.mux.HandleFunc("PATCH /api/newsletters/{slug}", s.handleRenameNewsletter)
	s.mux.HandleFunc("GET /api/dashboard/activity", s.handleGetArchiveActivity)
	s.mux.HandleFunc("GET /api/concepts", s.handleGetConcepts)
	s.mux.HandleFunc("GET /api/concepts/{slug}", s.handleGetConcept)
	s.mux.HandleFunc("GET /api/concepts/{slug}/provenance", s.handleGetConceptProvenance)
	s.mux.HandleFunc("DELETE /api/concepts/{slug}", s.handleDismissConcept)
	s.mux.HandleFunc("PATCH /api/concepts/{slug}", s.handleRenameConcept)
	s.mux.HandleFunc("GET /api/sessions", s.handleListAskSessions)
	s.mux.HandleFunc("GET /api/sessions/{slug}", s.handleGetAskSession)
	s.mux.HandleFunc("PATCH /api/sessions/{slug}", s.handleUpdateAskSession)
	s.mux.HandleFunc("DELETE /api/sessions/{slug}", s.handleDeleteAskSession)
	s.mux.HandleFunc("POST /api/sessions/{slug}/promote", s.handlePromoteAskSession)
	s.mux.HandleFunc("GET /api/notes", s.handleGetNotes)
	s.mux.HandleFunc("POST /api/notes", s.handleCreateNote)
	s.mux.HandleFunc("PATCH /api/notes", s.handleUpdateNote)
	s.mux.HandleFunc("DELETE /api/notes", s.handleDeleteNote)

	s.mux.HandleFunc("GET /api/search", s.handleSearch)

	s.mux.HandleFunc("POST /api/people/refresh", s.handlePeopleRefresh)
	s.mux.HandleFunc("POST /api/newsletters/detect", s.handleNewsletterDetect)
	s.mux.HandleFunc("DELETE /api/newsletters/{slug}", s.handleDismissNewsletter)
	s.mux.HandleFunc("POST /api/newsletters/{slug}/generate", s.handleNewsletterGenerate)

	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleJobStream)
	s.mux.HandleFunc("GET /api/jobs/{id}/status", s.handleJobStatus)

	// Draft CRUD — public, used by the curation UI.
	s.mux.HandleFunc("POST /api/drafts", s.handleCreateDraft)
	s.mux.HandleFunc("GET /api/drafts/{id}", s.handleGetDraft)
	s.mux.HandleFunc("PATCH /api/drafts/{id}/entities", s.handleUpdateDraftEntities)
	s.mux.HandleFunc("POST /api/drafts/{id}/commit", s.handleCommitDraft)
	s.mux.HandleFunc("POST /api/drafts/{id}/abandon", s.handleAbandonDraft)

	// Internal endpoints — only callable by the Next.js agent runtime.
	// Token-gated; never expose to the browser.
	s.mux.HandleFunc("POST /api/internal/agent-tools/ping",
		s.requireInternalToken(s.handleAgentToolsPing))
	s.mux.HandleFunc("GET /api/internal/agent-tools/manifest",
		s.requireInternalToken(s.handleAgentToolsManifest))
	s.mux.HandleFunc("POST /api/internal/agent-tools/debug-invoke",
		s.requireInternalToken(s.handleAgentToolDebugInvoke))
	s.mux.HandleFunc("POST /api/internal/agent-tools/fts-search",
		s.requireInternalToken(s.handleAgentFTSSearch))
	s.mux.HandleFunc("POST /api/internal/agent-tools/vector-search",
		s.requireInternalToken(s.handleAgentVectorSearch))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-message",
		s.requireInternalToken(s.handleAgentGetMessage))
	s.mux.HandleFunc("POST /api/internal/agent-tools/find-people",
		s.requireInternalToken(s.handleAgentFindPeople))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-thread",
		s.requireInternalToken(s.handleAgentGetThread))
	s.mux.HandleFunc("POST /api/internal/agent-tools/summarize-thread",
		s.requireInternalToken(s.handleAgentSummarizeThread))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-bundle-index",
		s.requireInternalToken(s.handleAgentGetBundleIndex))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-message-batch",
		s.requireInternalToken(s.handleAgentGetMessageBatch))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-project-bundle",
		s.requireInternalToken(s.handleAgentGetProjectBundle))
	s.mux.HandleFunc("POST /api/internal/agent-tools/write-section",
		s.requireInternalToken(s.handleAgentWriteSection))
	s.mux.HandleFunc("POST /api/internal/agent-tools/refresh-projects-rollup",
		s.requireInternalToken(s.handleAgentRefreshProjects))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-concept-bundle",
		s.requireInternalToken(s.handleAgentGetConceptBundle))
	s.mux.HandleFunc("POST /api/internal/agent-tools/cluster-messages",
		s.requireInternalToken(s.handleAgentClusterMessages))
	s.mux.HandleFunc("POST /api/internal/agent-tools/write-concept-section",
		s.requireInternalToken(s.handleAgentWriteConceptSection))
	s.mux.HandleFunc("POST /api/internal/agent-tools/refresh-concepts-rollup",
		s.requireInternalToken(s.handleAgentRefreshConcepts))
	s.mux.HandleFunc("POST /api/internal/agent-tools/list-person-messages",
		s.requireInternalToken(s.handleAgentListPersonMessages))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-notes",
		s.requireInternalToken(s.handleAgentGetNotes))
	s.mux.HandleFunc("POST /api/internal/agent-tools/fts-search-scoped",
		s.requireInternalToken(s.handleAgentFTSSearchScoped))
	s.mux.HandleFunc("POST /api/internal/agent-tools/reset-person-agent-output",
		s.requireInternalToken(s.handleAgentResetPersonOutput))
	s.mux.HandleFunc("POST /api/internal/agent-tools/write-facet",
		s.requireInternalToken(s.handleAgentWriteFacet))
	s.mux.HandleFunc("POST /api/internal/agent-tools/write-person-section",
		s.requireInternalToken(s.handleAgentWritePersonSection))
	s.mux.HandleFunc("POST /api/internal/agent-tools/refresh-people-rollup",
		s.requireInternalToken(s.handleAgentRefreshPeople))
	s.mux.HandleFunc("PATCH /api/internal/drafts/{id}/state",
		s.requireInternalToken(s.handleUpdateDraftState))

	// Agent sessions and step loops logging
	s.mux.HandleFunc("POST /api/internal/agent-sessions",
		s.requireInternalToken(s.handleCreateAgentSession))
	s.mux.HandleFunc("GET /api/internal/agent-sessions",
		s.requireInternalToken(s.handleListAgentSessions))
	s.mux.HandleFunc("PATCH /api/internal/agent-sessions/{id}",
		s.requireInternalToken(s.handleUpdateAgentSession))
	s.mux.HandleFunc("DELETE /api/internal/agent-sessions/{id}",
		s.requireInternalToken(s.handleDeleteAgentSession))
	s.mux.HandleFunc("POST /api/internal/agent-sessions/{id}/loops",
		s.requireInternalToken(s.handleLogAgentLoop))
	s.mux.HandleFunc("GET /api/internal/agent-sessions/{id}/logs",
		s.requireInternalToken(s.handleGetAgentSessionLogs))
	s.mux.HandleFunc("GET /api/internal/agent-sessions/latest-logs",
		s.requireInternalToken(s.handleGetLatestAgentSessionLogs))
	s.mux.HandleFunc("POST /api/internal/agent-decisions",
		s.requireInternalToken(s.handleCreateAgentDecision))
	s.mux.HandleFunc("GET /api/internal/agent-decisions/{id}",
		s.requireInternalToken(s.handleGetAgentDecision))
	s.mux.HandleFunc("PATCH /api/internal/agent-decisions/{id}",
		s.requireInternalToken(s.handleUpdateAgentDecision))
	s.mux.HandleFunc("POST /api/internal/agent-runs",
		s.requireInternalToken(s.handleCreateAgentRun))
	s.mux.HandleFunc("GET /api/internal/agent-runs/{id}/events",
		s.requireInternalToken(s.handleAgentRunEvents))
	s.mux.HandleFunc("POST /api/internal/agent-runs/{id}/cancel",
		s.requireInternalToken(s.handleCancelAgentRun))

	// Debug-only system info: project root, archive db path, configured
	// provider/model. Used by the Debug UI to make it obvious which
	// working directory and which LLM endpoint a given run was executed
	// against.
	s.mux.HandleFunc("GET /api/internal/debug/system",
		s.requireInternalToken(s.handleDebugSystemInfo))

	// Social graph internal agent tools.
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-person-network",
		s.requireInternalToken(s.handleAgentGetPersonNetwork))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-cluster",
		s.requireInternalToken(s.handleAgentGetCluster))
	s.mux.HandleFunc("POST /api/internal/agent-tools/find-bridges-between",
		s.requireInternalToken(s.handleAgentFindBridgesBetween))
	s.mux.HandleFunc("POST /api/internal/agent-tools/find-missing-collaborators",
		s.requireInternalToken(s.handleAgentFindMissingCollaborators))
	s.mux.HandleFunc("GET /api/internal/social/clusters/{id}/label-context",
		s.requireInternalToken(s.handleGetClusterLabelContext))
	s.mux.HandleFunc("POST /api/internal/social/clusters/{id}/label",
		s.requireInternalToken(s.handlePostClusterLabel))
	s.mux.HandleFunc("POST /api/internal/social/clusters/{id}/generate-label",
		s.requireInternalToken(s.handleGenerateClusterLabel))
	s.mux.HandleFunc("POST /api/internal/social/groups/{id}/generate-label",
		s.requireInternalToken(s.handleGenerateGroupLabel))

	// Phase 6 Memento Router Agent tools
	s.mux.HandleFunc("POST /api/internal/agent-tools/search-persons",
		s.requireInternalToken(s.handleAgentSearchPersons))
	s.mux.HandleFunc("POST /api/internal/agent-tools/search-projects",
		s.requireInternalToken(s.handleAgentSearchProjects))
	s.mux.HandleFunc("POST /api/internal/agent-tools/search-concepts",
		s.requireInternalToken(s.handleAgentSearchConcepts))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-person-summary",
		s.requireInternalToken(s.handleAgentGetPersonSummary))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-project-summary",
		s.requireInternalToken(s.handleAgentGetProjectSummary))
	s.mux.HandleFunc("POST /api/internal/agent-tools/get-concept-summary",
		s.requireInternalToken(s.handleAgentGetConceptSummary))
	s.mux.HandleFunc("POST /api/internal/agent-tools/detect-gaps",
		s.requireInternalToken(s.handleAgentDetectGaps))
	s.mux.HandleFunc("POST /api/internal/agent-tools/detect-gaps-with-results",
		s.requireInternalToken(s.handleAgentDetectGapsWithResults))
	s.mux.HandleFunc("POST /api/internal/agent-tools/add-project-messages",
		s.requireInternalToken(s.handleAgentAddProjectMessages))
	s.mux.HandleFunc("POST /api/internal/agent-tools/add-concept-messages",
		s.requireInternalToken(s.handleAgentAddConceptMessages))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	if status >= 500 {
		log.Printf("server error %d: %v", status, err)
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// isNotSetUp returns true when the error is a missing-table SQLite error,
// which means memento init has not been run yet.
func isNotSetUp(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"db_path": s.reader.Path(),
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

// --- READ ENDPOINTS ---

// reportMeta returns the generated_at timestamp for a given dimension from
// memento_report_meta. Returns "" if not found.
func (s *Server) reportMeta(ctx context.Context, dimension string) string {
	var ts string
	_ = s.db.QueryRowContext(ctx,
		`SELECT generated_at FROM memento_report_meta WHERE dimension = ?`, dimension,
	).Scan(&ts)
	return ts
}

func (s *Server) handleGetPeople(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "top", 50)
	classification := r.URL.Query().Get("classification")
	if classification == "" {
		classification = "active"
	}
	includeAllClassifications := classification == "all" || classification == "ALL"
	includeActiveClassifications := classification == "active" || classification == "ACTIVE"
	searchQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	ctx := r.Context()

	// Directory counts should match rows the tabs can render from the materialized
	// report. Excluded is intentionally sourced from the complete candidate ledger
	// so the audit tab remains real without bloating memento_people_report.
	counts := map[string]int{}
	countRows, countErr := s.db.QueryContext(ctx,
		`SELECT classification, COUNT(*) FROM memento_people_report GROUP BY classification`)
	if countErr == nil {
		defer countRows.Close()
		for countRows.Next() {
			var cls string
			var n int
			if err := countRows.Scan(&cls, &n); err == nil {
				counts[cls] = n
			}
		}
		_ = countRows.Err()
	}
	var excludedCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM memento_people_candidates mpc
		JOIN memento_person mp ON mp.id = mpc.person_id
		WHERE mpc.classification = 'excluded'
		  AND mp.dismissed_at IS NULL
	`).Scan(&excludedCount); err == nil {
		counts["excluded"] = excludedCount
	}

	if classification == "excluded" {
		persons, err := s.loadExcludedPeople(ctx, limit)
		if isNotSetUp(err) {
			writeJSON(w, http.StatusOK, map[string]any{"generated_at": "", "people": []any{}, "counts": counts})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, people.PeoplePagesReport{
			GeneratedAt: s.reportMeta(ctx, "people"),
			People:      persons,
			Counts:      counts,
		})
		return
	}

	query := `
		SELECT pr.person_id, pr.canonical_name, pr.primary_email, pr.domain, pr.email_count,
		       pr.total_messages, pr.from_contact_count, pr.to_contact_count,
		       pr.bidirectional_score, pr.classification,
		       COALESCE(pr.first_message_at, ''), COALESCE(pr.last_message_at, ''),
		       pr.slug, pr.aliases_json, pr.timeline_json, pr.top_correspondents_json,
		       COALESCE(sm.structural_role, ''), sm.cluster_id,
		       COALESCE(sc.label, ''), sm.dormancy_days
		FROM memento_people_report pr
		LEFT JOIN memento_social_metric sm ON sm.person_id = pr.person_id
		LEFT JOIN memento_social_cluster sc ON sc.cluster_id = sm.cluster_id
	`
	var args []any
	var where []string
	if !includeAllClassifications {
		if includeActiveClassifications {
			where = append(where, `pr.classification IN ('candidate', 'candidate_inbound_only')`)
		} else {
			where = append(where, `pr.classification = ?`)
			args = append(args, classification)
		}
	}
	if searchQuery != "" {
		like := "%" + strings.ToLower(searchQuery) + "%"
		where = append(where, `(lower(pr.canonical_name) LIKE ? OR lower(pr.primary_email) LIKE ? OR lower(pr.aliases_json) LIKE ?)`)
		args = append(args, like, like, like)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY pr.total_messages DESC, pr.last_message_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, map[string]any{"generated_at": "", "people": []any{}, "counts": counts})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var persons []people.PagePerson
	for rows.Next() {
		var p people.PagePerson
		var first, last, slug, aliasesJSON, timelineJSON, correspondentsJSON string
		var clusterID, dormancyDays sql.NullInt64
		if err := rows.Scan(
			&p.PersonID, &p.CanonicalName, &p.PrimaryEmail, &p.Domain, &p.EmailCount,
			&p.TotalMessages, &p.FromContactCount, &p.ToContactCount,
			&p.BidirectionalScore, &p.Classification, &first, &last,
			&slug, &aliasesJSON, &timelineJSON, &correspondentsJSON,
			&p.StructuralRole, &clusterID, &p.ClusterLabel, &dormancyDays,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		p.FirstMessageAt = first
		p.LastMessageAt = last
		p.Slug = slug
		if clusterID.Valid {
			p.ClusterID = &clusterID.Int64
		}
		if dormancyDays.Valid {
			p.DormancyDays = &dormancyDays.Int64
		}
		_ = json.Unmarshal([]byte(aliasesJSON), &p.Aliases)
		_ = json.Unmarshal([]byte(timelineJSON), &p.Timeline)
		_ = json.Unmarshal([]byte(correspondentsJSON), &p.TopCorrespondents)
		if p.Aliases == nil {
			p.Aliases = []people.Alias{}
		}
		if p.Timeline == nil {
			p.Timeline = []people.TimelineEntry{}
		}
		persons = append(persons, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if persons == nil {
		persons = []people.PagePerson{}
	}
	writeJSON(w, http.StatusOK, people.PeoplePagesReport{
		GeneratedAt: s.reportMeta(ctx, "people"),
		People:      persons,
		Counts:      counts,
	})
}

type peopleMergeProfile struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Email        string   `json:"email"`
	MessageCount int64    `json:"message_count"`
	LastSeen     string   `json:"last_seen,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	LockedCount  int      `json:"locked_count"`
}

type peopleMergeEvidence struct {
	SharedNeighborCount int     `json:"shared_neighbor_count"`
	NameSimilarity      float64 `json:"name_similarity"`
	TokenOverlap        float64 `json:"token_overlap"`
	SignatureScore      float64 `json:"signature_score"`
	TemporalScore       float64 `json:"temporal_score"`
	CombinedScore       float64 `json:"combined_score"`
}

type peopleMergeSuggestion struct {
	ID               string               `json:"id"`
	Confidence       int                  `json:"confidence"`
	RecommendedKeep  int64                `json:"recommended_keep_id"`
	RecommendedMerge int64                `json:"recommended_merge_id"`
	Sources          []string             `json:"sources"`
	ScoresPending    bool                 `json:"scores_pending"`
	Status           string               `json:"status"`
	People           []peopleMergeProfile `json:"people"`
	Evidence         peopleMergeEvidence  `json:"evidence"`
}

func (s *Server) handleGetPeopleMergeSuggestions(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 25)
	offset := parseIntQuery(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	sortKey := r.URL.Query().Get("sort")
	status := r.URL.Query().Get("status")
	suggestions, err := person.ListMergeSuggestions(r.Context(), s.db, person.ListMergeSuggestionOptions{
		Status: status,
		Sort:   sortKey,
		Limit:  limit,
		Offset: offset,
	})
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []any{}, "total": 0, "limit": limit, "offset": offset})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	out := make([]peopleMergeSuggestion, 0, len(suggestions))
	for _, suggestion := range suggestions {
		aProfile, err := s.loadPeopleMergeProfile(r.Context(), suggestion.PersonAID, "", "", nil, 0)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_, _ = person.MarkMergeSuggestionResolved(r.Context(), s.db, suggestion.ID, "rejected")
				continue
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		bProfile, err := s.loadPeopleMergeProfile(r.Context(), suggestion.PersonBID, "", "", nil, 0)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_, _ = person.MarkMergeSuggestionResolved(r.Context(), s.db, suggestion.ID, "rejected")
				continue
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		keepID, mergeID := recommendedMergeDirection(aProfile, bProfile)
		out = append(out, peopleMergeSuggestion{
			ID:               fmt.Sprintf("%d", suggestion.ID),
			Confidence:       int(suggestion.CombinedScore*100 + 0.5),
			RecommendedKeep:  keepID,
			RecommendedMerge: mergeID,
			Sources:          suggestion.Sources,
			ScoresPending:    suggestion.ScoresStale,
			Status:           suggestion.Status,
			People:           []peopleMergeProfile{aProfile, bProfile},
			Evidence: peopleMergeEvidence{
				NameSimilarity: suggestion.NameSimilarity,
				TokenOverlap:   suggestion.TokenOverlap,
				SignatureScore: suggestion.SignatureScore,
				CombinedScore:  suggestion.CombinedScore,
			},
		})
	}
	total, err := person.CountMergeSuggestions(r.Context(), s.db, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) loadPeopleMergeProfile(ctx context.Context, personID int64, fallbackName, fallbackEmail string, aliases []string, lockedCount int) (peopleMergeProfile, error) {
	profile := peopleMergeProfile{
		ID:          personID,
		Name:        fallbackName,
		Email:       fallbackEmail,
		Aliases:     aliases,
		LockedCount: lockedCount,
	}
	var name, email string
	var messages sql.NullInt64
	var lastSeen sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT mp.canonical_name,
		       mp.primary_email,
		       COALESCE(pr.total_messages, mpc.total_messages),
		       COALESCE(pr.last_message_at, mpc.last_message_at, '')
		FROM memento_person mp
		LEFT JOIN memento_people_report pr ON pr.person_id = mp.id
		LEFT JOIN memento_people_candidates mpc ON mpc.person_id = mp.id
		WHERE mp.id = ? AND mp.dismissed_at IS NULL
	`, personID).Scan(&name, &email, &messages, &lastSeen)
	if err != nil {
		return profile, err
	}
	profile.Name = name
	profile.Email = email
	if messages.Valid && messages.Int64 > 0 {
		profile.MessageCount = messages.Int64
		if lastSeen.Valid {
			profile.LastSeen = lastSeen.String
		}
	} else {
		// Fallback: calculate message count and last seen from raw vault tables
		var fallbackCount int64
		var fallbackLastSeen string
		err := s.db.QueryRowContext(ctx, `
			WITH person_participants AS (
				SELECT p.id FROM participants p
				JOIN memento_person_email pe ON pe.email_address = p.email_address
				WHERE pe.person_id = ?
			),
			person_msg_ids AS (
				SELECT m.id AS message_id, m.sent_at
				FROM messages m
				WHERE m.sender_id IN (SELECT id FROM person_participants)
				UNION
				SELECT mr.message_id, m.sent_at
				FROM message_recipients mr
				JOIN messages m ON m.id = mr.message_id
				WHERE mr.participant_id IN (SELECT id FROM person_participants)
			)
			SELECT COUNT(DISTINCT message_id), COALESCE(MAX(sent_at), '')
			FROM person_msg_ids
		`, personID).Scan(&fallbackCount, &fallbackLastSeen)
		if err == nil {
			profile.MessageCount = fallbackCount
			profile.LastSeen = fallbackLastSeen
		}
	}
	if len(profile.Aliases) == 0 {
		aliasRows, err := s.db.QueryContext(ctx, `
			SELECT email_address, locked
			FROM memento_person_email
			WHERE person_id = ?
			ORDER BY locked DESC, email_address ASC
		`, personID)
		if err == nil {
			defer aliasRows.Close()
			var aliases []string
			var lockedCount int
			for aliasRows.Next() {
				var email string
				var locked int
				if err := aliasRows.Scan(&email, &locked); err != nil {
					return profile, err
				}
				aliases = append(aliases, email)
				if locked != 0 {
					lockedCount++
				}
			}
			if err := aliasRows.Err(); err != nil {
				return profile, err
			}
			profile.Aliases = aliases
			profile.LockedCount = lockedCount
		}
	}
	return profile, nil
}

func recommendedMergeDirection(a, b peopleMergeProfile) (keepID, mergeID int64) {
	if a.LockedCount != b.LockedCount {
		if a.LockedCount > b.LockedCount {
			return a.ID, b.ID
		}
		return b.ID, a.ID
	}
	if len(a.Aliases) != len(b.Aliases) {
		if len(a.Aliases) > len(b.Aliases) {
			return a.ID, b.ID
		}
		return b.ID, a.ID
	}
	if a.ID < b.ID {
		return a.ID, b.ID
	}
	return b.ID, a.ID
}

func matchesMergeSuggestionPair(keepID, mergeID, personAID, personBID int64) bool {
	return (keepID == personAID && mergeID == personBID) || (keepID == personBID && mergeID == personAID)
}

type peopleMergeDecisionRequest struct {
	ID            string `json:"id"`
	Decision      string `json:"decision"`
	KeepPersonID  int64  `json:"keep_person_id,omitempty"`
	MergePersonID int64  `json:"merge_person_id,omitempty"`
}

func (s *Server) handlePostPeopleMergeDecision(w http.ResponseWriter, r *http.Request) {
	var req peopleMergeDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid suggestion id %q", req.ID))
		return
	}
	decision := strings.TrimSpace(req.Decision)
	switch decision {
	case "reject":
		row, err := person.MarkMergeSuggestionResolved(r.Context(), s.db, id, "rejected")
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"suggestion": row})
	case "accept":
		row, err := person.GetMergeSuggestion(r.Context(), s.db, id)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if row.Status != "pending" {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("merge suggestion %d is already %s", row.ID, row.Status))
			return
		}
		aProfile, err := s.loadPeopleMergeProfile(r.Context(), row.PersonAID, "", "", nil, 0)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_, _ = person.MarkMergeSuggestionResolved(r.Context(), s.db, id, "rejected")
				writeError(w, http.StatusNotFound, fmt.Errorf("merge suggestion %d references a missing person", row.ID))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		bProfile, err := s.loadPeopleMergeProfile(r.Context(), row.PersonBID, "", "", nil, 0)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				_, _ = person.MarkMergeSuggestionResolved(r.Context(), s.db, id, "rejected")
				writeError(w, http.StatusNotFound, fmt.Errorf("merge suggestion %d references a missing person", row.ID))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		keepID, mergeID := recommendedMergeDirection(aProfile, bProfile)
		if req.KeepPersonID != 0 || req.MergePersonID != 0 {
			if req.KeepPersonID <= 0 || req.MergePersonID <= 0 {
				writeError(w, http.StatusBadRequest, fmt.Errorf("keep_person_id and merge_person_id are both required when overriding merge direction"))
				return
			}
			if req.KeepPersonID == req.MergePersonID {
				writeError(w, http.StatusBadRequest, fmt.Errorf("keep_person_id and merge_person_id must differ"))
				return
			}
			if !matchesMergeSuggestionPair(req.KeepPersonID, req.MergePersonID, row.PersonAID, row.PersonBID) {
				writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("merge direction ids must match suggestion pair %d/%d", row.PersonAID, row.PersonBID))
				return
			}
			keepID, mergeID = req.KeepPersonID, req.MergePersonID
		}
		result, err := person.MergePersons(r.Context(), s.db, mergeID, keepID)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("merge %d into %d: %w", mergeID, keepID, err))
			return
		}
		resolved, err := person.GetMergeSuggestion(r.Context(), s.db, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"suggestion": resolved,
			"from_id":    mergeID,
			"into_id":    keepID,
			"result":     result,
		})
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid decision %q", req.Decision))
	}
}

type peopleMergeApplyRequest struct {
	Merges  []peopleMergeApplyItem `json:"merges"`
	Refresh bool                   `json:"refresh"`
}

type peopleMergeApplyItem struct {
	FromID int64 `json:"from_id"`
	IntoID int64 `json:"into_id"`
}

func (s *Server) handlePostPeopleMergeApply(w http.ResponseWriter, r *http.Request) {
	var req peopleMergeApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Merges) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one merge is required"))
		return
	}
	if len(req.Merges) > 50 {
		writeError(w, http.StatusBadRequest, errors.New("merge batch is too large"))
		return
	}

	results := make([]map[string]any, 0, len(req.Merges))
	for _, merge := range req.Merges {
		if merge.FromID <= 0 || merge.IntoID <= 0 || merge.FromID == merge.IntoID {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid merge from=%d into=%d", merge.FromID, merge.IntoID))
			return
		}
		result, err := person.MergePersons(r.Context(), s.db, merge.FromID, merge.IntoID)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, fmt.Errorf("merge %d into %d: %w", merge.FromID, merge.IntoID, err))
			return
		}
		results = append(results, map[string]any{
			"from_id": merge.FromID,
			"into_id": merge.IntoID,
			"result":  result,
		})
	}

	refreshed := false
	if req.Refresh {
		if err := refresh.RefreshAll(r.Context(), s.db); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh after merge: %w", err))
			return
		}
		refreshed = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"merged":    len(results),
		"results":   results,
		"refreshed": refreshed,
	})
}

func (s *Server) loadExcludedPeople(ctx context.Context, limit int) ([]people.PagePerson, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mpc.person_id, mpc.canonical_name, mpc.primary_email, mpc.domain, mpc.email_count,
		       mpc.total_messages, mpc.from_contact_count, mpc.to_contact_count,
		       mpc.bidirectional_score, mpc.classification, mpc.exclusion_reason,
		       COALESCE(mpc.first_message_at, ''), COALESCE(mpc.last_message_at, ''),
		       COALESCE(sm.structural_role, ''), sm.cluster_id,
		       COALESCE(sc.label, ''), sm.dormancy_days
		FROM memento_people_candidates mpc
		JOIN memento_person mp ON mp.id = mpc.person_id
		LEFT JOIN memento_social_metric sm ON sm.person_id = mpc.person_id
		LEFT JOIN memento_social_cluster sc ON sc.cluster_id = sm.cluster_id
		WHERE mpc.classification = 'excluded'
		  AND mp.dismissed_at IS NULL
		ORDER BY mpc.total_messages DESC, mpc.last_message_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExcludedPeople(rows)
}

func (s *Server) loadExcludedPerson(ctx context.Context, personID int64) (people.PagePerson, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mpc.person_id, mpc.canonical_name, mpc.primary_email, mpc.domain, mpc.email_count,
		       mpc.total_messages, mpc.from_contact_count, mpc.to_contact_count,
		       mpc.bidirectional_score, mpc.classification, mpc.exclusion_reason,
		       COALESCE(mpc.first_message_at, ''), COALESCE(mpc.last_message_at, ''),
		       COALESCE(sm.structural_role, ''), sm.cluster_id,
		       COALESCE(sc.label, ''), sm.dormancy_days
		FROM memento_people_candidates mpc
		JOIN memento_person mp ON mp.id = mpc.person_id
		LEFT JOIN memento_social_metric sm ON sm.person_id = mpc.person_id
		LEFT JOIN memento_social_cluster sc ON sc.cluster_id = sm.cluster_id
		WHERE mpc.person_id = ?
		  AND mpc.classification = 'excluded'
		  AND mp.dismissed_at IS NULL
		LIMIT 1
	`, personID)
	if err != nil {
		return people.PagePerson{}, err
	}
	defer rows.Close()
	persons, err := scanExcludedPeople(rows)
	if err != nil {
		return people.PagePerson{}, err
	}
	if len(persons) == 0 {
		return people.PagePerson{}, sql.ErrNoRows
	}

	p := persons[0]

	// Load aliases dynamically
	aliasRows, err := s.db.QueryContext(ctx, `
		SELECT email_address, display_name, link_source, locked
		FROM memento_person_email
		WHERE person_id = ?
		ORDER BY locked DESC, email_address ASC
	`, personID)
	if err != nil {
		return people.PagePerson{}, err
	}
	defer aliasRows.Close()

	for aliasRows.Next() {
		var a people.Alias
		var lockedInt int
		if err := aliasRows.Scan(&a.EmailAddress, &a.DisplayName, &a.LinkSource, &lockedInt); err != nil {
			return people.PagePerson{}, err
		}
		a.Locked = lockedInt != 0
		p.Aliases = append(p.Aliases, a)
	}
	if err := aliasRows.Err(); err != nil {
		return people.PagePerson{}, err
	}

	// Load timeline dynamically
	timelineRows, err := s.db.QueryContext(ctx, `
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
			FROM person_participants pp
			CROSS JOIN messages m ON m.sender_id = pp.id
			WHERE pp.id NOT IN (SELECT id FROM account_participants)
			UNION ALL
			SELECT pp.person_id, mr.message_id, m.sent_at,
			       'to_contact' AS direction, pp.email_address AS via_email
			FROM person_participants pp
			CROSS JOIN message_recipients mr ON mr.participant_id = pp.id
			  AND mr.recipient_type IN ('to', 'cc', 'bcc', 'mention')
			CROSS JOIN messages m ON m.id = mr.message_id
			WHERE m.sender_id IN (SELECT id FROM account_participants)
		),
		ranked AS (
			SELECT person_id, message_id,
			       COALESCE(sent_at, '') AS sent_at,
			       direction, via_email,
			       ROW_NUMBER() OVER (
			         PARTITION BY person_id
			         ORDER BY sent_at DESC, message_id DESC
			       ) AS rn
			FROM involvement
		)
		SELECT r.message_id, r.sent_at, r.direction, r.via_email,
		       COALESCE(m.subject, '') AS subject, COALESCE(m.snippet, '') AS snippet
		FROM ranked r
		JOIN messages m ON m.id = r.message_id
		WHERE r.rn <= 20
	`, personID)
	if err != nil {
		return people.PagePerson{}, err
	}
	defer timelineRows.Close()

	for timelineRows.Next() {
		var t people.TimelineEntry
		var sentAt string
		if err := timelineRows.Scan(&t.MessageID, &sentAt, &t.Direction, &t.ViaEmail, &t.Subject, &t.Snippet); err != nil {
			return people.PagePerson{}, err
		}
		t.Date = parseDBTime(sentAt)
		p.Timeline = append(p.Timeline, t)
	}
	if err := timelineRows.Err(); err != nil {
		return people.PagePerson{}, err
	}

	return p, nil
}

func scanExcludedPeople(rows *sql.Rows) ([]people.PagePerson, error) {
	var persons []people.PagePerson
	for rows.Next() {
		var p people.PagePerson
		var first, last string
		var clusterID, dormancyDays sql.NullInt64
		if err := rows.Scan(
			&p.PersonID, &p.CanonicalName, &p.PrimaryEmail, &p.Domain, &p.EmailCount,
			&p.TotalMessages, &p.FromContactCount, &p.ToContactCount,
			&p.BidirectionalScore, &p.Classification, &p.ExclusionReason, &first, &last,
			&p.StructuralRole, &clusterID, &p.ClusterLabel, &dormancyDays,
		); err != nil {
			return nil, err
		}
		p.FirstMessageAt = first
		p.LastMessageAt = last
		p.Slug = fmt.Sprintf("excluded-%d", p.PersonID)
		p.Aliases = []people.Alias{}
		p.Timeline = []people.TimelineEntry{}
		if clusterID.Valid {
			p.ClusterID = &clusterID.Int64
		}
		if dormancyDays.Valid {
			p.DormancyDays = &dormancyDays.Int64
		}
		persons = append(persons, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if persons == nil {
		persons = []people.PagePerson{}
	}
	return persons, nil
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

func (s *Server) handleGetPersonBySlug(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	ctx := r.Context()

	var p people.PagePerson
	var first, last, pslug, aliasesJSON, timelineJSON, correspondentsJSON string
	var clusterID, dormancyDays sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT pr.person_id, pr.canonical_name, pr.primary_email, pr.domain, pr.email_count,
		       pr.total_messages, pr.from_contact_count, pr.to_contact_count,
		       pr.bidirectional_score, pr.classification,
		       COALESCE(pr.first_message_at, ''), COALESCE(pr.last_message_at, ''),
		       pr.slug, pr.aliases_json, pr.timeline_json, pr.top_correspondents_json,
		       COALESCE(sm.structural_role, ''), sm.cluster_id,
		       COALESCE(sc.label, ''), sm.dormancy_days
		FROM memento_people_report pr
		LEFT JOIN memento_social_metric sm ON sm.person_id = pr.person_id
		LEFT JOIN memento_social_cluster sc ON sc.cluster_id = sm.cluster_id
		WHERE pr.slug = ?
	`, slug).Scan(
		&p.PersonID, &p.CanonicalName, &p.PrimaryEmail, &p.Domain, &p.EmailCount,
		&p.TotalMessages, &p.FromContactCount, &p.ToContactCount,
		&p.BidirectionalScore, &p.Classification, &first, &last,
		&pslug, &aliasesJSON, &timelineJSON, &correspondentsJSON,
		&p.StructuralRole, &clusterID, &p.ClusterLabel, &dormancyDays,
	)
	if err == sql.ErrNoRows && strings.HasPrefix(slug, "excluded-") {
		personID, parseErr := strconv.ParseInt(strings.TrimPrefix(slug, "excluded-"), 10, 64)
		if parseErr == nil {
			excluded, loadErr := s.loadExcludedPerson(ctx, personID)
			if loadErr == nil {
				writeJSON(w, http.StatusOK, excluded)
				return
			}
			if loadErr != sql.ErrNoRows && !isNotSetUp(loadErr) {
				writeError(w, http.StatusInternalServerError, loadErr)
				return
			}
		}
	}
	if err == sql.ErrNoRows || isNotSetUp(err) {
		writeError(w, http.StatusNotFound, errors.New("person not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	p.FirstMessageAt = first
	p.LastMessageAt = last
	p.Slug = pslug
	if clusterID.Valid {
		p.ClusterID = &clusterID.Int64
	}
	if dormancyDays.Valid {
		p.DormancyDays = &dormancyDays.Int64
	}
	_ = json.Unmarshal([]byte(aliasesJSON), &p.Aliases)
	_ = json.Unmarshal([]byte(timelineJSON), &p.Timeline)
	_ = json.Unmarshal([]byte(correspondentsJSON), &p.TopCorrespondents)
	if p.Aliases == nil {
		p.Aliases = []people.Alias{}
	}
	if p.Timeline == nil {
		p.Timeline = []people.TimelineEntry{}
	}
	p.Facets, err = loadPersonFacets(ctx, s.db, p.PersonID)
	if err != nil && !isNotSetUp(err) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	p.Narrative, err = loadPersonNarrative(ctx, s.db, p.PersonID)
	if err != nil && !isNotSetUp(err) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	p.Attributes, err = loadPersonAttributes(ctx, s.db, p.PersonID)
	if err != nil && !isNotSetUp(err) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleGetProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := s.db.QueryContext(ctx, `
		SELECT summary_json FROM memento_projects_report ORDER BY rowid
	`)
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, map[string]any{"generated_at": "", "projects": []any{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var summaries []project.ProjectSummary
	for rows.Next() {
		var summaryJSON string
		if err := rows.Scan(&summaryJSON); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var s project.ProjectSummary
		if err := json.Unmarshal([]byte(summaryJSON), &s); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		summaries = append(summaries, s)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if summaries == nil {
		summaries = []project.ProjectSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": s.reportMeta(ctx, "projects"),
		"projects":     summaries,
	})
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	report, err := project.BuildProjectReport(r.Context(), s.db, slug)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleGetNewsletters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := s.db.QueryContext(ctx, `
		SELECT source_id, slug, display_name, sender_email, domain,
		       message_count, unsubscribe_count,
		       first_seen, last_seen,
		       classification_reason, recent_subjects_json
		FROM memento_newsletters_report
		ORDER BY message_count DESC
	`)
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, map[string]any{"sources": []any{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var sources []newsletter.IndexSource
	for rows.Next() {
		var src newsletter.IndexSource
		var firstSeen, lastSeen sql.NullString
		var subjectsJSON string
		if err := rows.Scan(
			&src.ID, &src.Slug, &src.DisplayName, &src.SenderEmail, &src.Domain,
			&src.MessageCount, &src.UnsubscribeCount,
			&firstSeen, &lastSeen,
			&src.ClassificationReason, &subjectsJSON,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if firstSeen.Valid {
			src.FirstSeen = &firstSeen.String
		}
		if lastSeen.Valid {
			src.LastSeen = &lastSeen.String
		}
		_ = json.Unmarshal([]byte(subjectsJSON), &src.RecentSubjects)
		if src.RecentSubjects == nil {
			src.RecentSubjects = []string{}
		}
		sources = append(sources, src)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if sources == nil {
		sources = []newsletter.IndexSource{}
	}
	writeJSON(w, http.StatusOK, newsletter.IndexReport{
		GeneratedAt: s.reportMeta(ctx, "newsletters"),
		Sources:     sources,
	})
}

func (s *Server) handleGetNewsletter(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	limit := parseIntQuery(r, "timeline", defaultTimelineLimit)
	page, err := newsletter.BuildPage(r.Context(), s.db, slug, limit)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleDismissPerson(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var res sql.Result
	var err error
	if strings.HasPrefix(slug, "excluded-") {
		personID, parseErr := strconv.ParseInt(strings.TrimPrefix(slug, "excluded-"), 10, 64)
		if parseErr != nil {
			writeError(w, http.StatusNotFound, errors.New("person not found"))
			return
		}
		res, err = s.db.ExecContext(r.Context(), `
			UPDATE memento_person SET dismissed_at = CURRENT_TIMESTAMP WHERE id = ?
		`, personID)
	} else {
		res, err = s.db.ExecContext(r.Context(), `
			UPDATE memento_person SET dismissed_at = CURRENT_TIMESTAMP
			WHERE id = (SELECT person_id FROM memento_people_report WHERE slug = ?)
		`, slug)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, errors.New("person not found"))
		return
	}
	if _, err := refresh.RefreshPeopleReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh people report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolvePersonIDFromSlug(ctx context.Context, slug string) (int64, error) {
	if strings.HasPrefix(slug, "excluded-") {
		personID, err := strconv.ParseInt(strings.TrimPrefix(slug, "excluded-"), 10, 64)
		if err != nil {
			return 0, sql.ErrNoRows
		}
		return personID, nil
	}

	var personID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT person_id FROM memento_people_report WHERE slug = ?
	`, slug).Scan(&personID)
	if err != nil {
		return 0, err
	}
	return personID, nil
}

func (s *Server) handlePatchPersonMemory(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Kind        string `json:"kind"`
		Section     string `json:"section"`
		FacetID     int64  `json:"facet_id"`
		AttributeID int64  `json:"attribute_id"`
		Category    string `json:"category"`
		Label       string `json:"label"`
		Value       string `json:"value"`
		DateValue   string `json:"date_value"`
		Content     string `json:"content"`
	}

	var req reqBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	req.Content = strings.TrimSpace(req.Content)
	req.Label = strings.TrimSpace(req.Label)
	req.Value = strings.TrimSpace(req.Value)
	req.DateValue = strings.TrimSpace(req.DateValue)
	if req.Kind != "attribute" && req.Kind != "attribute_delete" && req.Content == "" {
		writeError(w, http.StatusBadRequest, errors.New("content is required"))
		return
	}

	slug := r.PathValue("slug")
	personID, err := s.resolvePersonIDFromSlug(r.Context(), slug)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, errors.New("person not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	switch req.Kind {
	case "narrative":
		if !allowedPersonSections[req.Section] {
			writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported section %q", req.Section))
			return
		}

		err := s.db.QueryRowContext(r.Context(), `
			SELECT 1
			FROM memento_person_narrative
			WHERE person_id = ? AND section = ?
		`, personID, req.Section).Scan(new(int))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := s.db.ExecContext(r.Context(), `
				INSERT INTO memento_person_narrative (
					person_id, section, content, source_message_ids, generated_at, edited_by
				) VALUES (?, ?, ?, '[]', CURRENT_TIMESTAMP, 'user')
			`, personID, req.Section, req.Content); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		} else {
			if _, err := s.db.ExecContext(r.Context(), `
				UPDATE memento_person_narrative
				SET content = ?, edited_by = 'user', generated_at = CURRENT_TIMESTAMP
				WHERE person_id = ? AND section = ?
			`, req.Content, personID, req.Section); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
	case "facet":
		if req.FacetID <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("facet_id must be > 0"))
			return
		}
		res, err := s.db.ExecContext(r.Context(), `
			UPDATE memento_person_facet
			SET content = ?, edited_by = 'user', generated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND person_id = ?
		`, req.Content, req.FacetID, personID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, errors.New("facet not found"))
			return
		}
	case "facet_delete":
		if req.FacetID <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("facet_id must be > 0"))
			return
		}
		res, err := s.db.ExecContext(r.Context(), `
			DELETE FROM memento_person_facet
			WHERE id = ? AND person_id = ?
		`, req.FacetID, personID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, errors.New("facet not found"))
			return
		}
	case "attribute":
		if req.AttributeID <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("attribute_id must be > 0"))
			return
		}
		if req.Label == "" || req.Value == "" {
			writeError(w, http.StatusBadRequest, errors.New("label and value are required"))
			return
		}
		res, err := s.db.ExecContext(r.Context(), `
			UPDATE memento_person_attribute
			SET label = ?, value = ?, date_value = NULLIF(?, ''), edited_by = 'user', generated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND person_id = ?
		`, req.Label, req.Value, req.DateValue, req.AttributeID, personID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, errors.New("attribute not found"))
			return
		}
	case "attribute_delete":
		if req.AttributeID <= 0 {
			writeError(w, http.StatusBadRequest, errors.New("attribute_id must be > 0"))
			return
		}
		res, err := s.db.ExecContext(r.Context(), `
			DELETE FROM memento_person_attribute
			WHERE id = ? AND person_id = ?
		`, req.AttributeID, personID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeError(w, http.StatusNotFound, errors.New("attribute not found"))
			return
		}
	default:
		writeError(w, http.StatusBadRequest, errors.New("kind must be one of 'narrative', 'facet', 'facet_delete', 'attribute', or 'attribute_delete'"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleOverrideClassification(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	var personID int64
	var err error
	personID, err = s.resolvePersonIDFromSlug(r.Context(), slug)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, errors.New("person not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var exists bool
	err = s.db.QueryRowContext(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM memento_person WHERE id = ? AND dismissed_at IS NULL)
	`, personID).Scan(&exists)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, errors.New("person not found or dismissed"))
		return
	}

	var req struct {
		Classification string `json:"classification"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	if req.Classification != "human" && req.Classification != "excluded" {
		writeError(w, http.StatusBadRequest, errors.New("classification must be either 'human' or 'excluded'"))
		return
	}

	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO memento_classification_override (person_id, classification_override, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(person_id) DO UPDATE SET
			classification_override = excluded.classification_override,
			updated_at = CURRENT_TIMESTAMP
	`, personID, req.Classification)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	// 1. Rebuild candidates table to apply overrides immediately.
	candReport, err := people.BuildCandidateReport(r.Context(), s.reader, people.CandidateOptions{Full: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("build candidate report: %w", err))
		return
	}
	if err := people.PersistCandidateReport(r.Context(), s.db, candReport); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("persist candidates: %w", err))
		return
	}

	// 2. Run full refresh to update memento_people_report and social graph.
	if err := refresh.RefreshAll(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh all: %w", err))
		return
	}

	var newSlug string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT slug FROM memento_people_report WHERE person_id = ?
	`, personID).Scan(&newSlug)
	if err == sql.ErrNoRows {
		if req.Classification == "excluded" {
			newSlug = fmt.Sprintf("excluded-%d", personID)
		} else {
			newSlug = ""
		}
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"slug": newSlug})
}

func (s *Server) handleDismissProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_project SET dismissed_at = CURRENT_TIMESTAMP WHERE slug = ?
	`, slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, errors.New("project not found"))
		return
	}
	if _, err := refresh.RefreshProjectsReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh projects report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissConcept(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE memento_concept SET dismissed_at = CURRENT_TIMESTAMP WHERE slug = ?
	`, slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, errors.New("concept not found"))
		return
	}
	if _, err := refresh.RefreshConceptsReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh concepts report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissNewsletter(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	err := newsletter.DismissSource(r.Context(), s.db, slug)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, errors.New("newsletter not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := refresh.RefreshNewslettersReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh newsletters report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type renameRequest struct {
	Name string `json:"name"`
}

func decodeRename(r *http.Request) (string, error) {
	var body renameRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", errors.New("invalid JSON body")
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return "", errors.New("name is required")
	}
	if len(name) > 200 {
		return "", errors.New("name must be 200 characters or fewer")
	}
	return name, nil
}

func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	name, err := decodeRename(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE memento_project SET name = ? WHERE slug = ?`, name, slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, errors.New("project not found"))
		return
	}
	if _, err := refresh.RefreshProjectsReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh projects report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRenameConcept(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	name, err := decodeRename(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE memento_concept SET name = ? WHERE slug = ?`, name, slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, errors.New("concept not found"))
		return
	}
	if _, err := refresh.RefreshConceptsReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh concepts report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRenameNewsletter(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	name, err := decodeRename(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(), `UPDATE memento_newsletter_source SET display_name = ? WHERE slug = ?`, name, slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, errors.New("newsletter not found"))
		return
	}
	if _, err := refresh.RefreshNewslettersReport(r.Context(), s.db); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("refresh newsletters report: %w", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetConcepts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	rows, err := s.db.QueryContext(ctx, `
		SELECT payload_json FROM memento_concepts_report ORDER BY name ASC
	`)
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, map[string]any{"generated_at": "", "concepts": []any{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var entries []concept.ConceptIndexEntry
	for rows.Next() {
		var payloadJSON string
		if err := rows.Scan(&payloadJSON); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var e concept.ConceptIndexEntry
		if err := json.Unmarshal([]byte(payloadJSON), &e); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if entries == nil {
		entries = []concept.ConceptIndexEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": s.reportMeta(ctx, "concepts"),
		"concepts":     entries,
	})
}

func (s *Server) handleGetConcept(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	report, err := concept.BuildConceptReport(r.Context(), s.db, slug)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// --- SEARCH (msgvault CLI proxy) ---

type searchResultItem struct {
	ID      int64  `json:"id"`
	Subject string `json:"subject,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	SentAt  string `json:"sent_at,omitempty"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing query param 'q'"))
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "hybrid"
	}
	limit := parseIntQuery(r, "limit", 20)
	results, err := runMsgvaultSearch(r.Context(), q, mode, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"mode":    mode,
		"limit":   limit,
		"results": results,
	})
}

// runMsgvaultSearch uses the msgvault HTTP API when configured, then falls back
// to the msgvault CLI. Hybrid falls back to FTS, and FTS-only query syntax skips
// hybrid up front to avoid msgvault FTS5 parser errors.
func runMsgvaultSearch(ctx context.Context, query, mode string, limit int) ([]searchResultItem, error) {
	if mode == "hybrid" && msgvaultapi.RequiresFTSMode(query) {
		mode = "fts"
	}
	if client, ok := msgvaultapi.FromEnv(); ok {
		results, err := runMsgvaultAPISearch(ctx, client, query, mode, limit)
		if err == nil {
			return results, nil
		}
		if mode != "fts" {
			results, err = runMsgvaultAPISearch(ctx, client, query, "fts", limit)
			if err == nil {
				return results, nil
			}
		}
	}

	args := []string{"search", query, "--mode", mode, "--json", "--limit", strconv.Itoa(limit)}
	cmd := exec.CommandContext(ctx, "msgvault", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if mode != "fts" {
			return runMsgvaultSearch(ctx, query, "fts", limit)
		}
		return nil, fmt.Errorf("msgvault search %q failed: %w (stderr: %s)", query, err, stderr.String())
	}
	raw := stdout.Bytes()
	if idx := bytes.IndexAny(raw, "{["); idx > 0 {
		raw = raw[idx:]
	}
	// Hybrid response: {"results": [...]}; FTS response: bare array.
	var hybrid struct {
		Results []searchResultItem `json:"results"`
	}
	if err := json.Unmarshal(raw, &hybrid); err == nil && hybrid.Results != nil {
		return hybrid.Results, nil
	}
	var fts []searchResultItem
	if err := json.Unmarshal(raw, &fts); err != nil {
		return nil, fmt.Errorf("unparseable msgvault output: %s", strings.TrimSpace(stdout.String()))
	}
	return fts, nil
}

func runMsgvaultAPISearch(ctx context.Context, client *msgvaultapi.Client, query, mode string, limit int) ([]searchResultItem, error) {
	response, err := client.Search(ctx, query, mode, limit, false)
	if err != nil {
		return nil, err
	}
	items := response.Items()
	results := make([]searchResultItem, 0, len(items))
	for _, item := range items {
		results = append(results, searchResultItem{
			ID:      item.ID,
			Subject: item.Subject,
			Snippet: item.Snippet,
			SentAt:  item.SentAt,
		})
	}
	return results, nil
}

// --- REFRESH / GENERATE (background jobs) ---

func (s *Server) handlePeopleRefresh(w http.ResponseWriter, r *http.Request) {
	job := s.jobs.Create("people-refresh")
	go s.runPeopleRefresh(job.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

func (s *Server) handleNewsletterDetect(w http.ResponseWriter, r *http.Request) {
	job := s.jobs.Create("newsletter-detect")
	go s.runNewsletterDetect(job.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

func (s *Server) handleNewsletterGenerate(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	job := s.jobs.Create("newsletter-generate:" + slug)
	go s.runNewsletterGenerate(job.ID, slug)
	writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
}

// --- JOB STREAM (Server-Sent Events) ---

func (s *Server) handleJobStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job := s.jobs.Get(id)
	if job == nil {
		writeError(w, http.StatusNotFound, errors.New("job not found"))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("server does not support streaming"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	events, unsub, ok := s.jobs.Subscribe(id)
	if !ok {
		return
	}
	defer unsub()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-events:
			if !open {
				return
			}
			payload, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			if ev.Status == JobSucceeded || ev.Status == JobFailed {
				return
			}
		}
	}
}

func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	snapshot, ok := s.jobs.Snapshot(id)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("job not found"))
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

// --- CONFIG ---

type configResponse struct {
	DBPath         string   `json:"db_path"`
	Port           int      `json:"port"`
	Origins        []string `json:"cors_allowed_origins"`
	OwnerName      string   `json:"owner_name,omitempty"`
	OwnerEmail     string   `json:"owner_email,omitempty"`
	OwnerAvatarURL string   `json:"owner_avatar_url,omitempty"`
	PrivacyEnabled bool     `json:"privacy_enabled"`
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ownerName, _ := store.GetConfig(ctx, s.db, "owner_name")
	ownerEmail, _ := store.GetConfig(ctx, s.db, "owner_email")
	privacyVal, _ := store.GetConfig(ctx, s.db, "privacy_enabled")
	privacyEnabled := privacyVal != "false"
	writeJSON(w, http.StatusOK, configResponse{
		DBPath:         s.reader.Path(),
		Port:           s.opts.Port,
		Origins:        s.opts.AllowedOrigins,
		OwnerName:      ownerName,
		OwnerEmail:     ownerEmail,
		OwnerAvatarURL: avatar.LocalURL(ownerEmail, 256, avatar.InitialsFromName(ownerName, ownerEmail)),
		PrivacyEnabled: privacyEnabled,
	})
}

type postConfigRequest struct {
	PrivacyEnabled *bool `json:"privacy_enabled"`
}

func (s *Server) handlePostConfig(w http.ResponseWriter, r *http.Request) {
	var req postConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid JSON body"))
		return
	}
	ctx := r.Context()
	if req.PrivacyEnabled != nil {
		val := "false"
		if *req.PrivacyEnabled {
			val = "true"
		}
		if err := store.SetConfig(ctx, s.db, "privacy_enabled", val); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	ownerName, _ := store.GetConfig(ctx, s.db, "owner_name")
	ownerEmail, _ := store.GetConfig(ctx, s.db, "owner_email")
	privacyVal, _ := store.GetConfig(ctx, s.db, "privacy_enabled")
	privacyEnabled := privacyVal != "false"

	writeJSON(w, http.StatusOK, configResponse{
		DBPath:         s.reader.Path(),
		Port:           s.opts.Port,
		Origins:        s.opts.AllowedOrigins,
		OwnerName:      ownerName,
		OwnerEmail:     ownerEmail,
		OwnerAvatarURL: avatar.LocalURL(ownerEmail, 256, avatar.InitialsFromName(ownerName, ownerEmail)),
		PrivacyEnabled: privacyEnabled,
	})
}

// --- Helpers ---

func parseIntQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// withStoreDB returns a *sql.DB opened in read-write mode (the same instance
// the server already holds — exposed for symmetry).
func (s *Server) withStoreDB() (interface{}, error) { return s.db, nil }

// handleGetArchiveActivity returns yearly message counts from msgvault for the
// dashboard "Archive Activity" widget. Ad-hoc GROUP BY on messages.sent_at —
// fast enough at archive sizes we care about, no rollup table needed.
func (s *Server) handleGetArchiveActivity(w http.ResponseWriter, r *http.Request) {
	type yearBucket struct {
		Year  int   `json:"year"`
		Count int64 `json:"count"`
	}
	empty := map[string]any{
		"years":            []yearBucket{},
		"total_messages":   0,
		"first_year":       0,
		"last_year":        0,
		"first_message_at": "",
		"last_message_at":  "",
		"peak_year":        0,
		"peak_count":       0,
	}

	rows, err := s.reader.DB().QueryContext(r.Context(), `
		SELECT CAST(strftime('%Y', sent_at) AS INTEGER) AS year,
		       COUNT(*) AS message_count
		FROM messages
		WHERE sent_at IS NOT NULL AND sent_at <> ''
		GROUP BY year
		ORDER BY year ASC
	`)
	if isNotSetUp(err) {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	years := []yearBucket{}
	var total int64
	var peakYear int
	var peakCount int64
	for rows.Next() {
		var b yearBucket
		if err := rows.Scan(&b.Year, &b.Count); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if b.Year == 0 {
			// Unparseable sent_at — skip rather than show a bogus year-0 bar.
			continue
		}
		years = append(years, b)
		total += b.Count
		if b.Count > peakCount {
			peakCount = b.Count
			peakYear = b.Year
		}
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if len(years) == 0 {
		writeJSON(w, http.StatusOK, empty)
		return
	}

	var firstAt, lastAt sql.NullString
	if err := s.reader.DB().QueryRowContext(r.Context(), `
		SELECT MIN(sent_at), MAX(sent_at)
		FROM messages
		WHERE sent_at IS NOT NULL AND sent_at <> ''
	`).Scan(&firstAt, &lastAt); err != nil && err != sql.ErrNoRows {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"years":            years,
		"total_messages":   total,
		"first_year":       years[0].Year,
		"last_year":        years[len(years)-1].Year,
		"first_message_at": firstAt.String,
		"last_message_at":  lastAt.String,
		"peak_year":        peakYear,
		"peak_count":       peakCount,
	})
}

// Ensure store import is used even if some refactors trim it.
var _ = store.Migrate
