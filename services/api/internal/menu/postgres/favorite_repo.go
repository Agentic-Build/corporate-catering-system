package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
)

// FavoriteRepo persists employee favorite_item rows and reads them back as
// chip-sized projections that include a target-day availability flag.
type FavoriteRepo struct{ pool *pgxpool.Pool }

func NewFavoriteRepo(p *pgxpool.Pool) *FavoriteRepo { return &FavoriteRepo{pool: p} }

// Add inserts a favorite. It is idempotent: a duplicate (user, item) is a no-op.
func (r *FavoriteRepo) Add(ctx context.Context, userID, menuItemID string) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO favorite_item (user_id, menu_item_id)
VALUES ($1, $2)
ON CONFLICT (user_id, menu_item_id) DO NOTHING`, userID, menuItemID)
	return err
}

// Remove deletes a favorite. Missing rows are not an error (idempotent delete).
func (r *FavoriteRepo) Remove(ctx context.Context, userID, menuItemID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM favorite_item WHERE user_id=$1 AND menu_item_id=$2`,
		userID, menuItemID)
	return err
}

// ListByUser returns the user's favorites newest-first, each carrying an
// available_today flag derived from meal_supply + the vendor-plant mapping.
// Archived menu items are excluded. limit is clamped to [1, 50].
// When cursor != nil, only rows with created_at < *cursor are returned.
// next_cursor is the created_at of the last returned row when the page is
// full (i.e. there may be more rows); nil otherwise.
func (r *FavoriteRepo) ListByUser(
	ctx context.Context,
	userID, targetDay, plant string,
	limit int,
	cursor *time.Time,
) ([]menu.FavoriteChip, *time.Time, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 50 {
		limit = 50
	}

	const q = `
SELECT
    mi.id, mi.name, mi.price_minor, mi.vendor_id, fi.created_at,
    EXISTS (
        SELECT 1
          FROM meal_supply ms
          JOIN vendor v ON v.id = mi.vendor_id AND v.status = 'approved'
          JOIN vendor_plant_mapping vpm
            ON vpm.vendor_id = v.id AND vpm.plant = $3 AND vpm.active = true
         WHERE ms.menu_item_id = mi.id AND ms.supply_date = $2::date
    ) AS available_today
  FROM favorite_item fi
  JOIN menu_item mi ON mi.id = fi.menu_item_id
 WHERE fi.user_id = $1
   AND mi.archived_at IS NULL
   AND ($4::timestamptz IS NULL OR fi.created_at < $4::timestamptz)
 ORDER BY fi.created_at DESC, mi.id
 LIMIT $5`

	rows, err := r.pool.Query(ctx, q, userID, targetDay, plant, cursor, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := make([]menu.FavoriteChip, 0, limit)
	for rows.Next() {
		var c menu.FavoriteChip
		if err := rows.Scan(&c.MenuItemID, &c.Name, &c.UnitPrice, &c.VendorID,
			&c.CreatedAt, &c.AvailableToday); err != nil {
			return nil, nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var next *time.Time
	if len(out) == limit {
		t := out[len(out)-1].CreatedAt
		next = &t
	}
	return out, next, nil
}
