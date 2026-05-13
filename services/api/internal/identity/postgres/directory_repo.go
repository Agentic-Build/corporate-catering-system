package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

type DirectoryRepo struct{ pool *pgxpool.Pool }

func NewDirectoryRepo(p *pgxpool.Pool) *DirectoryRepo { return &DirectoryRepo{pool: p} }

func (r *DirectoryRepo) GetByEmail(ctx context.Context, email string) (*identity.EmployeeDirectoryEntry, error) {
	var e identity.EmployeeDirectoryEntry
	var status string
	err := r.pool.QueryRow(ctx, `
SELECT employee_id, primary_email, display_name, plant, department, status
FROM employee_directory
WHERE primary_email = $1`, email,
	).Scan(&e.EmployeeID, &e.PrimaryEmail, &e.DisplayName, &e.Plant, &e.Department, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrNotInDirectory
	}
	if err != nil {
		return nil, fmt.Errorf("employee_directory scan: %w", err)
	}
	e.Status = identity.Status(status)
	return &e, nil
}
