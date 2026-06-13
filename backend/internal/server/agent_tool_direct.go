package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/concept"
	"memento/backend/internal/gaps"
	"memento/backend/internal/project"
	"memento/backend/internal/refresh"
	"memento/backend/internal/social"
)

func (s *Server) callAgentToolDirect(ctx context.Context, toolCtx agentrunner.ToolContext, name string, raw json.RawMessage) (any, error) {
	switch name {
	case "fts_search":
		var req ftsSearchRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		limit := clampLimit(req.Limit, 20, 50)
		ids, err := runMsgvaultSearchCLI(ctx, req.Query, limit)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []messageHit{}, nil
		}
		return enrichMessageIDs(ctx, s.reader.DB(), ids)
	case "vector_search":
		var req vectorSearchRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		limit := clampLimit(req.Limit, 20, 50)
		ids, err := runMsgvaultVectorSearchCLI(ctx, req.Query, limit)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []messageHit{}, nil
		}
		return enrichMessageIDs(ctx, s.reader.DB(), ids)
	case "get_message":
		var req getMessageRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getMessageDetailForAgent(ctx, req.MessageID)
	case "find_people":
		var req findPeopleRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.findPeopleForAgent(ctx, req)
	case "get_thread":
		var req getThreadRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getThreadForAgent(ctx, req.ThreadID)
	case "summarize_thread":
		var req summarizeThreadRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.summarizeThread(ctx, req)
	case "get_message_batch":
		var req messageBatchRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getMessageBatch(ctx, req)
	case "get_bundle_index":
		var req bundleIndexRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getBundleIndex(ctx, req)
	case "get_project_bundle":
		var req getProjectBundleRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if req.ProjectID <= 0 {
			return nil, fmt.Errorf("project_id is required")
		}
		if req.Detail == "" {
			req.Detail = "full"
		}
		bundle, err := project.GetProjectBundle(ctx, s.db, req.ProjectID, req.Detail)
		if err != nil {
			return nil, err
		}
		if bundle == nil {
			return []project.MessageBundleItem{}, nil
		}
		return bundle, nil
	case "write_section":
		var req writeSectionRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if err := validateWriteSection(req.ProjectID, req.Section, req.Content, req.SourceMessageIDs, allowedProjectSections); err != nil {
			return nil, err
		}
		return saveProjectSection(ctx, s.db, req.ProjectID, req.Section, req.Content, req.SourceMessageIDs)
	case "get_concept_bundle":
		var req getConceptBundleRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if req.ConceptID <= 0 {
			return nil, fmt.Errorf("concept_id is required")
		}
		if req.Detail == "" {
			req.Detail = "full"
		}
		bundle, err := concept.GetConceptBundle(ctx, s.db, req.ConceptID, req.Detail)
		if err != nil {
			return nil, err
		}
		if bundle == nil {
			return []concept.MessageBundleItem{}, nil
		}
		return bundle, nil
	case "cluster_messages_by_subject":
		var req clusterMessagesRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		docs, err := loadClusterDocs(ctx, s.db, req.MessageIDs)
		if err != nil {
			return nil, err
		}
		return clusterDocs(docs, req.K), nil
	case "write_concept_section":
		var req writeConceptSectionRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if err := validateWriteSection(req.ConceptID, req.Section, req.Content, req.SourceMessageIDs, allowedConceptSections); err != nil {
			return nil, err
		}
		if req.Section == "distilled_insights" {
			var insights []concept.LLMInsight
			if err := json.Unmarshal([]byte(req.Content), &insights); err != nil {
				return nil, fmt.Errorf("distilled_insights content must be a JSON array: %w", err)
			}
		}
		return saveConceptSection(ctx, s.db, req.ConceptID, req.Section, req.Content, req.SourceMessageIDs)
	case "list_person_messages":
		var req listPersonMessagesRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		limit := clampLimit(req.Limit, 50, 200)
		if req.PersonID <= 0 {
			return nil, fmt.Errorf("person_id is required")
		}
		if req.Fields == "" {
			req.Fields = "full"
		}
		return listPersonMessages(ctx, s.db, req.PersonID, limit, req.Fields)
	case "get_notes":
		var req getNotesRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if strings.TrimSpace(req.Dimension) == "" || req.EntityID <= 0 {
			return nil, fmt.Errorf("dimension and entity_id are required")
		}
		return loadNotes(ctx, s.db, req.Dimension, req.EntityID)
	case "fts_search_scoped":
		var req scopedSearchRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.ftsSearchScoped(ctx, req)
	case "write_facet":
		var req writeFacetRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.writeFacet(ctx, req)
	case "write_person_attribute":
		var req writePersonAttributeRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.writePersonAttribute(ctx, req)
	case "record_no_person_attributes":
		var req struct {
			Reason string `json:"reason"`
		}
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "reason": req.Reason}, nil
	case "write_person_section":
		var req writePersonSectionRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if err := validateWriteSection(req.PersonID, req.Section, req.Content, req.SourceMessageIDs, allowedPersonSections); err != nil {
			return nil, err
		}
		return savePersonSection(ctx, s.db, req.PersonID, req.Section, req.Content, req.SourceMessageIDs)
	case "get_person_network":
		var req getPersonNetworkRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if req.PersonID <= 0 {
			return nil, fmt.Errorf("person_id is required")
		}
		pn, err := social.LoadPersonNetwork(ctx, s.db, req.PersonID, req.Limit)
		if err != nil {
			return nil, err
		}
		if pn == nil {
			return nil, fmt.Errorf("person %d not found", req.PersonID)
		}
		return pn, nil
	case "get_cluster":
		var req getClusterRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getClusterForAgent(ctx, req)
	case "get_group":
		var req getGroupRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getGroupForAgent(ctx, req)
	case "find_bridges_between":
		var req findBridgesRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if req.PersonAID <= 0 || req.PersonBID <= 0 {
			return nil, fmt.Errorf("person_a_id and person_b_id are required")
		}
		if req.PersonAID == req.PersonBID {
			return nil, fmt.Errorf("person_a_id and person_b_id must differ")
		}
		bridges, err := social.FindBridgesBetween(ctx, s.db, req.PersonAID, req.PersonBID, req.Limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"person_a_id": req.PersonAID, "person_b_id": req.PersonBID, "bridges": bridges}, nil
	case "find_missing_collaborators":
		var req findMissingCollaboratorsRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if len(req.PersonIDs) == 0 {
			return nil, fmt.Errorf("person_ids must be non-empty")
		}
		missing, err := social.FindMissingCollaborators(ctx, s.db, req.PersonIDs, req.Limit, req.MinCombinedWeight)
		if err != nil {
			return nil, err
		}
		return map[string]any{"input_person_ids": req.PersonIDs, "missing_collaborators": missing}, nil
	case "get_person_summary":
		var req getPersonSummaryRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getPersonSummaryForAgent(ctx, req)
	case "get_project_summary":
		var req getProjectSummaryRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getProjectSummaryForAgent(ctx, req)
	case "get_concept_summary":
		var req getConceptSummaryRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.getConceptSummaryForAgent(ctx, req)
	case "search_persons":
		var req searchRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.searchPersonsForAgent(ctx, req.Query)
	case "search_projects":
		var req searchRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.searchProjectsForAgent(ctx, req.Query)
	case "search_concepts":
		var req searchRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.searchConceptsForAgent(ctx, req.Query)
	case "detect_gaps":
		var req detectGapsRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if len(req.MessageIDs) == 0 {
			return []gaps.Gap{}, nil
		}
		if req.Mode == "" {
			req.Mode = "chronological"
		}
		if req.MinSeverity == "" {
			req.MinSeverity = "low"
		}
		if req.MaxGaps <= 0 {
			req.MaxGaps = 5
		}
		detected, err := gaps.Detect(ctx, s.reader.DB(), req.MessageIDs, req.Mode)
		if err != nil {
			return nil, fmt.Errorf("detect gaps: %w", err)
		}
		var filtered []gaps.Gap
		minSevVal := severityInt(req.MinSeverity)
		for _, gap := range detected {
			if severityInt(gap.Severity) >= minSevVal {
				filtered = append(filtered, gap)
			}
		}
		if len(filtered) > req.MaxGaps {
			filtered = filtered[:req.MaxGaps]
		}
		if filtered == nil {
			filtered = []gaps.Gap{}
		}
		return filtered, nil
	case "detect_gaps_with_results":
		var req detectGapsWithResultsRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		if len(req.MessageIDs) == 0 {
			return []GapWithResults{}, nil
		}
		if req.Mode == "" {
			req.Mode = "chronological"
		}
		if req.MinSeverity == "" {
			req.MinSeverity = "low"
		}
		if req.MaxGaps <= 0 {
			req.MaxGaps = 5
		}
		detected, err := gaps.Detect(ctx, s.reader.DB(), req.MessageIDs, req.Mode)
		if err != nil {
			return nil, fmt.Errorf("detect gaps: %w", err)
		}
		var filtered []gaps.Gap
		minSevVal := severityInt(req.MinSeverity)
		for _, gap := range detected {
			if severityInt(gap.Severity) >= minSevVal {
				filtered = append(filtered, gap)
			}
		}
		if len(filtered) > req.MaxGaps {
			filtered = filtered[:req.MaxGaps]
		}
		var results []GapWithResults
		for _, gap := range filtered {
			var gapRes []messageHit
			seen := make(map[int64]bool)
			for _, hint := range gap.SearchHints {
				ids, err := runMsgvaultSearchCLI(ctx, hint, 5)
				if err != nil {
					continue
				}
				var uniqIDs []int64
				for _, id := range ids {
					if !seen[id] {
						seen[id] = true
						uniqIDs = append(uniqIDs, id)
					}
				}
				if len(uniqIDs) > 0 {
					hits, err := enrichMessageIDs(ctx, s.reader.DB(), uniqIDs)
					if err == nil {
						gapRes = append(gapRes, hits...)
					}
				}
			}
			if gapRes == nil {
				gapRes = []messageHit{}
			}
			results = append(results, GapWithResults{
				Kind:             gap.Kind,
				Description:      gap.Description,
				AnchorMessageIDs: gap.AnchorMessageIDs,
				SearchHints:      gap.SearchHints,
				Severity:         gap.Severity,
				Results:          gapRes,
			})
		}
		if results == nil {
			results = []GapWithResults{}
		}
		return results, nil
	case "add_project_messages":
		var req addProjectMessagesRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.addProjectMessages(ctx, req)
	case "add_concept_messages":
		var req addConceptMessagesRequest
		if err := decodeToolArgs(raw, &req); err != nil {
			return nil, err
		}
		return s.addConceptMessages(ctx, req)
	case "refresh-projects-rollup":
		_, err := refresh.RefreshProjectsReport(ctx, s.db)
		return map[string]any{"ok": err == nil}, err
	case "refresh-concepts-rollup":
		_, err := refresh.RefreshConceptsReport(ctx, s.db)
		return map[string]any{"ok": err == nil}, err
	case "refresh-people-rollup":
		_, err := refresh.RefreshPeopleReport(ctx, s.db)
		return map[string]any{"ok": err == nil}, err
	default:
		return nil, fmt.Errorf("unknown agent tool %q", name)
	}
}

func decodeToolArgs(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("invalid tool arguments JSON")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

func clampLimit(value, defaultValue, maxValue int) int {
	if value <= 0 {
		value = defaultValue
	}
	if maxValue > 0 && value > maxValue {
		value = maxValue
	}
	return value
}

func validateWriteSection(entityID int64, section, content string, sourceMessageIDs []int64, allowed map[string]bool) error {
	if entityID <= 0 {
		return fmt.Errorf("entity id is required")
	}
	if !allowed[section] {
		return fmt.Errorf("unsupported section %q", section)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("content is required")
	}
	if len(sourceMessageIDs) == 0 {
		return fmt.Errorf("source_message_ids must contain at least one message id")
	}
	return nil
}

func normalizeToolNoRows(err error, noun string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s not found", noun)
	}
	return err
}
