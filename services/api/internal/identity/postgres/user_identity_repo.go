package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

type UserIdentityRepo struct{ pool *pgxpool.Pool }

func NewUserIdentityRepo(p *pgxpool.Pool) *UserIdentityRepo { return &UserIdentityRepo{pool: p} }

func (r *UserIdentityRepo) Link(ctx context.Context, ui *identity.UserIdentity) error {
	claims := ui.RawClaims
	if claims == nil {
		claims = map[string]any{}
	}
	b, err := json.Marshal(claims)
	if err != nil {
		return fmt.Errorf("marshal raw_claims: %w", err)
	}
	return r.pool.QueryRow(ctx, `
INSERT INTO user_identity (user_id, provider, external_subject, raw_claims)
VALUES ($1, $2, $3, $4)
RETURNING id, linked_at`,
		ui.UserID, string(ui.Provider), ui.ExternalSubject, b,
	).Scan(&ui.ID, &ui.LinkedAt)
}

func (r *UserIdentityRepo) GetByProviderSubject(ctx context.Context, p identity.Provider, sub string) (*identity.UserIdentity, error) {
	ui, claims, err := r.scanOne(ctx,
		`WHERE provider = $1 AND external_subject = $2`,
		string(p), sub,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.ErrIdentityNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user_identity scan: %w", err)
	}
	if err := json.Unmarshal(claims, &ui.RawClaims); err != nil {
		return nil, fmt.Errorf("unmarshal raw_claims: %w", err)
	}
	return ui, nil
}

func (r *UserIdentityRepo) ListByUser(ctx context.Context, userID string) ([]*identity.UserIdentity, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, user_id, provider, external_subject, raw_claims, linked_at
FROM user_identity
WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("user_identity list: %w", err)
	}
	defer rows.Close()
	var out []*identity.UserIdentity
	for rows.Next() {
		var ui identity.UserIdentity
		var provider string
		var claims []byte
		if err := rows.Scan(&ui.ID, &ui.UserID, &provider, &ui.ExternalSubject, &claims, &ui.LinkedAt); err != nil {
			return nil, fmt.Errorf("user_identity row: %w", err)
		}
		ui.Provider = identity.Provider(provider)
		if err := json.Unmarshal(claims, &ui.RawClaims); err != nil {
			return nil, fmt.Errorf("unmarshal raw_claims: %w", err)
		}
		out = append(out, &ui)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user_identity rows: %w", err)
	}
	return out, nil
}

func (r *UserIdentityRepo) scanOne(ctx context.Context, where string, args ...any) (*identity.UserIdentity, []byte, error) {
	var ui identity.UserIdentity
	var provider string
	var claims []byte
	q := `SELECT id, user_id, provider, external_subject, raw_claims, linked_at FROM user_identity ` + where
	err := r.pool.QueryRow(ctx, q, args...).Scan(
		&ui.ID, &ui.UserID, &provider, &ui.ExternalSubject, &claims, &ui.LinkedAt,
	)
	if err != nil {
		return nil, nil, err
	}
	ui.Provider = identity.Provider(provider)
	return &ui, claims, nil
}
