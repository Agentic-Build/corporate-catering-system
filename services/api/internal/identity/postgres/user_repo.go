package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

type UserRepo struct{ pool *pgxpool.Pool }

func NewUserRepo(p *pgxpool.Pool) *UserRepo { return &UserRepo{pool: p} }

func (r *UserRepo) Create(ctx context.Context, u *identity.User) error {
	return r.pool.QueryRow(ctx, `
INSERT INTO "user"
  (primary_email, display_name, role, status, employee_id, vendor_id, plant, department)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, created_at, updated_at`,
		u.PrimaryEmail, u.DisplayName, string(u.Role), string(u.Status),
		u.EmployeeID, u.VendorID, u.Plant, u.Department,
	).Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt)
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	return r.scanOne(ctx, `WHERE primary_email = $1`, email)
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*identity.User, error) {
	return r.scanOne(ctx, `WHERE id = $1`, id)
}

func (r *UserRepo) scanOne(ctx context.Context, where string, args ...any) (*identity.User, error) {
	var u identity.User
	var role, status string
	q := `SELECT id, primary_email, display_name, role, status, employee_id, vendor_id, plant, department, created_at, updated_at FROM "user" ` + where
	err := r.pool.QueryRow(ctx, q, args...).Scan(
		&u.ID, &u.PrimaryEmail, &u.DisplayName, &role, &status,
		&u.EmployeeID, &u.VendorID, &u.Plant, &u.Department, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user scan: %w", err)
	}
	u.Role = identity.Role(role)
	u.Status = identity.Status(status)
	return &u, nil
}

func (r *UserRepo) UpdateStatus(ctx context.Context, id string, status identity.Status) error {
	_, err := r.pool.Exec(ctx, `UPDATE "user" SET status=$2, updated_at=now() WHERE id=$1`, id, string(status))
	return err
}

func (r *UserRepo) UpdateProfile(ctx context.Context, u *identity.User) error {
	return r.pool.QueryRow(ctx, `
UPDATE "user"
SET primary_email=$2,
    display_name=$3,
    role=$4,
    status=$5,
    employee_id=$6,
    vendor_id=$7,
    plant=$8,
    department=$9,
    updated_at=now()
WHERE id=$1
RETURNING created_at, updated_at`,
		u.ID, u.PrimaryEmail, u.DisplayName, string(u.Role), string(u.Status),
		u.EmployeeID, u.VendorID, u.Plant, u.Department,
	).Scan(&u.CreatedAt, &u.UpdatedAt)
}
