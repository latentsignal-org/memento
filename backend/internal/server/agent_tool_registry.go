package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/config"
	"memento/backend/internal/store"
)

type serverToolRegistry struct {
	s *Server
}

func (r serverToolRegistry) LookupTool(name string) (agentrunner.Tool, bool) {
	tools := r.s.agentTools()
	tool, ok := tools[name]
	return tool, ok
}

func (s *Server) toolSchemas(names ...string) []agentrunner.ToolSchema {
	tools := s.agentTools()
	out := make([]agentrunner.ToolSchema, 0, len(names))
	for _, name := range names {
		if tool, ok := tools[name]; ok {
			out = append(out, tool.Schema)
		}
	}
	return out
}

func (s *Server) agentTools() map[string]agentrunner.Tool {
	tool := func(name, desc string, kind agentrunner.ToolKind, augment func(agentrunner.RunSpec, map[string]any)) agentrunner.Tool {
		return agentrunner.Tool{
			Schema: agentrunner.ToolSchema{
				Type:        "function",
				Name:        name,
				Description: desc,
				Parameters:  schemaForTool(name, desc).Parameters,
			},
			Kind: kind,
			LockKey: func(spec agentrunner.RunSpec, _ json.RawMessage) string {
				switch kind {
				case agentrunner.ToolMutating, agentrunner.ToolHumanWaiting:
					return string(spec.AgentType) + ":" + spec.EntityID
				default:
					return ""
				}
			},
			Handler: func(ctx context.Context, toolCtx agentrunner.ToolContext, raw json.RawMessage) (any, error) {
				args := rawObject(raw)
				if augment != nil {
					augment(toolCtx.RunSpec, args)
				}
				nextRaw, err := json.Marshal(args)
				if err != nil {
					return nil, err
				}
				return s.callAgentToolDirect(ctx, toolCtx, name, nextRaw)
			},
		}
	}

	return map[string]agentrunner.Tool{
		"fts_search":                  tool("fts_search", "Search the msgvault archive with keyword/operator syntax.", agentrunner.ToolReadOnly, nil),
		"vector_search":               tool("vector_search", "Semantic search over the msgvault archive.", agentrunner.ToolReadOnly, nil),
		"get_message":                 tool("get_message", "Fetch a single message detail by id.", agentrunner.ToolReadOnly, nil),
		"find_people":                 tool("find_people", "Find resolved people by name or email fragment.", agentrunner.ToolReadOnly, nil),
		"get_thread":                  tool("get_thread", "Summarize all messages in a conversation thread.", agentrunner.ToolReadOnly, nil),
		"summarize_thread":            tool("summarize_thread", "Return a compact deterministic digest of a thread before reading full messages.", agentrunner.ToolReadOnly, nil),
		"get_message_batch":           tool("get_message_batch", "Fetch selected messages in deterministic order with optional bounded body text.", agentrunner.ToolReadOnly, nil),
		"get_bundle_index":            tool("get_bundle_index", "Fetch a compact body-free index for the bound project or concept bundle.", agentrunner.ToolReadOnly, injectBundleIndexMeta),
		"get_project_bundle":          tool("get_project_bundle", "Fetch all messages attached to the bound project.", agentrunner.ToolReadOnly, injectMetaInt("project_id")),
		"write_section":               tool("write_section", "Write a bound project narrative section.", agentrunner.ToolMutating, injectMetaInt("project_id")),
		"get_concept_bundle":          tool("get_concept_bundle", "Fetch all messages attached to the bound concept.", agentrunner.ToolReadOnly, injectMetaInt("concept_id")),
		"cluster_messages_by_subject": tool("cluster_messages_by_subject", "Cluster messages by deterministic subject/body terms.", agentrunner.ToolReadOnly, nil),
		"write_concept_section":       tool("write_concept_section", "Write a bound concept narrative section.", agentrunner.ToolMutating, injectMetaInt("concept_id")),
		"list_person_messages":        tool("list_person_messages", "List recent messages involving the bound person.", agentrunner.ToolReadOnly, injectMetaInt("person_id")),
		"get_notes": tool("get_notes", "Fetch notes for the bound person.", agentrunner.ToolReadOnly, func(spec agentrunner.RunSpec, args map[string]any) {
			args["dimension"] = "person"
			injectMetaInt("person_id")(spec, args)
			args["entity_id"] = args["person_id"]
			delete(args, "person_id")
		}),
		"fts_search_scoped":           tool("fts_search_scoped", "Search messages scoped to the bound person.", agentrunner.ToolReadOnly, injectMetaInt("person_id")),
		"write_facet":                 tool("write_facet", "Write a bound person facet.", agentrunner.ToolMutating, injectMetaInt("person_id")),
		"write_person_attribute":      tool("write_person_attribute", "Write a bound structured person detail such as a vital date, preference, or relationship marker.", agentrunner.ToolMutating, injectMetaInt("person_id")),
		"record_no_person_attributes": tool("record_no_person_attributes", "Call this when no structured attributes have strong evidence. Accepts a reason string for observability.", agentrunner.ToolReadOnly, nil),
		"write_person_section":        tool("write_person_section", "Write a bound person narrative section.", agentrunner.ToolMutating, injectMetaInt("person_id")),
		"get_person_network":          tool("get_person_network", "Fetch deterministic communication network for a person.", agentrunner.ToolReadOnly, injectMetaIntIfMissing("person_id")),
		"get_group":                   tool("get_group", "Fetch the bound person's actionable communication group.", agentrunner.ToolReadOnly, injectMetaInt("person_id")),
		"get_cluster":                 tool("get_cluster", "Fetch the bound person's legacy communication cluster.", agentrunner.ToolReadOnly, injectMetaInt("person_id")),
		"find_bridges_between":        tool("find_bridges_between", "Find bridge contacts between two people.", agentrunner.ToolReadOnly, nil),
		"find_missing_collaborators":  tool("find_missing_collaborators", "Find graph-connected collaborators absent from a draft.", agentrunner.ToolReadOnly, nil),
		"get_person_summary":          tool("get_person_summary", "Fetch compact person context by default; set brief=false for full facets and narrative.", agentrunner.ToolReadOnly, injectMetaIntIfMissing("person_id")),
		"get_project_summary":         tool("get_project_summary", "Fetch project metadata and narrative.", agentrunner.ToolReadOnly, nil),
		"get_concept_summary":         tool("get_concept_summary", "Fetch concept metadata and narrative.", agentrunner.ToolReadOnly, nil),
		"search_persons":              tool("search_persons", "Search people pages.", agentrunner.ToolReadOnly, nil),
		"search_projects":             tool("search_projects", "Search project pages.", agentrunner.ToolReadOnly, nil),
		"search_concepts":             tool("search_concepts", "Search concept pages.", agentrunner.ToolReadOnly, nil),
		"detect_gaps":                 tool("detect_gaps", "Detect chronological, thematic, or participant gaps.", agentrunner.ToolReadOnly, nil),
		"detect_gaps_with_results":    tool("detect_gaps_with_results", "Detect gaps and run search queries internally to return message hits.", agentrunner.ToolReadOnly, nil),
		"add_project_messages":        tool("add_project_messages", "Add messages to a project bundle.", agentrunner.ToolMutating, nil),
		"add_concept_messages":        tool("add_concept_messages", "Add messages to a concept bundle.", agentrunner.ToolMutating, nil),
		"refresh-projects-rollup":     tool("refresh-projects-rollup", "Refresh project rollup table.", agentrunner.ToolMutating, nil),
		"refresh-concepts-rollup":     tool("refresh-concepts-rollup", "Refresh concept rollup table.", agentrunner.ToolMutating, nil),
		"refresh-people-rollup":       tool("refresh-people-rollup", "Refresh people rollup table.", agentrunner.ToolMutating, nil),
		"context_status":              s.contextStatusTool(),
		"propose_bundle":              s.proposeBundleTool(),
		"create_project_draft":        s.createDraftTool("project"),
		"create_concept_draft":        s.createDraftTool("concept"),
		"propose_backfill":            s.proposeBackfillTool(),
	}
}

