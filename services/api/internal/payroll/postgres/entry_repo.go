package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
)

type EntryRepo struct{ pool *pgxpool.Pool }

func NewEntryRepo(p *pgxpool.Pool) *EntryRepo { return &EntryRepo{pool: p} }

const entryCols = `id, batch_id, user_id, order_ids, amount_minor, refunded_minor, created_at`

func (r *EntryRepo) CreateTx(ctx context.Context, tx pgx.Tx, e *payroll.Entry) error {
	if tx == nil {
		return errors.New("EntryRepo.CreateTx requires a tx")
	}
	err := tx.QueryRow(ctx, `
INSERT INTO payroll_entry (batch_id, user_id, order_ids, amount_minor, refunded_minor)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at`,
		e.BatchID, e.UserID, e.OrderIDs, e.AmountMinor, e.RefundedMinor,
	).Scan(&e.ID, &e.CreatedAt)
	if err != nil {
		return fmt.Errorf("create entry: %w", err)
	}
	return nil
}

func (r *EntryRepo) GetByID(ctx context.Context, id string) (*payroll.Entry, error) {
	var e payroll.Entry
	err := r.pool.QueryRow(ctx, `SELECT `+entryCols+` FROM payroll_entry WHERE id=$1`, id).Scan(
		&e.ID, &e.BatchID, &e.UserID, &e.OrderIDs, &e.AmountMinor, &e.RefundedMinor, &e.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, payroll.ErrEntryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan entry: %w", err)
	}
	return &e, nil
}

func (r *EntryRepo) ListByBatch(ctx context.Context, batchID string) ([]*payroll.Entry, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+entryCols+` FROM payroll_entry WHERE batch_id=$1 ORDER BY created_at, id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*payroll.Entry
	for rows.Next() {
		var e payroll.Entry
		if err := rows.Scan(&e.ID, &e.BatchID, &e.UserID, &e.OrderIDs, &e.AmountMinor, &e.RefundedMinor, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (r *EntryRepo) IncrementRefundedTx(ctx context.Context, tx pgx.Tx, id string, refund int64) error {
	if tx == nil {
		return errors.New("EntryRepo.IncrementRefundedTx requires a tx")
	}
	tag, err := tx.Exec(ctx, `
UPDATE payroll_entry
   SET refunded_minor = refunded_minor + $2
 WHERE id=$1`, id, refund)
	if err != nil {
		return fmt.Errorf("increment refunded: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return payroll.ErrEntryNotFound
	}
	return nil
}
