package quota_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
)

// ----- Mocks -----

type supplyKey struct {
	itemID string
	date   time.Time
}

type fakeSupplyRepo struct {
	mu     sync.Mutex
	byKey  map[supplyKey]*quota.Supply
	byID   map[string]*quota.Supply
	nextID int
}

func newFakeSupplyRepo() *fakeSupplyRepo {
	return &fakeSupplyRepo{
		byKey: map[supplyKey]*quota.Supply{},
		byID:  map[string]*quota.Supply{},
	}
}

func (r *fakeSupplyRepo) Upsert(_ context.Context, s *quota.Supply) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := supplyKey{itemID: s.MenuItemID, date: s.SupplyDate}
	now := time.Now().UTC()
	if existing, ok := r.byKey[k]; ok {
		s.ID = existing.ID
		s.CreatedAt = existing.CreatedAt
	} else {
		r.nextID++
		s.ID = "sup-" + strconv.Itoa(r.nextID)
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	// Mirror the DB CHECK that remain <= capacity.
	if s.Remain > s.Capacity {
		s.Remain = s.Capacity
	}
	clone := *s
	r.byKey[k] = &clone
	r.byID[s.ID] = &clone
	return nil
}

func (r *fakeSupplyRepo) Get(_ context.Context, itemID string, date time.Time) (*quota.Supply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byKey[supplyKey{itemID: itemID, date: date}]; ok {
		clone := *s
		return &clone, nil
	}
	return nil, quota.ErrSupplyNotFound
}

func (r *fakeSupplyRepo) ListByVendor(_ context.Context, vendorID string, date time.Time) ([]*quota.Supply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*quota.Supply
	for _, s := range r.byKey {
		if s.SupplyDate.Equal(date) && itemVendorIndex[s.MenuItemID] == vendorID {
			clone := *s
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (r *fakeSupplyRepo) Decrement(_ context.Context, itemID string, date time.Time, n int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byKey[supplyKey{itemID: itemID, date: date}]
	if !ok {
		return 0, quota.ErrSupplyNotFound
	}
	if s.Remain < n {
		return 0, quota.ErrOutOfStock
	}
	s.Remain -= n
	return s.Remain, nil
}

func (r *fakeSupplyRepo) Restore(_ context.Context, itemID string, date time.Time, n int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byKey[supplyKey{itemID: itemID, date: date}]
	if !ok {
		return quota.ErrSupplyNotFound
	}
	s.Remain += n
	if s.Remain > s.Capacity {
		s.Remain = s.Capacity
	}
	return nil
}

func (r *fakeSupplyRepo) SetSoldOut(_ context.Context, itemID string, date time.Time, soldOut bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byKey[supplyKey{itemID: itemID, date: date}]
	if !ok {
		return quota.ErrSupplyNotFound
	}
	s.SoldOut = soldOut
	return nil
}

// itemVendorIndex is a tiny side-channel used by fakeSupplyRepo.ListByVendor
// (which needs to know which vendor owns each item) and shared with fakeItemRepo.
var itemVendorIndex = map[string]string{}

type fakeItemRepo struct {
	mu   sync.Mutex
	byID map[string]*menu.Item
}

func newFakeItemRepo() *fakeItemRepo {
	return &fakeItemRepo{byID: map[string]*menu.Item{}}
}

func (r *fakeItemRepo) seed(id, vendorID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[id] = &menu.Item{ID: id, VendorID: vendorID, Status: menu.ItemStatusActive}
	itemVendorIndex[id] = vendorID
}

func (r *fakeItemRepo) Create(_ context.Context, _ *menu.Item) error {
	return nil
}
func (r *fakeItemRepo) Update(_ context.Context, _ *menu.Item) error { return nil }
func (r *fakeItemRepo) SetStatus(_ context.Context, _ string, _ menu.ItemStatus) error {
	return nil
}
func (r *fakeItemRepo) GetByID(_ context.Context, id string) (*menu.Item, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i, ok := r.byID[id]; ok {
		return i, nil
	}
	return nil, menu.ErrItemNotFound
}
func (r *fakeItemRepo) ListByVendor(_ context.Context, _ string, _ bool) ([]*menu.MerchantItemRow, error) {
	return nil, nil
}
func (r *fakeItemRepo) ListActiveByPlant(_ context.Context, _ menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	return nil, nil
}

// ----- Helpers -----

func newSvc(t *testing.T) (*quota.Service, *fakeSupplyRepo, *fakeItemRepo) {
	t.Helper()
	// Reset shared index between tests to avoid leaking vendor bindings.
	itemVendorIndex = map[string]string{}
	sr := newFakeSupplyRepo()
	ir := newFakeItemRepo()
	return &quota.Service{Supplies: sr, Items: ir}, sr, ir
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ----- Tests -----

func TestService_SetCapacity_NewSupply_RemainEqualsCapacity(t *testing.T) {
	svc, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	ctx := context.Background()
	d := day(2026, 5, 14)
	cutoff := time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC)

	got, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID:   "item-1",
		Date:         d,
		Capacity:     100,
		PickupWindow: "12:00-12:30",
		ETALabel:     "10 分鐘後可取",
		CutoffAt:     cutoff,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, 100, got.Capacity)
	assert.Equal(t, 100, got.Remain)
	assert.Equal(t, "12:00-12:30", got.PickupWindow)
}

func TestService_SetCapacity_WrongVendor_Forbidden(t *testing.T) {
	svc, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	_, err := svc.SetCapacity(context.Background(), "v-other", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: day(2026, 5, 14), Capacity: 50,
	})
	assert.ErrorIs(t, err, menu.ErrForbidden)
}

func TestService_SetCapacity_LowerCapacity_ClampsRemainDown(t *testing.T) {
	svc, sr, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	ctx := context.Background()
	d := day(2026, 5, 14)

	// First: capacity=100, remain=100.
	_, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 100,
	})
	require.NoError(t, err)
	// Simulate 30 sales — remain now 70.
	_, err = sr.Decrement(ctx, "item-1", d, 30)
	require.NoError(t, err)

	// Now drop capacity to 50: sold=30, so remain = 50-30 = 20.
	got, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, 50, got.Capacity)
	assert.Equal(t, 20, got.Remain)
}

