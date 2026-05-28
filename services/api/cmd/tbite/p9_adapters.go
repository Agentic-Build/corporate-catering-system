package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	mpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/readmodel"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
	qpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota/postgres"
)

// p9ItemRepoAdapter bridges *mpgrepo.ItemRepo into order.ReorderService's
// local item-repo interface, mapping menu.ErrItemNotFound →
// order.ErrReorderItemNotFound. Mirrors the test fixture in
// services/api/internal/order/reorder_service_test.go (kept in sync).
type p9ItemRepoAdapter struct{ inner *mpgrepo.ItemRepo }

func (a p9ItemRepoAdapter) GetByID(ctx context.Context, id string) (*order.ReorderMenuItem, error) {
	mi, err := a.inner.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, menu.ErrItemNotFound) {
			return nil, order.ErrReorderItemNotFound
		}
		return nil, err
	}
	return &order.ReorderMenuItem{
		ID:         mi.ID,
		Name:       mi.Name,
		PriceMinor: mi.PriceMinor,
		Archived:   mi.Status == menu.ItemStatusArchived || mi.ArchivedAt != nil,
	}, nil
}

// p9SupplyRepoAdapter bridges *qpgrepo.SupplyRepo into the reorder service,
// mapping quota.ErrSupplyNotFound → order.ErrReorderSupplyNotFound.
type p9SupplyRepoAdapter struct{ inner *qpgrepo.SupplyRepo }

func (a p9SupplyRepoAdapter) Get(ctx context.Context, itemID string, date time.Time) (*order.ReorderSupply, error) {
	s, err := a.inner.Get(ctx, itemID, date)
	if err != nil {
		if errors.Is(err, quota.ErrSupplyNotFound) {
			return nil, order.ErrReorderSupplyNotFound
		}
		return nil, err
	}
	return &order.ReorderSupply{Remain: s.Remain, CutoffAt: s.CutoffAt}, nil
}

func (a p9SupplyRepoAdapter) DecrementTx(ctx context.Context, tx pgx.Tx, itemID string, date time.Time, n int) (int, error) {
	return a.inner.DecrementTx(ctx, tx, itemID, date, n)
}

// cachedPopularityAdapter satisfies menu.PopularityForHome by serving
// PlantPopularity from the read-model cache (the only raw-orders aggregate)
// while delegating MetaByIDs / AllCutoffsPassed (menu_item / meal_supply
// lookups, not order aggregates) to the concrete repo.
type cachedPopularityAdapter struct {
	cached *readmodel.CachedPopularity
	repo   *mpgrepo.PopularityRepo
}

func (a cachedPopularityAdapter) PlantPopularity(ctx context.Context, plant string, day time.Time) (map[string]float64, error) {
	return a.cached.PlantPopularity(ctx, plant, day)
}

func (a cachedPopularityAdapter) MetaByIDs(ctx context.Context, ids []string) ([]menu.MenuItemMeta, error) {
	return a.repo.MetaByIDs(ctx, ids)
}

func (a cachedPopularityAdapter) AllCutoffsPassed(ctx context.Context, plant string, day, now time.Time) (bool, error) {
	return a.repo.AllCutoffsPassed(ctx, plant, day, now)
}
