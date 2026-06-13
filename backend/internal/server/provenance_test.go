package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"memento/backend/internal/store"
)

func TestProjectProvenanceEndpoint_ReturnsRevisionsAndCollectorLoops(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	draftID, err := store.CreateDraft(ctx, db, "project", "Hackathon bundle")
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if err := store.UpdateDraftEntities(ctx, db, draftID, `{"name":"Hackathons","messages":[{"message_id":1}]}`); err != nil {
		t.Fatalf("UpdateDraftEntities: %v", err)
	}
	if err := store.UpdateDraftState(ctx, db, draftID, "interaction_a", `[{"role":"user","text":"I meant hackathons in 2026"}]`); err != nil {
		t.Fatalf("UpdateDraftState: %v", err)
	}

	projectRes, err := db.ExecContext(ctx, `
		INSERT INTO memento_project (slug, name, aliases, status, note, origin_draft_id)
		VALUES ('proj-provenance', 'Proj Provenance', '[]', 'active', '', ?)`, draftID,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}
	projectID, _ := projectRes.LastInsertId()
	if err := store.MarkDraftCommitted(ctx, db, draftID, projectID); err != nil {
		t.Fatalf("MarkDraftCommitted: %v", err)
	}

	bodyBytes, _ := json.Marshal(createAgentSessionRequest{
		SessionType: "collector",
		EntityID:    "1",
	})
	req := httptest.NewRequest("POST", "/api/internal/agent-sessions", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w := httptest.NewRecorder()
	t.Setenv("MEMENTO_INTERNAL_TOKEN", "test-secret-token")
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create session status = %d", w.Code)
	}
	var sessionResp createAgentSessionResponse
	if err := json.NewDecoder(w.Body).Decode(&sessionResp); err != nil {
		t.Fatalf("decode session resp: %v", err)
	}

	loopReq := logAgentLoopRequest{
		StepIndex:       1,
		InputType:       "user_input",
		InputContent:    "I meant hackathons in 2026",
		AssistantText:   "Adjusted bundle.",
		ToolCallsJSON:   `[{"id":"call_1","name":"fts_search","args":{"query":"hackathon after:2026-01-01"}}]`,
		ToolResultsJSON: `[{"name":"fts_search","call_id":"call_1","result":[{"message_id":1}]}]`,
		DurationMs:      120,
	}
	bodyBytes, _ = json.Marshal(loopReq)
	req = httptest.NewRequest("POST", "/api/internal/agent-sessions/"+strconv.FormatInt(sessionResp.SessionID, 10)+"/loops", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Internal-Token", "test-secret-token")
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("log loop status = %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/projects/proj-provenance/provenance", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provenance status = %d body=%s", w.Code, w.Body.String())
	}

	var resp draftProvenanceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode provenance: %v", err)
	}
	if resp.DraftID != draftID {
		t.Fatalf("draft id = %d, want %d", resp.DraftID, draftID)
	}
	if len(resp.Revisions) < 4 {
		t.Fatalf("revision len = %d, want >= 4", len(resp.Revisions))
	}
	if len(resp.CollectorLoops) != 1 {
		t.Fatalf("collector loop len = %d, want 1", len(resp.CollectorLoops))
	}
}

func TestConceptProvenanceEndpoint_404WithoutOriginDraft(t *testing.T) {
	srv, db := newTestServer(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO memento_concept (slug, name, scope_description, seed_keywords, status, note)
		VALUES ('concept-no-origin', 'Concept No Origin', '', '[]', 'active', '')`)
	if err != nil {
		t.Fatalf("insert concept: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/concepts/concept-no-origin/provenance", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
