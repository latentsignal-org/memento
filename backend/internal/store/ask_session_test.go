package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func newAskTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestAskSessionTitleFromQuery(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"short", "Who is Kenneth Lay?", "Who is Kenneth Lay?"},
		{"collapses whitespace", "  what\n\nchanged   after bankruptcy ", "what changed after bankruptcy"},
		{"truncates on word boundary", "tell me about the long running negotiation with Dynegy that happened during the final quarter", "tell me about the long running negotiation with Dynegy that happened during the…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AskSessionTitleFromQuery(tc.query); got != tc.want {
				t.Fatalf("AskSessionTitleFromQuery(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func TestCreateAskSession_SlugsAreUnique(t *testing.T) {
	db := newAskTestDB(t)
	ctx := context.Background()

	first, err := CreateAskSession(ctx, db, "Dynegy collapse question")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	if first.Slug != "dynegy-collapse-question" {
		t.Fatalf("slug = %q, want dynegy-collapse-question", first.Slug)
	}
	second, err := CreateAskSession(ctx, db, "Dynegy collapse question")
	if err != nil {
		t.Fatalf("CreateAskSession (duplicate title): %v", err)
	}
	if second.Slug != "dynegy-collapse-question-2" {
		t.Fatalf("second slug = %q, want dynegy-collapse-question-2", second.Slug)
	}

	empty, err := CreateAskSession(ctx, db, "???")
	if err != nil {
		t.Fatalf("CreateAskSession (symbol title): %v", err)
	}
	if empty.Slug != "ask" {
		t.Fatalf("symbol-title slug = %q, want ask", empty.Slug)
	}
}

func TestCreateAskSession_AvoidsReservedRouteSlugs(t *testing.T) {
	for _, title := range []string{"_", "new", "merge review", "merge-review"} {
		t.Run(title, func(t *testing.T) {
			db := newAskTestDB(t)
			session, err := CreateAskSession(context.Background(), db, title)
			if err != nil {
				t.Fatalf("CreateAskSession(%q): %v", title, err)
			}
			if session.Slug != "ask" {
				t.Fatalf("CreateAskSession(%q) slug = %q, want ask", title, session.Slug)
			}
		})
	}
}

func TestAppendAskTurn_AssignsSequentialIndexes(t *testing.T) {
	db := newAskTestDB(t)
	ctx := context.Background()

	session, err := CreateAskSession(ctx, db, "test session")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	for i := 0; i < 3; i++ {
		turn, err := AppendAskTurn(ctx, db, session.ID, "question")
		if err != nil {
			t.Fatalf("AppendAskTurn #%d: %v", i, err)
		}
		if turn.TurnIndex != i {
			t.Fatalf("turn index = %d, want %d", turn.TurnIndex, i)
		}
		if turn.Status != "running" {
			t.Fatalf("turn status = %q, want running", turn.Status)
		}
	}
}

func TestCompleteAskTurn_PersistsAnswerAndSessionSummary(t *testing.T) {
	db := newAskTestDB(t)
	ctx := context.Background()

	session, err := CreateAskSession(ctx, db, "test session")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	turn, err := AppendAskTurn(ctx, db, session.ID, "what happened?")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}

	err = CompleteAskTurn(ctx, db, turn.ID, "The full answer [msg:42].", "The full answer.", "[42]", `{"tool_calls":1}`)
	if err != nil {
		t.Fatalf("CompleteAskTurn: %v", err)
	}

	got, err := GetAskTurn(ctx, db, turn.ID)
	if err != nil {
		t.Fatalf("GetAskTurn: %v", err)
	}
	if got.Status != "complete" {
		t.Fatalf("status = %q, want complete", got.Status)
	}
	if got.AssistantAnswer != "The full answer [msg:42]." {
		t.Fatalf("answer = %q", got.AssistantAnswer)
	}
	if got.CitedMessageIDsJSON != "[42]" {
		t.Fatalf("cited ids = %q, want [42]", got.CitedMessageIDsJSON)
	}

	refreshed, err := GetAskSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetAskSessionByID: %v", err)
	}
	if refreshed.Summary != "The full answer." {
		t.Fatalf("session summary = %q, want first answer summary", refreshed.Summary)
	}

	// A second completed turn must not overwrite the session summary.
	turn2, err := AppendAskTurn(ctx, db, session.ID, "follow up")
	if err != nil {
		t.Fatalf("AppendAskTurn 2: %v", err)
	}
	if err := CompleteAskTurn(ctx, db, turn2.ID, "Another answer.", "Another answer.", "[]", "{}"); err != nil {
		t.Fatalf("CompleteAskTurn 2: %v", err)
	}
	refreshed, err = GetAskSessionByID(ctx, db, session.ID)
	if err != nil {
		t.Fatalf("GetAskSessionByID after second turn: %v", err)
	}
	if refreshed.Summary != "The full answer." {
		t.Fatalf("session summary changed to %q, want stable first summary", refreshed.Summary)
	}
}

