package settler

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgUserLookup resolves payroll user details directly from the "user" table.
type PgUserLookup struct{ pool *pgxpool.Pool }

func NewPgUserLookup(p *pgxpool.Pool) *PgUserLookup { return &PgUserLookup{pool: p} }

func (l *PgUserLookup) GetByID(ctx context.Context, id string) (*PayrollUser, error) {
	var u PayrollUser
	err := l.pool.QueryRow(ctx, `
SELECT id, employee_id, primary_email, display_name, plant, department
  FROM "user" WHERE id = $1`, id).Scan(
		&u.ID, &u.EmployeeID, &u.PrimaryEmail, &u.DisplayName, &u.Plant, &u.Department,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
