package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

type ImageRepo struct{ pool *pgxpool.Pool }

func NewImageRepo(p *pgxpool.Pool) *ImageRepo { return &ImageRepo{pool: p} }

func (r *ImageRepo) Add(ctx context.Context, img *menu.Image) error {
	return r.pool.QueryRow(ctx, `
INSERT INTO menu_item_image (menu_item_id, blob_uri, alt, sort_order)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at`,
		img.ItemID, img.BlobURI, img.Alt, img.SortOrder).
		Scan(&img.ID, &img.CreatedAt)
}

func (r *ImageRepo) Remove(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM menu_item_image WHERE id=$1`, id)
	return err
}

func (r *ImageRepo) ListByItem(ctx context.Context, itemID string) ([]*menu.Image, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, menu_item_id, blob_uri, alt, sort_order, created_at
  FROM menu_item_image WHERE menu_item_id=$1 ORDER BY sort_order, created_at`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*menu.Image
	for rows.Next() {
		var img menu.Image
		if err := rows.Scan(&img.ID, &img.ItemID, &img.BlobURI, &img.Alt, &img.SortOrder, &img.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &img)
	}
	return out, rows.Err()
}
