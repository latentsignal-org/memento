package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/config"
	"memento/backend/internal/people"
	"memento/backend/internal/refresh"
	"memento/backend/internal/store"
)

type createAgentRunRequest struct {
	AgentType             string         `json:"agent_type"`
	EntityID              string         `json:"entity_id"`
	UserMessage           string         `json:"user_message"`
	PreviousInteractionID string         `json:"previous_interaction_id"`
	Provider              string         `json:"provider"`
	Model                 string         `json:"model"`
	RequestMetadata       map[string]any `json:"request_metadata"`
}

type concurrentAgentRunError struct {
	SessionType string
	EntityID    string
	ActiveRunID int64
}

func (e *concurrentAgentRunError) Error() string {
	return fmt.Sprintf("%s already in progress for %q (run %d)", e.SessionType, e.EntityID, e.ActiveRunID)
}

func (s *Server) handleCreateAgentRun(w http.ResponseWriter, r *http.Request) {
	var req createAgentRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	spec, err := s.buildAgentRunSpec(r.Context(), req)
	if err != nil {
		if concurrent, ok := errors.AsType[*concurrentAgentRunError](err); ok {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":         err.Error(),
				"active_run_id": concurrent.ActiveRunID,
			})
			return
		}
		writeError(w, http.StatusBadRequest, err)
		return
	}
	askTurnID, hasAskTurn := askTurnIDFromMetadata(spec.RequestMetadata)
	hasAskTurn = hasAskTurn && spec.AgentType == agentrunner.AgentDashboard
	runID, err := s.agents.Start(r.Context(), spec)
	if err != nil {
		if hasAskTurn {
			_ = store.MarkAskTurnFailed(r.Context(), s.db, askTurnID)
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	resp := map[string]any{
		"run_id":     runID,
		"session_id": runID,
	}
	if hasAskTurn {
		// Link the product turn to its debug run, and surface the Ask
		// Session identity so the client can pin it in the URL.
		_ = store.LinkAskTurnRun(r.Context(), s.db, askTurnID, runID)
		resp["ask_session_id"] = spec.RequestMetadata["ask_session_id"]
		resp["ask_session_slug"] = spec.RequestMetadata["ask_session_slug"]
		resp["ask_turn_id"] = askTurnID
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// askTurnIDFromMetadata reads the ask turn id stamped by buildAgentRunSpec
// for dashboard runs. Other agent types have no Ask Session linkage.
func askTurnIDFromMetadata(meta map[string]any) (int64, bool) {
	raw, ok := meta["ask_turn_id"]
	if !ok || raw == nil {
		return 0, false
	}
	id, err := metadataInt64(raw)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (s *Server) handleAgentRunEvents(w http.ResponseWriter, r *http.Request) {
	runID, err := parseRunID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	afterSeq := agentrunner.ParseAfterSeq(r.URL.Query().Get("after_seq"))
	if last := r.Header.Get("Last-Event-ID"); last != "" && afterSeq == 0 {
		afterSeq = agentrunner.ParseAfterSeq(last)
	}
	if _, err := store.GetAgentRun(r.Context(), s.db, runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, fmt.Errorf("run %d not found", runID))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	s.streamAgentRun(w, r, runID, afterSeq)
}

// streamAgentRun writes the SSE event stream for an agent run, starting after
// afterSeq. Shared by the internal events endpoint and the browser-facing
// composed start+stream endpoints (generate/enrich/turn).
func (s *Server) streamAgentRun(w http.ResponseWriter, r *http.Request, runID int64, afterSeq int64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsubscribe := s.agents.Subscribe(runID)
	defer unsubscribe()

	if err := s.replayAgentEvents(r.Context(), w, flusher, runID, &afterSeq); err != nil {
		return
	}
	if s.agents.IsTerminal(r.Context(), runID) {
		_ = s.replayAgentEvents(r.Context(), w, flusher, runID, &afterSeq)
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				_ = s.replayAgentEvents(r.Context(), w, flusher, runID, &afterSeq)
				return
			}
			if ev.Seq <= afterSeq {
				continue
			}
			if err := writeAgentSSE(w, ev); err != nil {
				return
			}
			flusher.Flush()
			afterSeq = ev.Seq
		case <-heartbeat.C:
			if err := s.replayAgentEvents(r.Context(), w, flusher, runID, &afterSeq); err != nil {
				return
			}
			if s.agents.IsTerminal(r.Context(), runID) {
				_ = s.replayAgentEvents(r.Context(), w, flusher, runID, &afterSeq)
				return
			}
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleCancelAgentRun(w http.ResponseWriter, r *http.Request) {
	runID, err := parseRunID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.agents.Cancel(r.Context(), runID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeAgentSSE(w http.ResponseWriter, ev store.AgentEvent) error {
	payload := strings.TrimSpace(ev.PayloadJSON)
	if payload == "" {
		payload = "{}"
	}
	_, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Seq, payload)
	return err
}

func (s *Server) replayAgentEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, runID int64, afterSeq *int64) error {
	events, err := store.ListAgentEventsAfter(ctx, s.db, runID, *afterSeq)
	if err != nil {
		return err
	}
	for _, ev := range events {
		if err := writeAgentSSE(w, ev); err != nil {
			return err
		}
		flusher.Flush()
		*afterSeq = ev.Seq
	}
	return nil
}

func parseRunID(r *http.Request) (int64, error) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid run id")
	}
	return id, nil
}

func (s *Server) buildAgentRunSpec(ctx context.Context, req createAgentRunRequest) (agentrunner.RunSpec, error) {
	if req.AgentType == "" || req.EntityID == "" {
		return agentrunner.RunSpec{}, fmt.Errorf("agent_type and entity_id are required")
	}
	meta := req.RequestMetadata
	if meta == nil {
		meta = map[string]any{}
	}
	provider := req.Provider
	if provider == "" {
		provider = config.ModelProvider()
	}
	model := req.Model
	if model == "" {
		model = modelForAgentRun(req.AgentType, provider)
	}
	spec := agentrunner.RunSpec{
		AgentType:             agentrunner.AgentType(req.AgentType),
		EntityID:              req.EntityID,
		UserMessage:           req.UserMessage,
		PreviousInteractionID: req.PreviousInteractionID,
		Provider:              provider,
		Model:                 model,
		RequestMetadata:       meta,
		MaxSteps:              agentStepLimit(),
	}
	switch agentrunner.AgentType(req.AgentType) {
	case agentrunner.AgentCollector:
		var kind, transcriptJSON string
		if err := s.db.QueryRowContext(ctx, `SELECT kind, transcript_json FROM memento_draft WHERE id = ?`, req.EntityID).Scan(&kind, &transcriptJSON); err != nil {
			return spec, fmt.Errorf("draft %s not found: %w", req.EntityID, err)
		}
		meta["draft_id"] = req.EntityID
		meta["kind"] = kind
		spec.InitialTranscript = transcriptFromStoredLines(transcriptJSON)
		spec.System = collectorPrompt(kind)
		spec.Tools = s.toolSchemas("fts_search", "vector_search", "get_message", "get_message_batch", "find_people", "get_thread", "summarize_thread", "propose_bundle", "get_person_summary", "detect_gaps", "detect_gaps_with_results", "context_status", "propose_backfill", "find_missing_collaborators")
		spec.RequiredOutcomes = []agentrunner.OutcomeRequirement{
			requiredAnyTool("collector_close", "propose_bundle", "propose_bundle must stage the curated draft bundle"),
			requiredAnyTool("collector_close", "propose_backfill", "propose_backfill may close a backfill decision turn"),
		}
		spec.MaxRepairAttempts = 1
		spec.AllowClarifyingText = true
		spec.AfterDone = func(ctx context.Context, done agentrunner.AfterDoneContext) error {
			draftID, err := strconv.ParseInt(req.EntityID, 10, 64)
			if err != nil {
				return err
			}
			draft, err := store.GetDraft(ctx, s.db, draftID)
			if err != nil {
				return err
			}
			var transcript []map[string]string
			_ = json.Unmarshal([]byte(draft.TranscriptJSON), &transcript)
			transcript = append(transcript,
				map[string]string{"role": "user", "content": spec.UserMessage},
				map[string]string{"role": "assistant", "content": done.AssistantText},
			)
			raw, _ := json.Marshal(transcript)
			return store.UpdateDraftState(ctx, s.db, draftID, done.InteractionID, string(raw))
		}
	case agentrunner.AgentProjectCompile:
		var id int64
		var name string
		if err := s.db.QueryRowContext(ctx, `SELECT id, name FROM memento_project WHERE slug = ?`, req.EntityID).Scan(&id, &name); err != nil {
			return spec, fmt.Errorf("project %s not found: %w", req.EntityID, err)
		}
		meta["project_id"] = id
		meta["project_slug"] = req.EntityID
		spec.System = projectPrompt(name)
		if spec.UserMessage == "" {
			spec.UserMessage = fmt.Sprintf(`Compile the narrative for project "%s". Follow the workflow exactly: bundle, expand as needed, then four write_section calls.`, name)
		}
		spec.Tools = s.toolSchemas("get_bundle_index", "get_message_batch", "summarize_thread", "get_project_bundle", "get_message", "fts_search", "vector_search", "write_section", "get_person_summary", "detect_gaps", "detect_gaps_with_results", "context_status", "get_person_network", "find_bridges_between")
		spec.RequiredOutcomes = sectionRequirements("write_section", []string{"summary", "phases", "friction_points", "current_understanding"})
		spec.MaxRepairAttempts = 1
		spec.AfterDone = func(ctx context.Context, _ agentrunner.AfterDoneContext) error {
			_, err := refresh.RefreshProjectsReport(ctx, s.db)
			return err
		}
	case agentrunner.AgentConceptCompile:
		var id int64
		var name, scope string
		if err := s.db.QueryRowContext(ctx, `SELECT id, name, scope_description FROM memento_concept WHERE slug = ?`, req.EntityID).Scan(&id, &name, &scope); err != nil {
			return spec, fmt.Errorf("concept %s not found: %w", req.EntityID, err)
		}
		meta["concept_id"] = id
		meta["concept_slug"] = req.EntityID
		spec.System = conceptPrompt(name, scope)
		if spec.UserMessage == "" {
			spec.UserMessage = fmt.Sprintf(`Compile the thematic concept narrative for "%s". Follow the workflow exactly: bundle, cluster, expand only as needed, then three write_concept_section calls.`, name)
		}
		spec.Tools = s.toolSchemas("get_bundle_index", "get_message_batch", "summarize_thread", "get_concept_bundle", "cluster_messages_by_subject", "get_message", "fts_search", "vector_search", "write_concept_section", "get_person_summary", "detect_gaps", "detect_gaps_with_results", "context_status")
		spec.RequiredOutcomes = sectionRequirements("write_concept_section", []string{"scope_summary", "distilled_insights", "evolving_understanding"})
		spec.MaxRepairAttempts = 1
		spec.AfterDone = func(ctx context.Context, _ agentrunner.AfterDoneContext) error {
			_, err := refresh.RefreshConceptsReport(ctx, s.db)
			return err
		}
	case agentrunner.AgentPersonEnrich:
		if activeID, err := store.ActiveAgentRunForEntity(ctx, s.db, string(agentrunner.AgentPersonEnrich), req.EntityID); err != nil {
			return spec, err
		} else if activeID > 0 {
			return spec, &concurrentAgentRunError{
				SessionType: string(agentrunner.AgentPersonEnrich),
				EntityID:    req.EntityID,
				ActiveRunID: activeID,
			}
		}
		var id int64
		var name, email string
		if err := s.db.QueryRowContext(ctx, `SELECT person_id, canonical_name, primary_email FROM memento_people_report WHERE slug = ?`, req.EntityID).Scan(&id, &name, &email); err != nil {
			return spec, fmt.Errorf("person %s not found: %w", req.EntityID, err)
		}
		displayName := name
		if displayName == "" {
			displayName = email
		}
		meta["person_id"] = id
		meta["person_slug"] = req.EntityID
		bootstrap, err := s.buildPersonEnrichBootstrap(ctx, id)
		if err != nil {
			return spec, err
		}
		meta["person_enrich_bootstrap"] = bootstrap
		meta["person_enrich_generation_mode"] = bootstrap.Mode
		meta["person_enrich_replacement_cutoff"] = bootstrap.ReplacementCutoff
		if raw, err := json.Marshal(bootstrap); err == nil {
			spec.InitialTranscript = append(spec.InitialTranscript, agentrunner.ModelMessage{
				Role:    "user",
				Content: "Deterministic bootstrap context for this person_enrich run. Use this before calling any tools; it is also persisted in request_metadata for debug analysis.\n\n```json\n" + string(raw) + "\n```",
			})
		}
		spec.System = personPrompt(displayName)
		if spec.UserMessage == "" {
			spec.UserMessage = fmt.Sprintf(`Enrich the relationship wiki for "%s". Use the preloaded deterministic bootstrap first, expand only as needed, then write structured attributes when strongly evidenced, facets, and the three narrative sections.`, displayName)
		}
		spec.Tools = s.toolSchemas("list_person_messages", "fts_search_scoped", "get_message", "get_message_batch", "write_facet", "write_person_attribute", "record_no_person_attributes", "write_person_section", "get_person_network", "get_group", "get_cluster", "context_status")
		sectionReqs := sectionRequirements("write_person_section", []string{"summary", "relationship_arc", "current_status"})
		for i, req := range sectionReqs {
			if secName, ok := req.ArgEquals["section"]; ok {
				if _, found := bootstrap.ExistingMemory.Narrative[secName]; found {
					// Allow skipping narrative sections if they already exist in the bootstrap (either user-edited or LLM-generated)
					sectionReqs[i] = agentrunner.OutcomeRequirement{}
				}
			}
		}
		spec.RequiredOutcomes = append(
			[]agentrunner.OutcomeRequirement{
				requiredTool("write_facet", "at least one write_facet call must persist a sourced relationship fact"),
				requiredAnyTool("person_attr_decision", "write_person_attribute", "write_person_attribute when strong evidence exists"),
				requiredAnyTool("person_attr_decision", "record_no_person_attributes", "record_no_person_attributes when no strong attribute evidence"),
			},
			sectionReqs...,
		)
		spec.MaxRepairAttempts = 1
		spec.AfterDone = func(ctx context.Context, _ agentrunner.AfterDoneContext) error {
			hasOutput, err := s.personAgentProducedOutput(ctx, id)
			if err != nil {
				return err
			}
			if !hasOutput {
				return fmt.Errorf("person enrich finished without producing any facets or narrative sections")
			}
			if err := s.cleanupSupersededPersonLLMMemory(ctx, id, bootstrap); err != nil {
				return err
			}
			return refresh.RefreshPeopleReportForPerson(ctx, s.db, id)
		}
	case agentrunner.AgentDashboard:
		if spec.UserMessage == "" {
			return spec, fmt.Errorf("user_message is required")
		}
		contextRefs, err := s.validateAskContextRefs(ctx, meta["context_refs"])
		if err != nil {
			return spec, err
		}
		// Every Ask Memento run is recorded as a turn on a product Ask
		// Session (new session when ask_session_id is absent) so the answer
		// survives navigation and debug purges.
		askSession, askTurn, err := s.resolveAskSessionForRun(ctx, meta, spec.UserMessage)
		if err != nil {
			return spec, err
		}
		meta["ask_session_id"] = askSession.ID
		meta["ask_session_slug"] = askSession.Slug
		meta["ask_turn_id"] = askTurn.ID
		if len(contextRefs.Refs) > 0 {
			if err := store.AddAskContextRefs(ctx, s.db, askTurn.ID, contextRefs.Refs); err != nil {
				return spec, err
			}
			meta["context_refs"] = contextRefs.DisplayRefs
			meta["ask_bootstrap"] = contextRefs.Bootstrap
			if len(contextRefs.Warnings) > 0 {
				meta["context_ref_warnings"] = contextRefs.Warnings
			}
			spec.InitialEvents = append(spec.InitialEvents, contextLoadedEvent(contextRefs))
			spec.InitialTranscript = append(spec.InitialTranscript, askBootstrapMessages(contextRefs)...)
		}
		spec.InitialTranscript = append(spec.InitialTranscript, transcriptFromMetadata(meta["history"])...)
		spec.System = mementoPrompt
		spec.Tools = s.toolSchemas("fts_search", "vector_search", "get_message_batch", "summarize_thread", "search_persons", "search_projects", "search_concepts", "get_person_summary", "get_project_summary", "get_concept_summary", "create_project_draft", "create_concept_draft", "detect_gaps", "detect_gaps_with_results", "context_status", "get_person_network", "find_bridges_between")
		spec.AllowClarifyingText = true
		spec.AfterDone = func(ctx context.Context, done agentrunner.AfterDoneContext) error {
			return s.completeAskTurnFromRun(ctx, askTurn.ID, done.RunID, done.AssistantText)
		}
	default:
		return spec, fmt.Errorf("unsupported agent_type %q", req.AgentType)
	}
	return spec, nil
}

type personEnrichBootstrap struct {
	Mode              string                 `json:"mode"`
	ReplacementCutoff string                 `json:"replacement_cutoff"`
	PersonSummary     map[string]any         `json:"person_summary"`
	RecentMessages    []personMessageSummary `json:"recent_messages"`
	ExistingMemory    personBootstrapMemory  `json:"existing_memory"`
}

type personBootstrapMemory struct {
	Facets     []storePersonFacetBootstrap     `json:"facets"`
	Narrative  peopleNarrativeBootstrap        `json:"narrative"`
	Attributes []storePersonAttributeBootstrap `json:"attributes"`
	Counts     map[string]int                  `json:"counts"`
}

type storePersonFacetBootstrap struct {
	ID               int64   `json:"id"`
	FacetType        string  `json:"facet_type"`
	Content          string  `json:"content"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
	EditedBy         string  `json:"edited_by"`
}

type storePersonAttributeBootstrap struct {
	ID               int64   `json:"id"`
	Category         string  `json:"category"`
	Label            string  `json:"label"`
	Value            string  `json:"value"`
	DateValue        string  `json:"date_value,omitempty"`
	SourceMessageIDs []int64 `json:"source_message_ids"`
	EditedBy         string  `json:"edited_by"`
}

type peopleNarrativeBootstrap map[string]map[string]any

func (s *Server) buildPersonEnrichBootstrap(ctx context.Context, personID int64) (personEnrichBootstrap, error) {
	var cutoff string
	if err := s.db.QueryRowContext(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&cutoff); err != nil {
		return personEnrichBootstrap{}, err
	}
	summary, err := s.loadPersonBriefForAgent(ctx, personID)
	if err != nil {
		return personEnrichBootstrap{}, err
	}
	recent, err := listPersonMessages(ctx, s.db, personID, 20, "compact")
	if err != nil && !isNotSetUp(err) {
		return personEnrichBootstrap{}, err
	}
	if recent == nil {
		recent = []personMessageSummary{}
	}
	facets, err := loadPersonFacets(ctx, s.db, personID)
	if err != nil && !isNotSetUp(err) {
		return personEnrichBootstrap{}, err
	}
	narrative, err := loadPersonNarrative(ctx, s.db, personID)
	if err != nil && !isNotSetUp(err) {
		return personEnrichBootstrap{}, err
	}
	attrs, err := loadPersonAttributes(ctx, s.db, personID)
	if err != nil && !isNotSetUp(err) {
		return personEnrichBootstrap{}, err
	}

	memory := personBootstrapMemory{
		Facets:     make([]storePersonFacetBootstrap, 0, len(facets)),
		Narrative:  peopleNarrativeBootstrap{},
		Attributes: make([]storePersonAttributeBootstrap, 0, len(attrs)),
		Counts: map[string]int{
			"facets":     len(facets),
			"attributes": len(attrs),
		},
	}
	for _, f := range facets {
		memory.Facets = append(memory.Facets, storePersonFacetBootstrap{
			ID:               f.ID,
			FacetType:        f.FacetType,
			Content:          f.Content,
			SourceMessageIDs: f.SourceMessageIDs,
			EditedBy:         f.EditedBy,
		})
	}
	addNarrative := func(section string, n peopleNarrativeSectionLike) {
		if strings.TrimSpace(n.content()) == "" {
			return
		}
		memory.Narrative[section] = map[string]any{
			"content":            n.content(),
			"source_message_ids": n.sourceIDs(),
			"edited_by":          n.editedBy(),
		}
	}
	addNarrative("summary", narrativeSectionAdapter(narrative.Summary))
	addNarrative("relationship_arc", narrativeSectionAdapter(narrative.RelationshipArc))
	addNarrative("current_status", narrativeSectionAdapter(narrative.CurrentStatus))
	memory.Counts["narrative_sections"] = len(memory.Narrative)
	for _, attr := range attrs {
		memory.Attributes = append(memory.Attributes, storePersonAttributeBootstrap{
			ID:               attr.ID,
			Category:         attr.Category,
			Label:            attr.Label,
			Value:            attr.Value,
			DateValue:        attr.DateValue,
			SourceMessageIDs: attr.SourceMessageIDs,
			EditedBy:         attr.EditedBy,
		})
	}

	mode := "regenerate"
	if len(memory.Facets) == 0 && len(memory.Narrative) == 0 && len(memory.Attributes) == 0 {
		mode = "first_enrich"
	}
	return personEnrichBootstrap{
		Mode:              mode,
		ReplacementCutoff: cutoff,
		PersonSummary:     summary,
		RecentMessages:    recent,
		ExistingMemory:    memory,
	}, nil
}

type peopleNarrativeSectionLike interface {
	content() string
	sourceIDs() []int64
	editedBy() string
}

type narrativeSectionAdapter people.NarrativeSection

func (n narrativeSectionAdapter) content() string { return people.NarrativeSection(n).Content }
func (n narrativeSectionAdapter) sourceIDs() []int64 {
	return people.NarrativeSection(n).SourceMessageIDs
}
func (n narrativeSectionAdapter) editedBy() string { return people.NarrativeSection(n).EditedBy }

func (s *Server) cleanupSupersededPersonLLMMemory(ctx context.Context, personID int64, bootstrap personEnrichBootstrap) error {
	currentFacets, err := loadPersonFacets(ctx, s.db, personID)
	if err != nil && !isNotSetUp(err) {
		return err
	}
	oldFacetIDs := map[int64]bool{}
	for _, facet := range bootstrap.ExistingMemory.Facets {
		if facet.EditedBy == "llm" {
			oldFacetIDs[facet.ID] = true
		}
	}
	hasNewFacet := false
	for _, f := range currentFacets {
		if f.EditedBy == "llm" && !oldFacetIDs[f.ID] {
			hasNewFacet = true
			break
		}
	}
	if hasNewFacet {
		for _, facet := range bootstrap.ExistingMemory.Facets {
			if facet.EditedBy != "llm" {
				continue
			}
			if _, err := s.db.ExecContext(ctx, `
				DELETE FROM memento_person_facet
				WHERE id = ? AND person_id = ? AND edited_by = 'llm' AND generated_at <= ?
			`, facet.ID, personID, bootstrap.ReplacementCutoff); err != nil {
				return err
			}
		}
	}
	currentAttrs, err := loadPersonAttributes(ctx, s.db, personID)
	if err != nil && !isNotSetUp(err) {
		return err
	}
	oldAttrIDs := map[int64]bool{}
	for _, attr := range bootstrap.ExistingMemory.Attributes {
		if attr.EditedBy == "llm" {
			oldAttrIDs[attr.ID] = true
		}
	}
	hasNewAttr := false
	for _, attr := range currentAttrs {
		if attr.EditedBy == "llm" && !oldAttrIDs[attr.ID] {
			hasNewAttr = true
			break
		}
	}
	if !hasNewAttr {
		return nil
	}
	for _, attr := range bootstrap.ExistingMemory.Attributes {
		if attr.EditedBy != "llm" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			DELETE FROM memento_person_attribute
			WHERE id = ? AND person_id = ? AND edited_by = 'llm' AND generated_at <= ?
		`, attr.ID, personID, bootstrap.ReplacementCutoff); err != nil && !isNotSetUp(err) {
			return err
		}
	}
	return nil
}

func (s *Server) personAgentProducedOutput(ctx context.Context, personID int64) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM memento_person_facet
			 WHERE person_id = ? AND edited_by = 'llm')
			+
			(SELECT COUNT(*) FROM memento_person_narrative
			 WHERE person_id = ? AND edited_by = 'llm' AND trim(content) != '')
	`, personID, personID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func requiredTool(toolName, description string) agentrunner.OutcomeRequirement {
	return agentrunner.OutcomeRequirement{
		ToolName:      toolName,
		RequiredCount: 1,
		Description:   description,
	}
}

func requiredAnyTool(group, toolName, description string) agentrunner.OutcomeRequirement {
	req := requiredTool(toolName, description)
	req.AnyOfGroup = group
	return req
}

func sectionRequirements(toolName string, sections []string) []agentrunner.OutcomeRequirement {
	requirements := make([]agentrunner.OutcomeRequirement, 0, len(sections))
	for _, section := range sections {
		requirements = append(requirements, agentrunner.OutcomeRequirement{
			ToolName:      toolName,
			ArgEquals:     map[string]string{"section": section},
			RequiredCount: 1,
			Description:   fmt.Sprintf("%s(section=%q) must persist successfully", toolName, section),
		})
	}
	return requirements
}

func defaultModelProvider() string {
	return config.ModelProvider()
}

func llmBaseURL(provider string) string {
	if provider == config.ProviderOpenAICompatible {
		return config.ModelBaseURL()
	}
	return config.GeminiEndpoint()
}

func transcriptFromStoredLines(raw string) []agentrunner.ModelMessage {
	var lines []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if raw == "" || json.Unmarshal([]byte(raw), &lines) != nil {
		return nil
	}
	out := make([]agentrunner.ModelMessage, 0, len(lines))
	for _, line := range lines {
		if line.Role != "user" && line.Role != "assistant" {
			continue
		}
		content := strings.TrimSpace(line.Content)
		if content == "" {
			continue
		}
		out = append(out, agentrunner.ModelMessage{Role: line.Role, Content: content})
	}
	return out
}

func transcriptFromMetadata(raw any) []agentrunner.ModelMessage {
	lines, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]agentrunner.ModelMessage, 0, len(lines))
	for _, item := range lines {
		line, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := line["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}
		content, _ := line["content"].(string)
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		out = append(out, agentrunner.ModelMessage{Role: role, Content: content})
	}
	return out
}

func agentStepLimit() int {
	return config.AgentStepLimit()
}

func agentMaxParallelTools() int {
	return config.AgentMaxParallelTools()
}

func staleAgentRunAfter() time.Duration {
	return config.AgentStaleAfter()
}

func modelForAgentRun(agentType, provider string) string {
	return config.AgentModelFor(agentType, provider)
}
