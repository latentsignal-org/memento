package store

import (
	"context"
	"database/sql"
	"fmt"
)

type AgentDecision struct {
	ID           string `json:"id"`
	DecisionType string `json:"decision_type"`
	EntityType   string `json:"entity_type"`
	EntityID     string `json:"entity_id"`
	Status       string `json:"status"`
	PayloadJSON  string `json:"payload_json"`
	ResultJSON   string `json:"result_json"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	ResolvedAt   string `json:"resolved_at"`
}

func CreateAgentDecision(ctx context.Context, db *sql.DB, d AgentDecision) error {
	if d.ID == "" {
		return fmt.Errorf("decision id is required")
	}
	if d.DecisionType == "" || d.EntityType == "" || d.EntityID == "" {
		return fmt.Errorf("decision_type, entity_type, and entity_id are required")
	}
	if d.PayloadJSON == "" {
		d.PayloadJSON = "{}"
	}
	if d.ResultJSON == "" {
		d.ResultJSON = "{}"
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO memento_agent_decision (
			id, decision_type, entity_type, entity_id, status, payload_json, result_json
		) VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
		d.ID, d.DecisionType, d.EntityType, d.EntityID, d.PayloadJSON, d.ResultJSON,
	)
	return err
}

func GetAgentDecision(ctx context.Context, db *sql.DB, id string) (AgentDecision, error) {
	var d AgentDecision
	var resolved sql.NullString
	err := db.QueryRowContext(ctx, `
		SELECT id, decision_type, entity_type, entity_id, status,
		       payload_json, result_json, created_at, updated_at, resolved_at
		FROM memento_agent_decision
		WHERE id = ?`, id,
	).Scan(
		&d.ID, &d.DecisionType, &d.EntityType, &d.EntityID, &d.Status,
		&d.PayloadJSON, &d.ResultJSON, &d.CreatedAt, &d.UpdatedAt, &resolved,
	)
	if err != nil {
		return d, err
	}
	if resolved.Valid {
		d.ResolvedAt = resolved.String
	}
	return d, nil
}

func ResolveAgentDecision(ctx context.Context, db *sql.DB, id, status, resultJSON string) (AgentDecision, error) {
	if status != "accepted" && status != "skipped" && status != "expired" {
		return AgentDecision{}, fmt.Errorf("invalid decision status %q", status)
	}
	if resultJSON == "" {
		resultJSON = "{}"
	}
	res, err := db.ExecContext(ctx, `
		UPDATE memento_agent_decision
		SET status = ?, result_json = ?, resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'`,
		status, resultJSON, id,
	)
	if err != nil {
		return AgentDecision{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		existing, getErr := GetAgentDecision(ctx, db, id)
		if getErr != nil {
			return AgentDecision{}, getErr
		}
		return existing, fmt.Errorf("decision %s is already %s", id, existing.Status)
	}
	return GetAgentDecision(ctx, db, id)
}
