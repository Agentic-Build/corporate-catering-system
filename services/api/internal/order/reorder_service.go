package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// ReorderMenuItem is the menu_item view ReorderService needs, mirrored locally
// to avoid an import cycle with the menu package.
type ReorderMenuItem struct {
	ID         string
	Name       string
	PriceMinor int64
	Archived   bool // true when menu_item.status='archived' or archived_at IS NOT NULL
}

// ReorderSupply is the meal_supply view ReorderService needs.
type ReorderSupply struct {
	Remain   int
	CutoffAt time.Time
}

// ErrReorderSupplyNotFound is returned by reorderSupplyRepo.Get when no
// meal_supply row exists for the (item, date) pair.
var ErrReorderSupplyNotFound = errors.New("reorder: supply not found")

// ErrReorderItemNotFound is returned by reorderItemRepo.Get when the
// menu_item row no longer exists.
var ErrReorderItemNotFound = errors.New("reorder: menu item not found")

// reorderOrderRepo is the smallest order-repo surface ReorderService needs.
type reorderOrderRepo interface {
	GetByID(ctx context.Context, id string) (*Order, error)
	CreateTx(ctx context.Context, tx pgx.Tx, o *Order) error
}

// reorderSupplyRepo is the smallest meal_supply surface ReorderService needs.
// Callers adapt their *qpg.SupplyRepo with a shim that maps
// quota.ErrSupplyNotFound → ErrReorderSupplyNotFound.
type reorderSupplyRepo interface {
	Get(ctx context.Context, itemID string, date time.Time) (*ReorderSupply, error)
	DecrementTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) (int, error)
}

// reorderItemRepo is the smallest menu_item surface ReorderService needs.
type reorderItemRepo interface {
	GetByID(ctx context.Context, id string) (*ReorderMenuItem, error)
}

// ReorderService clones a previous order into a new pending order on the
// target supply_date. Items that are unavailable (no supply, cutoff passed,
// out of quota, or archived) are dropped and reported back to the caller so
// the UI can render a partial-fallback toast. If zero items survive the
// service does NOT create an order — the handler maps that to HTTP 409.
type ReorderService struct {
	pool    *pgxpool.Pool
	orders  reorderOrderRepo
	supply  reorderSupplyRepo
	items   reorderItemRepo
	vendors VendorReader
	plants  vendor.PlantMappingRepository
	state   StateEventTx
	audit   AuditTx
	outbox  OutboxTx
	clock   Clock
	// loc is the timezone for computing the order-level cutoff_at, matching
	// Service.Location. Nil means UTC.
	loc *time.Location
}

// NewReorderService wires the service.
func NewReorderService(
	pool *pgxpool.Pool,
	orders reorderOrderRepo,
	supply reorderSupplyRepo,
	items reorderItemRepo,
	vendors VendorReader,
	plants vendor.PlantMappingRepository,
	state StateEventTx,
	audit AuditTx,
	outbox OutboxTx,
	clock Clock,
	loc *time.Location,
) *ReorderService {
	return &ReorderService{
		pool:    pool,
		orders:  orders,
		supply:  supply,
		items:   items,
		vendors: vendors,
		plants:  plants,
		state:   state,
		audit:   audit,
		outbox:  outbox,
		clock:   clock,
		loc:     loc,
	}
}

// ReorderInput captures the request parameters.
type ReorderInput struct {
	UserID        string
	SourceOrderID string
	SupplyDate    string // target day, "YYYY-MM-DD"
	Plant         string // from authenticated user
}

// UnavailableItem records a single source item that could not be cloned.
type UnavailableItem struct {
	MenuItemID string
	Name       string
	Reason     string // "no_supply" | "cutoff_passed" | "out_of_quota" | "archived"
}

// ReorderResult is the outcome of a Reorder call. NewOrderID is empty when
// zero items survived availability checks; UnavailableItems is never nil.
type ReorderResult struct {
	NewOrderID       string
	UnavailableItems []UnavailableItem
}

const (
	reasonNoSupply     = "no_supply"
	reasonCutoffPassed = "cutoff_passed"
	reasonOutOfQuota   = "out_of_quota"
	reasonArchived     = "archived"
)

type reorderCandidate struct {
	item  *ReorderMenuItem
	qty   int
	price int64
}

// Reorder clones the source order's surviving items into a new placed order.
// Decrement-and-insert runs in one transaction so a mid-flight failure leaves
// no partial order and restores any quota decremented. Availability checks
// (supply/cutoff/archived) run outside the tx — read-only, no rollback needed.
func (s *ReorderService) Reorder(ctx context.Context, in ReorderInput) (*ReorderResult, error) {
	targetDay, err := time.Parse("2006-01-02", in.SupplyDate)
	if err != nil {
		return nil, fmt.Errorf("reorder: invalid supply_date %q: %w", in.SupplyDate, err)
	}
	src, err := s.validateReorderRequest(ctx, in)
	if err != nil {
		return nil, err
	}

	now := s.clock.Now()
	survivors, unavailable, err := s.classifyReorderItems(ctx, src.Items, targetDay, now)
	if err != nil {
		return nil, err
	}
	if len(survivors) == 0 {
		return &ReorderResult{NewOrderID: "", UnavailableItems: unavailable}, nil
	}

	o, err := s.buildReorderOrder(ctx, in, src, targetDay, now, survivors)
	if err != nil {
		return nil, err
	}
	if err := s.persistReorderTx(ctx, in, src, o, targetDay); err != nil {
		// Handler maps quota.ErrOutOfStock to 409.
		return nil, err
	}
	return &ReorderResult{NewOrderID: o.ID, UnavailableItems: unavailable}, nil
}

