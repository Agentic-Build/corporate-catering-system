package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

type OrderRepo struct{ pool *pgxpool.Pool }

func NewOrderRepo(p *pgxpool.Pool) *OrderRepo { return &OrderRepo{pool: p} }

// orderSelectCols is the canonical SELECT projection for the "order" table.
// Keep the column order in lock-step with the Scan calls below.
const orderSelectCols = `id, user_id, vendor_id, plant, supply_date, status, total_price_minor,
       notes, totp_secret, placed_at, cutoff_at, ready_at, picked_up_at, no_show_at,
       cancelled_at, created_at, updated_at, order_number`

// scanOrder reads one row using the orderSelectCols projection.
func scanOrder(row pgx.Row, o *order.Order) error {
	var status string
	if err := row.Scan(
		&o.ID, &o.UserID, &o.VendorID, &o.Plant, &o.SupplyDate, &status, &o.TotalPriceMinor,
		&o.Notes, &o.TOTPSecret, &o.PlacedAt, &o.CutoffAt, &o.ReadyAt, &o.PickedUpAt, &o.NoShowAt,
		&o.CancelledAt, &o.CreatedAt, &o.UpdatedAt, &o.OrderNumber,
	); err != nil {
		return err
	}
	o.Status = order.Status(status)
	return nil
}

