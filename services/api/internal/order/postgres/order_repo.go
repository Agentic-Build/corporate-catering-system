package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

type OrderRepo struct{ pool *pgxpool.Pool }

func NewOrderRepo(p *pgxpool.Pool) *OrderRepo { return &OrderRepo{pool: p} }

// CreateTx inserts the order + items inside the provided transaction. Service
// callers wrap this in a larger tx that also touches quota, state events, and
// outbox; pass nil tx via Create() if a standalone insert is desired.
func (r *OrderRepo) CreateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error {
	if tx == nil {
		return errors.New("OrderRepo.CreateTx requires a tx")
	}
	err := tx.QueryRow(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING id, created_at, updated_at`,
		o.UserID, o.VendorID, o.Plant, o.SupplyDate, string(o.Status),
		o.TotalPriceMinor, o.PlacedAt, o.CutoffAt,
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert order: %w", err)
	}
	for i := range o.Items {
		item := &o.Items[i]
		err := tx.QueryRow(ctx, `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1,$2,$3,$4) RETURNING id`,
			o.ID, item.MenuItemID, item.Qty, item.UnitPriceMinor,
		).Scan(&item.ID)
		if err != nil {
			return fmt.Errorf("insert order_item: %w", err)
		}
		item.OrderID = o.ID
	}
	return nil
}

func (r *OrderRepo) Create(ctx context.Context, o *order.Order) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return r.CreateTx(ctx, tx, o)
	})
}

func (r *OrderRepo) GetByID(ctx context.Context, id string) (*order.Order, error) {
	var o order.Order
	var status string
	err := r.pool.QueryRow(ctx, `
SELECT id, user_id, vendor_id, plant, supply_date, status, total_price_minor,
       placed_at, cutoff_at, cancelled_at, created_at, updated_at
  FROM "order" WHERE id=$1`, id).Scan(
		&o.ID, &o.UserID, &o.VendorID, &o.Plant, &o.SupplyDate, &status, &o.TotalPriceMinor,
		&o.PlacedAt, &o.CutoffAt, &o.CancelledAt, &o.CreatedAt, &o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, order.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}
	o.Status = order.Status(status)

	rows, err := r.pool.Query(ctx, `
SELECT id, order_id, menu_item_id, qty, unit_price_minor FROM order_item WHERE order_id=$1`, id)
	if err != nil {
		return nil, fmt.Errorf("get items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it order.Item
		if err := rows.Scan(&it.ID, &it.OrderID, &it.MenuItemID, &it.Qty, &it.UnitPriceMinor); err != nil {
			return nil, err
		}
		o.Items = append(o.Items, it)
	}
	return &o, rows.Err()
}

// UpdateStatusTx performs a conditional UPDATE inside the given transaction.
// Returns ErrInvalidTransition if 0 rows updated (row missing or status != from).
func (r *OrderRepo) UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to order.Status) error {
	tag, err := tx.Exec(ctx, `
UPDATE "order"
   SET status=$3::order_status,
       cancelled_at = CASE WHEN $3::text = 'cancelled' THEN now() ELSE cancelled_at END,
       updated_at = now()
 WHERE id=$1 AND status=$2::order_status`, id, string(from), string(to))
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return order.ErrInvalidTransition
	}
	return nil
}

// UpdateStatus is the Repository-interface variant. Service callers that need
// transactional orchestration use UpdateStatusTx directly.
func (r *OrderRepo) UpdateStatus(ctx context.Context, id string, from, to order.Status, _actorID *string, _actorRole *string, _reason string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return r.UpdateStatusTx(ctx, tx, id, from, to)
	})
}

func (r *OrderRepo) ListByUser(ctx context.Context, userID string, sinceDate time.Time) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, user_id, vendor_id, plant, supply_date, status, total_price_minor,
       placed_at, cutoff_at, cancelled_at, created_at, updated_at
  FROM "order"
 WHERE user_id=$1 AND supply_date >= $2
 ORDER BY supply_date DESC, created_at DESC`, userID, sinceDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*order.Order
	for rows.Next() {
		var o order.Order
		var status string
		if err := rows.Scan(&o.ID, &o.UserID, &o.VendorID, &o.Plant, &o.SupplyDate, &status,
			&o.TotalPriceMinor, &o.PlacedAt, &o.CutoffAt, &o.CancelledAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		o.Status = order.Status(status)
		out = append(out, &o)
	}
	return out, rows.Err()
}

func (r *OrderRepo) ListPlacedDueForCutoff(ctx context.Context, before time.Time) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, user_id, vendor_id, plant, supply_date, status, total_price_minor,
       placed_at, cutoff_at, cancelled_at, created_at, updated_at
  FROM "order"
 WHERE status='placed' AND cutoff_at <= $1
 ORDER BY cutoff_at`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*order.Order
	for rows.Next() {
		var o order.Order
		var status string
		if err := rows.Scan(&o.ID, &o.UserID, &o.VendorID, &o.Plant, &o.SupplyDate, &status,
			&o.TotalPriceMinor, &o.PlacedAt, &o.CutoffAt, &o.CancelledAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		o.Status = order.Status(status)
		out = append(out, &o)
	}
	return out, rows.Err()
}
