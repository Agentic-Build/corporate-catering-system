package menu_test

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	mpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
	opgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/clock"
)

// ---------- Postgres testcontainer boilerplate (local to this _test package) ----------

func setupHomePostgres(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start pg: %v", err)
	}
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("conn string: %v", err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/menu/home_service_test.go → ../../../../migrations
	migDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
	m, err := migrate.New("file://"+migDir, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Up(); err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("migrate up: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("pool: %v", err)
	}
	return pool, func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
}

var homeUserCounter atomic.Uint64
var homeVendorCounter atomic.Uint64
var homeItemCounter atomic.Uint64

func seedHomeEmployee(t *testing.T, pool *pgxpool.Pool, plant string) string {
	t.Helper()
	n := homeUserCounter.Add(1)
	var id string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role, status, plant)
VALUES ($1, $2, 'employee', 'active', $3) RETURNING id`,
		fmt.Sprintf("home-u-%d@test.com", n),
		fmt.Sprintf("home-u-%d", n),
		plant,
	).Scan(&id))
	return id
}

func seedHomeVendor(t *testing.T, pool *pgxpool.Pool, plant string) string {
	t.Helper()
	n := homeVendorCounter.Add(1)
	var vid string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved') RETURNING id`,
		fmt.Sprintf("home-v-%d", n),
		fmt.Sprintf("home-v-%d Ltd", n),
		fmt.Sprintf("home-v-%d@test.com", n),
	).Scan(&vid))
	_, err := pool.Exec(context.Background(),
		`INSERT INTO plant (code, label) VALUES ($1, $1) ON CONFLICT DO NOTHING`, plant)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
INSERT INTO vendor_plant_mapping (vendor_id, plant, active) VALUES ($1, $2, true)`, vid, plant)
	require.NoError(t, err)
	return vid
}

func seedHomeItem(t *testing.T, pool *pgxpool.Pool, vendorID string) string {
	t.Helper()
	n := homeItemCounter.Add(1)
	var id string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO menu_item (vendor_id, name, description, price_minor, tags, status)
VALUES ($1, $2, '', 12000, '{}', 'active') RETURNING id`,
		vendorID, fmt.Sprintf("item-%d", n)).Scan(&id))
	return id
}

func seedHomeMealSupply(t *testing.T, pool *pgxpool.Pool, itemID string, day time.Time, cutoff time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, 50, 30, '11:50-12:10', '11:50-12:10', $3)`, itemID, day, cutoff)
	require.NoError(t, err)
}

func seedHomeOrder(
	t *testing.T, pool *pgxpool.Pool,
	userID, vendorID, plant string, day time.Time, status string,
) string {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	var oid string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at)
VALUES ($1,$2,$3,$4,$5::order_status,1000,$6,$7,$6) RETURNING id`,
		userID, vendorID, plant, day, status, day.Add(10*time.Hour), secret).Scan(&oid))
	return oid
}

// ---------- Tests ----------

func newHomeServiceForTest(pool *pgxpool.Pool, now time.Time, alpha float64) *menu.HomeService {
	return &menu.HomeService{
		Clock:          clock.FixedClock{T: now},
		ServerTZ:       time.UTC,
		RecentOrders:   opgrepo.NewRecentOrdersRepo(pool),
		Popularity:     mpgrepo.NewPopularityRepo(pool),
		Affinity:       mpgrepo.NewAffinityRepo(pool),
		FavoritesRepo:  mpgrepo.NewFavoriteRepo(pool),
		Alpha:          alpha,
		ReorderLimit:   5,
		FavoriteLimit:  5,
		RecommendLimit: 5,
	}
}

