package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAgentToolRegistryCoversTypeScriptTools(t *testing.T) {
	srv, _ := newTestServer(t)
	tools := srv.agentTools()

	tsToolNames := []string{
		"fts_search",
		"vector_search",
		"get_message",
		"get_message_batch",
		"find_people",
		"get_thread",
		"summarize_thread",
		"get_bundle_index",
		"get_project_bundle",
		"write_section",
		"get_concept_bundle",
		"cluster_messages_by_subject",
		"write_concept_section",
		"list_person_messages",
		"get_notes",
		"fts_search_scoped",
		"write_facet",
		"write_person_attribute",
		"record_no_person_attributes",
		"write_person_section",
		"get_person_network",
		"get_group",
		"get_cluster",
		"find_bridges_between",
		"find_missing_collaborators",
		"get_person_summary",
		"get_project_summary",
		"get_concept_summary",
		"search_persons",
		"search_projects",
		"search_concepts",
		"detect_gaps",
		"context_status",
		"propose_bundle",
		"create_project_draft",
		"create_concept_draft",
		"propose_backfill",
	}

	for _, name := range tsToolNames {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("tool %s missing from Go registry", name)
		}
		if tool.Schema.Type != "function" || tool.Schema.Name != name || tool.Schema.Description == "" {
			t.Fatalf("tool %s has invalid schema metadata: %+v", name, tool.Schema)
		}
		var params map[string]any
		if err := json.Unmarshal(tool.Schema.Parameters, &params); err != nil {
			t.Fatalf("tool %s parameters are invalid JSON: %v", name, err)
		}
		if params["type"] != "object" {
			t.Fatalf("tool %s parameters type = %v, want object", name, params["type"])
		}
		if _, ok := params["properties"].(map[string]any); !ok {
			t.Fatalf("tool %s parameters missing object properties", name)
		}
	}
}

func TestAgentToolSchemasMatchRequiredCompatibilityFields(t *testing.T) {
	cases := map[string][]string{
		"get_project_bundle":          {"nonce"},
		"get_concept_bundle":          {"nonce"},
		"get_notes":                   {"nonce"},
		"get_group":                   {"nonce"},
		"get_cluster":                 {"nonce"},
		"cluster_messages_by_subject": {"message_ids", "k"},
		"list_person_messages":        {"limit"},
		"get_person_network":          {"nonce"},
		"get_bundle_index":            {"nonce"},
		"get_message_batch":           {"message_ids"},
		"summarize_thread":            {"thread_id"},
		"context_status":              {"nonce"},
		"write_section":               {"section", "content", "source_message_ids"},
		"write_concept_section":       {"section", "content", "source_message_ids"},
		"write_person_section":        {"section", "content", "source_message_ids"},
		"write_facet":                 {"facet_type", "content", "source_message_ids", "confidence"},
		"write_person_attribute":      {"category", "label", "value", "source_message_ids", "confidence"},
		"record_no_person_attributes": {"reason"},
		"propose_backfill":            {"rationale", "candidate_message_ids", "gap_kind"},
	}

	for name, want := range cases {
		schema := schemaForTool(name, "test")
		var params map[string]any
		if err := json.Unmarshal(schema.Parameters, &params); err != nil {
			t.Fatalf("tool %s parameters are invalid JSON: %v", name, err)
		}
		gotRaw, _ := params["required"].([]any)
		got := make(map[string]bool, len(gotRaw))
		for _, v := range gotRaw {
			got[v.(string)] = true
		}
		for _, field := range want {
			if !got[field] {
				t.Fatalf("tool %s missing required field %s in %v", name, field, gotRaw)
			}
		}
	}
}