func injectBundleIndexMeta(spec agentrunner.RunSpec, args map[string]any) {
	switch spec.AgentType {
	case agentrunner.AgentProjectCompile:
		args["kind"] = "project"
		injectMetaInt("project_id")(spec, args)
	case agentrunner.AgentConceptCompile:
		args["kind"] = "concept"
		injectMetaInt("concept_id")(spec, args)
	}
}

func (s *Server) contextStatusTool() agentrunner.Tool {
	return agentrunner.Tool{
		Schema: schemaForTool("context_status", "Return persisted token usage and budget guidance for the current run."),
		Kind:   agentrunner.ToolReadOnly,
		Handler: func(ctx context.Context, toolCtx agentrunner.ToolContext, _ json.RawMessage) (any, error) {
			run, err := store.GetAgentRun(ctx, s.db, toolCtx.RunID)
			if err != nil {
				return nil, err
			}
			limit := config.AgentContextLimitTokens()
			used := run.TotalModelInputTokens
			if used == 0 {
				used = run.TotalEstimatedInputTokens + run.TotalEstimatedToolResultTokens
			}
			level := "normal"
			if used > limit*9/10 {
				level = "critical"
			} else if used > limit*3/4 {
				level = "low"
			} else if used > limit/2 {
				level = "watch"
			}
			return map[string]any{
				"run_id":                             toolCtx.RunID,
				"budget_level":                       level,
				"context_limit_tokens":               limit,
				"estimated_context_tokens":           used,
				"total_estimated_input_tokens":       run.TotalEstimatedInputTokens,
				"total_estimated_output_tokens":      run.TotalEstimatedOutputTokens,
				"total_estimated_tool_result_tokens": run.TotalEstimatedToolResultTokens,
				"total_model_input_tokens":           run.TotalModelInputTokens,
				"total_model_output_tokens":          run.TotalModelOutputTokens,
				"total_model_tokens":                 run.TotalModelTokens,
				"guidance":                           contextBudgetGuidance(level),
			}, nil
		},
	}
}

