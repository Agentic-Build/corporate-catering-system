package order

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// QuotaTx is the subset of the quota repo Service needs inside a transaction.
type QuotaTx interface {
	DecrementTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) (int, error)
	RestoreTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) error
}

// OrderTx is the order repo subset Service uses inside a transaction.
type OrderTx interface {
	CreateTx(ctx context.Context, tx pgx.Tx, o *Order) error
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to Status) error
}

// StateEventTx is the state-event repo subset used inside a transaction.
type StateEventTx interface {
	AppendTx(ctx context.Context, tx pgx.Tx, ev *StateEvent) error
}

// AuditTx is the audit repo subset used inside a transaction.
type AuditTx interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

// OutboxTx is the outbox repo subset used inside a transaction.
type OutboxTx interface {
	AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
}

// Clock allows tests to control "now" for cutoff checks.
type Clock interface{ Now() time.Time }

// Service orchestrates Place / Cancel across order, state-event, outbox, audit,
// and quota repos. All multi-table writes are wrapped in pgx.BeginFunc so a
// failure at any step (including ErrOutOfStock) rolls the entire transaction
// back atomically.
type Service struct {
	Pool        *pgxpool.Pool
	Orders      Repository
	OrdersTx    OrderTx
	StateEvents StateEventRepository
	StateTx     StateEventTx
	Audit       AuditRepository
	AuditTx     AuditTx
	Outbox      OutboxRepository
	OutboxTx    OutboxTx
	QuotaTx     QuotaTx
	Items       menu.ItemRepository
	Plants      vendor.PlantMappingRepository
	Clock       Clock
}

type PlaceItem struct {
	MenuItemID string
	Qty        int
}

type PlaceOrderInput struct {
	UserID     string
	Plant      string
	SupplyDate time.Time
	Items      []PlaceItem
}

// Place creates an order in PLACED state inside a single transaction.
// On any failure (including ErrOutOfStock) the entire transaction rolls back,
// so quota decrements are released and no order / state / outbox / audit row
// is left behind.
func (s *Service) Place(ctx context.Context, in PlaceOrderInput) (*Order, error) {
	if len(in.Items) == 0 {
		return nil, ErrEmptyOrder
	}

	// Resolve menu items, verify a single vendor, and compute total price.
	// Read-only lookups happen outside the tx so we don't hold row locks on
	// menu_item rows that we never mutate.
	var vendorID string
	var totalPrice int64
	domainItems := make([]Item, 0, len(in.Items))
	for _, pi := range in.Items {
		if pi.Qty <= 0 {
			return nil, fmt.Errorf("order: item qty must be positive")
		}
		mi, err := s.Items.GetByID(ctx, pi.MenuItemID)
		if err != nil {
			return nil, err
		}
		if vendorID == "" {
			vendorID = mi.VendorID
		} else if vendorID != mi.VendorID {
			return nil, fmt.Errorf("order: items must be from the same vendor")
		}
		domainItems = append(domainItems, Item{
			MenuItemID:     pi.MenuItemID,
			Qty:            pi.Qty,
			UnitPriceMinor: mi.PriceMinor,
		})
		totalPrice += mi.PriceMinor * int64(pi.Qty)
	}

	// Verify the vendor serves the requesting plant.
	plants, err := s.Plants.ListByVendor(ctx, vendorID)
	if err != nil {
		return nil, err
	}
	served := false
	for _, p := range plants {
		if p.Plant == in.Plant {
			served = true
			break
		}
	}
	if !served {
		return nil, ErrVendorPlantMismatch
	}

	// Cutoff: 17:00 UTC the day before the supply date (P3 default).
	cutoffAt := time.Date(in.SupplyDate.Year(), in.SupplyDate.Month(), in.SupplyDate.Day()-1, 17, 0, 0, 0, time.UTC)
	now := s.Clock.Now()
	if !now.Before(cutoffAt) {
		return nil, ErrCutoffPassed
	}

	placedAt := now
	o := &Order{
		UserID:          in.UserID,
		VendorID:        vendorID,
		Plant:           in.Plant,
		SupplyDate:      in.SupplyDate,
		Status:          StatusPlaced,
		TotalPriceMinor: totalPrice,
		PlacedAt:        &placedAt,
		CutoffAt:        cutoffAt,
		Items:           domainItems,
	}

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		// 1. Decrement quota for each item — conditional UPDATE per row.
		for _, it := range o.Items {
			if _, err := s.QuotaTx.DecrementTx(ctx, tx, it.MenuItemID, in.SupplyDate, it.Qty); err != nil {
				return err
			}
		}
		// 2. Insert order + items (assigns o.ID).
		if err := s.OrdersTx.CreateTx(ctx, tx, o); err != nil {
			return err
		}
		// 3. State event: nil → placed.
		role := "employee"
		if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
			OrderID:   o.ID,
			FromState: nil,
			ToState:   StatusPlaced,
			ActorID:   &in.UserID,
			ActorRole: &role,
			Reason:    "place_order",
			Payload:   map[string]any{},
		}); err != nil {
			return err
		}
		// 4. Outbox event for downstream consumers.
		payload := buildOrderPayload(o)
		if err := s.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.placed.v1", payload, map[string]any{}); err != nil {
			return err
		}
		// 5. Audit trail.
		return s.AuditTx.WriteTx(ctx, tx, &in.UserID, &role, "order.place", "order", o.ID, payload, "")
	})
	if err != nil {
		return nil, err
	}
	return o, nil
}

