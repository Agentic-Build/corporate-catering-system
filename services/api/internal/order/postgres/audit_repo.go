package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditRepo struct{ pool *pgxpool.Pool }

func NewAuditRepo(p *pgxpool.Pool) *AuditRepo { return &AuditRepo{pool: p} }

func (r *AuditRepo) WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_event (actor_id, actor_role, action, target_kind, target_id, payload, request_id)
VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		actorID, actorRole, action, targetKind, targetID, raw, requestID)
	return err
}

func (r *AuditRepo) Write(ctx context.Context, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return r.WriteTx(ctx, tx, actorID, actorRole, action, targetKind, targetID, payload, requestID)
	})
}
