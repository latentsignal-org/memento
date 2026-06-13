package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestAgentDecisionLifecycle(t *testing.T) {
	db := newDraftTestDB(t)
	ctx := context.Background()

	decision := AgentDecision{
		ID:           "decision-1",
		DecisionType: "backfill",
		EntityType:   "draft",
		EntityID:     "41",
		PayloadJSON:  `{"message_ids":[1,2,3]}`,
	}
	if err := CreateAgentDecision(ctx, db, decision); err != nil {
		t.Fatalf("CreateAgentDecision: %v", err)
	}

	created, err := GetAgentDecision(ctx, db, decision.ID)
	if err != nil {
		t.Fatalf("GetAgentDecision: %v", err)
	}
	if created.Status != "pending" {
		t.Fatalf("created status = %q, want pending", created.Status)
	}
	if created.PayloadJSON != decision.PayloadJSON {
		t.Fatalf("payload_json = %q, want %q", created.PayloadJSON, decision.PayloadJSON)
	}

	resolved, err := ResolveAgentDecision(ctx, db, decision.ID, "accepted", `{"accepted":true,"added_count":3}`)
	if err != nil {
		t.Fatalf("ResolveAgentDecision: %v", err)
	}
	if resolved.Status != "accepted" {
		t.Fatalf("resolved status = %q, want accepted", resolved.Status)
	}
	if resolved.ResolvedAt == "" {
		t.Fatal("expected resolved_at to be set")
	}

	_, err = ResolveAgentDecision(ctx, db, decision.ID, "skipped", `{"accepted":false,"added_count":0}`)
	if err == nil {
		t.Fatal("expected resolving an already-resolved decision to fail")
	}
	if !strings.Contains(err.Error(), "already accepted") {
		t.Fatalf("unexpected second resolve error: %v", err)
	}
}

func TestGetAgentDecisionMissing(t *testing.T) {
	db := newDraftTestDB(t)
	_, err := GetAgentDecision(context.Background(), db, "missing")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing decision error = %v", err)
	}
}
