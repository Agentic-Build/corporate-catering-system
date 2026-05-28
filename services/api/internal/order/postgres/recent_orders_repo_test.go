package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
)

func TestRecentOrdersRepo_ByUser_OrdersByFreqDescThenSupplyDateDesc(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	uid := seedUser(t, pool, "employee")
	vA := seedApprovedVendor(t, pool)
	vB := seedApprovedVendor(t, pool)
	vC := seedApprovedVendor(t, pool)
	iA := seedActiveMenuItem(t, pool, vA, 10000)
	iB := seedActiveMenuItem(t, pool, vB, 12000)
	iC := seedActiveMenuItem(t, pool, vC, 14000)

	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Helper: insert a materialised order (status='ready' so it is counted).
	mk := func(vendorID, itemID string, day time.Time) string {
		secret := make([]byte, 32)
		for i := range secret {
			secret[i] = 0xab
		}
		var oid string
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,$3,$4,'ready'::order_status,1000,$5,$6,$5,$5)
RETURNING id`,
			uid, vendorID, "F12B-3F", day, day.Add(10*time.Hour), secret).Scan(&oid))
		_, err := pool.Exec(ctx, `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1,$2,1,1000)`, oid, itemID)
		require.NoError(t, err)
		return oid
	}

	// Vendor A: 4 orders, most-recent = today
	mk(vA, iA, today.AddDate(0, 0, -5))
	mk(vA, iA, today.AddDate(0, 0, -3))
	mk(vA, iA, today.AddDate(0, 0, -1))
	vAMostRecent := mk(vA, iA, today)
	// Vendor B: 2 orders, most-recent = yesterday
	mk(vB, iB, today.AddDate(0, 0, -10))
	vBMostRecent := mk(vB, iB, today.AddDate(0, 0, -1))
	// Vendor C: 1 order, most-recent = 5 days ago
	vCMostRecent := mk(vC, iC, today.AddDate(0, 0, -5))
	// Older than 30 days → ignored
	mk(vA, iA, today.AddDate(0, 0, -45))

	repo := pgrepo.NewRecentOrdersRepo(pool)
	chips, err := repo.RecentOrdersByUser(ctx, uid, 10, 0)
	require.NoError(t, err)
	require.Len(t, chips, 3)

	// Order: A (freq=4), B (freq=2), C (freq=1)
	assert.Equal(t, vAMostRecent, chips[0].OrderID)
	assert.Equal(t, vA, chips[0].VendorID)
	assert.InDelta(t, 4.0, float64(chips[0].Freq), 0)

	assert.Equal(t, vBMostRecent, chips[1].OrderID)
	assert.Equal(t, vB, chips[1].VendorID)
	assert.InDelta(t, 2.0, float64(chips[1].Freq), 0)

	assert.Equal(t, vCMostRecent, chips[2].OrderID)
	assert.Equal(t, vC, chips[2].VendorID)
	assert.InDelta(t, 1.0, float64(chips[2].Freq), 0)
}

func TestRecentOrdersRepo_LimitAndOffsetPaginate(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	uid := seedUser(t, pool, "employee")
	vA := seedApprovedVendor(t, pool)
	vB := seedApprovedVendor(t, pool)
	vC := seedApprovedVendor(t, pool)
	iA := seedActiveMenuItem(t, pool, vA, 10000)
	iB := seedActiveMenuItem(t, pool, vB, 11000)
	iC := seedActiveMenuItem(t, pool, vC, 12000)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	mk := func(vendorID, itemID string, day time.Time) {
		secret := make([]byte, 32)
		_, err := pool.Exec(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,'F12B-3F',$3,'ready'::order_status,1000,$4,$5,$4,$4)`,
			uid, vendorID, day, day.Add(10*time.Hour), secret)
		require.NoError(t, err)
	}
	_ = iA
	_ = iB
	_ = iC

	// Each vendor: distinct freq so ordering is unambiguous.
	mk(vA, iA, today.AddDate(0, 0, -1)) // freq=1
	mk(vB, iB, today.AddDate(0, 0, -1)) // freq=2
	mk(vB, iB, today.AddDate(0, 0, -2))
	mk(vC, iC, today.AddDate(0, 0, -1)) // freq=3
	mk(vC, iC, today.AddDate(0, 0, -2))
	mk(vC, iC, today.AddDate(0, 0, -3))

	// Need order_item rows so the window-function CTE has something to join in
	// future variants — for now we only need rows on order_item if the query
	// joins; the current query is order-only.
	repo := pgrepo.NewRecentOrdersRepo(pool)
	page1, err := repo.RecentOrdersByUser(ctx, uid, 2, 0)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	assert.Equal(t, vC, page1[0].VendorID) // freq=3
	assert.Equal(t, vB, page1[1].VendorID) // freq=2

	page2, err := repo.RecentOrdersByUser(ctx, uid, 2, 2)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Equal(t, vA, page2[0].VendorID) // freq=1
}

