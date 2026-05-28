package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/observability"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// defaultMealWindow tags order/quota metrics; this system supports one meal
// per day. When multi-window support lands it becomes a field on Order/Supply.
const defaultMealWindow = "lunch"

// txBeginner is the transaction-starting surface of *pgxpool.Pool, taken as
// an interface so tests can inject a fake.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// QuotaTx is the subset of the quota repo Service needs inside a transaction.
type QuotaTx interface {
	DecrementTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) (int, error)
	RestoreTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) error
}

// OrderTx is the order repo subset Service uses inside a transaction.
type OrderTx interface {
	CreateTx(ctx context.Context, tx pgx.Tx, o *Order) error
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to Status) error
	ReplaceItemsTx(ctx context.Context, tx pgx.Tx, orderID string, items []Item, totalMinor int64, notes string) error
	MarkReadyTx(ctx context.Context, tx pgx.Tx, id string) error
	MarkPickedUpTx(ctx context.Context, tx pgx.Tx, id string) error
	MarkNoShowTx(ctx context.Context, tx pgx.Tx, id string) error
}

// StateEventTx is the state-event repo subset used inside a transaction.
type StateEventAppender interface {
	AppendTx(ctx context.Context, tx pgx.Tx, ev *StateEvent) error
}

// AuditTx is the audit repo subset used inside a transaction.
type AuditTxWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

// OutboxTx is the outbox repo subset used inside a transaction.
type OutboxAppender interface {
	AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
}

// Clock allows tests to control "now" for cutoff checks.
type Nower interface{ Now() time.Time }

// VendorReader is the vendor read dependency Place needs to resolve a vendor's
// per-vendor cutoff hour.
type VendorReader interface {
	GetByID(ctx context.Context, id string) (*vendor.Vendor, error)
}

// Service orchestrates Place / Cancel across order, state-event, outbox,
// audit, and quota repos. All multi-table writes run inside pgx.BeginFunc so
// any failure (including ErrOutOfStock) rolls back atomically.
type Service struct {
	Pool        txBeginner
	Orders      Repository
	OrdersTx    OrderTx
	StateEvents StateEventRepository
	StateTx     StateEventAppender
	Audit       AuditWriter
	AuditTx     AuditTxWriter
	Outbox      OutboxRepository
	OutboxTx    OutboxAppender
	QuotaTx     QuotaTx
	Items       menu.ItemRepository
	Plants      vendor.PlantMappingRepository
	Vendors     VendorReader
	Clock       Nower
	// Location is the timezone for computing a vendor's cutoff hour. Nil means
	// UTC; production wires time.Local.
	Location *time.Location
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
	Notes      string
}

// resolvedItems is the validated item-set + derived totals shared by Place's
// resolution and tx phases.
type resolvedItems struct {
	VendorID   string
	TotalPrice int64
	Items      []Item
}

// resolvePlaceItems verifies each PlaceItem (qty, menu lookup, single vendor)
// and returns the domain items plus the derived totals.
func (s *Service) resolvePlaceItems(ctx context.Context, items []PlaceItem, outcome *string) (*resolvedItems, error) {
	out := &resolvedItems{Items: make([]Item, 0, len(items))}
	for _, pi := range items {
		if pi.Qty <= 0 {
			*outcome = "invalid_qty"
			return nil, fmt.Errorf("order: item qty must be positive")
		}
		mi, err := s.Items.GetByID(ctx, pi.MenuItemID)
		if err != nil {
			*outcome = "menu_item_lookup_failed"
			return nil, err
		}
		if out.VendorID == "" {
			out.VendorID = mi.VendorID
		} else if out.VendorID != mi.VendorID {
			*outcome = "multi_vendor"
			return nil, ErrMultiVendor
		}
		out.Items = append(out.Items, Item{
			MenuItemID:     pi.MenuItemID,
			Qty:            pi.Qty,
			UnitPriceMinor: mi.PriceMinor,
		})
		out.TotalPrice += mi.PriceMinor * int64(pi.Qty)
	}
	return out, nil
}

