package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
	qpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
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
