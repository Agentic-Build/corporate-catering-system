package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
)

func newOrder(t *testing.T, userID, vendorID, itemID string, supplyDate time.Time) *order.Order {
	t.Helper()
	// totp_secret is NOT NULL on the order table; tests that build orders manually
	// need a placeholder secret so CreateTx's $9 param is valid.
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	return &order.Order{
		UserID:          userID,
		VendorID:        vendorID,
		Plant:           "F12B-3F",
		SupplyDate:      supplyDate,
		Status:          order.StatusDraft,
		TotalPriceMinor: 24000,
		TOTPSecret:      secret,
		CutoffAt:        supplyDate.Add(10 * time.Hour),
		Items: []order.Item{
			{MenuItemID: itemID, Qty: 2, UnitPriceMinor: 12000},
		},
	}
}

func TestOrderRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)

	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := newOrder(t, uid, vid, iid, day)

	require.NoError(t, repo.Create(ctx, o))
	require.NotEmpty(t, o.ID)
	require.NotEmpty(t, o.Items[0].ID)
	assert.Equal(t, o.ID, o.Items[0].OrderID)

	got, err := repo.GetByID(ctx, o.ID)
	require.NoError(t, err)
	assert.Equal(t, o.UserID, got.UserID)
	assert.Equal(t, o.VendorID, got.VendorID)
	assert.Equal(t, order.StatusDraft, got.Status)
	assert.Equal(t, int64(24000), got.TotalPriceMinor)
	require.Len(t, got.Items, 1)
	assert.Equal(t, 2, got.Items[0].Qty)
	assert.Equal(t, int64(12000), got.Items[0].UnitPriceMinor)
}

func TestOrderRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewOrderRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

func TestOrderRepo_UpdateStatus_Conditional(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)

	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := newOrder(t, uid, vid, iid, day)
	require.NoError(t, repo.Create(ctx, o))

	// Happy: draft → placed
	require.NoError(t, repo.UpdateStatus(ctx, o.ID, order.StatusDraft, order.StatusPlaced, nil, nil, ""))

	got, _ := repo.GetByID(ctx, o.ID)
	assert.Equal(t, order.StatusPlaced, got.Status)

	// Conflict: trying draft → placed again must fail (already placed)
	err := repo.UpdateStatus(ctx, o.ID, order.StatusDraft, order.StatusPlaced, nil, nil, "")
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestOrderRepo_UpdateStatus_CancelledSetsCancelledAt(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)

	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := newOrder(t, uid, vid, iid, day)
	o.Status = order.StatusPlaced
	now := time.Now()
	o.PlacedAt = &now
	require.NoError(t, repo.Create(ctx, o))

	require.NoError(t, repo.UpdateStatus(ctx, o.ID, order.StatusPlaced, order.StatusCancelled, nil, nil, "user_cancel"))
	got, _ := repo.GetByID(ctx, o.ID)
	assert.Equal(t, order.StatusCancelled, got.Status)
	require.NotNil(t, got.CancelledAt)
}

func TestOrderRepo_ListByUser_OrdersBySupplyDateDesc(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	repo := pgrepo.NewOrderRepo(pool)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	o1 := newOrder(t, uid, vid, iid, today)
	o2 := newOrder(t, uid, vid, iid, today.AddDate(0, 0, 1))
	o3 := newOrder(t, uid, vid, iid, today.AddDate(0, 0, -1))
	require.NoError(t, repo.Create(ctx, o1))
	require.NoError(t, repo.Create(ctx, o2))
	require.NoError(t, repo.Create(ctx, o3))

	// since today → o3 (yesterday) excluded
	list, err := repo.ListByUser(ctx, uid, today)
	require.NoError(t, err)
	require.Len(t, list, 2)
	// First is tomorrow (later supply_date), then today
	assert.Equal(t, o2.ID, list[0].ID)
	assert.Equal(t, o1.ID, list[1].ID)
}

func TestOrderRepo_ListPlacedDueForCutoff(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	repo := pgrepo.NewOrderRepo(pool)

	now := time.Now().UTC()
	pastCutoff := now.Add(-1 * time.Hour)
	futureCutoff := now.Add(1 * time.Hour)
	day := now.Truncate(24 * time.Hour)

	// placed, cutoff in the past → expected to appear
	placedPast := newOrder(t, uid, vid, iid, day)
	placedPast.Status = order.StatusPlaced
	pt := now.Add(-2 * time.Hour)
	placedPast.PlacedAt = &pt
	placedPast.CutoffAt = pastCutoff
	require.NoError(t, repo.Create(ctx, placedPast))

	// placed, cutoff in the future → excluded
	placedFuture := newOrder(t, uid, vid, iid, day)
	placedFuture.Status = order.StatusPlaced
	placedFuture.PlacedAt = &pt
	placedFuture.CutoffAt = futureCutoff
	require.NoError(t, repo.Create(ctx, placedFuture))

	// draft, cutoff in the past → excluded (status filter)
	draftPast := newOrder(t, uid, vid, iid, day)
	draftPast.CutoffAt = pastCutoff
	require.NoError(t, repo.Create(ctx, draftPast))

	got, err := repo.ListPlacedDueForCutoff(ctx, now)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, placedPast.ID, got[0].ID)
}