func TestHomeCompute_NoOrder_ReturnsToday(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	// "now" is 09:00 UTC of day → before any cutoff
	now := day.Add(9 * time.Hour)

	svc := newHomeServiceForTest(pool, now, 1.0)
	state, err := svc.Compute(ctx, user, plant, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15", state.TargetDay)
	assert.False(t, state.HasOrdered)
	assert.Nil(t, state.OrderSummary)
}

func TestHomeCompute_PlacedOrderToday_ReturnsToday_HasOrdered(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	now := day.Add(9 * time.Hour)
	oid := seedHomeOrder(t, pool, user, vendor, plant, day, "placed")

	svc := newHomeServiceForTest(pool, now, 1.0)
	state, err := svc.Compute(ctx, user, plant, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15", state.TargetDay)
	assert.True(t, state.HasOrdered)
	require.NotNil(t, state.OrderSummary)
	assert.Equal(t, oid, state.OrderSummary.OrderID)
	assert.Equal(t, "placed", state.OrderSummary.Status)
}

func TestHomeCompute_PickedUpToday_ReturnsTomorrow(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	now := day.Add(13 * time.Hour) // after lunch
	seedHomeOrder(t, pool, user, vendor, plant, day, "picked_up")

	svc := newHomeServiceForTest(pool, now, 1.0)
	state, err := svc.Compute(ctx, user, plant, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-16", state.TargetDay)
	assert.False(t, state.HasOrdered)
	assert.Nil(t, state.OrderSummary)
}

func TestHomeCompute_NoShowToday_ReturnsTomorrow(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	now := day.Add(15 * time.Hour)
	seedHomeOrder(t, pool, user, vendor, plant, day, "no_show")

	svc := newHomeServiceForTest(pool, now, 1.0)
	state, err := svc.Compute(ctx, user, plant, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-16", state.TargetDay)
	assert.False(t, state.HasOrdered)
}

func TestHomeCompute_AllCutoffsPassed_ReturnsTomorrow(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)
	item := seedHomeItem(t, pool, vendor)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	// All meal_supply for today have cutoff at 10:00; now is 11:00 → all passed.
	seedHomeMealSupply(t, pool, item, day, day.Add(10*time.Hour))
	now := day.Add(11 * time.Hour)

	svc := newHomeServiceForTest(pool, now, 1.0)
	state, err := svc.Compute(ctx, user, plant, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-16", state.TargetDay)
	assert.False(t, state.HasOrdered)
}

func TestHomeCompute_TomorrowCutoffAlsoPassed_SkipsToNextOrderableDay(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)
	item := seedHomeItem(t, pool, vendor)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	tomorrow := day.AddDate(0, 0, 1)
	dayAfter := day.AddDate(0, 0, 2)
	now := day.Add(11 * time.Hour)

	seedHomeMealSupply(t, pool, item, day, day.Add(10*time.Hour))
	seedHomeMealSupply(t, pool, item, tomorrow, day.Add(10*time.Hour))
	seedHomeMealSupply(t, pool, item, dayAfter, dayAfter.Add(10*time.Hour))

	svc := newHomeServiceForTest(pool, now, 1.0)
	state, err := svc.Compute(ctx, user, plant, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-17", state.TargetDay)
	assert.False(t, state.HasOrdered)
	assert.Nil(t, state.OrderSummary)
}

func TestHomeCompute_DayOverrideTakesPrecedence(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)
	seedHomeOrder(t, pool, user, vendor, plant, time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC), "picked_up")

	now := time.Date(2026, 5, 15, 13, 0, 0, 0, time.UTC)
	svc := newHomeServiceForTest(pool, now, 1.0)

	// Without override → tomorrow.
	state, err := svc.Compute(ctx, user, plant, "")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-16", state.TargetDay)

	// With override = today → today (regardless of picked_up).
	state, err = svc.Compute(ctx, user, plant, "2026-05-15")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-15", state.TargetDay)
	assert.True(t, state.HasOrdered, "?day=today should still surface the order")

	// With override = a totally different day → that day, no order context.
	state, err = svc.Compute(ctx, user, plant, "2026-05-20")
	require.NoError(t, err)
	assert.Equal(t, "2026-05-20", state.TargetDay)
	assert.False(t, state.HasOrdered)
}

func TestHomeCompute_InvalidDayFormat_Errors(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	now := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
	svc := newHomeServiceForTest(pool, now, 1.0)
	_, err := svc.Compute(ctx, user, plant, "not-a-date")
	require.Error(t, err)
}

// ----- Recommendations integration -----

func TestHomeRecommendations_IncludesPlantPopularityAndAffinityReason(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	other1 := seedHomeEmployee(t, pool, plant)
	other2 := seedHomeEmployee(t, pool, plant)

	vendorA := seedHomeVendor(t, pool, plant) // user has history here
	vendorB := seedHomeVendor(t, pool, plant)
	itemA := seedHomeItem(t, pool, vendorA)
	itemB := seedHomeItem(t, pool, vendorB)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	now := day.Add(9 * time.Hour)

	// User affinity: 3 past materialised orders at vendorA in last 30 days.
	for i := 0; i < 3; i++ {
		oid := seedHomeOrder(t, pool, user, vendorA, plant, now.AddDate(0, 0, -i-1).Truncate(24*time.Hour), "picked_up")
		_, err := pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1, $2, 1, 1000)`, oid, itemA)
		require.NoError(t, err)
	}

	// Today's plant popularity: itemA chosen by other1 (qty=2), itemB chosen by other2 (qty=5).
	oid1 := seedHomeOrder(t, pool, other1, vendorA, plant, day, "ready")
	_, err := pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1,$2,2,1000)`, oid1, itemA)
	require.NoError(t, err)
	oid2 := seedHomeOrder(t, pool, other2, vendorB, plant, day, "picked_up")
	_, err = pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1,$2,5,1000)`, oid2, itemB)
	require.NoError(t, err)

	svc := newHomeServiceForTest(pool, now, 2.0) // high alpha so vendorA item boost beats vendorB
	chips, _, err := svc.RecommendChips(ctx, user, plant, day, 0, 5)
	require.NoError(t, err)
	require.Len(t, chips, 2)

	// First chip should be itemA (vendor affinity boost) with "因為你常點此家" reason.
	assert.Equal(t, itemA, chips[0].MenuItemID)
	assert.Equal(t, "因為你常點此家", chips[0].Reason)
	assert.Equal(t, itemB, chips[1].MenuItemID)
	assert.Equal(t, "同事熱門", chips[1].Reason)
}

func TestHomeRecommendations_ExcludesFavorites(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	other := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)
	item := seedHomeItem(t, pool, vendor)

	day := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	now := day.Add(9 * time.Hour)

	// Popularity for the item.
	oid := seedHomeOrder(t, pool, other, vendor, plant, day, "ready")
	_, err := pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1,$2,3,1000)`, oid, item)
	require.NoError(t, err)

	// Mark as a favorite for the user.
	require.NoError(t, mpgrepo.NewFavoriteRepo(pool).Add(ctx, user, item))

	svc := newHomeServiceForTest(pool, now, 1.0)
	chips, _, err := svc.RecommendChips(ctx, user, plant, day, 0, 5)
	require.NoError(t, err)
	for _, c := range chips {
		assert.NotEqual(t, item, c.MenuItemID, "favorites must be excluded from recommendations")
	}
}