// CreateTx inserts the order + items inside the provided transaction.
// Use Create() for a standalone insert.
func (r *OrderRepo) CreateTx(ctx context.Context, tx pgx.Tx, o *order.Order) error {
	if tx == nil {
		return errors.New("OrderRepo.CreateTx requires a tx")
	}
	// totp_secret intentionally omitted (legacy TOTP pickup removed; NOT NULL
	// DEFAULT covers the unused column).
	err := tx.QueryRow(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, notes, placed_at, cutoff_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id, created_at, updated_at, order_number`,
		o.UserID, o.VendorID, o.Plant, o.SupplyDate, string(o.Status),
		o.TotalPriceMinor, o.Notes, o.PlacedAt, o.CutoffAt,
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt, &o.OrderNumber)
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
	err := scanOrder(r.pool.QueryRow(ctx,
		`SELECT `+orderSelectCols+` FROM "order" WHERE id=$1`, id), &o)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, order.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
SELECT oi.id, oi.order_id, oi.menu_item_id, COALESCE(mi.name, ''), oi.qty, oi.unit_price_minor
  FROM order_item oi
  LEFT JOIN menu_item mi ON mi.id = oi.menu_item_id
 WHERE oi.order_id=$1`, id)
	if err != nil {
		return nil, fmt.Errorf("get items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it order.Item
		if err := rows.Scan(&it.ID, &it.OrderID, &it.MenuItemID, &it.Name, &it.Qty, &it.UnitPriceMinor); err != nil {
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

// ReplaceItemsTx swaps the order's item rows for a new set and updates the
// stored total + notes. Callers must adjust quota separately.
func (r *OrderRepo) ReplaceItemsTx(ctx context.Context, tx pgx.Tx, orderID string, items []order.Item, totalMinor int64, notes string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM order_item WHERE order_id=$1`, orderID); err != nil {
		return fmt.Errorf("delete order_items: %w", err)
	}
	for i := range items {
		it := &items[i]
		err := tx.QueryRow(ctx, `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1,$2,$3,$4) RETURNING id`,
			orderID, it.MenuItemID, it.Qty, it.UnitPriceMinor,
		).Scan(&it.ID)
		if err != nil {
			return fmt.Errorf("insert order_item: %w", err)
		}
		it.OrderID = orderID
	}
	tag, err := tx.Exec(ctx, `
UPDATE "order" SET total_price_minor=$2, notes=$3, updated_at=now() WHERE id=$1`,
		orderID, totalMinor, notes)
	if err != nil {
		return fmt.Errorf("update order total: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return order.ErrOrderNotFound
	}
	return nil
}

// MarkReadyTx flips a placed/cutoff order to ready, stamping ready_at.
func (r *OrderRepo) MarkReadyTx(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `
UPDATE "order"
   SET status='ready', ready_at=now(), updated_at=now()
 WHERE id=$1 AND status IN ('cutoff','placed')`, id)
	if err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return order.ErrInvalidTransition
	}
	return nil
}

// MarkPickedUpTx atomically flips a ready order to picked_up.
func (r *OrderRepo) MarkPickedUpTx(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `
UPDATE "order"
   SET status='picked_up', picked_up_at=now(), updated_at=now()
 WHERE id=$1 AND status='ready'`, id)
	if err != nil {
		return fmt.Errorf("mark picked_up: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return order.ErrInvalidTransition
	}
	return nil
}

// MarkNoShowTx flips a stale ready order to no_show.
func (r *OrderRepo) MarkNoShowTx(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `
UPDATE "order"
   SET status='no_show', no_show_at=now(), updated_at=now()
 WHERE id=$1 AND status='ready'`, id)
	if err != nil {
		return fmt.Errorf("mark no_show: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return order.ErrInvalidTransition
	}
	return nil
}

func (r *OrderRepo) ListByUser(ctx context.Context, userID string, sinceDate time.Time) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+orderSelectCols+`
  FROM "order"
 WHERE user_id=$1 AND supply_date >= $2
 ORDER BY supply_date DESC, created_at DESC`, userID, sinceDate)
	if err != nil {
		return nil, err
	}
	orders, err := collectOrders(rows)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateItems(ctx, orders); err != nil {
		return nil, err
	}
	return orders, nil
}

func (r *OrderRepo) ListPlacedDueForCutoff(ctx context.Context, before time.Time) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+orderSelectCols+`
  FROM "order"
 WHERE status='placed' AND cutoff_at <= $1
 ORDER BY cutoff_at`, before)
	if err != nil {
		return nil, err
	}
	return collectOrders(rows)
}

// ListReadyOlderThan returns READY orders whose ready_at is older than threshold.
// Used by the NoShow sweeper.
func (r *OrderRepo) ListReadyOlderThan(ctx context.Context, threshold time.Time) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+orderSelectCols+`
  FROM "order"
 WHERE status='ready' AND ready_at IS NOT NULL AND ready_at < $1
 ORDER BY ready_at`, threshold)
	if err != nil {
		return nil, err
	}
	return collectOrders(rows)
}

// ListByVendorDay returns the vendor's orders on a given supply_date, optionally
// filtered by status. Used by the merchant board.
func (r *OrderRepo) ListByVendorDay(ctx context.Context, vendorID string, day time.Time, statuses []order.Status) ([]*order.Order, error) {
	if len(statuses) == 0 {
		rows, err := r.pool.Query(ctx, `
SELECT `+orderSelectCols+`
  FROM "order"
 WHERE vendor_id=$1 AND supply_date=$2
 ORDER BY created_at`, vendorID, day)
		if err != nil {
			return nil, err
		}
		orders, err := collectOrders(rows)
		if err != nil {
			return nil, err
		}
		if err := r.hydrateItems(ctx, orders); err != nil {
			return nil, err
		}
		return orders, nil
	}
	args := []any{vendorID, day}
	placeholders := make([]string, len(statuses))
	for i, s := range statuses {
		placeholders[i] = fmt.Sprintf("$%d::order_status", i+3)
		args = append(args, string(s))
	}
	q := `
SELECT ` + orderSelectCols + `
  FROM "order"
 WHERE vendor_id=$1 AND supply_date=$2 AND status IN (` + strings.Join(placeholders, ",") + `)
 ORDER BY created_at`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	orders, err := collectOrders(rows)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateItems(ctx, orders); err != nil {
		return nil, err
	}
	return orders, nil
}

// ListPickedOrNoShowInPeriod returns orders in {picked_up, no_show} whose
// supply_date falls within [from, to] inclusive. Used by the payroll Build step.
func (r *OrderRepo) ListPickedOrNoShowInPeriod(ctx context.Context, from, to time.Time) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+orderSelectCols+`
  FROM "order"
 WHERE status IN ('picked_up','no_show')
   AND supply_date >= $1 AND supply_date <= $2
 ORDER BY user_id, supply_date`, from, to)
	if err != nil {
		return nil, err
	}
	return collectOrders(rows)
}

func collectOrders(rows pgx.Rows) ([]*order.Order, error) {
	defer rows.Close()
	var out []*order.Order
	for rows.Next() {
		var o order.Order
		if err := scanOrder(rows, &o); err != nil {
			return nil, err
		}
		out = append(out, &o)
	}
	return out, rows.Err()
}

// hydrateItems loads order_item rows for every order in one query (no N+1).
// collectOrders by itself returns the bare projection without items.
func (r *OrderRepo) hydrateItems(ctx context.Context, orders []*order.Order) error {
	if len(orders) == 0 {
		return nil
	}
	byID := make(map[string]*order.Order, len(orders))
	ids := make([]string, 0, len(orders))
	for _, o := range orders {
		byID[o.ID] = o
		ids = append(ids, o.ID)
	}
	rows, err := r.pool.Query(ctx, `
SELECT oi.id, oi.order_id, oi.menu_item_id, COALESCE(mi.name, ''), oi.qty, oi.unit_price_minor
  FROM order_item oi
  LEFT JOIN menu_item mi ON mi.id = oi.menu_item_id
 WHERE oi.order_id = ANY($1)
 ORDER BY oi.order_id, oi.id`, ids)
	if err != nil {
		return fmt.Errorf("hydrate items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it order.Item
		if err := rows.Scan(&it.ID, &it.OrderID, &it.MenuItemID, &it.Name, &it.Qty, &it.UnitPriceMinor); err != nil {
			return err
		}
		if o := byID[it.OrderID]; o != nil {
			o.Items = append(o.Items, it)
		}
	}
	return rows.Err()
}
