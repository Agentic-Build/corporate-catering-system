package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
)

type DisputeRepo struct{ pool *pgxpool.Pool }

func NewDisputeRepo(p *pgxpool.Pool) *DisputeRepo { return &DisputeRepo{pool: p} }

const disputeCols = `id, entry_id, order_id, opened_by, reason, status, resolution, resolved_by, resolved_at, refund_minor, created_at, updated_at`

func (r *DisputeRepo) Create(ctx context.Context, d *payroll.Dispute) error {
	status := d.Status
	if status == "" {
		status = payroll.DisputeStatusOpen
	}
	err := r.pool.QueryRow(ctx, `
INSERT INTO payroll_dispute (entry_id, order_id, opened_by, reason, status, resolution, refund_minor)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, created_at, updated_at`,
		d.EntryID, d.OrderID, d.OpenedBy, d.Reason, string(status), d.Resolution, d.RefundMinor,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create dispute: %w", err)
	}
	d.Status = status
	return nil
}

func (r *DisputeRepo) GetByID(ctx context.Context, id string) (*payroll.Dispute, error) {
	var d payroll.Dispute
	var status string
	err := r.pool.QueryRow(ctx, `SELECT `+disputeCols+` FROM payroll_dispute WHERE id=$1`, id).Scan(
		&d.ID, &d.EntryID, &d.OrderID, &d.OpenedBy, &d.Reason, &status,
		&d.Resolution, &d.ResolvedBy, &d.ResolvedAt, &d.RefundMinor,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, payroll.ErrDisputeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan dispute: %w", err)
	}
	d.Status = payroll.DisputeStatus(status)
	return &d, nil
}

func (r *DisputeRepo) UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, status payroll.DisputeStatus, resolvedBy *string, resolution string, refundMinor int64) error {
	if tx == nil {
		return errors.New("DisputeRepo.UpdateStatusTx requires a tx")
	}
	tag, err := tx.Exec(ctx, `
UPDATE payroll_dispute
   SET status=$2::payroll_dispute_status,
       resolved_by=$3,
       resolved_at=CASE WHEN $2::text IN ('resolved_refund','resolved_reject') THEN now() ELSE resolved_at END,
       resolution=$4,
       refund_minor=$5,
       updated_at=now()
 WHERE id=$1`, id, string(status), resolvedBy, resolution, refundMinor)
	if err != nil {
		return fmt.Errorf("update dispute status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return payroll.ErrDisputeNotFound
	}
	return nil
}

func (r *DisputeRepo) ListByStatus(ctx context.Context, statuses []payroll.DisputeStatus) ([]*payroll.Dispute, error) {
	args := []any{}
	where := ""
	if len(statuses) > 0 {
		ph := make([]string, len(statuses))
		for i, s := range statuses {
			args = append(args, string(s))
			ph[i] = fmt.Sprintf("$%d::payroll_dispute_status", i+1)
		}
		where = "WHERE status IN (" + strings.Join(ph, ",") + ")"
	}
	q := `SELECT ` + disputeCols + ` FROM payroll_dispute ` + where + ` ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return r.collect(rows)
}

func (r *DisputeRepo) ListByUser(ctx context.Context, userID string) ([]*payroll.Dispute, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+disputeCols+` FROM payroll_dispute WHERE opened_by=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return r.collect(rows)
}

func (r *DisputeRepo) collect(rows pgx.Rows) ([]*payroll.Dispute, error) {
	defer rows.Close()
	var out []*payroll.Dispute
	for rows.Next() {
		var d payroll.Dispute
		var status string
		if err := rows.Scan(&d.ID, &d.EntryID, &d.OrderID, &d.OpenedBy, &d.Reason, &status,
			&d.Resolution, &d.ResolvedBy, &d.ResolvedAt, &d.RefundMinor,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Status = payroll.DisputeStatus(status)
		out = append(out, &d)
	}
	return out, rows.Err()
}
