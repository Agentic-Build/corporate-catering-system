package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminWhitelistRepo struct{ pool *pgxpool.Pool }

func NewAdminWhitelistRepo(p *pgxpool.Pool) *AdminWhitelistRepo {
	return &AdminWhitelistRepo{pool: p}
}

func (r *AdminWhitelistRepo) IsAllowed(ctx context.Context, email string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM admin_email_whitelist WHERE email = $1)`, email,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("admin_email_whitelist scan: %w", err)
	}
	return exists, nil
}
