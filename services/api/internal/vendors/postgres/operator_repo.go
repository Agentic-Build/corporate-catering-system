package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

type OperatorRepo struct{ pool *pgxpool.Pool }

func NewOperatorRepo(p *pgxpool.Pool) *OperatorRepo { return &OperatorRepo{pool: p} }

func (r *OperatorRepo) Get(ctx context.Context, vendorID, operatorID string) (*vendor.OperatorAccount, error) {
	return r.one(ctx, `WHERE id = $1 AND vendor_id = $2`, operatorID, vendorID)
}

func (r *OperatorRepo) ListByVendor(ctx context.Context, vendorID string) ([]*vendor.OperatorAccount, error) {
	return r.list(ctx, `WHERE vendor_id = $1 ORDER BY created_at DESC`, vendorID)
}

func (r *OperatorRepo) ListByVendorStatus(ctx context.Context, vendorID string, statuses []vendor.OperatorStatus) ([]*vendor.OperatorAccount, error) {
	if len(statuses) == 0 {
		return r.ListByVendor(ctx, vendorID)
	}
	args := []any{vendorID}
	placeholders := make([]string, len(statuses))
	for i, s := range statuses {
		args = append(args, string(s))
		placeholders[i] = fmt.Sprintf("$%d", i+2)
	}
	return r.list(ctx,
		`WHERE vendor_id = $1 AND status IN (`+strings.Join(placeholders, ",")+`) ORDER BY created_at DESC`,
		args...,
	)
}

func (r *OperatorRepo) Upsert(ctx context.Context, op *vendor.OperatorAccount) error {
	return r.pool.QueryRow(ctx, `
INSERT INTO vendor_operator_account
  (vendor_id, email, display_name, provider, external_subject, status, setup_url, last_synced_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (vendor_id, email) DO UPDATE
SET display_name = EXCLUDED.display_name,
    provider = EXCLUDED.provider,
    external_subject = EXCLUDED.external_subject,
    status = EXCLUDED.status,
    setup_url = EXCLUDED.setup_url,
    last_synced_at = EXCLUDED.last_synced_at,
    updated_at = now()
RETURNING id, created_at, updated_at`,
		op.VendorID, op.Email, op.DisplayName, op.Provider, op.ExternalSubject,
		string(op.Status), op.SetupURL, op.LastSyncedAt,
	).Scan(&op.ID, &op.CreatedAt, &op.UpdatedAt)
}

func (r *OperatorRepo) SetStatus(ctx context.Context, vendorID, operatorID string, status vendor.OperatorStatus) error {
	cmd, err := r.pool.Exec(ctx, `
UPDATE vendor_operator_account
   SET status = $3, last_synced_at = now(), updated_at = now()
 WHERE vendor_id = $1 AND id = $2`, vendorID, operatorID, string(status))
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return vendor.ErrOperatorNotFound
	}
	return nil
}

func (r *OperatorRepo) SetStatuses(ctx context.Context, vendorID string, from []vendor.OperatorStatus, to vendor.OperatorStatus) error {
	if len(from) == 0 {
		return nil
	}
	args := []any{vendorID, string(to)}
	placeholders := make([]string, len(from))
	for i, s := range from {
		args = append(args, string(s))
		placeholders[i] = fmt.Sprintf("$%d", i+3)
	}
	_, err := r.pool.Exec(ctx, `
UPDATE vendor_operator_account
   SET status = $2, last_synced_at = now(), updated_at = now()
 WHERE vendor_id = $1 AND status IN (`+strings.Join(placeholders, ",")+`)`, args...)
	return err
}

func (r *OperatorRepo) one(ctx context.Context, where string, args ...any) (*vendor.OperatorAccount, error) {
	rows, err := r.list(ctx, where, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, vendor.ErrOperatorNotFound
	}
	return rows[0], nil
}

func (r *OperatorRepo) list(ctx context.Context, where string, args ...any) ([]*vendor.OperatorAccount, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, vendor_id, email, display_name, provider, external_subject, status,
       setup_url, last_synced_at, created_at, updated_at
  FROM vendor_operator_account `+where, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*vendor.OperatorAccount
	for rows.Next() {
		var op vendor.OperatorAccount
		var status string
		if err := rows.Scan(
			&op.ID, &op.VendorID, &op.Email, &op.DisplayName, &op.Provider,
			&op.ExternalSubject, &status, &op.SetupURL, &op.LastSyncedAt,
			&op.CreatedAt, &op.UpdatedAt,
		); err != nil {
			return nil, err
		}
		op.Status = vendor.OperatorStatus(status)
		out = append(out, &op)
	}
	return out, rows.Err()
}
