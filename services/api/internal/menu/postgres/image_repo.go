package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
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

// ReplaceForItem swaps the item's image set atomically: it deletes existing
// rows and inserts uris in order (sort_order = index). A nil/empty slice
// leaves the item with no images.
func (r *ImageRepo) ReplaceForItem(ctx context.Context, itemID string, uris []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit
	if _, err := tx.Exec(ctx, `DELETE FROM menu_item_image WHERE menu_item_id=$1`, itemID); err != nil {
		return err
	}
	for i, uri := range uris {
		if _, err := tx.Exec(ctx, `
INSERT INTO menu_item_image (menu_item_id, blob_uri, sort_order)
VALUES ($1, $2, $3)`, itemID, uri, i); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
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

// ListByItems loads images for many items in a single query, grouped by
// menu_item_id (menu.BatchImageRepository). Avoids the N+1 in
// menu.Service.ListForEmployee. Items with no images are simply absent.
func (r *ImageRepo) ListByItems(ctx context.Context, itemIDs []string) (map[string][]*menu.Image, error) {
	out := make(map[string][]*menu.Image, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT id, menu_item_id, blob_uri, alt, sort_order, created_at
  FROM menu_item_image WHERE menu_item_id = ANY($1) ORDER BY menu_item_id, sort_order, created_at`, itemIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var img menu.Image
		if err := rows.Scan(&img.ID, &img.ItemID, &img.BlobURI, &img.Alt, &img.SortOrder, &img.CreatedAt); err != nil {
			return nil, err
		}
		out[img.ItemID] = append(out[img.ItemID], &img)
	}
	return out, rows.Err()
}