func contextBudgetGuidance(level string) string {
	switch level {
	case "critical":
		return "Do not run more broad searches. Write final sections or answer from current evidence."
	case "low":
		return "Avoid broad bundle/body reads. Prefer index scans, small message batches, and writing with known evidence."
	case "watch":
		return "Use compact tools and small batches unless more evidence is clearly needed."
	default:
		return "Budget is available. Prefer compact index tools before full message bodies."
	}
}

func (s *Server) proposeBundleTool() agentrunner.Tool {
	return agentrunner.Tool{
		Schema: schemaForTool("propose_bundle", "Persist a draft bundle for user review."),
		Kind:   agentrunner.ToolMutating,
		LockKey: func(spec agentrunner.RunSpec, _ json.RawMessage) string {
			return "draft:" + spec.EntityID
		},
		Handler: func(ctx context.Context, toolCtx agentrunner.ToolContext, raw json.RawMessage) (any, error) {
			if toolCtx.RunSpec.AgentType != agentrunner.AgentCollector {
				return nil, fmt.Errorf("propose_bundle is only available to collector runs")
			}
			draftID, err := strconv.ParseInt(toolCtx.RunSpec.EntityID, 10, 64)
			if err != nil || draftID <= 0 {
				return nil, fmt.Errorf("invalid draft id %q", toolCtx.RunSpec.EntityID)
			}
			if len(raw) == 0 || !json.Valid(raw) {
				return nil, fmt.Errorf("invalid bundle JSON")
			}
			if err := store.UpdateDraftEntities(ctx, s.db, draftID, string(raw)); err != nil {
				return nil, err
			}
			return map[string]any{"status": "proposed", "draft_id": draftID}, nil
		},
	}
}

func (s *Server) createDraftTool(kind string) agentrunner.Tool {
	name := "create_project_draft"
	if kind == "concept" {
		name = "create_concept_draft"
	}
	return agentrunner.Tool{
		Schema: schemaForTool(name, "Create a new draft staging area."),
		Kind:   agentrunner.ToolMutating,
		Handler: func(ctx context.Context, _ agentrunner.ToolContext, raw json.RawMessage) (any, error) {
			args := rawObject(raw)
			nameValue, _ := args["name"].(string)
			draftID, err := store.CreateDraft(ctx, s.db, kind, nameValue)
			if err != nil {
				return nil, err
			}
			summary := ""
			if kind == "project" {
				summary, _ = args["rationale"].(string)
			} else {
				summary, _ = args["scope_description"].(string)
			}
			bundle := map[string]any{
				"name":         nameValue,
				"summary_hint": summary,
				"people":       []any{},
				"threads":      []any{},
				"messages":     seedMessages(args["message_ids"], summary),
			}
			bundleRaw, _ := json.Marshal(bundle)
			if err := store.UpdateDraftEntities(ctx, s.db, draftID, string(bundleRaw)); err != nil {
				return nil, err
			}
			prefix := "/projects/new"
			if kind == "concept" {
				prefix = "/concepts/new"
			}
			return map[string]any{"draft_id": draftID, "url": fmt.Sprintf("%s?draftId=%d", prefix, draftID)}, nil
		},
	}
}

