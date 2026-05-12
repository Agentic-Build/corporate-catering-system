package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

type VendorInviteRepo struct{ pool *pgxpool.Pool }

func NewVendorInviteRepo(p *pgxpool.Pool) *VendorInviteRepo { return &VendorInviteRepo{pool: p} }

func (r *VendorInviteRepo) Get(ctx context.Context, code string) (*identity.VendorInvite, error) {
	var inv identity.VendorInvite
	err := r.pool.QueryRow(ctx, `
SELECT code, vendor_id, email_hint, expires_at, consumed_at, consumed_by
FROM vendor_invite
WHERE code = $1`, code,
	).Scan(&inv.Code, &inv.VendorID, &inv.EmailHint, &inv.ExpiresAt, &inv.ConsumedAt, &inv.ConsumedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrInviteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("vendor_invite scan: %w", err)
	}
	return &inv, nil
}

func (r *VendorInviteRepo) Put(ctx context.Context, inv *identity.VendorInvite) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO vendor_invite (code, vendor_id, email_hint, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (code) DO NOTHING`,
		inv.Code, inv.VendorID, inv.EmailHint, inv.ExpiresAt)
	if err != nil {
		return fmt.Errorf("vendor_invite put: %w", err)
	}
	return nil
}

func (r *VendorInviteRepo) Consume(ctx context.Context, code string, userID string) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vendor_invite
SET consumed_at = now(), consumed_by = $2
WHERE code = $1 AND consumed_at IS NULL`, code, userID)
	if err != nil {
		return fmt.Errorf("vendor_invite consume: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrInviteAlreadyUsed
	}
	return nil
}
