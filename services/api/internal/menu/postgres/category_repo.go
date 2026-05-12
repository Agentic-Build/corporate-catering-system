package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

type CategoryRepo struct{ pool *pgxpool.Pool }

func NewCategoryRepo(p *pgxpool.Pool) *CategoryRepo { return &CategoryRepo{pool: p} }

func (r *CategoryRepo) Create(ctx context.Context, c *menu.Category) error {
	return r.pool.QueryRow(ctx, `
INSERT INTO menu_category (vendor_id, name, sort_order)
VALUES ($1, $2, $3)
RETURNING id, created_at`,
		c.VendorID, c.Name, c.SortOrder).
		Scan(&c.ID, &c.CreatedAt)
}

func (r *CategoryRepo) Update(ctx context.Context, c *menu.Category) error {
	_, err := r.pool.Exec(ctx, `
UPDATE menu_category SET name=$2, sort_order=$3 WHERE id=$1`, c.ID, c.Name, c.SortOrder)
	return err
}

func (r *CategoryRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM menu_category WHERE id=$1`, id)
	return err
}

func (r *CategoryRepo) GetByID(ctx context.Context, id string) (*menu.Category, error) {
	var c menu.Category
	err := r.pool.QueryRow(ctx, `
SELECT id, vendor_id, name, sort_order, created_at
  FROM menu_category WHERE id=$1`, id).
		Scan(&c.ID, &c.VendorID, &c.Name, &c.SortOrder, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, menu.ErrCategoryNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("category scan: %w", err)
	}
	return &c, nil
}

func (r *CategoryRepo) ListByVendor(ctx context.Context, vendorID string) ([]*menu.Category, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, vendor_id, name, sort_order, created_at
  FROM menu_category WHERE vendor_id=$1 ORDER BY sort_order, name`, vendorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*menu.Category
	for rows.Next() {
		var c menu.Category
		if err := rows.Scan(&c.ID, &c.VendorID, &c.Name, &c.SortOrder, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}