func (s *Server) proposeBackfillTool() agentrunner.Tool {
	return agentrunner.Tool{
		Schema: schemaForTool("propose_backfill", "Propose adding candidate messages and wait for the user's durable decision."),
		Kind:   agentrunner.ToolHumanWaiting,
		LockKey: func(spec agentrunner.RunSpec, _ json.RawMessage) string {
			return "decision:" + string(spec.AgentType) + ":" + spec.EntityID
		},
		Handler: func(ctx context.Context, toolCtx agentrunner.ToolContext, raw json.RawMessage) (any, error) {
			args := rawObject(raw)
			ids := int64Slice(args["candidate_message_ids"])
			rationale, _ := args["rationale"].(string)
			gapKind, _ := args["gap_kind"].(string)
			entityType := "draft"
			if toolCtx.RunSpec.AgentType == agentrunner.AgentProjectCompile {
				entityType = "project"
			} else if toolCtx.RunSpec.AgentType == agentrunner.AgentConceptCompile {
				entityType = "concept"
			}
			decisionSuffix := randomHex(8)
			if decisionSuffix == "" {
				return nil, fmt.Errorf("generate decision id")
			}
			decisionID := fmt.Sprintf("%s-%s-%s", entityType, toolCtx.RunSpec.EntityID, decisionSuffix)
			if err := store.CreateAgentDecision(ctx, s.db, store.AgentDecision{
				ID:           decisionID,
				DecisionType: "backfill",
				EntityType:   entityType,
				EntityID:     toolCtx.RunSpec.EntityID,
				PayloadJSON:  string(raw),
			}); err != nil {
				return nil, err
			}
			if err := toolCtx.Emit(ctx, agentrunner.NewProposedBackfillEvent(decisionID, rationale, ids, gapKind, toolCtx.RunSpec.EntityID)); err != nil {
				return nil, err
			}
			_ = toolCtx.SetStatus(ctx, store.AgentRunWaitingForUser)
			result, err := s.waitForBackfillDecision(ctx, decisionID, agentDecisionTimeout())
			_ = toolCtx.SetStatus(ctx, store.AgentRunRunning)
			if expired, _ := result["expired"].(bool); expired {
				_ = toolCtx.Emit(context.Background(), agentrunner.NewProposedBackfillExpiredEvent(decisionID))
			}
			return result, err
		},
	}
}

func (s *Server) waitForBackfillDecision(ctx context.Context, decisionID string, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			d, err := store.GetAgentDecision(ctx, s.db, decisionID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				return nil, err
			}
			if d.Status != "pending" {
				return decisionResult(d.ResultJSON), nil
			}
			if time.Now().After(deadline) {
				result := `{"accepted":false,"added_count":0,"expired":true}`
				_, _ = store.ResolveAgentDecision(ctx, s.db, decisionID, "expired", result)
				return decisionResult(result), nil
			}
		}
	}
}

func agentDecisionTimeout() time.Duration {
	return config.AgentDecisionTimeout()
}

func rawObject(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func injectMetaInt(key string) func(agentrunner.RunSpec, map[string]any) {
	return func(spec agentrunner.RunSpec, args map[string]any) {
		if v, ok := spec.RequestMetadata[key]; ok {
			args[key] = v
		}
	}
}

func injectMetaIntIfMissing(key string) func(agentrunner.RunSpec, map[string]any) {
	return func(spec agentrunner.RunSpec, args map[string]any) {
		if _, ok := args[key]; ok {
			return
		}
		if v, ok := spec.RequestMetadata[key]; ok {
			args[key] = v
		}
	}
}

func seedMessages(raw any, reason string) []map[string]any {
	ids := int64Slice(raw)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"message_id": id, "include_reason": reason})
	}
	return out
}

func int64Slice(raw any) []int64 {
	switch v := raw.(type) {
	case []int64:
		return v
	case []any:
		out := make([]int64, 0, len(v))
		for _, item := range v {
			switch n := item.(type) {
			case float64:
				out = append(out, int64(n))
			case int64:
				out = append(out, n)
			case int:
				out = append(out, int64(n))
			}
		}
		return out
	default:
		return []int64{}
	}
}

func decisionResult(raw string) map[string]any {
	out := map[string]any{"accepted": false, "added_count": float64(0)}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}
