package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"memento/backend/internal/agentrunner"
)

func TestAgentContextTools_BundleIndexAndMessageBatch(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	createMsgvaultTablesForContextTools(t, db)

	if _, err := srv.db.ExecContext(ctx, `INSERT INTO sources (identifier) VALUES ('owner@example.com')`); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO participants (id, email_address, display_name) VALUES
		  (1, 'owner@example.com', 'Owner'),
		  (2, 'alex@example.com', 'Alex')`); err != nil {
		t.Fatalf("insert participants: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO messages (id, subject, snippet, sent_at, sender_id, conversation_id) VALUES
		  (101, 'First', 'First snippet', '2026-01-01', 2, 700),
		  (102, 'Second', 'Second snippet', '2026-01-02', 1, 700)`); err != nil {
		t.Fatalf("insert messages: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO message_bodies (message_id, body_text) VALUES
		  (101, 'abcdefghijklmnopqrstuvwxyz'),
		  (102, 'short body')`); err != nil {
		t.Fatalf("insert bodies: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `INSERT INTO message_recipients (message_id, participant_id, recipient_type) VALUES (102, 2, 'to')`); err != nil {
		t.Fatalf("insert recipients: %v", err)
	}
	projectRes, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_project (slug, name, aliases, status, note)
		VALUES ('ctx-tools', 'Context Tools', '[]', 'active', '')`)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	projectID, _ := projectRes.LastInsertId()
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO memento_project_message (project_id, message_id, included_by) VALUES
		  (?, 101, 'test'),
		  (?, 102, 'test')`, projectID, projectID); err != nil {
		t.Fatalf("insert project messages: %v", err)
	}

	index, err := srv.getBundleIndex(ctx, bundleIndexRequest{Kind: "project", ProjectID: projectID})
	if err != nil {
		t.Fatalf("get bundle index: %v", err)
	}
	if index.MessageCount != 2 || index.Messages[0].MessageID != 101 || index.Messages[1].Direction != "from_account" {
		t.Fatalf("unexpected index: %+v", index)
	}
	if index.Messages[0].Snippet == "" || index.EstimatedTokens == 0 {
		t.Fatalf("index missing compact metadata: %+v", index)
	}

	batch, err := srv.getMessageBatch(ctx, messageBatchRequest{
		MessageIDs:     []int64{102, 101, 102},
		IncludeBody:    true,
		BodyCharLimit:  10,
		IncludeHeaders: true,
	})
	if err != nil {
		t.Fatalf("get message batch: %v", err)
	}
	if len(batch) != 2 || batch[0].MessageID != 102 || batch[1].MessageID != 101 {
		t.Fatalf("batch order/dedupe mismatch: %+v", batch)
	}
	if batch[1].BodyText != "abcdefghij [...]" {
		t.Fatalf("body was not truncated: %+v", batch[1])
	}
	if len(batch[0].Recipients) != 1 || batch[0].Recipients[0] != "alex@example.com" {
		t.Fatalf("recipients not loaded: %+v", batch[0])
	}
}

func TestAgentToolRegistryDispatchesDirectly(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	createMsgvaultTablesForContextTools(t, db)
	if _, err := srv.db.ExecContext(ctx, `INSERT INTO participants (id, email_address, display_name) VALUES (1, 'alex@example.com', 'Alex')`); err != nil {
		t.Fatalf("insert participant: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO messages (id, subject, snippet, sent_at, sender_id, conversation_id)
		VALUES (301, 'Registry direct', 'Direct snippet', '2026-02-01', 1, 900)`); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	tool, ok := srv.agentTools()["get_message_batch"]
	if !ok {
		t.Fatal("get_message_batch tool missing")
	}
	raw := json.RawMessage(`{"message_ids":[301],"include_body":false}`)
	result, err := tool.Handler(ctx, agentrunner.ToolContext{RunID: 1, RunSpec: agentrunner.RunSpec{AgentType: agentrunner.AgentDashboard}}, raw)
	if err != nil {
		t.Fatalf("tool handler: %v", err)
	}
	items, ok := result.([]messageBatchItem)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if len(items) != 1 || items[0].MessageID != 301 {
		t.Fatalf("unexpected direct result: %+v", items)
	}
}

