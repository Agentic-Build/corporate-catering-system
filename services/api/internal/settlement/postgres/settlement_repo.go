package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
)

// SettlementRepo is the postgres implementation of both
// settlement.SettlementRepository and settlement.OrderAggregateRepository.
// Order aggregation lives here (rather than in the order module) because it is
// a settlement-specific read shape; it mirrors order's ListPickedOrNoShowInPeriod
// inclusion (status ∈ {picked_up, no_show}, sliced by supply_date).
type SettlementRepo struct{ pool *pgxpool.Pool }

func NewSettlementRepo(p *pgxpool.Pool) *SettlementRepo { return &SettlementRepo{pool: p} }

const settlementCols = `id, vendor_id, period_start, period_end, order_count, portion_count, gross_minor, order_ids, status, closed_at, closed_by, created_at`

func (r *SettlementRepo) CreateTx(ctx context.Context, tx pgx.Tx, s *settlement.Settlement) error {
	if tx == nil {
		return errors.New("SettlementRepo.CreateTx requires a tx")
	}
	status := s.Status
	if status == "" {
		status = settlement.StatusClosed
	}
	err := tx.QueryRow(ctx, `
INSERT INTO vendor_settlement
  (vendor_id, period_start, period_end, order_count, portion_count, gross_minor, order_ids, status, closed_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::vendor_settlement_status, $9)
RETURNING id, closed_at, created_at`,
		s.VendorID, s.PeriodStart, s.PeriodEnd, s.OrderCount, s.PortionCount,
		s.GrossMinor, s.OrderIDs, string(status), s.ClosedBy,
	).Scan(&s.ID, &s.ClosedAt, &s.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "vendor_settlement_active_idx") {
			return settlement.ErrPeriodAlreadyClosed
		}
		return fmt.Errorf("create settlement: %w", err)
	}
	s.Status = status
	return nil
}

func (r *SettlementRepo) GetByID(ctx context.Context, id string) (*settlement.Settlement, error) {
	var s settlement.Settlement
	var status string
	err := r.pool.QueryRow(ctx, `SELECT `+settlementCols+` FROM vendor_settlement WHERE id=$1`, id).Scan(
		&s.ID, &s.VendorID, &s.PeriodStart, &s.PeriodEnd, &s.OrderCount,
		&s.PortionCount, &s.GrossMinor, &s.OrderIDs, &status, &s.ClosedAt,
		&s.ClosedBy, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, settlement.ErrSettlementNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan settlement: %w", err)
	}
	s.Status = settlement.Status(status)
	return &s, nil
}

func (r *SettlementRepo) ListByVendor(ctx context.Context, vendorID string) ([]*settlement.Settlement, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+settlementCols+` FROM vendor_settlement
 WHERE vendor_id=$1
 ORDER BY period_start DESC, created_at DESC`, vendorID)
	if err != nil {
		return nil, err
	}
	return collectSettlements(rows)
}

func (r *SettlementRepo) ListByPeriod(ctx context.Context, start, end time.Time) ([]*settlement.Settlement, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+settlementCols+` FROM vendor_settlement
 WHERE period_start <= $2 AND period_end >= $1
 ORDER BY period_start DESC, vendor_id`, start, end)
	if err != nil {
		return nil, err
	}
	return collectSettlements(rows)
}

