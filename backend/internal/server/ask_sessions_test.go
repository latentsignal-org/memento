package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"memento/backend/internal/agentrunner"
	"memento/backend/internal/store"
)

func TestExtractCitedMessageIDs(t *testing.T) {
	got := extractCitedMessageIDs("Intro [msg:5] middle [msg:7], repeat [msg:5] end.")
	if len(got) != 2 || got[0] != 5 || got[1] != 7 {
		t.Fatalf("extractCitedMessageIDs = %v, want [5 7]", got)
	}
	if got := extractCitedMessageIDs("no citations here"); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestAskAnswerSummary_StripsCitationsAndTruncates(t *testing.T) {
	got := askAnswerSummary("Kenneth Lay [msg:12]   announced the\nmerger [msg:9].")
	if got != "Kenneth Lay announced the merger." {
		t.Fatalf("askAnswerSummary = %q", got)
	}
	long := ""
	for i := 0; i < 60; i++ {
		long += "word "
	}
	summary := askAnswerSummary(long)
	if len(summary) > 245 {
		t.Fatalf("summary too long: %d chars", len(summary))
	}
}

func TestEffectiveTurnStatus(t *testing.T) {
	cases := []struct {
		turn, run, want string
	}{
		{"running", store.AgentRunFailed, "failed"},
		{"running", store.AgentRunCancelled, "failed"},
		{"running", store.AgentRunRunning, "running"},
		{"complete", store.AgentRunFailed, "complete"},
	}
	for _, tc := range cases {
		if got := effectiveTurnStatus(tc.turn, tc.run); got != tc.want {
			t.Fatalf("effectiveTurnStatus(%q, %q) = %q, want %q", tc.turn, tc.run, got, tc.want)
		}
	}
}

func TestBuildAgentRunSpec_DashboardCreatesAskSessionAndPersistsAnswer(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	spec, err := srv.buildAgentRunSpec(ctx, createAgentRunRequest{
		AgentType:   "dashboard",
		EntityID:    "dashboard",
		UserMessage: "Who is Kenneth Lay?",
	})
	if err != nil {
		t.Fatalf("buildAgentRunSpec: %v", err)
	}

	sessionID, err := metadataInt64(spec.RequestMetadata["ask_session_id"])
	if err != nil {
		t.Fatalf("ask_session_id missing from metadata: %v", err)
	}
	turnID, err := metadataInt64(spec.RequestMetadata["ask_turn_id"])
	if err != nil {
		t.Fatalf("ask_turn_id missing from metadata: %v", err)
	}
	if spec.RequestMetadata["ask_session_slug"] != "who-is-kenneth-lay" {
		t.Fatalf("slug = %v, want who-is-kenneth-lay", spec.RequestMetadata["ask_session_slug"])
	}
	if spec.AfterDone == nil {
		t.Fatal("dashboard spec must set AfterDone to persist the answer")
	}

	turn, err := store.GetAskTurn(ctx, db, turnID)
	if err != nil {
		t.Fatalf("GetAskTurn: %v", err)
	}
	if turn.UserMessage != "Who is Kenneth Lay?" || turn.Status != "running" {
		t.Fatalf("unexpected turn: %+v", turn)
	}

	run, err := store.CreateAgentRun(ctx, db, store.AgentRun{SessionType: "dashboard", EntityID: "dashboard"})
	if err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}
	err = spec.AfterDone(ctx, agentrunner.AfterDoneContext{
		RunID:         run.ID,
		InteractionID: "int-1",
		AssistantText: "Kenneth Lay was CEO [msg:42] and chairman [msg:7].",
	})
	if err != nil {
		t.Fatalf("AfterDone: %v", err)
	}

	turn, err = store.GetAskTurn(ctx, db, turnID)
	if err != nil {
		t.Fatalf("GetAskTurn after done: %v", err)
	}
	if turn.Status != "complete" {
		t.Fatalf("turn status = %q, want complete", turn.Status)
	}
	if turn.CitedMessageIDsJSON != "[42,7]" {
		t.Fatalf("cited ids = %q, want [42,7]", turn.CitedMessageIDsJSON)
	}

	session, err := store.GetAskSessionByID(ctx, db, sessionID)
	if err != nil {
		t.Fatalf("GetAskSessionByID: %v", err)
	}
	if session.Summary == "" {
		t.Fatal("session summary should be set from the first completed answer")
	}

	// A follow-up turn referencing the session must append, not create.
	spec2, err := srv.buildAgentRunSpec(ctx, createAgentRunRequest{
		AgentType:       "dashboard",
		EntityID:        "dashboard",
		UserMessage:     "What happened next?",
		RequestMetadata: map[string]any{"ask_session_id": float64(sessionID)},
	})
	if err != nil {
		t.Fatalf("buildAgentRunSpec follow-up: %v", err)
	}
	turn2ID, err := metadataInt64(spec2.RequestMetadata["ask_turn_id"])
	if err != nil {
		t.Fatalf("follow-up ask_turn_id: %v", err)
	}
	turn2, err := store.GetAskTurn(ctx, db, turn2ID)
	if err != nil {
		t.Fatalf("GetAskTurn follow-up: %v", err)
	}
	if turn2.AskSessionID != sessionID || turn2.TurnIndex != 1 {
		t.Fatalf("follow-up turn = %+v, want session %d index 1", turn2, sessionID)
	}
}

