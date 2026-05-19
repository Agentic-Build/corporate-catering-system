package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
)

// CurrentLinesRepo loads the per-order lines for an employee's in-progress
// (not-yet-locked) payroll period. Read-only; kept in its own file so the
// per-order detail query stays decoupled from the batch/entry settler code.
// It delegates to payroll.QueryCurrentLines so the production query lives in
// exactly one place.
type CurrentLinesRepo struct{ pool *pgxpool.Pool }

func NewCurrentLinesRepo(p *pgxpool.Pool) *CurrentLinesRepo { return &CurrentLinesRepo{pool: p} }

// ListCurrentLines returns one line per chargeable order belonging to userID
// in the in-progress payroll period.
func (r *CurrentLinesRepo) ListCurrentLines(ctx context.Context, userID string) ([]payroll.CurrentPayrollLine, error) {
	return payroll.QueryCurrentLines(ctx, r.pool, userID)
}