func (s *ReorderService) validateReorderRequest(ctx context.Context, in ReorderInput) (*Order, error) {
	src, err := s.orders.GetByID(ctx, in.SourceOrderID)
	if err != nil {
		return nil, err
	}
	if src.UserID != in.UserID {
		return nil, ErrForbidden
	}
	// Source vendor must still serve the employee's (possibly changed) plant.
	servingPlants, err := s.plants.ListByVendor(ctx, src.VendorID)
	if err != nil {
		return nil, err
	}
	for _, p := range servingPlants {
		if p.Plant == in.Plant {
			return src, nil
		}
	}
	return nil, ErrVendorPlantMismatch
}

func (s *ReorderService) classifyReorderItems(ctx context.Context, items []Item, targetDay, now time.Time) ([]reorderCandidate, []UnavailableItem, error) {
	survivors := make([]reorderCandidate, 0, len(items))
	unavailable := make([]UnavailableItem, 0)
	for _, it := range items {
		c, u, err := s.classifyOne(ctx, it, targetDay, now)
		if err != nil {
			return nil, nil, err
		}
		if u != nil {
			unavailable = append(unavailable, *u)
			continue
		}
		survivors = append(survivors, *c)
	}
	return survivors, unavailable, nil
}

func (s *ReorderService) classifyOne(ctx context.Context, it Item, targetDay, now time.Time) (*reorderCandidate, *UnavailableItem, error) {
	mi, err := s.items.GetByID(ctx, it.MenuItemID)
	if err != nil {
		// menu_item gone → treat as archived (no name to render).
		if errors.Is(err, ErrReorderItemNotFound) {
			return nil, &UnavailableItem{MenuItemID: it.MenuItemID, Reason: reasonArchived}, nil
		}
		return nil, nil, err
	}
	if mi.Archived {
		return nil, &UnavailableItem{MenuItemID: mi.ID, Name: mi.Name, Reason: reasonArchived}, nil
	}
	sup, err := s.supply.Get(ctx, mi.ID, targetDay)
	if err != nil {
		if errors.Is(err, ErrReorderSupplyNotFound) {
			return nil, &UnavailableItem{MenuItemID: mi.ID, Name: mi.Name, Reason: reasonNoSupply}, nil
		}
		return nil, nil, err
	}
	if !now.Before(sup.CutoffAt) {
		return nil, &UnavailableItem{MenuItemID: mi.ID, Name: mi.Name, Reason: reasonCutoffPassed}, nil
	}
	// Pre-check capacity from sup.Get so the partial-fallback UI can show
	// "out_of_quota" even when no order is created. The authoritative atomic
	// check still happens inside the tx via DecrementTx.
	if sup.Remain < it.Qty {
		return nil, &UnavailableItem{MenuItemID: mi.ID, Name: mi.Name, Reason: reasonOutOfQuota}, nil
	}
	return &reorderCandidate{item: mi, qty: it.Qty, price: mi.PriceMinor}, nil, nil
}

func (s *ReorderService) buildReorderOrder(ctx context.Context, in ReorderInput, src *Order, targetDay, now time.Time, survivors []reorderCandidate) (*Order, error) {
	domainItems := make([]Item, 0, len(survivors))
	var totalPrice int64
	for _, c := range survivors {
		domainItems = append(domainItems, Item{
			MenuItemID:     c.item.ID,
			Qty:            c.qty,
			UnitPriceMinor: c.price,
		})
		totalPrice += c.price * int64(c.qty)
	}
	// Order cutoff_at gates a later Modify; compute it the same way Service.Place
	// does (vendor cutoff hour, service tz, day before supply_date).
	v, err := s.vendors.GetByID(ctx, src.VendorID)
	if err != nil {
		return nil, err
	}
	loc := s.loc
	if loc == nil {
		loc = time.UTC
	}
	newCutoff := time.Date(targetDay.Year(), targetDay.Month(), targetDay.Day()-1, v.CutoffHour, 0, 0, 0, loc)
	placedAt := now
	return &Order{
		UserID:          in.UserID,
		VendorID:        src.VendorID,
		Plant:           in.Plant,
		SupplyDate:      targetDay,
		Status:          StatusPlaced,
		TotalPriceMinor: totalPrice,
		PlacedAt:        &placedAt,
		CutoffAt:        newCutoff,
		Items:           domainItems,
	}, nil
}

// persistReorderTx writes the reorder atomically: decrement quota for each
// survivor, insert order + items, state event + outbox + audit. Quota race on
// any survivor → ErrOutOfStock rolls back the whole tx (we don't try to drop
// one survivor and keep the rest — caller retries).
func (s *ReorderService) persistReorderTx(ctx context.Context, in ReorderInput, src *Order, o *Order, targetDay time.Time) error {
	role := "employee"
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, it := range o.Items {
			if _, err := s.supply.DecrementTx(ctx, tx, it.MenuItemID, targetDay, it.Qty); err != nil {
				return err
			}
		}
		if err := s.orders.CreateTx(ctx, tx, o); err != nil {
			return err
		}
		if err := s.state.AppendTx(ctx, tx, &StateEvent{
			OrderID:   o.ID,
			FromState: nil,
			ToState:   StatusPlaced,
			ActorID:   &in.UserID,
			ActorRole: &role,
			Reason:    "reorder",
			Payload:   map[string]any{"source_order_id": src.ID},
		}); err != nil {
			return err
		}
		payload := buildOrderPayload(o)
		payload["source_order_id"] = src.ID
		if err := s.outbox.AppendTx(ctx, tx, "order", o.ID, "order.placed.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.audit.WriteTx(ctx, tx, &in.UserID, &role, "order.reorder", "order", o.ID, payload, "")
	})
}