// ----- Reorder chips integration -----

func TestHomeReorderChips_PopulatesPreviewAndAvailability(t *testing.T) {
	pool, cleanup := setupHomePostgres(t)
	defer cleanup()
	ctx := context.Background()
	plant := "F12B-3F"
	user := seedHomeEmployee(t, pool, plant)
	vendor := seedHomeVendor(t, pool, plant)
	item := seedHomeItem(t, pool, vendor)
	// vendor display_name renaming for assertion.
	_, err := pool.Exec(ctx, `UPDATE vendor SET display_name='便當王' WHERE id=$1`, vendor)
	require.NoError(t, err)

	today := time.Now().UTC().Truncate(24 * time.Hour)
	// Past materialised order from the user at this vendor.
	oid := seedHomeOrder(t, pool, user, vendor, plant, today.AddDate(0, 0, -1), "picked_up")
	_, err = pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1,$2,1,1000)`, oid, item)
	require.NoError(t, err)
	// meal_supply for target day → AvailableToday=true.
	seedHomeMealSupply(t, pool, item, today, today.Add(10*time.Hour))

	svc := newHomeServiceForTest(pool, today.Add(9*time.Hour), 1.0)
	// Wire a vendor-name lookup so chips render with display_name.
	svc.VendorNames = func(ctx context.Context, ids []string) (map[string]string, error) {
		out := map[string]string{}
		rows, err := pool.Query(ctx, `SELECT id, display_name FROM vendor WHERE id = ANY($1)`, ids)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				return nil, err
			}
			out[id] = name
		}
		return out, nil
	}
	chips, _, err := svc.ReorderChips(ctx, user, today, 0, 5)
	require.NoError(t, err)
	require.Len(t, chips, 1)
	assert.Equal(t, oid, chips[0].SourceOrderID)
	assert.Equal(t, vendor, chips[0].VendorID)
	assert.Equal(t, "便當王", chips[0].VendorName)
	assert.True(t, chips[0].AvailableToday)
	require.NotEmpty(t, chips[0].ItemsPreview)
}
