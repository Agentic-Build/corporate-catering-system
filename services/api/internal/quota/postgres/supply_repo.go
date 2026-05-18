package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
)

type SupplyRepo struct{ pool *pgxpool.Pool }

func NewSupplyRepo(p *pgxpool.Pool) *SupplyRepo { return &SupplyRepo{pool: p} }

func (r *SupplyRepo) Upsert(ctx context.Context, s *quota.Supply) error {
	return r.pool.QueryRow(ctx, `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (menu_item_id, supply_date) DO UPDATE
   SET capacity = EXCLUDED.capacity,
       remain   = LEAST(EXCLUDED.remain, EXCLUDED.capacity),
       pickup_window = EXCLUDED.pickup_window,
       eta_label     = EXCLUDED.eta_label,
       cutoff_at     = EXCLUDED.cutoff_at,
       updated_at = now()
RETURNING id, created_at, updated_at`,
		s.MenuItemID, s.SupplyDate, s.Capacity, s.Remain, s.PickupWindow, s.ETALabel, s.CutoffAt,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
}

func (r *SupplyRepo) Get(ctx context.Context, itemID string, date time.Time) (*quota.Supply, error) {
	var s quota.Supply
	err := r.pool.QueryRow(ctx, `
SELECT id, menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at, sold_out, created_at, updated_at
  FROM meal_supply WHERE menu_item_id=$1 AND supply_date=$2`, itemID, date).Scan(
		&s.ID, &s.MenuItemID, &s.SupplyDate, &s.Capacity, &s.Remain,
		&s.PickupWindow, &s.ETALabel, &s.CutoffAt, &s.SoldOut, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, quota.ErrSupplyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("supply scan: %w", err)
	}
	return &s, nil
}

func (r *SupplyRepo) ListByVendor(ctx context.Context, vendorID string, date time.Time) ([]*quota.Supply, error) {
	rows, err := r.pool.Query(ctx, `
SELECT ms.id, ms.menu_item_id, ms.supply_date, ms.capacity, ms.remain, ms.pickup_window, ms.eta_label, ms.cutoff_at, ms.sold_out, ms.created_at, ms.updated_at
  FROM meal_supply ms
  JOIN menu_item mi ON mi.id = ms.menu_item_id
 WHERE mi.vendor_id = $1 AND ms.supply_date = $2
 ORDER BY mi.name`, vendorID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*quota.Supply
	for rows.Next() {
		var s quota.Supply
		if err := rows.Scan(&s.ID, &s.MenuItemID, &s.SupplyDate, &s.Capacity, &s.Remain,
			&s.PickupWindow, &s.ETALabel, &s.CutoffAt, &s.SoldOut, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// SetSoldOut flips the temporary sold-out flag. Returns ErrSupplyNotFound when
// no supply row exists for the (item, date) pair.
func (r *SupplyRepo) SetSoldOut(ctx context.Context, itemID string, date time.Time, soldOut bool) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE meal_supply SET sold_out=$3, updated_at=now()
 WHERE menu_item_id=$1 AND supply_date=$2`, itemID, date, soldOut)
	if err != nil {
		return fmt.Errorf("set sold_out: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return quota.ErrSupplyNotFound
	}
	return nil
}

// Decrement is the source-of-truth quota operation.
// Conditional UPDATE: only decrements when remain >= n. Returns new remain or ErrOutOfStock.
func (r *SupplyRepo) Decrement(ctx context.Context, itemID string, date time.Time, n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("quota: n must be positive (got %d)", n)
	}
	var newRemain int
	err := r.pool.QueryRow(ctx, `
UPDATE meal_supply
   SET remain = remain - $3,
       updated_at = now()
 WHERE menu_item_id = $1
   AND supply_date  = $2
   AND remain >= $3
RETURNING remain`, itemID, date, n).Scan(&newRemain)
	if errors.Is(err, pgx.ErrNoRows) {
		// Either supply doesn't exist OR remain < n. Disambiguate.
		var exists bool
		if err2 := r.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM meal_supply WHERE menu_item_id=$1 AND supply_date=$2)`,
			itemID, date).Scan(&exists); err2 != nil {
			return 0, err2
		}
		if !exists {
			return 0, quota.ErrSupplyNotFound
		}
		return 0, quota.ErrOutOfStock
	}
	if err != nil {
		return 0, fmt.Errorf("decrement: %w", err)
	}
	return newRemain, nil
}

// Restore increments remain, capped at capacity. Used for cancellations.
func (r *SupplyRepo) Restore(ctx context.Context, itemID string, date time.Time, n int) error {
	if n <= 0 {
		return fmt.Errorf("quota: n must be positive (got %d)", n)
	}
	_, err := r.pool.Exec(ctx, `
UPDATE meal_supply
   SET remain = LEAST(remain + $3, capacity),
       updated_at = now()
 WHERE menu_item_id = $1 AND supply_date = $2`,
		itemID, date, n)
	return err
}

// DecrementTx is the transactional variant of Decrement. The caller owns the
// pgx.Tx so the quota update participates in a larger order-write transaction;
// on tx rollback the decrement is also reverted.
func (r *SupplyRepo) DecrementTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("quota: n must be positive (got %d)", n)
	}
	var newRemain int
	err := tx.QueryRow(ctx, `
UPDATE meal_supply
   SET remain = remain - $3,
       updated_at = now()
 WHERE menu_item_id = $1
   AND supply_date  = $2
   AND remain >= $3
RETURNING remain`, itemID, date, n).Scan(&newRemain)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err2 := tx.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM meal_supply WHERE menu_item_id=$1 AND supply_date=$2)`,
			itemID, date).Scan(&exists); err2 != nil {
			return 0, err2
		}
		if !exists {
			return 0, quota.ErrSupplyNotFound
		}
		return 0, quota.ErrOutOfStock
	}
	if err != nil {
		return 0, fmt.Errorf("decrement: %w", err)
	}
	return newRemain, nil
}

// RestoreTx is the transactional variant of Restore.
func (r *SupplyRepo) RestoreTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) error {
	if n <= 0 {
		return fmt.Errorf("quota: n must be positive (got %d)", n)
	}
	_, err := tx.Exec(ctx, `
UPDATE meal_supply
   SET remain = LEAST(remain + $3, capacity),
       updated_at = now()
 WHERE menu_item_id = $1 AND supply_date = $2`,
		itemID, date, n)
	return err
}