// validateVendorPlacement verifies the vendor serves the plant and that the
// supply_date falls inside the cutoff + preorder window. Returns cutoffAt + now.
func (s *Service) validateVendorPlacement(ctx context.Context, in PlaceOrderInput, vendorID string, outcome *string) (time.Time, time.Time, error) {
	plants, err := s.Plants.ListByVendor(ctx, vendorID)
	if err != nil {
		*outcome = "plants_lookup_failed"
		return time.Time{}, time.Time{}, err
	}
	served := false
	for _, p := range plants {
		if p.Plant == in.Plant {
			served = true
			break
		}
	}
	if !served {
		*outcome = "vendor_plant_mismatch"
		return time.Time{}, time.Time{}, ErrVendorPlantMismatch
	}
	v, err := s.Vendors.GetByID(ctx, vendorID)
	if err != nil {
		*outcome = "vendor_lookup_failed"
		return time.Time{}, time.Time{}, err
	}
	loc := s.Location
	if loc == nil {
		loc = time.UTC
	}
	cutoffAt := time.Date(in.SupplyDate.Year(), in.SupplyDate.Month(), in.SupplyDate.Day()-1, v.CutoffHour, 0, 0, 0, loc)
	now := s.Clock.Now()
	if !now.Before(cutoffAt) {
		*outcome = "cutoff_passed"
		return time.Time{}, time.Time{}, ErrCutoffPassed
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if in.SupplyDate.After(today.AddDate(0, 0, v.PreorderWindowDays)) {
		*outcome = "outside_preorder_window"
		return time.Time{}, time.Time{}, ErrOutsidePreorderWindow
	}
	return cutoffAt, now, nil
}

// persistPlacedOrder runs the decrement-quota / insert-order / state / outbox /
// audit writes inside one tx. Sets *quotaExhaustedItem when DecrementTx
// returns ErrOutOfStock.
func (s *Service) persistPlacedOrder(ctx context.Context, in PlaceOrderInput, o *Order, quotaExhaustedItem *string) error {
	return MaybeConcurrencyErr(pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, it := range o.Items {
			if _, err := s.QuotaTx.DecrementTx(ctx, tx, it.MenuItemID, in.SupplyDate, it.Qty); err != nil {
				if errors.Is(err, quota.ErrOutOfStock) {
					*quotaExhaustedItem = it.MenuItemID
				}
				return err
			}
		}
		if err := s.OrdersTx.CreateTx(ctx, tx, o); err != nil {
			return err
		}
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
		payload := buildOrderPayload(o)
		if err := s.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.placed.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.AuditTx.WriteTx(ctx, tx, &in.UserID, &role, "order.place", "order", o.ID, payload, "")
	}))
}

// Place creates an order in PLACED state inside a single transaction. On any
// failure (including ErrOutOfStock) everything rolls back, so quota decrements
// are released and no order / state / outbox / audit row is left behind.
func (s *Service) Place(ctx context.Context, in PlaceOrderInput) (*Order, error) {
	startedAt := time.Now()
	outcome := "success"
	vendorIDRef := ""
	defer func() {
		observability.RecordOrderPlaceLatency(ctx, time.Since(startedAt).Seconds(), in.Plant, defaultMealWindow, outcome)
		observability.RecordOrderPlaced(ctx, in.Plant, vendorIDRef, defaultMealWindow, outcome)
	}()
	if len(in.Items) == 0 {
		outcome = "empty"
		return nil, ErrEmptyOrder
	}

	resolved, err := s.resolvePlaceItems(ctx, in.Items, &outcome)
	if err != nil {
		return nil, err
	}
	vendorIDRef = resolved.VendorID

	cutoffAt, now, err := s.validateVendorPlacement(ctx, in, resolved.VendorID, &outcome)
	if err != nil {
		return nil, err
	}

	placedAt := now
	o := &Order{
		UserID:          in.UserID,
		VendorID:        resolved.VendorID,
		Plant:           in.Plant,
		SupplyDate:      in.SupplyDate,
		Status:          StatusPlaced,
		TotalPriceMinor: resolved.TotalPrice,
		Notes:           in.Notes,
		PlacedAt:        &placedAt,
		CutoffAt:        cutoffAt,
		Items:           resolved.Items,
	}

	var quotaExhaustedItem string
	if err := s.persistPlacedOrder(ctx, in, o, &quotaExhaustedItem); err != nil {
		switch {
		case quotaExhaustedItem != "":
			outcome = "quota_exhausted"
			observability.RecordQuotaExhausted(ctx, in.Plant, resolved.VendorID, defaultMealWindow, quotaExhaustedItem)
		case errors.Is(err, ErrConcurrentModification):
			outcome = "concurrent_modification"
		default:
			outcome = "tx_error"
		}
		return nil, err
	}
	observability.RecordOrderPrice(ctx, resolved.TotalPrice, in.Plant, resolved.VendorID)
	return o, nil
}

