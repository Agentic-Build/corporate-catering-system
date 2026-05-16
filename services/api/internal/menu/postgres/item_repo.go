package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

type ItemRepo struct{ pool *pgxpool.Pool }

func NewItemRepo(p *pgxpool.Pool) *ItemRepo { return &ItemRepo{pool: p} }

func (r *ItemRepo) Create(ctx context.Context, i *menu.Item) error {
	if i.Tags == nil {
		i.Tags = []string{}
	}
	if i.Badges == nil {
		i.Badges = []string{}
	}
	if i.Status == "" {
		i.Status = menu.ItemStatusDraft
	}
	return r.pool.QueryRow(ctx, `
INSERT INTO menu_item (vendor_id, category_id, name, description, price_minor, tags, badges, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, created_at, updated_at`,
		i.VendorID, i.CategoryID, i.Name, i.Description, i.PriceMinor, i.Tags, i.Badges, string(i.Status),
	).Scan(&i.ID, &i.CreatedAt, &i.UpdatedAt)
}

func (r *ItemRepo) Update(ctx context.Context, i *menu.Item) error {
	if i.Tags == nil {
		i.Tags = []string{}
	}
	if i.Badges == nil {
		i.Badges = []string{}
	}
	_, err := r.pool.Exec(ctx, `
UPDATE menu_item
   SET category_id=$2, name=$3, description=$4, price_minor=$5, tags=$6, badges=$7, updated_at=now()
 WHERE id=$1`,
		i.ID, i.CategoryID, i.Name, i.Description, i.PriceMinor, i.Tags, i.Badges)
	return err
}

func (r *ItemRepo) SetStatus(ctx context.Context, id string, status menu.ItemStatus) error {
	if status == menu.ItemStatusArchived {
		_, err := r.pool.Exec(ctx, `
UPDATE menu_item SET status=$2, archived_at=now(), updated_at=now() WHERE id=$1`,
			id, string(status))
		return err
	}
	_, err := r.pool.Exec(ctx, `
UPDATE menu_item SET status=$2, archived_at=NULL, updated_at=now() WHERE id=$1`,
		id, string(status))
	return err
}

func (r *ItemRepo) GetByID(ctx context.Context, id string) (*menu.Item, error) {
	var i menu.Item
	var status string
	err := r.pool.QueryRow(ctx, `
SELECT id, vendor_id, category_id, name, description, price_minor, tags, badges, status, archived_at, created_at, updated_at
  FROM menu_item WHERE id=$1`, id).
		Scan(&i.ID, &i.VendorID, &i.CategoryID, &i.Name, &i.Description, &i.PriceMinor,
			&i.Tags, &i.Badges, &status, &i.ArchivedAt, &i.CreatedAt, &i.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, menu.ErrItemNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("item scan: %w", err)
	}
	i.Status = menu.ItemStatus(status)
	return &i, nil
}

// ListByVendor returns the vendor's items with read-only usage stats for the
// merchant meal-library view. last_used is the most recent meal_supply.supply_date
// (NULL if never scheduled); total_sold is the cumulative order_item.qty over
// orders in status 'picked_up' (0 if none). Both are computed set-based via
// correlated aggregate subqueries in a single round trip — no per-item query.
func (r *ItemRepo) ListByVendor(ctx context.Context, vendorID string, includeArchived bool) ([]*menu.MerchantItemRow, error) {
	where := "WHERE mi.vendor_id=$1"
	if !includeArchived {
		where += " AND mi.status != 'archived'"
	}
	q := `
SELECT mi.id, mi.vendor_id, mi.category_id, mi.name, mi.description, mi.price_minor,
       mi.tags, mi.badges, mi.status, mi.archived_at, mi.created_at, mi.updated_at,
       (SELECT max(ms.supply_date) FROM meal_supply ms
         WHERE ms.menu_item_id = mi.id) AS last_used,
       COALESCE((SELECT sum(oi.qty) FROM order_item oi
         JOIN "order" o ON o.id = oi.order_id
        WHERE oi.menu_item_id = mi.id AND o.status = 'picked_up'), 0) AS total_sold
  FROM menu_item mi ` + where + ` ORDER BY mi.created_at`
	rows, err := r.pool.Query(ctx, q, vendorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*menu.MerchantItemRow
	for rows.Next() {
		var row menu.MerchantItemRow
		var status string
		if err := rows.Scan(&row.Item.ID, &row.Item.VendorID, &row.Item.CategoryID, &row.Item.Name,
			&row.Item.Description, &row.Item.PriceMinor, &row.Item.Tags, &row.Item.Badges, &status,
			&row.Item.ArchivedAt, &row.Item.CreatedAt, &row.Item.UpdatedAt,
			&row.LastUsed, &row.TotalSold); err != nil {
			return nil, err
		}
		row.Item.Status = menu.ItemStatus(status)
		out = append(out, &row)
	}
	return out, rows.Err()
}

func (r *ItemRepo) ListActiveByPlant(ctx context.Context, plant string, day time.Time) ([]*menu.ActiveItemRow, error) {
	rows, err := r.pool.Query(ctx, `
SELECT
    mi.id, mi.vendor_id, mi.category_id, mi.name, mi.description, mi.price_minor,
    mi.tags, mi.badges, mi.status, mi.archived_at, mi.created_at, mi.updated_at,
    v.display_name,
    ms.supply_date, ms.capacity, ms.remain, ms.pickup_window, ms.eta_label, ms.cutoff_at
FROM menu_item mi
JOIN vendor v ON v.id = mi.vendor_id AND v.status = 'approved'
JOIN vendor_plant_mapping vpm ON vpm.vendor_id = v.id AND vpm.plant = $1 AND vpm.active = true
JOIN meal_supply ms ON ms.menu_item_id = mi.id AND ms.supply_date = $2
WHERE mi.status = 'active'
ORDER BY v.display_name, mi.name`, plant, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*menu.ActiveItemRow
	for rows.Next() {
		var row menu.ActiveItemRow
		var status string
		if err := rows.Scan(
			&row.Item.ID, &row.Item.VendorID, &row.Item.CategoryID, &row.Item.Name, &row.Item.Description, &row.Item.PriceMinor,
			&row.Item.Tags, &row.Item.Badges, &status, &row.Item.ArchivedAt, &row.Item.CreatedAt, &row.Item.UpdatedAt,
			&row.VendorName,
			&row.SupplyDate, &row.Capacity, &row.Remain, &row.PickupWindow, &row.ETALabel, &row.CutoffAt,
		); err != nil {
			return nil, err
		}
		row.Item.Status = menu.ItemStatus(status)
		out = append(out, &row)
	}
	return out, rows.Err()
}
