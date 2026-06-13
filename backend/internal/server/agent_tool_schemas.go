package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"memento/backend/internal/agentrunner"
)

type debugToolSchema struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type debugInvokeRequest struct {
	Tool   string          `json:"tool"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) handleAgentToolsManifest(w http.ResponseWriter, r *http.Request) {
	tools := s.agentTools()
	out := make(map[string]debugToolSchema, len(tools))
	for name, tool := range tools {
		if !isDebugInvocableTool(name, tool) {
			continue
		}
		out[name] = debugSchemaForTool(name, tool.Schema)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAgentToolDebugInvoke(w http.ResponseWriter, r *http.Request) {
	var req debugInvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Tool == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("tool is required"))
		return
	}
	tool, ok := s.agentTools()[req.Tool]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown tool %q", req.Tool))
		return
	}
	if !isDebugInvocableTool(req.Tool, tool) {
		writeError(w, http.StatusForbidden, fmt.Errorf("tool %q is not available in read-only debug mode", req.Tool))
		return
	}
	raw := req.Params
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("params must be valid JSON"))
		return
	}
	result, err := tool.Handler(r.Context(), agentrunner.ToolContext{
		RunID: 0,
		RunSpec: agentrunner.RunSpec{
			AgentType:       agentrunner.AgentDashboard,
			EntityID:        "debug-tools",
			RequestMetadata: map[string]any{},
		},
		Emit: func(context.Context, agentrunner.AgentEvent) error {
			return nil
		},
		SetStatus: func(context.Context, string) error {
			return nil
		},
	}, raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func isDebugInvocableTool(name string, tool agentrunner.Tool) bool {
	if tool.Kind != agentrunner.ToolReadOnly {
		return false
	}
	return name != "context_status"
}

func debugSchemaForTool(name string, schema agentrunner.ToolSchema) debugToolSchema {
	out := debugToolSchema{
		Type:        schema.Type,
		Name:        schema.Name,
		Description: schema.Description,
		Parameters:  schema.Parameters,
	}
	switch name {
	case "get_bundle_index":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"kind":       enumProp("Bundle kind.", "project", "concept"),
			"project_id": numberProp("Project id when kind is project."),
			"concept_id": numberProp("Concept id when kind is concept."),
		}, "kind"))
	case "get_project_bundle":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"project_id": numberProp("Project id."),
			"detail":     enumProp("Bundle detail level. 'full' returns full text, 'index' skips body texts.", "full", "index"),
		}, "project_id"))
	case "get_concept_bundle":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"concept_id": numberProp("Concept id."),
			"detail":     enumProp("Bundle detail level. 'full' returns full text, 'index' skips body texts.", "full", "index"),
		}, "concept_id"))
	case "list_person_messages":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"person_id": numberProp("Person id."),
			"limit":     numberProp("Number of messages to fetch. Defaults to 50, max 200."),
			"fields":    enumProp("Omit snippets and email fields for compact timeline queries.", "full", "compact"),
		}, "person_id"))
	case "get_notes":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"person_id": numberProp("Person id."),
		}, "person_id"))
	case "fts_search_scoped":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"person_id": numberProp("Person id."),
			"query":     stringProp("Scoped search query."),
			"limit":     numberProp("Max scoped hits."),
		}, "person_id", "query"))
	case "get_cluster":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"person_id":  numberProp("Person id. Either person_id or cluster_id is required."),
			"cluster_id": numberProp("Social cluster id. Either person_id or cluster_id is required."),
		}))
	case "get_group":
		out.Parameters = mustMarshalToolParams(objectSchema(props{
			"person_id": numberProp("Person id. Either person_id or group_id is required."),
			"group_id":  numberProp("Actionable group id. Either person_id or group_id is required."),
		}))
	}
	return out
}

func mustMarshalToolParams(params map[string]any) json.RawMessage {
	raw, _ := json.Marshal(params)
	return raw
}

func schemaForTool(name, desc string) agentrunner.ToolSchema {
	params := toolParameters(name)
	raw, _ := json.Marshal(params)
	return agentrunner.ToolSchema{
		Type:        "function",
		Name:        name,
		Description: desc,
		Parameters:  raw,
	}
}

func toolParameters(name string) map[string]any {
	switch name {
	case "fts_search", "vector_search":
		return objectSchema(props{
			"query": stringProp("Search query. Required."),
			"limit": numberProp("Max hits to return."),
		}, "query")
	case "get_message":
		return objectSchema(props{"message_id": numberProp("Message id.")}, "message_id")
	case "find_people":
		return objectSchema(props{
			"query": stringProp("Name or email fragment."),
			"limit": numberProp("Max matches."),
		}, "query")
	case "get_thread":
		return objectSchema(props{"thread_id": numberProp("Conversation/thread id.")}, "thread_id")
	case "summarize_thread":
		return objectSchema(props{
			"thread_id":    numberProp("Conversation/thread id."),
			"max_messages": numberProp("Maximum representative snippets to return. Defaults to 12, max 30."),
		}, "thread_id")
	case "get_message_batch":
		return objectSchema(props{
			"message_ids":     numberArrayProp("Message ids to fetch, in desired order. Max 25."),
			"include_body":    boolProp("Whether to include bounded body text."),
			"body_char_limit": numberProp("Maximum body characters per message. Defaults to 1200, max 4000."),
			"include_headers": boolProp("Whether to include recipients."),
		}, "message_ids")
	case "get_bundle_index":
		return objectSchema(props{
			"nonce": stringProp("Any short sentinel string. Entity id is bound by runtime for project/concept compile runs."),
			"kind":  enumProp("Bundle kind when not bound by runtime.", "project", "concept"),
		}, "nonce")
	case "get_project_bundle", "get_concept_bundle":
		return objectSchema(props{
			"nonce":  stringProp("Any short sentinel string."),
			"detail": enumProp("Bundle detail level. 'full' returns full text, 'index' skips body texts.", "full", "index"),
		}, "nonce")
	case "get_notes", "get_group", "get_cluster":
		return objectSchema(props{"nonce": stringProp("Any short sentinel string.")}, "nonce")
	case "write_section":
		return objectSchema(props{
			"section":            enumProp("Project section.", "summary", "phases", "friction_points", "current_understanding"),
			"content":            stringProp("Section content with inline citations."),
			"source_message_ids": numberArrayProp("Every message id this section is grounded in."),
		}, "section", "content", "source_message_ids")
	case "cluster_messages_by_subject":
		return objectSchema(props{
			"message_ids": numberArrayProp("Message ids to cluster."),
			"k":           numberProp("Desired number of clusters."),
		}, "message_ids", "k")
	case "write_concept_section":
		return objectSchema(props{
			"section":            enumProp("Concept section.", "scope_summary", "distilled_insights", "evolving_understanding"),
			"content":            stringProp("Section content with inline citations."),
			"source_message_ids": numberArrayProp("Every message id this section is grounded in."),
		}, "section", "content", "source_message_ids")
	case "list_person_messages":
		return objectSchema(props{
			"limit":  numberProp("Number of messages to fetch. Defaults to 50, max 200."),
			"fields": enumProp("Omit snippets and email fields for compact timeline queries.", "full", "compact"),
		}, "limit")
	case "fts_search_scoped":
		return objectSchema(props{
			"query": stringProp("Scoped search query."),
			"limit": numberProp("Max scoped hits."),
		}, "query")
	case "write_facet":
		return objectSchema(props{
			"facet_type":         enumProp("Facet type.", "interest", "life_event", "recurring_topic", "relationship_signal", "fact"),
			"content":            stringProp("Facet content with inline citations."),
			"source_message_ids": numberArrayProp("Message ids grounding this facet."),
			"confidence":         numberProp("0.0-1.0 confidence score."),
		}, "facet_type", "content", "source_message_ids", "confidence")
	case "write_person_attribute":
		return objectSchema(props{
			"category":           enumProp("Structured detail category.", "vital_date", "preference", "interest", "relationship_marker", "household", "work", "location", "routine", "identifier"),
			"label":              stringProp("Single-line label, 40 characters or fewer, e.g. Birthday, Anniversary, Favorite trail, Spouse."),
			"value":              stringProp("Single-line right-rail value, 160 characters or fewer. No multi-line prose."),
			"date_value":         stringProp("Optional ISO date when the detail has a specific date."),
			"source_message_ids": numberArrayProp("Message ids grounding this detail."),
			"confidence":         numberProp("0.0-1.0 confidence score."),
		}, "category", "label", "value", "source_message_ids", "confidence")
	case "record_no_person_attributes":
		return objectSchema(props{
			"reason": stringProp("Why no structured attributes are being written from the available evidence."),
		}, "reason")
	case "write_person_section":
		return objectSchema(props{
			"section":            enumProp("Person narrative section.", "summary", "relationship_arc", "current_status"),
			"content":            stringProp("Prose with inline citations."),
			"source_message_ids": numberArrayProp("Message ids grounding this section."),
		}, "section", "content", "source_message_ids")
	case "get_person_network":
		return objectSchema(props{
			"nonce":     stringProp("Any short sentinel string."),
			"person_id": numberProp("Person id when not bound by runtime."),
			"limit":     numberProp("Max neighbors."),
		}, "nonce")
	case "find_bridges_between":
		return objectSchema(props{
			"person_a_id": numberProp("First person id."),
			"person_b_id": numberProp("Second person id."),
			"limit":       numberProp("Max bridges."),
		}, "person_a_id", "person_b_id")
	case "find_missing_collaborators":
		return objectSchema(props{
			"person_ids":          numberArrayProp("Person ids already in the draft."),
			"limit":               numberProp("Max suggestions."),
			"min_combined_weight": numberProp("Minimum combined edge weight."),
		}, "person_ids")
	case "get_person_summary":
		return objectSchema(props{
			"person_id": numberProp("Person id."),
			"slug":      stringProp("Person slug."),
			"brief":     boolProp("Defaults to true. Set false only when full generated facets, narrative, aliases, and timeline are needed."),
		})
	case "get_project_summary":
		return objectSchema(props{
			"project_id": numberProp("Project id."),
			"slug":       stringProp("Project slug."),
			"brief":      boolProp("Whether to return only metadata, omitting narratives."),
		})
	case "get_concept_summary":
		return objectSchema(props{
			"concept_id": numberProp("Concept id."),
			"slug":       stringProp("Concept slug."),
			"brief":      boolProp("Whether to return only metadata, omitting narratives."),
		})
	case "search_persons", "search_projects", "search_concepts":
		return objectSchema(props{"query": stringProp("Search query.")}, "query")
	case "detect_gaps":
		return objectSchema(props{
			"message_ids":  numberArrayProp("Message ids to analyze."),
			"mode":         enumProp("Detection mode.", "chronological", "thematic", "participant"),
			"min_severity": enumProp("Minimum gap severity to return. Defaults to 'low'.", "low", "medium", "high"),
			"max_gaps":     numberProp("Maximum gaps to return. Defaults to 5."),
		}, "message_ids", "mode")
	case "detect_gaps_with_results":
		return objectSchema(props{
			"message_ids":  numberArrayProp("Message ids to analyze."),
			"mode":         enumProp("Detection mode.", "chronological", "thematic", "participant"),
			"min_severity": enumProp("Minimum gap severity to process. Defaults to 'low'.", "low", "medium", "high"),
			"max_gaps":     numberProp("Maximum gaps to search and return. Defaults to 5."),
		}, "message_ids", "mode")
	case "add_project_messages":
		return objectSchema(props{
			"project_slug": stringProp("Project slug."),
			"message_ids":  numberArrayProp("Messages to add."),
		}, "project_slug", "message_ids")
	case "add_concept_messages":
		return objectSchema(props{
			"concept_slug": stringProp("Concept slug."),
			"message_ids":  numberArrayProp("Messages to add."),
		}, "concept_slug", "message_ids")
	case "propose_bundle":
		return objectSchema(props{
			"name":         stringProp("Short project/concept name."),
			"summary_hint": stringProp("One-line summary hint."),
			"people": arrayOf("Resolved people directly involved.", objectSchema(props{
				"person_id":            numberProp("Resolved person id."),
				"display_name":         stringProp("Display name."),
				"role":                 stringProp("Role in this project."),
				"evidence_message_ids": numberArrayProp("Message ids backing this person's involvement."),
			}, "person_id", "display_name")),
			"messages": arrayOf("Individual messages to include.", objectSchema(props{
				"message_id":       numberProp("Message id."),
				"subject":          stringProp("Message subject."),
				"date":             stringProp("Message date."),
				"include_reason":   stringProp("Why this message belongs."),
				"agent_confidence": numberProp("Agent confidence score."),
			}, "message_id")),
			"threads": arrayOf("Whole threads to include.", objectSchema(props{
				"thread_id":      numberProp("Thread id."),
				"subject":        stringProp("Thread subject."),
				"message_count":  numberProp("Number of messages in the thread."),
				"include_reason": stringProp("Why this thread belongs."),
			}, "thread_id")),
		}, "name")
	case "create_project_draft":
		return objectSchema(props{
			"name":        stringProp("Proposed project name."),
			"rationale":   stringProp("Why this project is being created."),
			"message_ids": numberArrayProp("Seed message ids."),
		}, "name", "rationale", "message_ids")
	case "create_concept_draft":
		return objectSchema(props{
			"name":              stringProp("Proposed concept name."),
			"scope_description": stringProp("Evergreen concept scope."),
			"message_ids":       numberArrayProp("Seed message ids."),
		}, "name", "scope_description", "message_ids")
	case "propose_backfill":
		return objectSchema(props{
			"rationale":             stringProp("Why these messages fill the gap."),
			"candidate_message_ids": numberArrayProp("Candidate message ids."),
			"gap_kind":              enumProp("Gap kind.", "chronological", "thematic", "participant", "missing_collaborator"),
		}, "rationale", "candidate_message_ids", "gap_kind")
	case "context_status":
		return objectSchema(props{"nonce": stringProp("Any short sentinel string.")}, "nonce")
	default:
		return objectSchema(props{})
	}
}

type props map[string]any

func objectSchema(properties props, required ...string) map[string]any {
	out := map[string]any{
		"type":       "object",
		"properties": map[string]any(properties),
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func numberProp(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func boolProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func numberArrayProp(description string) map[string]any {
	return arrayOf(description, map[string]any{"type": "number"})
}

func arrayOf(description string, item map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": item, "description": description}
}

func enumProp(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}
