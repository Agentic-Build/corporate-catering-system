package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
)

type AuditRepo struct{ pool *pgxpool.Pool }

func NewAuditRepo(p *pgxpool.Pool) *AuditRepo { return &AuditRepo{pool: p} }

func (r *AuditRepo) WriteTx(ctx context.Context, tx pgx.Tx, e plaudit.Entry) error {
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_event (actor_id, actor_role, action, target_kind, target_id, payload, request_id)
VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ActorID, e.ActorRole, e.Action, e.TargetKind, e.TargetID, raw, e.RequestID)
	return err
}

func (r *AuditRepo) Write(ctx context.Context, e plaudit.Entry) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return r.WriteTx(ctx, tx, e)
	})
}

func (r *AuditRepo) List(ctx context.Context, filter compliance.AuditFilter) ([]compliance.AuditRow, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	args := []any{}
	where := []string{}
	if filter.TargetKind != "" {
		args = append(args, filter.TargetKind)
		where = append(where, fmt.Sprintf("target_kind = $%d", len(args)))
	}
	if filter.TargetID != "" {
		args = append(args, filter.TargetID)
		where = append(where, fmt.Sprintf("target_id = $%d", len(args)))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since)
		where = append(where, fmt.Sprintf("at >= $%d", len(args)))
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit)
	limitPh := fmt.Sprintf("$%d", len(args))

	q := `SELECT id, actor_id, actor_role, action, target_kind, target_id, payload, at, request_id
         FROM audit_event ` + whereClause + ` ORDER BY at DESC LIMIT ` + limitPh
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []compliance.AuditRow{}
	for rows.Next() {
		var row compliance.AuditRow
		var payloadJSON []byte
		if err := rows.Scan(
			&row.ID, &row.ActorID, &row.ActorRole, &row.Action,
			&row.TargetKind, &row.TargetID, &payloadJSON, &row.At, &row.RequestID,
		); err != nil {
			return nil, err
		}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &row.Payload)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