func TestService_SetCapacity_HigherCapacity_RemainStays(t *testing.T) {
	svc, sr, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	ctx := context.Background()
	d := day(2026, 5, 14)

	// First: capacity=100.
	_, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 100,
	})
	require.NoError(t, err)
	// Sell 40 — remain 60.
	_, err = sr.Decrement(ctx, "item-1", d, 40)
	require.NoError(t, err)

	// Raise capacity to 200: sold=40, so remain = 200-40 = 160.
	got, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 200,
	})
	require.NoError(t, err)
	assert.Equal(t, 200, got.Capacity)
	assert.Equal(t, 160, got.Remain)
}

func TestService_GetForItem_WrongVendor_Forbidden(t *testing.T) {
	svc, sr, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	d := day(2026, 5, 14)
	require.NoError(t, sr.Upsert(context.Background(), &quota.Supply{
		MenuItemID: "item-1", SupplyDate: d, Capacity: 10, Remain: 10,
	}))
	_, err := svc.GetForItem(context.Background(), "v-other", "item-1", d)
	assert.ErrorIs(t, err, menu.ErrForbidden)
}

func TestService_ListForVendor_ReturnsSupplies(t *testing.T) {
	svc, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	ir.seed("item-2", "v-owner")
	ir.seed("item-3", "v-other")
	ctx := context.Background()
	d := day(2026, 5, 14)

	for _, id := range []string{"item-1", "item-2", "item-3"} {
		_, err := svc.SetCapacity(ctx, vendorOf(id), quota.SetCapacityInput{
			MenuItemID: id, Date: d, Capacity: 5,
		})
		require.NoError(t, err)
	}

	got, err := svc.ListForVendor(ctx, "v-owner", d)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	for _, s := range got {
		assert.Contains(t, []string{"item-1", "item-2"}, s.MenuItemID)
	}
}

func vendorOf(itemID string) string {
	return itemVendorIndex[itemID]
}

func TestService_SetSoldOut(t *testing.T) {
	svc, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	ctx := context.Background()
	d := day(2026, 5, 14)
	_, err := svc.SetCapacity(ctx, "v-owner", quota.SetCapacityInput{
		MenuItemID: "item-1", Date: d, Capacity: 50,
		CutoffAt: time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	sp, err := svc.SetSoldOut(ctx, "v-owner", "item-1", d, true)
	require.NoError(t, err)
	assert.True(t, sp.SoldOut)
	assert.Equal(t, 50, sp.Capacity, "capacity untouched")
	assert.Equal(t, 50, sp.Remain, "remain untouched")

	sp, err = svc.SetSoldOut(ctx, "v-owner", "item-1", d, false)
	require.NoError(t, err)
	assert.False(t, sp.SoldOut)
}

func TestService_SetSoldOut_WrongVendorForbidden(t *testing.T) {
	svc, _, ir := newSvc(t)
	ir.seed("item-1", "v-owner")
	_, err := svc.SetSoldOut(context.Background(), "v-other", "item-1", day(2026, 5, 14), true)
	assert.ErrorIs(t, err, menu.ErrForbidden)
}
