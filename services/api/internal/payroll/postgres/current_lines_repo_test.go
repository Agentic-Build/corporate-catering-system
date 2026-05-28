package postgres_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/postgres"
)

var menuItemSeedCounter atomic.Uint64

// seedMenuItem inserts an active menu item under vendor and returns its UUID.
func seedMenuItem(t *testing.T, pool *pgxpool.Pool, vendorID, name string, priceMinor int64) string {
	t.Helper()
	menuItemSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO menu_item (vendor_id, name, price_minor, status)
VALUES ($1, $2, $3, 'active')
RETURNING id`, vendorID, name, priceMinor).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedOrderWithStatus inserts an order in the given status on supplyDate and
// returns its UUID. Mirrors seedOrder but lets the caller pin status/date.
func seedOrderWithStatus(t *testing.T, pool *pgxpool.Pool, userID, vendorID, status string, supplyDate time.Time, totalMinor int64) string {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	day := supplyDate.UTC().Truncate(24 * time.Hour)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at, totp_secret)
VALUES ($1,$2,$3,$4,$5::order_status,$6,now(),$7,$8)
RETURNING id`,
		userID, vendorID, "F12B-3F", day, status, totalMinor, day.Add(10*time.Hour), secret,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedOrderItem adds an order_item line to an order.
func seedOrderItem(t *testing.T, pool *pgxpool.Pool, orderID, menuItemID string, qty int, unitMinor int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1, $2, $3, $4)`, orderID, menuItemID, qty, unitMinor)
	require.NoError(t, err)
}

// seedRating inserts a meal_rating row for an order.
func seedRating(t *testing.T, pool *pgxpool.Pool, orderID, userID, vendorID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO meal_rating (order_id, user_id, vendor_id, score, comment)
VALUES ($1, $2, $3, 5, 'good')`, orderID, userID, vendorID)
	require.NoError(t, err)
}

// seedComplaint inserts a meal_complaint row and returns its UUID.
func seedComplaint(t *testing.T, pool *pgxpool.Pool, orderID, userID, vendorID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO meal_complaint (order_id, user_id, vendor_id, category, description)
VALUES ($1, $2, $3, 'quality', 'cold meal')
RETURNING id`, orderID, userID, vendorID).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedLockedBatch inserts a locked batch over [start,end].
func seedLockedBatch(t *testing.T, pool *pgxpool.Pool, start, end time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO payroll_batch (period_start, period_end, status, locked_at)
VALUES ($1, $2, 'locked', now())`, start, end)
	require.NoError(t, err)
}

func TestCurrentLinesRepo_BasicChargedLine(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewCurrentLinesRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	item := seedMenuItem(t, pool, vendor, "Chicken Rice", 8000)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := seedOrderWithStatus(t, pool, user, vendor, "picked_up", day, 8000)
	seedOrderItem(t, pool, o, item, 1, 8000)

	lines, err := repo.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	l := lines[0]
	assert.Equal(t, o, l.OrderID)
	assert.Equal(t, day.Format("2006-01-02"), l.SupplyDate)
	assert.Contains(t, l.VendorName, "payroll-vendor-")
	assert.Equal(t, "1x Chicken Rice", l.ItemsSummary)
	assert.Equal(t, int64(8000), l.AmountMinor)
	assert.Equal(t, "charged", l.Status)
	assert.False(t, l.Rated)
	assert.Nil(t, l.ComplaintID)
}

func TestCurrentLinesRepo_StatusMapping(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewCurrentLinesRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	item := seedMenuItem(t, pool, vendor, "Beef Noodle", 12000)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	charged := seedOrderWithStatus(t, pool, user, vendor, "picked_up", day, 12000)
	seedOrderItem(t, pool, charged, item, 1, 12000)
	noShow := seedOrderWithStatus(t, pool, user, vendor, "no_show", day, 12000)
	seedOrderItem(t, pool, noShow, item, 1, 12000)
	reversed := seedOrderWithStatus(t, pool, user, vendor, "refunded", day, 12000)
	seedOrderItem(t, pool, reversed, item, 1, 12000)

	lines, err := repo.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 3)

	byOrder := map[string]string{}
	for _, l := range lines {
		byOrder[l.OrderID] = l.Status
	}
	assert.Equal(t, "charged", byOrder[charged])
	assert.Equal(t, "no_show", byOrder[noShow])
	assert.Equal(t, "reversed", byOrder[reversed])
}

