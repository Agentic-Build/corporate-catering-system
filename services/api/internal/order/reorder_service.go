package order

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// ReorderMenuItem is the menu_item view ReorderService needs. We mirror just
// the fields used here to avoid an import cycle with the menu package (which
// already imports order indirectly via P9 Task 2's favorites wiring).
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
// Concrete *opg.OrderRepo satisfies this via structural typing.
type reorderOrderRepo interface {
	GetByID(ctx context.Context, id string) (*Order, error)
	CreateTx(ctx context.Context, tx pgx.Tx, o *Order) error
}

// reorderSupplyRepo is the smallest meal_supply surface ReorderService needs.
// Callers adapt their concrete *qpg.SupplyRepo with a tiny shim that maps
// quota.ErrSupplyNotFound → ErrReorderSupplyNotFound.
type reorderSupplyRepo interface {
	Get(ctx context.Context, itemID string, date time.Time) (*ReorderSupply, error)
	DecrementTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) (int, error)
}

// reorderItemRepo is the smallest menu_item surface ReorderService needs.
// Callers adapt their concrete *mpg.ItemRepo with a tiny shim.
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

// NewReorderService wires the service. Each repo argument can be the concrete
// type already in use elsewhere (e.g. *opg.OrderRepo, *qpg.SupplyRepo) — Go's
// structural typing lets them satisfy the small local interfaces above.
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

// Reorder clones the source order's surviving items into a new placed order.
// The whole survivors-decrement-and-insert path runs in a single transaction
// so a mid-flight failure leaves no partial order and restores any quota that
// was decremented. Availability checks (supply/cutoff/archived) run outside
// the tx — those are read-only and never produce DB writes that would need
// rolling back.
func (s *ReorderService) Reorder(ctx context.Context, in ReorderInput) (*ReorderResult, error) {
	targetDay, err := time.Parse("2006-01-02", in.SupplyDate)
	if err != nil {
		return nil, fmt.Errorf("reorder: invalid supply_date %q: %w", in.SupplyDate, err)
	}

	src, err := s.orders.GetByID(ctx, in.SourceOrderID)
	if err != nil {
		return nil, err
	}
	if src.UserID != in.UserID {
		return nil, ErrForbidden
	}

	// The source vendor must still serve the employee's (possibly changed)
	// plant — mirror Service.Place so a transferred employee can't reorder from
	// a vendor that no longer serves their new plant.
	servingPlants, err := s.plants.ListByVendor(ctx, src.VendorID)
	if err != nil {
		return nil, err
	}
	served := false
	for _, p := range servingPlants {
		if p.Plant == in.Plant {
			served = true
			break
		}
	}
	if !served {
		return nil, ErrVendorPlantMismatch
	}

	// First pass: classify each source item without touching quota. This lets
	// us decide whether to open a transaction at all, and what items it should
	// contain.
	type candidate struct {
		item  *ReorderMenuItem
		qty   int
		price int64
	}
	survivors := make([]candidate, 0, len(src.Items))
	unavailable := make([]UnavailableItem, 0)
	now := s.clock.Now()

	for _, it := range src.Items {
		mi, err := s.items.GetByID(ctx, it.MenuItemID)
		if err != nil {
			// If the menu_item row is gone we can't even render a name; treat
			// as archived (closest semantic match — item is no longer orderable).
			if errors.Is(err, ErrReorderItemNotFound) {
				unavailable = append(unavailable, UnavailableItem{
					MenuItemID: it.MenuItemID,
					Name:       "",
					Reason:     reasonArchived,
				})
				continue
			}
			return nil, err
		}
		if mi.Archived {
			unavailable = append(unavailable, UnavailableItem{
				MenuItemID: mi.ID,
				Name:       mi.Name,
				Reason:     reasonArchived,
			})
			continue
		}
		sup, err := s.supply.Get(ctx, mi.ID, targetDay)
		if err != nil {
			if errors.Is(err, ErrReorderSupplyNotFound) {
				unavailable = append(unavailable, UnavailableItem{
					MenuItemID: mi.ID,
					Name:       mi.Name,
					Reason:     reasonNoSupply,
				})
				continue
			}
			return nil, err
		}
		if !now.Before(sup.CutoffAt) {
			unavailable = append(unavailable, UnavailableItem{
				MenuItemID: mi.ID,
				Name:       mi.Name,
				Reason:     reasonCutoffPassed,
			})
			continue
		}
		// Pre-check capacity using the snapshot from supply.Get so the partial-
		// fallback UI can show "out_of_quota" even when no order is ever created.
		// The authoritative atomic check still happens inside the tx via
		// DecrementTx; if a concurrent placement exhausts the quota between the
		// snapshot read and the tx, DecrementTx returns ErrOutOfStock and the
		// whole tx rolls back (no partial state).
		if sup.Remain < it.Qty {
			unavailable = append(unavailable, UnavailableItem{
				MenuItemID: mi.ID,
				Name:       mi.Name,
				Reason:     reasonOutOfQuota,
			})
			continue
		}
		survivors = append(survivors, candidate{item: mi, qty: it.Qty, price: mi.PriceMinor})
	}

	if len(survivors) == 0 {
		return &ReorderResult{NewOrderID: "", UnavailableItems: unavailable}, nil
	}

	// Build the new order shell. ID/CreatedAt/UpdatedAt are filled in by CreateTx.
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
	placedAt := now
	// Order-level cutoff_at gates a later Modify (Service.Modify), so compute it
	// the same way Service.Place does — the vendor's configured cutoff hour, in
	// the service timezone, the day before the supply date — rather than a
	// hardcoded prev-day 17:00 UTC that ignores vendor settings and locale.
	v, err := s.vendors.GetByID(ctx, src.VendorID)
	if err != nil {
		return nil, err
	}
	loc := s.loc
	if loc == nil {
		loc = time.UTC
	}
	newCutoff := time.Date(targetDay.Year(), targetDay.Month(), targetDay.Day()-1, v.CutoffHour, 0, 0, 0, loc)
	o := &Order{
		UserID:          in.UserID,
		VendorID:        src.VendorID,
		Plant:           in.Plant,
		SupplyDate:      targetDay,
		Status:          StatusPlaced,
		TotalPriceMinor: totalPrice,
		PlacedAt:        &placedAt,
		CutoffAt:        newCutoff,
		Items:           domainItems,
	}

	// One transaction: decrement quota for every survivor, insert order +
	// items, write state event + outbox + audit. If quota for any survivor is
	// exhausted between the read-only check and the tx, DecrementTx returns
	// ErrOutOfStock and the whole tx rolls back, releasing any earlier
	// decrements and leaving no partial state behind. The caller then sees the
	// error and can retry — we deliberately do NOT convert this race to a
	// per-item "out_of_quota" entry because there is no safe way to drop one
	// survivor and keep the rest without re-running the whole pipeline.
	role := "employee"
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
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
	if err != nil {
		// If quota was exhausted between the pre-check and the tx, surface a
		// clean error to the caller — handler maps quota.ErrOutOfStock to 409.
		return nil, err
	}

	return &ReorderResult{NewOrderID: o.ID, UnavailableItems: unavailable}, nil
}
