package quota_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
)

// errItemRepo returns a fixed error from GetByID to exercise lookup-failure
// branches in the service. All other methods are unused.
type errItemRepo struct {
	err error
}

func (r *errItemRepo) Create(context.Context, *menu.Item) error                 { return nil }
func (r *errItemRepo) Update(context.Context, *menu.Item) error                 { return nil }
func (r *errItemRepo) SetStatus(context.Context, string, menu.ItemStatus) error { return nil }
func (r *errItemRepo) GetByID(context.Context, string) (*menu.Item, error) {
	return nil, r.err
}
func (r *errItemRepo) ListByVendor(context.Context, string, bool) ([]*menu.MerchantItemRow, error) {
	return nil, nil
}
func (r *errItemRepo) ListActiveByPlant(context.Context, menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	return nil, nil
}

// stubSupplyRepo lets each method return a configured error / value so the
// service's error-propagation branches can be driven independently.
type stubSupplyRepo struct {
	getErr     error
	getSupply  *quota.Supply
	upsertErr  error
	soldOutErr error
}

func (r *stubSupplyRepo) Upsert(context.Context, *quota.Supply) error { return r.upsertErr }
func (r *stubSupplyRepo) Get(context.Context, string, time.Time) (*quota.Supply, error) {
	return r.getSupply, r.getErr
}
func (r *stubSupplyRepo) ListByVendor(context.Context, string, time.Time) ([]*quota.Supply, error) {
	return nil, nil
}
func (r *stubSupplyRepo) Decrement(context.Context, string, time.Time, int) (int, error) {
	return 0, nil
}
func (r *stubSupplyRepo) Restore(context.Context, string, time.Time, int) error { return nil }
func (r *stubSupplyRepo) SetSoldOut(context.Context, string, time.Time, bool) error {
	return r.soldOutErr
}

var errBoom = errors.New("boom")

// --- SetCapacity error paths ---

func TestService_SetCapacity_ItemLookupError_Propagates(t *testing.T) {
	svc := &quota.Service{
		Supplies: &stubSupplyRepo{},
		Items:    &errItemRepo{err: errBoom},
	}
	_, err := svc.SetCapacity(context.Background(), "v", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: day(2026, 5, 14), Capacity: 10,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_SetCapacity_SupplyGetError_Propagates(t *testing.T) {
	_, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	// Owned item, but the existing-supply lookup fails with a non-NotFound error.
	svc := &quota.Service{
		Supplies: &stubSupplyRepo{getErr: errBoom},
		Items:    ir,
	}
	_, err := svc.SetCapacity(context.Background(), "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: day(2026, 5, 14), Capacity: 10,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestService_SetCapacity_UpsertError_Propagates(t *testing.T) {
	_, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	// Existing supply lookup returns NotFound (new supply), then Upsert fails.
	svc := &quota.Service{
		Supplies: &stubSupplyRepo{getErr: quota.ErrSupplyNotFound, upsertErr: errBoom},
		Items:    ir,
	}
	_, err := svc.SetCapacity(context.Background(), "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: day(2026, 5, 14), Capacity: 10,
	})
	assert.ErrorIs(t, err, errBoom)
}

// TestService_SetCapacity_OversoldClampsRemainToZero drives the newRemain<0
// clamp: existing capacity 10 fully sold (remain 0 => sold 10), new capacity 5
// gives 5-10 = -5, which must clamp to 0.
func TestService_SetCapacity_OversoldClampsRemainToZero(t *testing.T) {
	svc, sr, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	ctx := context.Background()
	d := day(2026, 5, 14)

	_, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 10,
	})
	require.NoError(t, err)
	// Sell all 10 — remain 0, sold 10.
	_, err = sr.Decrement(ctx, "item-1", d, 10)
	require.NoError(t, err)

	got, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, 5, got.Capacity)
	assert.Equal(t, 0, got.Remain, "remain clamped to 0, never negative")
}

// TestService_SetCapacity_SameCapacity_NoEvent re-upserts with an unchanged
// capacity so emitSupplyAdjusted hits its delta==0 early return.
func TestService_SetCapacity_SameCapacity_NoEvent(t *testing.T) {
	svc, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	ctx := context.Background()
	d := day(2026, 5, 14)

	_, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 30,
	})
	require.NoError(t, err)
	// Same capacity again: delta is 0, no supply-adjusted event emitted.
	got, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 30,
	})
	require.NoError(t, err)
	assert.Equal(t, 30, got.Capacity)
	assert.Equal(t, 30, got.Remain)
}

// --- SetSoldOut error paths ---

func TestService_SetSoldOut_ItemLookupError_Propagates(t *testing.T) {
	svc := &quota.Service{
		Supplies: &stubSupplyRepo{},
		Items:    &errItemRepo{err: errBoom},
	}
	_, err := svc.SetSoldOut(context.Background(), "v", "item-1", day(2026, 5, 14), true)
	assert.ErrorIs(t, err, errBoom)
}

func TestService_SetSoldOut_RepoError_Propagates(t *testing.T) {
	_, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	svc := &quota.Service{
		Supplies: &stubSupplyRepo{soldOutErr: errBoom},
		Items:    ir,
	}
	_, err := svc.SetSoldOut(context.Background(), "v-owner", "item-1", day(2026, 5, 14), true)
	assert.ErrorIs(t, err, errBoom)
}

// --- GetForItem paths ---

func TestService_GetForItem_ItemLookupError_Propagates(t *testing.T) {
	svc := &quota.Service{
		Supplies: &stubSupplyRepo{},
		Items:    &errItemRepo{err: errBoom},
	}
	_, err := svc.GetForItem(context.Background(), "v", "item-1", day(2026, 5, 14))
	assert.ErrorIs(t, err, errBoom)
}

func TestService_GetForItem_Owned_ReturnsSupply(t *testing.T) {
	svc, sr, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	d := day(2026, 5, 14)
	require.NoError(t, sr.Upsert(context.Background(), &quota.Supply{
		MenuItemID: "item-1", SupplyDate: d, Capacity: 10, Remain: 7,
	}))

	got, err := svc.GetForItem(context.Background(), "v-owner", "item-1", d)
	require.NoError(t, err)
	assert.Equal(t, "item-1", got.MenuItemID)
	assert.Equal(t, 10, got.Capacity)
	assert.Equal(t, 7, got.Remain)
}