func TestLinkAskTurnRun_AndFailedStatus(t *testing.T) {
	db := newAskTestDB(t)
	ctx := context.Background()

	session, err := CreateAskSession(ctx, db, "fail case")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	turn, err := AppendAskTurn(ctx, db, session.ID, "question")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}

	run, err := CreateAgentRun(ctx, db, AgentRun{SessionType: "dashboard", EntityID: "dashboard"})
	if err != nil {
		t.Fatalf("CreateAgentRun: %v", err)
	}
	if err := LinkAskTurnRun(ctx, db, turn.ID, run.ID); err != nil {
		t.Fatalf("LinkAskTurnRun: %v", err)
	}
	got, err := GetAskTurn(ctx, db, turn.ID)
	if err != nil {
		t.Fatalf("GetAskTurn: %v", err)
	}
	if got.RunID == nil || *got.RunID != run.ID {
		t.Fatalf("run id = %v, want %d", got.RunID, run.ID)
	}

	if err := MarkAskTurnFailed(ctx, db, turn.ID); err != nil {
		t.Fatalf("MarkAskTurnFailed: %v", err)
	}
	got, err = GetAskTurn(ctx, db, turn.ID)
	if err != nil {
		t.Fatalf("GetAskTurn after fail: %v", err)
	}
	if got.Status != "failed" {
		t.Fatalf("status = %q, want failed", got.Status)
	}

	if err := LinkAskTurnRun(ctx, db, 99999, run.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("LinkAskTurnRun(missing turn) err = %v, want sql.ErrNoRows", err)
	}
}

func TestListAskSessions_OrdersPinnedAndUnarchivedFirst(t *testing.T) {
	db := newAskTestDB(t)
	ctx := context.Background()

	plain, err := CreateAskSession(ctx, db, "plain")
	if err != nil {
		t.Fatalf("CreateAskSession plain: %v", err)
	}
	pinned, err := CreateAskSession(ctx, db, "pinned")
	if err != nil {
		t.Fatalf("CreateAskSession pinned: %v", err)
	}
	archived, err := CreateAskSession(ctx, db, "archived")
	if err != nil {
		t.Fatalf("CreateAskSession archived: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE memento_ask_session SET pinned = 1 WHERE id = ?`, pinned.ID); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE memento_ask_session SET archived_at = CURRENT_TIMESTAMP WHERE id = ?`, archived.ID); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := AppendAskTurn(ctx, db, plain.ID, "q"); err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}

	sessions, err := ListAskSessions(ctx, db, false)
	if err != nil {
		t.Fatalf("ListAskSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("unarchived session count = %d, want 2", len(sessions))
	}
	if sessions[0].ID != pinned.ID {
		t.Fatalf("first session = %d, want pinned %d", sessions[0].ID, pinned.ID)
	}
	if sessions[1].TurnCount != 1 {
		t.Fatalf("plain turn_count = %d, want 1", sessions[1].TurnCount)
	}

	all, err := ListAskSessions(ctx, db, true)
	if err != nil {
		t.Fatalf("ListAskSessions(all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all session count = %d, want 3", len(all))
	}
	if all[2].ID != archived.ID {
		t.Fatalf("archived session should sort last, got %d", all[2].ID)
	}
}

func TestAskContextRefs_RoundTrip(t *testing.T) {
	db := newAskTestDB(t)
	ctx := context.Background()

	session, err := CreateAskSession(ctx, db, "refs")
	if err != nil {
		t.Fatalf("CreateAskSession: %v", err)
	}
	turn, err := AppendAskTurn(ctx, db, session.ID, "q")
	if err != nil {
		t.Fatalf("AppendAskTurn: %v", err)
	}

	refs := []AskContextRef{
		{RefKind: "person", RefID: "109", Label: "Kenneth Lay", PayloadJSON: `{"slug":"kenneth-lay"}`},
		{RefKind: "project", RefID: "audit-pressure", Label: "Audit Pressure"},
	}
	if err := AddAskContextRefs(ctx, db, turn.ID, refs); err != nil {
		t.Fatalf("AddAskContextRefs: %v", err)
	}

	byTurn, err := ListAskContextRefs(ctx, db, []int64{turn.ID})
	if err != nil {
		t.Fatalf("ListAskContextRefs: %v", err)
	}
	got := byTurn[turn.ID]
	if len(got) != 2 {
		t.Fatalf("ref count = %d, want 2", len(got))
	}
	if got[0].RefKind != "person" || got[0].RefID != "109" || got[0].Label != "Kenneth Lay" {
		t.Fatalf("first ref = %+v", got[0])
	}
	if got[1].PayloadJSON != "{}" {
		t.Fatalf("empty payload should default to {}, got %q", got[1].PayloadJSON)
	}
}