func TestRecentOrdersRepo_ExcludesNonMaterialisedStatuses(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	uid := seedUser(t, pool, "employee")
	v := seedApprovedVendor(t, pool)
	i := seedActiveMenuItem(t, pool, v, 10000)
	_ = i
	today := time.Now().UTC().Truncate(24 * time.Hour)

	for _, status := range []string{"placed", "cancelled", "no_show"} {
		secret := make([]byte, 32)
		_, err := pool.Exec(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at)
VALUES ($1,$2,'F12B-3F',$3,$4::order_status,1000,$5,$6,$5)`,
			uid, v, today, status, today.Add(10*time.Hour), secret)
		require.NoError(t, err)
	}

	repo := pgrepo.NewRecentOrdersRepo(pool)
	chips, err := repo.RecentOrdersByUser(ctx, uid, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, chips)
}

func TestRecentOrdersRepo_GetByUserDate_Returns_Status_AndNilIfMissing(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	uid := seedUser(t, pool, "employee")
	v := seedApprovedVendor(t, pool)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	// No order yet → nil, no error.
	repo := pgrepo.NewRecentOrdersRepo(pool)
	got, err := repo.GetOrderByUserDate(ctx, uid, today, "F12B-3F")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Seed a placed order for the user today.
	secret := make([]byte, 32)
	var oid string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at)
VALUES ($1,$2,'F12B-3F',$3,'placed'::order_status,1000,$4,$5,$4) RETURNING id`,
		uid, v, today, today.Add(10*time.Hour), secret).Scan(&oid))

	got, err = repo.GetOrderByUserDate(ctx, uid, today, "F12B-3F")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, oid, got.OrderID)
	assert.Equal(t, "placed", got.Status)
	assert.Equal(t, int64(1000), got.TotalPriceMinor)
	assert.Equal(t, v, got.VendorID)

	// Plant filter must not match a different plant.
	gotOther, err := repo.GetOrderByUserDate(ctx, uid, today, "OTHER-PLANT")
	require.NoError(t, err)
	assert.Nil(t, gotOther)
}

func TestRecentOrdersRepo_ItemNamesAndAvailability(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	uid := seedUser(t, pool, "employee")
	v := seedApprovedVendor(t, pool)
	i1 := seedActiveMenuItem(t, pool, v, 10000)
	i2 := seedActiveMenuItem(t, pool, v, 11000)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	secret := make([]byte, 32)
	var oid string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,'F12B-3F',$3,'ready'::order_status,1000,$4,$5,$4,$4) RETURNING id`,
		uid, v, today, today.Add(10*time.Hour), secret).Scan(&oid))
	_, err := pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1,$2,1,1000),($1,$3,1,1000)`, oid, i1, i2)
	require.NoError(t, err)

	// One supply row → only i1 is "available today" for the target_day.
	_, err = pool.Exec(ctx, `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, 10, 5, '11:50-12:10','11:50-12:10', $3)`, i1, today, today.Add(10*time.Hour))
	require.NoError(t, err)

	repo := pgrepo.NewRecentOrdersRepo(pool)

	// Item names preview for the order, capped at 2.
	names, err := repo.ItemNamesByOrderIDs(ctx, []string{oid}, 2)
	require.NoError(t, err)
	require.Contains(t, names, oid)
	assert.Len(t, names[oid], 2)

	// Availability: at least one item has supply on target_day → AvailableToday=true.
	availability, err := repo.OrderAvailability(ctx, []string{oid}, today)
	require.NoError(t, err)
	assert.True(t, availability[oid])
}

func TestRecentOrdersRepo_OrderAvailability_FalseWhenNoSupplyExists(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	uid := seedUser(t, pool, "employee")
	v := seedApprovedVendor(t, pool)
	i := seedActiveMenuItem(t, pool, v, 10000)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	secret := make([]byte, 32)
	var oid string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order" (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,'F12B-3F',$3,'ready'::order_status,1000,$4,$5,$4,$4) RETURNING id`,
		uid, v, today, today.Add(10*time.Hour), secret).Scan(&oid))
	_, err := pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1,$2,1,1000)`, oid, i)
	require.NoError(t, err)

	repo := pgrepo.NewRecentOrdersRepo(pool)
	got, err := repo.OrderAvailability(ctx, []string{oid}, today)
	require.NoError(t, err)
	assert.False(t, got[oid])
}