func TestBuildAgentRunSpec_DashboardRejectsUnknownAskSession(t *testing.T) {
	srv, _ := newTestServer(t)
	_, err := srv.buildAgentRunSpec(context.Background(), createAgentRunRequest{
		AgentType:       "dashboard",
		EntityID:        "dashboard",
		UserMessage:     "hello",
		RequestMetadata: map[string]any{"ask_session_id": float64(999)},
	})
	if err == nil {
		t.Fatal("expected error for unknown ask_session_id")
	}
}

func TestAskSessionHTTPHandlers(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	session, err := store.CreateAskSession(ctx, db, "Dynegy collapse question")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	turn, err := store.AppendAskTurn(ctx, db, session.ID, "What happened with Dynegy?")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}
	if err := store.CompleteAskTurn(ctx, db, turn.ID, "It collapsed [msg:3].", "It collapsed.", "[3]", "{}"); err != nil {
		t.Fatalf("CompleteAskTurn: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions = %d: %s", rec.Code, rec.Body.String())
	}
	var index struct {
		Sessions []store.AskSession `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if len(index.Sessions) != 1 || index.Sessions[0].Slug != session.Slug || index.Sessions[0].TurnCount != 1 {
		t.Fatalf("unexpected index: %+v", index.Sessions)
	}

	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.Slug, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions/%s = %d: %s", session.Slug, rec.Code, rec.Body.String())
	}
	var detail struct {
		Session store.AskSession  `json:"session"`
		Turns   []askTurnResponse `json:"turns"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Turns) != 1 {
		t.Fatalf("turn count = %d, want 1", len(detail.Turns))
	}
	got := detail.Turns[0]
	if got.AssistantAnswer != "It collapsed [msg:3]." || got.Status != "complete" {
		t.Fatalf("unexpected turn: %+v", got)
	}
	if len(got.CitedMessageIDs) != 1 || got.CitedMessageIDs[0] != 3 {
		t.Fatalf("cited ids = %v, want [3]", got.CitedMessageIDs)
	}

	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing session = %d, want 404", rec.Code)
	}
}

func TestAskSessionMutationsAndPromotion(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	session, err := store.CreateAskSession(ctx, db, "Original title")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	turn, err := store.AppendAskTurn(ctx, db, session.ID, "What happened with Dynegy?")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}
	if err := store.CompleteAskTurn(ctx, db, turn.ID, "The deal fell apart [msg:11].", "The deal fell apart.", "[11]", "{}"); err != nil {
		t.Fatalf("CompleteAskTurn: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+session.Slug, strings.NewReader(`{"title":"Renamed session","pinned":true,"archived":true}`))
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/sessions/%s = %d: %s", session.Slug, rec.Code, rec.Body.String())
	}
	var updateResp struct {
		Session store.AskSession `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updateResp.Session.Title != "Renamed session" || !updateResp.Session.Pinned || updateResp.Session.ArchivedAt == nil {
		t.Fatalf("unexpected updated session: %+v", updateResp.Session)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.Slug+"/promote", strings.NewReader(`{"kind":"project"}`))
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST promote = %d: %s", rec.Code, rec.Body.String())
	}
	var promoteResp struct {
		DraftID int64  `json:"draft_id"`
		Kind    string `json:"kind"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &promoteResp); err != nil {
		t.Fatalf("decode promote: %v", err)
	}
	if promoteResp.DraftID == 0 || promoteResp.Kind != "project" || !strings.Contains(promoteResp.URL, "/projects/new?draftId=") {
		t.Fatalf("unexpected promote response: %+v", promoteResp)
	}
	draft, err := store.GetDraft(ctx, db, promoteResp.DraftID)
	if err != nil {
		t.Fatalf("GetDraft promoted: %v", err)
	}
	if !strings.Contains(draft.TranscriptJSON, "What happened with Dynegy?") ||
		!strings.Contains(draft.TranscriptJSON, "The deal fell apart.") ||
		strings.Contains(draft.TranscriptJSON, "[msg:11]") {
		t.Fatalf("unexpected promoted transcript: %s", draft.TranscriptJSON)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.Slug, nil)
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/sessions/%s = %d: %s", session.Slug, rec.Code, rec.Body.String())
	}
	if _, err := store.GetAskSessionByID(ctx, db, session.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted session err = %v, want sql.ErrNoRows", err)
	}
}

