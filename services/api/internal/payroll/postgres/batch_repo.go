package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

type BatchRepo struct{ pool *pgxpool.Pool }

func NewBatchRepo(p *pgxpool.Pool) *BatchRepo { return &BatchRepo{pool: p} }

const batchCols = `id, period_start, period_end, status, locked_at, locked_by, exported_at, export_uri, created_at, updated_at`

func (r *BatchRepo) Create(ctx context.Context, b *payroll.Batch) error {
	status := b.Status
	if status == "" {
		status = payroll.BatchStatusDraft
	}
	err := r.pool.QueryRow(ctx, `
INSERT INTO payroll_batch (period_start, period_end, status)
VALUES ($1, $2, $3)
RETURNING id, created_at, updated_at`,
		b.PeriodStart, b.PeriodEnd, string(status),
	).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "payroll_batch_period_idx") {
			return payroll.ErrBatchPeriodExists
		}
		return fmt.Errorf("create batch: %w", err)
	}
	b.Status = status
	return nil
}

// CreateTx inserts a batch row using the caller's transaction so the batch and
// the entries derived from it commit (or roll back) atomically.
func (r *BatchRepo) CreateTx(ctx context.Context, tx pgx.Tx, b *payroll.Batch) error {
	status := b.Status
	if status == "" {
		status = payroll.BatchStatusDraft
	}
	err := tx.QueryRow(ctx, `
INSERT INTO payroll_batch (period_start, period_end, status)
VALUES ($1, $2, $3)
RETURNING id, created_at, updated_at`,
		b.PeriodStart, b.PeriodEnd, string(status),
	).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "payroll_batch_period_idx") {
			return payroll.ErrBatchPeriodExists
		}
		return fmt.Errorf("create batch tx: %w", err)
	}
	b.Status = status
	return nil
}

func (r *BatchRepo) GetByID(ctx context.Context, id string) (*payroll.Batch, error) {
	return r.scanOne(ctx, `WHERE id=$1`, id)
}

func (r *BatchRepo) GetByPeriod(ctx context.Context, start, end time.Time) (*payroll.Batch, error) {
	return r.scanOne(ctx, `WHERE period_start=$1 AND period_end=$2`, start, end)
}

func (r *BatchRepo) scanOne(ctx context.Context, where string, args ...any) (*payroll.Batch, error) {
	var b payroll.Batch
	var status string
	q := `SELECT ` + batchCols + ` FROM payroll_batch ` + where
	err := r.pool.QueryRow(ctx, q, args...).Scan(
		&b.ID, &b.PeriodStart, &b.PeriodEnd, &status,
		&b.LockedAt, &b.LockedBy, &b.ExportedAt, &b.ExportURI,
		&b.CreatedAt, &b.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, payroll.ErrBatchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan batch: %w", err)
	}
	b.Status = payroll.BatchStatus(status)
	return &b, nil
}

func (r *BatchRepo) UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to payroll.BatchStatus, lockedBy *string) error {
	var tag pgconn.CommandTag
	var err error
	if to == payroll.BatchStatusLocked {
		tag, err = tx.Exec(ctx, `
UPDATE payroll_batch
   SET status=$3::payroll_batch_status, locked_at=now(), locked_by=$4, updated_at=now()
 WHERE id=$1 AND status=$2::payroll_batch_status`, id, string(from), string(to), lockedBy)
	} else {
		tag, err = tx.Exec(ctx, `
UPDATE payroll_batch
   SET status=$3::payroll_batch_status, updated_at=now()
 WHERE id=$1 AND status=$2::payroll_batch_status`, id, string(from), string(to))
	}
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return payroll.ErrInvalidTransition
	}
	return nil
}

func (r *BatchRepo) SetExportInfoTx(ctx context.Context, tx pgx.Tx, id, uri string, exportedAt time.Time) error {
	_, err := tx.Exec(ctx, `
UPDATE payroll_batch
   SET status='exported', exported_at=$2, export_uri=$3, updated_at=now()
 WHERE id=$1`, id, exportedAt, uri)
	if err != nil {
		return fmt.Errorf("set export info: %w", err)
	}
	return nil
}

func (r *BatchRepo) List(ctx context.Context, statuses []payroll.BatchStatus) ([]*payroll.Batch, error) {
	args := []any{}
	where := ""
	if len(statuses) > 0 {
		ph := make([]string, len(statuses))
		for i, s := range statuses {
			args = append(args, string(s))
			ph[i] = fmt.Sprintf("$%d::payroll_batch_status", i+1)
		}
		where = "WHERE status IN (" + strings.Join(ph, ",") + ")"
	}
	q := `SELECT ` + batchCols + ` FROM payroll_batch ` + where + ` ORDER BY period_end DESC, period_start DESC`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*payroll.Batch
	for rows.Next() {
		var b payroll.Batch
		var status string
		if err := rows.Scan(&b.ID, &b.PeriodStart, &b.PeriodEnd, &status,
			&b.LockedAt, &b.LockedBy, &b.ExportedAt, &b.ExportURI,
			&b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		b.Status = payroll.BatchStatus(status)
		out = append(out, &b)
	}
	return out, rows.Err()
}