func TestAgentToolsManifestOnlyIncludesReadOnlyDebugTools(t *testing.T) {
	old := os.Getenv("MEMENTO_INTERNAL_TOKEN")
	os.Setenv("MEMENTO_INTERNAL_TOKEN", testInternalToken)
	defer func() {
		if old == "" {
			os.Unsetenv("MEMENTO_INTERNAL_TOKEN")
		} else {
			os.Setenv("MEMENTO_INTERNAL_TOKEN", old)
		}
	}()

	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/internal/agent-tools/manifest", nil)
	req.Header.Set("X-Internal-Token", testInternalToken)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, body: %s", w.Code, w.Body.String())
	}
	var manifest map[string]debugToolSchema
	if err := json.NewDecoder(w.Body).Decode(&manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	excluded := []string{
		"write_section",
		"write_concept_section",
		"write_facet",
		"write_person_attribute",
		"write_person_section",
		"add_project_messages",
		"add_concept_messages",
		"refresh-projects-rollup",
		"refresh-concepts-rollup",
		"refresh-people-rollup",
		"propose_bundle",
		"create_project_draft",
		"create_concept_draft",
		"propose_backfill",
		"context_status",
	}
	for _, name := range excluded {
		if _, ok := manifest[name]; ok {
			t.Fatalf("manifest unexpectedly includes %s", name)
		}
	}
	if _, ok := manifest["fts_search"]; !ok {
		t.Fatalf("manifest missing fts_search")
	}
	var params map[string]any
	if err := json.Unmarshal(manifest["get_person_summary"].Parameters, &params); err != nil {
		t.Fatalf("get_person_summary debug params invalid: %v", err)
	}
	props, _ := params["properties"].(map[string]any)
	if _, ok := props["person_id"]; !ok {
		t.Fatalf("get_person_summary debug params should expose person_id, got %v", props)
	}
}

func TestAgentToolDebugInvokeRejectsUnsafeTools(t *testing.T) {
	old := os.Getenv("MEMENTO_INTERNAL_TOKEN")
	os.Setenv("MEMENTO_INTERNAL_TOKEN", testInternalToken)
	defer func() {
		if old == "" {
			os.Unsetenv("MEMENTO_INTERNAL_TOKEN")
		} else {
			os.Setenv("MEMENTO_INTERNAL_TOKEN", old)
		}
	}()

	srv, _ := newTestServer(t)
	for _, body := range []string{
		`{"tool":"missing_tool","params":{}}`,
		`{"tool":"write_section","params":{}}`,
		`{"tool":"context_status","params":{}}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/internal/agent-tools/debug-invoke", bytes.NewReader([]byte(body)))
		req.Header.Set("X-Internal-Token", testInternalToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code < 400 {
			t.Fatalf("debug invoke %s status = %d, want failure", body, w.Code)
		}
	}
}

func TestAgentToolDebugInvokeReadOnlyTool(t *testing.T) {
	old := os.Getenv("MEMENTO_INTERNAL_TOKEN")
	os.Setenv("MEMENTO_INTERNAL_TOKEN", testInternalToken)
	defer func() {
		if old == "" {
			os.Unsetenv("MEMENTO_INTERNAL_TOKEN")
		} else {
			os.Setenv("MEMENTO_INTERNAL_TOKEN", old)
		}
	}()

	srv, db := newTestServer(t)
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (1, 'Ada Lovelace', 'ada@example.com');
		INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence)
		VALUES ('ada@example.com', 1, 'Ada Lovelace', 'manual', 1.0);
	`); err != nil {
		t.Fatalf("seed person: %v", err)
	}

	body := []byte(`{"tool":"find_people","params":{"query":"Ada","limit":5}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/internal/agent-tools/debug-invoke", bytes.NewReader(body))
	req.Header.Set("X-Internal-Token", testInternalToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("debug invoke status = %d, body: %s", w.Code, w.Body.String())
	}
	var results []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatalf("decode results: %v", err)
	}
	if len(results) == 0 || results[0]["display_name"] != "Ada Lovelace" {
		t.Fatalf("unexpected results: %+v", results)
	}
}