// persistCancelOrder applies the status flip + quota restore + state/outbox/
// audit writes inside one tx.
func (s *Service) persistCancelOrder(ctx context.Context, o *Order, userID string) error {
	return MaybeConcurrencyErr(pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
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
		payload := map[string]any{"order_id": o.ID, "vendor_id": o.VendorID, "by": "user"}
		if err := s.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.cancelled.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.AuditTx.WriteTx(ctx, tx, &userID, &role, "order.cancel", "order", o.ID, payload, "")
	}))
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
	if err := s.persistCancelOrder(ctx, o, userID); err != nil {
		return err
	}
	observability.RecordOrderCancelled(ctx, o.Plant, o.VendorID, "user_cancel", "employee")
	return nil
}

type ModifyOrderInput struct {
	OrderID string
	UserID  string
	Items   []PlaceItem
	Notes   string
}

// resolveModifyItems verifies the new items belong to the order's vendor and
// returns the domain items + total price.
func (s *Service) resolveModifyItems(ctx context.Context, items []PlaceItem, vendorID string) ([]Item, int64, error) {
	var totalPrice int64
	newItems := make([]Item, 0, len(items))
	for _, pi := range items {
		if pi.Qty <= 0 {
			return nil, 0, fmt.Errorf("order: item qty must be positive")
		}
		mi, err := s.Items.GetByID(ctx, pi.MenuItemID)
		if err != nil {
			return nil, 0, err
		}
		if mi.VendorID != vendorID {
			return nil, 0, ErrMultiVendor
		}
		newItems = append(newItems, Item{
			MenuItemID:     pi.MenuItemID,
			Qty:            pi.Qty,
			UnitPriceMinor: mi.PriceMinor,
		})
		totalPrice += mi.PriceMinor * int64(pi.Qty)
	}
	return newItems, totalPrice, nil
}

// quotaDeltaForModify returns the per-menu-item desired_qty − current_qty delta.
func quotaDeltaForModify(current, desired []Item) map[string]int {
	delta := map[string]int{}
	for _, it := range current {
		delta[it.MenuItemID] -= it.Qty
	}
	for _, it := range desired {
		delta[it.MenuItemID] += it.Qty
	}
	return delta
}

// persistModifiedOrder applies the quota delta, swaps the item rows + total +
// notes, then writes outbox + audit, all inside a single tx.
func (s *Service) persistModifiedOrder(ctx context.Context, in ModifyOrderInput, o *Order, newItems []Item, totalPrice int64, delta map[string]int) error {
	return MaybeConcurrencyErr(pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for itemID, d := range delta {
			switch {
			case d > 0:
				if _, err := s.QuotaTx.DecrementTx(ctx, tx, itemID, o.SupplyDate, d); err != nil {
					return err
				}
			case d < 0:
				if err := s.QuotaTx.RestoreTx(ctx, tx, itemID, o.SupplyDate, -d); err != nil {
					return err
				}
			}
		}
		if err := s.OrdersTx.ReplaceItemsTx(ctx, tx, o.ID, newItems, totalPrice, in.Notes); err != nil {
			return err
		}
		o.Items = newItems
		o.TotalPriceMinor = totalPrice
		o.Notes = in.Notes
		payload := buildOrderPayload(o)
		if err := s.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.modified.v1", payload, map[string]any{}); err != nil {
			return err
		}
		role := "employee"
		return s.AuditTx.WriteTx(ctx, tx, &in.UserID, &role, "order.modify", "order", o.ID, payload, "")
	}))
}