func collectSettlements(rows pgx.Rows) ([]*settlement.Settlement, error) {
	defer rows.Close()
	out := []*settlement.Settlement{}
	for rows.Next() {
		var s settlement.Settlement
		var status string
		if err := rows.Scan(
			&s.ID, &s.VendorID, &s.PeriodStart, &s.PeriodEnd, &s.OrderCount,
			&s.PortionCount, &s.GrossMinor, &s.OrderIDs, &status, &s.ClosedAt,
			&s.ClosedBy, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		s.Status = settlement.Status(status)
		out = append(out, &s)
	}
	return out, rows.Err()
}

func (r *SettlementRepo) VoidTx(ctx context.Context, tx pgx.Tx, id string) error {
	if tx == nil {
		return errors.New("SettlementRepo.VoidTx requires a tx")
	}
	tag, err := tx.Exec(ctx, `
UPDATE vendor_settlement
   SET status='void'
 WHERE id=$1 AND status='closed'`, id)
	if err != nil {
		return fmt.Errorf("void settlement: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return settlement.ErrInvalidTransition
	}
	return nil
}

// AggregateByVendor rolls up picked_up/no_show orders per vendor over the
// period. portion_count joins order_item and sums qty; gross_minor sums
// total_price_minor. order_ids is gathered with array_agg.
func (r *SettlementRepo) AggregateByVendor(ctx context.Context, start, end time.Time) ([]*settlement.VendorAggregate, error) {
	rows, err := r.pool.Query(ctx, `
SELECT o.vendor_id,
       COUNT(*)                                  AS order_count,
       COALESCE(SUM(oi.portions), 0)              AS portion_count,
       COALESCE(SUM(o.total_price_minor), 0)      AS gross_minor,
       array_agg(o.id ORDER BY o.supply_date, o.id) AS order_ids
  FROM "order" o
  LEFT JOIN (
       SELECT order_id, COALESCE(SUM(qty), 0) AS portions
         FROM order_item GROUP BY order_id
  ) oi ON oi.order_id = o.id
 WHERE o.status IN ('picked_up','no_show')
   AND o.supply_date >= $1 AND o.supply_date <= $2
 GROUP BY o.vendor_id
 ORDER BY o.vendor_id`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*settlement.VendorAggregate{}
	for rows.Next() {
		var a settlement.VendorAggregate
		if err := rows.Scan(&a.VendorID, &a.OrderCount, &a.PortionCount, &a.GrossMinor, &a.OrderIDs); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// AggregateForVendor is the single-vendor variant. It always returns a non-nil
// aggregate; when the vendor has no picked_up/no_show orders the counts are zero
// and OrderIDs is an empty slice.
func (r *SettlementRepo) AggregateForVendor(ctx context.Context, vendorID string, start, end time.Time) (*settlement.VendorAggregate, error) {
	a := &settlement.VendorAggregate{VendorID: vendorID, OrderIDs: []string{}}
	err := r.pool.QueryRow(ctx, `
SELECT COUNT(*)                                   AS order_count,
       COALESCE(SUM(oi.portions), 0)              AS portion_count,
       COALESCE(SUM(o.total_price_minor), 0)      AS gross_minor,
       COALESCE(array_agg(o.id ORDER BY o.supply_date, o.id)
                FILTER (WHERE o.id IS NOT NULL), '{}') AS order_ids
  FROM "order" o
  LEFT JOIN (
       SELECT order_id, COALESCE(SUM(qty), 0) AS portions
         FROM order_item GROUP BY order_id
  ) oi ON oi.order_id = o.id
 WHERE o.vendor_id = $1
   AND o.status IN ('picked_up','no_show')
   AND o.supply_date >= $2 AND o.supply_date <= $3`, vendorID, start, end).
		Scan(&a.OrderCount, &a.PortionCount, &a.GrossMinor, &a.OrderIDs)
	if err != nil {
		return nil, fmt.Errorf("aggregate for vendor: %w", err)
	}
	if a.OrderIDs == nil {
		a.OrderIDs = []string{}
	}
	return a, nil
}

// StatusBreakdownForVendor counts a vendor's orders by status over the period.
func (r *SettlementRepo) StatusBreakdownForVendor(ctx context.Context, vendorID string, start, end time.Time) (settlement.StatusBreakdown, error) {
	var b settlement.StatusBreakdown
	err := r.pool.QueryRow(ctx, `
SELECT
  COUNT(*) FILTER (WHERE status = 'picked_up') AS picked_up,
  COUNT(*) FILTER (WHERE status = 'no_show')   AS no_show,
  COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled,
  COUNT(*) FILTER (WHERE status = 'refunded')  AS refunded
  FROM "order"
 WHERE vendor_id = $1
   AND supply_date >= $2 AND supply_date <= $3`, vendorID, start, end).
		Scan(&b.PickedUp, &b.NoShow, &b.Cancelled, &b.Refunded)
	if err != nil {
		return b, fmt.Errorf("status breakdown: %w", err)
	}
	return b, nil
}

// OrderLinesByIDs expands a settlement's frozen order_ids into order-level rows.
func (r *SettlementRepo) OrderLinesByIDs(ctx context.Context, orderIDs []string) ([]*settlement.SettlementOrderLine, error) {
	if len(orderIDs) == 0 {
		return []*settlement.SettlementOrderLine{}, nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT o.id, o.supply_date, o.status, o.total_price_minor,
       COALESCE(oi.portions, 0) AS portion_count
  FROM "order" o
  LEFT JOIN (
       SELECT order_id, COALESCE(SUM(qty), 0) AS portions
         FROM order_item GROUP BY order_id
  ) oi ON oi.order_id = o.id
 WHERE o.id = ANY($1)
 ORDER BY o.supply_date, o.id`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*settlement.SettlementOrderLine{}
	for rows.Next() {
		var l settlement.SettlementOrderLine
		if err := rows.Scan(&l.OrderID, &l.SupplyDate, &l.Status, &l.TotalPriceMinor, &l.PortionCount); err != nil {
			return nil, err
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}
