package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

type ExceptionRepo struct{ pool *pgxpool.Pool }

func NewExceptionRepo(p *pgxpool.Pool) *ExceptionRepo { return &ExceptionRepo{pool: p} }

const exceptionCols = `id, batch_id, entry_id, user_id, kind, status, detail, resolution, resolved_by, resolved_at, created_at, updated_at`

// upsertDepartedSQL inserts one employee_departed exception per batch entry
// whose employee is no longer active. ON CONFLICT DO NOTHING (against the
// (batch_id, entry_id, kind) unique index) makes re-detection idempotent.
const upsertDepartedSQL = `
INSERT INTO payroll_exception (batch_id, entry_id, user_id, kind, detail)
SELECT e.batch_id, e.id, e.user_id, 'employee_departed', 'employee status: ' || u.status
  FROM payroll_entry e
  JOIN "user" u ON u.id = e.user_id
 WHERE e.batch_id = $1 AND u.status <> 'active'
ON CONFLICT (batch_id, entry_id, kind) DO NOTHING`

func (r *ExceptionRepo) UpsertDepartedTx(ctx context.Context, tx pgx.Tx, batchID string) error {
	if _, err := tx.Exec(ctx, upsertDepartedSQL, batchID); err != nil {
		return fmt.Errorf("upsert departed exceptions: %w", err)
	}
	return nil
}

func (r *ExceptionRepo) UpsertDeparted(ctx context.Context, batchID string) error {
	if _, err := r.pool.Exec(ctx, upsertDepartedSQL, batchID); err != nil {
		return fmt.Errorf("upsert departed exceptions: %w", err)
	}
	return nil
}

func (r *ExceptionRepo) Create(ctx context.Context, e *payroll.Exception) error {
	status := e.Status
	if status == "" {
		status = payroll.ExceptionOpen
	}
	err := r.pool.QueryRow(ctx, `
INSERT INTO payroll_exception (batch_id, entry_id, user_id, kind, status, detail)
VALUES ($1,$2,$3,$4::payroll_exception_kind,$5::payroll_exception_status,$6)
RETURNING id, created_at, updated_at`,
		e.BatchID, e.EntryID, e.UserID, string(e.Kind), string(status), e.Detail,
	).Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create exception: %w", err)
	}
	e.Status = status
	return nil
}

func scanException(row pgx.Row, e *payroll.Exception) error {
	var kind, status string
	if err := row.Scan(&e.ID, &e.BatchID, &e.EntryID, &e.UserID, &kind, &status,
		&e.Detail, &e.Resolution, &e.ResolvedBy, &e.ResolvedAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return err
	}
	e.Kind = payroll.ExceptionKind(kind)
	e.Status = payroll.ExceptionStatus(status)
	return nil
}

func (r *ExceptionRepo) GetByID(ctx context.Context, id string) (*payroll.Exception, error) {
	var e payroll.Exception
	err := scanException(r.pool.QueryRow(ctx,
		`SELECT `+exceptionCols+` FROM payroll_exception WHERE id=$1`, id), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, payroll.ErrExceptionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get exception: %w", err)
	}
	return &e, nil
}

func (r *ExceptionRepo) ListByBatch(ctx context.Context, batchID string) ([]*payroll.Exception, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+exceptionCols+` FROM payroll_exception WHERE batch_id=$1 ORDER BY created_at`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*payroll.Exception
	for rows.Next() {
		var e payroll.Exception
		if err := scanException(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (r *ExceptionRepo) Resolve(ctx context.Context, id string, status payroll.ExceptionStatus, resolution, resolvedBy string) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE payroll_exception
   SET status=$2::payroll_exception_status, resolution=$3, resolved_by=$4, resolved_at=now(), updated_at=now()
 WHERE id=$1`, id, string(status), resolution, resolvedBy)
	if err != nil {
		return fmt.Errorf("resolve exception: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return payroll.ErrExceptionNotFound
	}
	return nil
}