func TestAgentContextTools_SummarizeThread(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx := context.Background()
	createMsgvaultTablesForContextTools(t, srv.db)
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO participants (id, email_address, display_name) VALUES
		  (1, 'owner@example.com', 'Owner'),
		  (2, 'alex@example.com', 'Alex')`); err != nil {
		t.Fatalf("insert participants: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `
		INSERT INTO messages (id, subject, snippet, sent_at, sender_id, conversation_id) VALUES
		  (201, 'Thread', 'Opening', '2026-01-01', 1, 800),
		  (202, 'Thread', 'Middle', '2026-01-02', 2, 800),
		  (203, 'Thread', 'Close', '2026-01-03', 1, 800)`); err != nil {
		t.Fatalf("insert messages: %v", err)
	}
	if _, err := srv.db.ExecContext(ctx, `INSERT INTO message_recipients (message_id, participant_id, recipient_type) VALUES (201, 2, 'to')`); err != nil {
		t.Fatalf("insert recipients: %v", err)
	}
	digest, err := srv.summarizeThread(ctx, summarizeThreadRequest{ThreadID: 800, MaxMessages: 2})
	if err != nil {
		t.Fatalf("summarize thread: %v", err)
	}
	if digest.MessageCount != 3 || len(digest.Representative) != 2 || digest.EstimatedTokens == 0 {
		t.Fatalf("unexpected digest: %+v", digest)
	}
	if len(digest.Participants) == 0 {
		t.Fatalf("expected participants: %+v", digest)
	}
}

func createMsgvaultTablesForContextTools(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sources (identifier TEXT)`,
		`CREATE TABLE IF NOT EXISTS participants (id INTEGER PRIMARY KEY, email_address TEXT, display_name TEXT)`,
		`CREATE TABLE IF NOT EXISTS messages (id INTEGER PRIMARY KEY, subject TEXT, snippet TEXT, sent_at TEXT, sender_id INTEGER, conversation_id INTEGER)`,
		`CREATE TABLE IF NOT EXISTS message_bodies (message_id INTEGER PRIMARY KEY, body_text TEXT)`,
		`CREATE TABLE IF NOT EXISTS message_recipients (message_id INTEGER, participant_id INTEGER, recipient_type TEXT)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("create msgvault table: %v", err)
		}
	}
}

func TestAgentContextTools_OptimizationFeatures(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	createMsgvaultTablesForContextTools(t, db)

	// Create memento note table for notes testing
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_note (
			id INTEGER PRIMARY KEY,
			dimension TEXT,
			entity_id INTEGER,
			content TEXT,
			created_at TEXT,
			updated_at TEXT
		)`); err != nil {
		t.Fatalf("create note table: %v", err)
	}

	// Insert notes
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_note (dimension, entity_id, content, created_at, updated_at) VALUES
		  ('person', 42, 'User note about John', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatalf("insert note: %v", err)
	}

	// Insert person details so loadPersonForAgent fallback works
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_person (
			id INTEGER PRIMARY KEY,
			canonical_name TEXT,
			primary_email TEXT
		)`); err != nil {
		t.Fatalf("create memento_person table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_person_email (
			email_address TEXT PRIMARY KEY,
			person_id INTEGER,
			display_name TEXT,
			link_source TEXT,
			confidence REAL,
			locked INTEGER
		)`); err != nil {
		t.Fatalf("create memento_person_email table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (42, 'John Doe', 'john@example.com')`); err != nil {
		t.Fatalf("insert person: %v", err)
	}

	// 1. Verify person profile note embedding
	p, err := loadPersonForAgent(ctx, db, 42)
	if err != nil {
		t.Fatalf("loadPersonForAgent: %v", err)
	}
	notesSlice, ok := p["notes"].([]noteRow)
	if !ok {
		t.Fatalf("notes not returned or of wrong type: %T", p["notes"])
	}
	if len(notesSlice) != 1 || notesSlice[0].Content != "User note about John" {
		t.Errorf("unexpected notes returned: %+v", notesSlice)
	}

	// 2. Verify list_person_messages compact mode
	// First insert sources/participants/messages
	if _, err := db.ExecContext(ctx, `INSERT INTO sources (identifier) VALUES ('owner@example.com')`); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO participants (id, email_address) VALUES (420, 'john@example.com')`); err != nil {
		t.Fatalf("insert participant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence, locked)
		VALUES ('john@example.com', 42, 'John Doe', 'manual', 1.0, 1)`); err != nil {
		t.Fatalf("insert person email: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO messages (id, subject, snippet, sent_at, sender_id, conversation_id)
		VALUES (501, 'Testing fields', 'Secret Snippet', '2026-01-05', 420, 1000)`); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	msgs, err := listPersonMessages(ctx, db, 42, 10, "compact")
	if err != nil {
		t.Fatalf("listPersonMessages compact: %v", err)
	}
	if len(msgs) != 1 || msgs[0].MessageID != 501 {
		t.Fatalf("expected message 501, got: %+v", msgs)
	}
	if msgs[0].Snippet != "" || msgs[0].ViaEmail != "" || msgs[0].FromEmail != "" {
		t.Errorf("snippet or email not cleared in compact mode: %+v", msgs[0])
	}

	// 3. Verify get_project_summary / get_concept_summary brief parameter
	// Create required reports tables
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_projects_report (
			project_id INTEGER PRIMARY KEY,
			slug TEXT,
			name TEXT,
			status TEXT,
			started_at TEXT
		)`); err != nil {
		t.Fatalf("create projects report table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_project (
			id INTEGER PRIMARY KEY,
			slug TEXT,
			name TEXT,
			status TEXT,
			started_at TEXT,
			updated_at TEXT,
			note TEXT
		)`); err != nil {
		t.Fatalf("create project table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_project_member (
			project_id INTEGER,
			person_id INTEGER,
			role TEXT,
			PRIMARY KEY (project_id, person_id)
		)`); err != nil {
		t.Fatalf("create project member table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS memento_project_message (
			project_id INTEGER,
			message_id INTEGER,
			included_by TEXT,
			PRIMARY KEY (project_id, message_id)
		)`); err != nil {
		t.Fatalf("create project message table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_project (id, slug, name, status)
		VALUES (10, 'proj-test', 'Proj Test', 'active')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	projSummaryBrief, err := srv.getProjectSummaryForAgent(ctx, getProjectSummaryRequest{ProjectID: 10, Brief: true})
	if err != nil {
		t.Fatalf("getProjectSummaryForAgent brief: %v", err)
	}
	if _, ok := projSummaryBrief["members"]; ok {
		t.Errorf("members included in brief summary: %+v", projSummaryBrief)
	}
	if _, ok := projSummaryBrief["narrative"]; ok {
		t.Errorf("narrative included in brief summary: %+v", projSummaryBrief)
	}

	// 4. Verify decision timeout reduction default
	timeoutVal := agentDecisionTimeout()
	if timeoutVal != 90*time.Second {
		t.Errorf("expected 90s timeout, got %v", timeoutVal)
	}
}

func TestGetPersonSummaryDefaultsToCompactHighSignalPayload(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()
	createMsgvaultTablesForContextTools(t, db)

	if _, err := db.ExecContext(ctx, `INSERT INTO sources (identifier) VALUES ('owner@example.com')`); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO participants (id, email_address, display_name) VALUES
		  (1, 'owner@example.com', 'Owner'),
		  (2, 'ann@example.com', 'Ann'),
		  (3, 'noreply@docs.example.com', 'Docs')`); err != nil {
		t.Fatalf("insert participants: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO messages (id, subject, snippet, sent_at, sender_id, conversation_id) VALUES
		  (701, 'From Ann', 'hello', '2026-02-01', 2, 1),
		  (702, 'To Ann', 'reply', '2026-02-02', 1, 1),
		  (703, 'Shared doc', 'doc', '2026-02-03', 3, 2)`); err != nil {
		t.Fatalf("insert messages: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO message_recipients (message_id, participant_id, recipient_type) VALUES
		  (702, 2, 'to'),
		  (703, 1, 'to')`); err != nil {
		t.Fatalf("insert recipients: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person (id, canonical_name, primary_email)
		VALUES (5, 'Ann Example', 'ann@example.com')`); err != nil {
		t.Fatalf("insert person: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence, locked) VALUES
		  ('ann@example.com', 5, 'Ann Example', 'manual', 1.0, 1),
		  ('noreply@docs.example.com', 5, 'Ann via Docs', 'forwarder_unwrap', 0.7, 0)`); err != nil {
		t.Fatalf("insert person emails: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count, bidirectional_score,
			classification, first_message_at, last_message_at, slug,
			aliases_json, timeline_json, top_correspondents_json
		) VALUES (
			5, 'Ann Example', 'ann@example.com', 'example.com', 2,
			3, 2, 1, 0.8, 'candidate', '2026-02-01', '2026-02-03', 'ann-example',
			'[]', '[]', '[{"person_id":9,"canonical_name":"Liza","primary_email":"liza@example.com","shared_count":4}]'
		)`); err != nil {
		t.Fatalf("insert people report: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_note (dimension, entity_id, content, created_at, updated_at)
		VALUES ('person', 5, 'Ann is my wife', '2026-02-04', '2026-02-04')`); err != nil {
		t.Fatalf("insert note: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person_facet (person_id, facet_type, content, source_message_ids)
		VALUES (5, 'fact', 'Important fact [msg:701]', '[701]')`); err != nil {
		t.Fatalf("insert facet: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_person_narrative (person_id, section, content, source_message_ids)
		VALUES (5, 'summary', 'Narrative [msg:701]', '[701]')`); err != nil {
		t.Fatalf("insert narrative: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_social_metric (
			person_id, degree, weighted_degree, direct_degree, co_recipient_degree,
			cluster_id, dormancy_days, structural_role
		) VALUES (5, 7, 12.5, 3, 4, 2, 1, 'hub')`); err != nil {
		t.Fatalf("insert social metric: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memento_social_group (
			group_id, size, density, display_name, label, saved_at, note
		) VALUES (11, 4, 0.5, 'Family', 'Family group', '2026-02-05', 'Saved group note');
		INSERT INTO memento_social_group_member (group_id, person_id)
		VALUES (11, 5)`); err != nil {
		t.Fatalf("insert saved group: %v", err)
	}

	summary, err := srv.getPersonSummaryForAgent(ctx, getPersonSummaryRequest{PersonID: 5})
	if err != nil {
		t.Fatalf("getPersonSummaryForAgent: %v", err)
	}
	if _, ok := summary["aliases"]; ok {
		t.Fatalf("compact summary should not include full aliases: %+v", summary)
	}
	if _, ok := summary["recent_timeline_sample"]; ok {
		t.Fatalf("compact summary should not include timeline sample: %+v", summary)
	}
	notes, ok := summary["authoritative_notes"].([]string)
	if !ok || len(notes) != 1 || notes[0] != "Ann is my wife" {
		t.Fatalf("authoritative notes missing: %+v", summary["authoritative_notes"])
	}
	aliases, ok := summary["aliases_summary"].(personAliasesSummary)
	if !ok || aliases.Count != 2 || len(aliases.HighVolume) == 0 || aliases.ForwarderOrServiceCount != 1 {
		t.Fatalf("unexpected alias summary: %+v", summary["aliases_summary"])
	}
	memory, ok := summary["existing_memory"].(personExistingMemorySummary)
	if !ok || memory.FacetCount != 1 || len(memory.NarrativeSections) != 1 || memory.NarrativeSections[0] != "summary" {
		t.Fatalf("unexpected existing memory: %+v", summary["existing_memory"])
	}
	graph, ok := summary["social_graph"].(personSocialSummary)
	if !ok || graph.StructuralRole != "hub" || len(graph.SavedGroups) != 1 || graph.SavedGroups[0].DisplayName != "Family" {
		t.Fatalf("unexpected social graph summary: %+v", summary["social_graph"])
	}
}