// Cancel transitions a user-owned PLACED order to CANCELLED, restoring quota
// and emitting state-event + audit + outbox entries atomically.
func (s *Service) Cancel(ctx context.Context, orderID, userID string) error {
	o, err := s.Orders.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if o.UserID != userID {
		return ErrForbidden
	}
	// Users may only cancel orders that are still in PLACED state;
	// CUTOFF / READY etc. require admin intervention.
	if o.Status != StatusPlaced {
		return ErrInvalidTransition
	}
	if !CanTransition(o.Status, StatusCancelled) {
		return ErrInvalidTransition
	}

	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.OrdersTx.UpdateStatusTx(ctx, tx, o.ID, StatusPlaced, StatusCancelled); err != nil {
			return err
		}
		for _, it := range o.Items {
			if err := s.QuotaTx.RestoreTx(ctx, tx, it.MenuItemID, o.SupplyDate, it.Qty); err != nil {
				return err
			}
		}
		role := "employee"
		from := StatusPlaced
		if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
			OrderID:   o.ID,
			FromState: &from,
			ToState:   StatusCancelled,
			ActorID:   &userID,
			ActorRole: &role,
			Reason:    "user_cancel",
			Payload:   map[string]any{},
		}); err != nil {
			return err
		}
		payload := map[string]any{"order_id": o.ID, "by": "user"}
		if err := s.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.cancelled.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.AuditTx.WriteTx(ctx, tx, &userID, &role, "order.cancel", "order", o.ID, payload, "")
	})
}

// ListByUser returns the user's orders for the last 30 days.
func (s *Service) ListByUser(ctx context.Context, userID string) ([]*Order, error) {
	return s.Orders.ListByUser(ctx, userID, s.Clock.Now().AddDate(0, 0, -30))
}

// Get returns a single order if the requester owns it.
func (s *Service) Get(ctx context.Context, id, userID string) (*Order, error) {
	o, err := s.Orders.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if o.UserID != userID {
		return nil, ErrForbidden
	}
	return o, nil
}

// buildOrderPayload renders the order.placed.v1 JSON payload.
func buildOrderPayload(o *Order) map[string]any {
	items := make([]map[string]any, len(o.Items))
	for i, it := range o.Items {
		items[i] = map[string]any{
			"menu_item_id":     it.MenuItemID,
			"qty":              it.Qty,
			"unit_price_minor": it.UnitPriceMinor,
		}
	}
	return map[string]any{
		"order_id":          o.ID,
		"user_id":           o.UserID,
		"vendor_id":         o.VendorID,
		"plant":             o.Plant,
		"supply_date":       o.SupplyDate.Format("2006-01-02"),
		"total_price_minor": o.TotalPriceMinor,
		"items":             items,
	}
}