func TestCurrentLinesRepo_ExcludesLockedPeriod(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewCurrentLinesRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	item := seedMenuItem(t, pool, vendor, "Dumplings", 6000)

	// Locked batch covers Jan 2026.
	janStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	janEnd := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC)
	seedLockedBatch(t, pool, janStart, janEnd)

	// Order inside the locked period — must be excluded.
	old := seedOrderWithStatus(t, pool, user, vendor, "picked_up", janStart.AddDate(0, 0, 10), 6000)
	seedOrderItem(t, pool, old, item, 1, 6000)
	// Order after the locked period — must be included.
	cur := seedOrderWithStatus(t, pool, user, vendor, "picked_up", janEnd.AddDate(0, 0, 5), 6000)
	seedOrderItem(t, pool, cur, item, 1, 6000)

	lines, err := repo.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, cur, lines[0].OrderID)
}

func TestCurrentLinesRepo_OnlyOwnOrdersAndChargeableStatuses(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewCurrentLinesRepo(pool)

	user := seedEmployeeUser(t, pool)
	other := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	item := seedMenuItem(t, pool, vendor, "Salad", 5000)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	mine := seedOrderWithStatus(t, pool, user, vendor, "picked_up", day, 5000)
	seedOrderItem(t, pool, mine, item, 1, 5000)
	// Another employee's order — must not appear.
	notMine := seedOrderWithStatus(t, pool, other, vendor, "picked_up", day, 5000)
	seedOrderItem(t, pool, notMine, item, 1, 5000)
	// Non-chargeable statuses — must not appear.
	placed := seedOrderWithStatus(t, pool, user, vendor, "placed", day, 5000)
	seedOrderItem(t, pool, placed, item, 1, 5000)
	cancelled := seedOrderWithStatus(t, pool, user, vendor, "cancelled", day, 5000)
	seedOrderItem(t, pool, cancelled, item, 1, 5000)

	lines, err := repo.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, mine, lines[0].OrderID)
}

func TestCurrentLinesRepo_RatedAndComplaint(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewCurrentLinesRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	item := seedMenuItem(t, pool, vendor, "Curry", 9000)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	rated := seedOrderWithStatus(t, pool, user, vendor, "picked_up", day, 9000)
	seedOrderItem(t, pool, rated, item, 1, 9000)
	seedRating(t, pool, rated, user, vendor)

	complained := seedOrderWithStatus(t, pool, user, vendor, "picked_up", day, 9000)
	seedOrderItem(t, pool, complained, item, 1, 9000)
	complaintID := seedComplaint(t, pool, complained, user, vendor)

	lines, err := repo.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 2)

	byOrder := map[string]struct {
		rated     bool
		complaint *string
	}{}
	for _, l := range lines {
		byOrder[l.OrderID] = struct {
			rated     bool
			complaint *string
		}{l.Rated, l.ComplaintID}
	}
	assert.True(t, byOrder[rated].rated)
	assert.Nil(t, byOrder[rated].complaint)
	assert.False(t, byOrder[complained].rated)
	require.NotNil(t, byOrder[complained].complaint)
	assert.Equal(t, complaintID, *byOrder[complained].complaint)
}

func TestCurrentLinesRepo_ItemsSummaryMultipleItems(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewCurrentLinesRepo(pool)

	user := seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := seedOrderWithStatus(t, pool, user, vendor, "picked_up", day, 20000)
	a := seedMenuItem(t, pool, vendor, "Apple Pie", 5000)
	b := seedMenuItem(t, pool, vendor, "Banana Split", 5000)
	seedOrderItem(t, pool, o, a, 2, 5000)
	seedOrderItem(t, pool, o, b, 1, 5000)

	lines, err := repo.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	// Both item names present in the summary.
	assert.Contains(t, lines[0].ItemsSummary, "Apple Pie")
	assert.Contains(t, lines[0].ItemsSummary, "Banana Split")
}

func TestCurrentLinesRepo_Empty(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewCurrentLinesRepo(pool)

	user := seedEmployeeUser(t, pool)
	lines, err := repo.ListCurrentLines(ctx, user)
	require.NoError(t, err)
	assert.Empty(t, lines)
}