func TestContextSearchAndDashboardContextRefs(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	personID := seedContextPerson(t, db)
	priorSession, err := store.CreateAskSession(ctx, db, "Dynegy investigation")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	priorTurn, err := store.AppendAskTurn(ctx, db, priorSession.ID, "What happened with Dynegy?")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}
	if err := store.CompleteAskTurn(ctx, db, priorTurn.ID, "The deal fell apart [msg:11].", "The deal fell apart.", "[11]", "{}"); err != nil {
		t.Fatalf("CompleteAskTurn: %v", err)
	}
	seedContextProjectAndConcept(t, db)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/context-search?trigger=@&q=Kenneth", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("people context search = %d: %s", rec.Code, rec.Body.String())
	}
	var peopleSearch struct {
		Results []contextSearchResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &peopleSearch); err != nil {
		t.Fatalf("decode people search: %v", err)
	}
	if len(peopleSearch.Results) != 1 || peopleSearch.Results[0].Kind != "person" || peopleSearch.Results[0].ID != "1" {
		t.Fatalf("unexpected people results: %+v", peopleSearch.Results)
	}

	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/context-search?trigger=%23&q=Dynegy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("artifact context search = %d: %s", rec.Code, rec.Body.String())
	}
	var artifactSearch struct {
		Results []contextSearchResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &artifactSearch); err != nil {
		t.Fatalf("decode artifact search: %v", err)
	}
	if len(artifactSearch.Results) < 2 {
		t.Fatalf("expected session/project artifact results, got %+v", artifactSearch.Results)
	}

	spec, err := srv.buildAgentRunSpec(ctx, createAgentRunRequest{
		AgentType:   "dashboard",
		EntityID:    "dashboard",
		UserMessage: "Use the selected context.",
		RequestMetadata: map[string]any{
			"context_refs": []map[string]any{
				{"kind": "person", "person_id": float64(personID), "label": "Kenneth Lay"},
				{"kind": "ask_session", "session_id": float64(priorSession.ID), "label": priorSession.Title},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildAgentRunSpec with context refs: %v", err)
	}
	if len(spec.InitialEvents) != 1 || spec.InitialEvents[0]["type"] != "context_loaded" {
		t.Fatalf("initial events = %+v, want one context_loaded", spec.InitialEvents)
	}
	if len(spec.InitialTranscript) == 0 || !strings.Contains(spec.InitialTranscript[0].Content, "Deterministic context loaded") {
		t.Fatalf("initial transcript missing context bootstrap: %+v", spec.InitialTranscript)
	}
	turnID, err := metadataInt64(spec.RequestMetadata["ask_turn_id"])
	if err != nil {
		t.Fatalf("ask_turn_id missing: %v", err)
	}
	refs, err := store.ListAskContextRefs(ctx, db, []int64{turnID})
	if err != nil {
		t.Fatalf("ListAskContextRefs: %v", err)
	}
	if len(refs[turnID]) != 2 {
		t.Fatalf("persisted refs = %+v, want 2", refs[turnID])
	}
}

func seedContextPerson(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO memento_person (canonical_name, primary_email)
		VALUES ('Kenneth Lay', 'kenneth@example.test')`)
	if err != nil {
		t.Fatalf("insert person: %v", err)
	}
	personID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("person id: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO memento_person_email (email_address, person_id, display_name, link_source, confidence)
		VALUES ('kenneth@example.test', ?, 'Kenneth Lay', 'test', 1.0)`, personID); err != nil {
		t.Fatalf("insert person email: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO memento_people_report (
			person_id, canonical_name, primary_email, domain, email_count,
			total_messages, from_contact_count, to_contact_count,
			bidirectional_score, classification, first_message_at, last_message_at, slug
		)
		VALUES (?, 'Kenneth Lay', 'kenneth@example.test', 'example.test', 1,
			42, 20, 22, 0.9, 'person', '2001-01-01', '2001-12-01', 'kenneth-lay')`, personID); err != nil {
		t.Fatalf("insert people report: %v", err)
	}
	return personID
}

func seedContextProjectAndConcept(t *testing.T, db *sql.DB) {
	t.Helper()
	res, err := db.Exec(`
		INSERT INTO memento_project (slug, name, status)
		VALUES ('dynegy-project', 'Dynegy Project', 'active')`)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	projectID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("project id: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO memento_projects_report (project_id, slug, name, status, summary_json)
		VALUES (?, 'dynegy-project', 'Dynegy Project', 'active', '{"summary":"Dynegy work"}')`, projectID); err != nil {
		t.Fatalf("insert project report: %v", err)
	}
	res, err = db.Exec(`
		INSERT INTO memento_concept (slug, name, scope_description, status)
		VALUES ('dynegy-concept', 'Dynegy Concept', 'Acquisition context', 'active')`)
	if err != nil {
		t.Fatalf("insert concept: %v", err)
	}
	conceptID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("concept id: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO memento_concepts_report (concept_id, slug, name, status, scope_description, message_count)
		VALUES (?, 'dynegy-concept', 'Dynegy Concept', 'active', 'Acquisition context', 9)`, conceptID); err != nil {
		t.Fatalf("insert concept report: %v", err)
	}
}