// Modify replaces the items of a user-owned PLACED order before its cutoff.
// Quota is adjusted by per-menu-item delta inside one transaction so failures
// don't leak quota. Order ID and status are unchanged — only items + total —
// so no state event is written, only an audit row + order.modified.v1 outbox.
func (s *Service) Modify(ctx context.Context, in ModifyOrderInput) (*Order, error) {
	if len(in.Items) == 0 {
		return nil, ErrEmptyOrder
	}
	o, err := s.Orders.GetByID(ctx, in.OrderID)
	if err != nil {
		return nil, err
	}
	if o.UserID != in.UserID {
		return nil, ErrForbidden
	}
	if o.Status != StatusPlaced {
		return nil, ErrInvalidTransition
	}
	if !s.Clock.Now().Before(o.CutoffAt) {
		return nil, ErrCutoffPassed
	}
	newItems, totalPrice, err := s.resolveModifyItems(ctx, in.Items, o.VendorID)
	if err != nil {
		return nil, err
	}
	delta := quotaDeltaForModify(o.Items, newItems)
	if err := s.persistModifiedOrder(ctx, in, o, newItems, totalPrice, delta); err != nil {
		return nil, err
	}
	observability.RecordOrderModified(ctx, o.Plant, o.VendorID)
	return s.Orders.GetByID(ctx, o.ID)
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

// ListByVendorDay returns the vendor's orders on a given supply_date, optionally
// filtered by status. Used by the merchant prep-board.
func (s *Service) ListByVendorDay(ctx context.Context, vendorID string, day time.Time, statuses []Status) ([]*Order, error) {
	return s.Orders.ListByVendorDay(ctx, vendorID, day, statuses)
}

// markOneReady runs the lookup + ownership/transition check + persist for one
// order id inside the caller's tx.
func (s *Service) markOneReady(ctx context.Context, tx pgx.Tx, id, vendorID, actorID string) error {
	o, err := s.Orders.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if o.VendorID != vendorID {
		return ErrForbidden
	}
	if !CanTransition(o.Status, StatusReady) {
		return ErrInvalidTransition
	}
	if err := s.OrdersTx.MarkReadyTx(ctx, tx, id); err != nil {
		return err
	}
	from := o.Status
	evRole := "vendor_operator"
	if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
		OrderID:   id,
		FromState: &from,
		ToState:   StatusReady,
		ActorID:   &actorID,
		ActorRole: &evRole,
		Reason:    "vendor_ready",
		Payload:   map[string]any{},
	}); err != nil {
		return err
	}
	payload := map[string]any{"order_id": id, "vendor_id": vendorID}
	if err := s.OutboxTx.AppendTx(ctx, tx, "order", id, "order.ready.v1", payload, map[string]any{}); err != nil {
		return err
	}
	return s.AuditTx.WriteTx(ctx, tx, &actorID, &evRole, "order.ready", "order", id, payload, "")
}

// MarkReady transitions orders from cutoff/placed → ready (vendor side).
// All orders must belong to vendorID; any forbidden / invalid-state order
// aborts the whole batch (transaction rolls back).
func (s *Service) MarkReady(ctx context.Context, vendorID string, orderIDs []string, actorID string) error {
	err := MaybeConcurrencyErr(pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, id := range orderIDs {
			if err := s.markOneReady(ctx, tx, id, vendorID, actorID); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return err
	}
	if len(orderIDs) > 0 {
		observability.RecordOrderReady(ctx, vendorID, len(orderIDs))
	}
	return nil
}

// Pickup atomically transitions READY → PICKED_UP for the order's OWNER.
// The scanned QR carries only the order id; ownership is enforced here.
// MarkPickedUpTx's conditional UPDATE guarantees one-time idempotency —
// exactly one concurrent caller wins, others see ErrInvalidTransition.
func (s *Service) Pickup(ctx context.Context, orderID, employeeID string) (err error) {
	var plant, vendor string
	outcome := "tx_error"
	defer func() {
		observability.RecordPickupVerified(ctx, plant, vendor, outcome)
	}()

	o, err := s.Orders.GetByID(ctx, orderID)
	if err != nil {
		outcome = "order_lookup_failed"
		return err
	}
	plant, vendor = o.Plant, o.VendorID
	if o.UserID != employeeID {
		outcome = "forbidden"
		return ErrForbidden
	}
	if o.Status != StatusReady {
		outcome = "wrong_state"
		return ErrInvalidTransition
	}

	err = MaybeConcurrencyErr(pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.OrdersTx.MarkPickedUpTx(ctx, tx, orderID); err != nil {
			return err
		}
		from := StatusReady
		evRole := "employee"
		if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
			OrderID:   orderID,
			FromState: &from,
			ToState:   StatusPickedUp,
			ActorID:   &employeeID,
			ActorRole: &evRole,
			Reason:    "qr_pickup",
			Payload:   map[string]any{},
		}); err != nil {
			return err
		}
		payload := map[string]any{"order_id": orderID, "vendor_id": o.VendorID}
		if err := s.OutboxTx.AppendTx(ctx, tx, "order", orderID, "order.picked_up.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.AuditTx.WriteTx(ctx, tx, &employeeID, &evRole, "order.picked_up", "order", orderID, payload, "")
	}))
	if err != nil {
		switch {
		case errors.Is(err, ErrConcurrentModification):
			outcome = "concurrent_modification"
		case errors.Is(err, ErrInvalidTransition):
			// Lost the in-tx race (another caller flipped READY between our
			// pre-check and MarkPickedUpTx). Report as wrong_state, not tx_error.
			outcome = "wrong_state"
		}
		return err
	}
	outcome = "success"
	return nil
}

// MarkNoShow transitions READY orders whose ready_at is older than cutoffAge
// to NO_SHOW. Each order's transition happens in its own transaction; errors
// on individual orders are skipped so a single bad row never stalls the sweep.
// Returns the number of orders successfully transitioned.
func (s *Service) MarkNoShow(ctx context.Context, cutoffAge time.Duration) (int, error) {
	threshold := s.Clock.Now().Add(-cutoffAge)
	pending, err := s.Orders.ListReadyOlderThan(ctx, threshold)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range pending {
		err := MaybeConcurrencyErr(pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
			if err := s.OrdersTx.MarkNoShowTx(ctx, tx, o.ID); err != nil {
				return err
			}
			from := StatusReady
			sysRole := "welfare_admin"
			if err := s.StateTx.AppendTx(ctx, tx, &StateEvent{
				OrderID:   o.ID,
				FromState: &from,
				ToState:   StatusNoShow,
				ActorRole: &sysRole,
				Reason:    "no_show_timeout",
				Payload:   map[string]any{},
			}); err != nil {
				return err
			}
			payload := map[string]any{"order_id": o.ID, "vendor_id": o.VendorID}
			if err := s.OutboxTx.AppendTx(ctx, tx, "order", o.ID, "order.no_show.v1", payload, map[string]any{}); err != nil {
				return err
			}
			return s.AuditTx.WriteTx(ctx, tx, nil, &sysRole, "order.no_show", "order", o.ID, payload, "")
		}))
		if err == nil {
			n++
		}
	}
	if n > 0 {
		observability.RecordOrderNoShow(ctx, n)
	}
	return n, nil
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
		"notes":             o.Notes,
		"items":             items,
	}
}
