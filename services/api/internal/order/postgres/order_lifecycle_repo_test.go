package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
)

// createOrderWithStatus seeds an order at the given status and returns its id.
func createOrderWithStatus(t *testing.T, pool *pgxpool.Pool, uid, vid string, day time.Time, status order.Status) string {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	var oid string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,'F12B-3F',$3,$4::order_status,24000,$5::timestamptz,$6,$5::timestamptz,
        CASE WHEN $4 IN ('ready','picked_up','no_show') THEN $5::timestamptz ELSE NULL END)
RETURNING id`,
		uid, vid, day, string(status), day.Add(10*time.Hour), secret).Scan(&oid))
	return oid
}

func TestOrderRepo_CreateTx_NilTxErrors(t *testing.T) {
	repo := pgrepo.NewOrderRepo(nil)
	err := repo.CreateTx(context.Background(), nil, &order.Order{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")
}

func TestOrderRepo_MarkReadyTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	// Happy: placed → ready stamps ready_at.
	placed := createOrderWithStatus(t, pool, uid, vid, day, order.StatusPlaced)
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.MarkReadyTx(ctx, tx, placed)
	}))
	got, err := repo.GetByID(ctx, placed)
	require.NoError(t, err)
	assert.Equal(t, order.StatusReady, got.Status)
	require.NotNil(t, got.ReadyAt)

	// Also valid from cutoff.
	cutoff := createOrderWithStatus(t, pool, uid, vid, day, order.StatusCutoff)
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.MarkReadyTx(ctx, tx, cutoff)
	}))

	// Invalid: a draft order cannot go ready → ErrInvalidTransition (0 rows).
	draft := createOrderWithStatus(t, pool, uid, vid, day, order.StatusDraft)
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.MarkReadyTx(ctx, tx, draft)
	})
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestOrderRepo_MarkPickedUpTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	// Happy: ready → picked_up.
	ready := createOrderWithStatus(t, pool, uid, vid, day, order.StatusReady)
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.MarkPickedUpTx(ctx, tx, ready)
	}))
	got, err := repo.GetByID(ctx, ready)
	require.NoError(t, err)
	assert.Equal(t, order.StatusPickedUp, got.Status)
	require.NotNil(t, got.PickedUpAt)

	// Invalid: placed order cannot be picked up.
	placed := createOrderWithStatus(t, pool, uid, vid, day, order.StatusPlaced)
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.MarkPickedUpTx(ctx, tx, placed)
	})
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestOrderRepo_MarkNoShowTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	// Happy: ready → no_show.
	ready := createOrderWithStatus(t, pool, uid, vid, day, order.StatusReady)
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.MarkNoShowTx(ctx, tx, ready)
	}))
	got, err := repo.GetByID(ctx, ready)
	require.NoError(t, err)
	assert.Equal(t, order.StatusNoShow, got.Status)
	require.NotNil(t, got.NoShowAt)

	// Invalid: a picked_up order cannot become no_show.
	picked := createOrderWithStatus(t, pool, uid, vid, day, order.StatusPickedUp)
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.MarkNoShowTx(ctx, tx, picked)
	})
	assert.ErrorIs(t, err, order.ErrInvalidTransition)
}

func TestOrderRepo_ReplaceItemsTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	i1 := seedActiveMenuItem(t, pool, vid, 12000)
	i2 := seedActiveMenuItem(t, pool, vid, 5000)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	o := newOrder(t, uid, vid, i1, day)
	require.NoError(t, repo.Create(ctx, o))

	// Replace the single i1 line with two i2 lines and a new total + notes.
	newItems := []order.Item{
		{MenuItemID: i2, Qty: 3, UnitPriceMinor: 5000},
		{MenuItemID: i1, Qty: 1, UnitPriceMinor: 12000},
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.ReplaceItemsTx(ctx, tx, o.ID, newItems, 27000, "edited")
	}))
	for _, it := range newItems {
		assert.NotEmpty(t, it.ID)
		assert.Equal(t, o.ID, it.OrderID)
	}

	got, err := repo.GetByID(ctx, o.ID)
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
	assert.Equal(t, int64(27000), got.TotalPriceMinor)
	assert.Equal(t, "edited", got.Notes)

	// Not-found: with no items to insert, the final UPDATE matches 0 rows and
	// returns ErrOrderNotFound. (A non-empty list would trip the order_item FK
	// first, so the empty-list case is what reaches this branch.)
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.ReplaceItemsTx(ctx, tx, "00000000-0000-0000-0000-000000000000",
			nil, 12000, "x")
	})
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}

func TestOrderRepo_ListByVendorDay(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	other := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	// Two orders for vid on `day`: one placed, one ready (with an item line).
	o1 := newOrder(t, uid, vid, iid, day)
	o1.Status = order.StatusPlaced
	require.NoError(t, repo.Create(ctx, o1))
	o2 := newOrder(t, uid, vid, iid, day)
	o2.Status = order.StatusReady
	require.NoError(t, repo.Create(ctx, o2))
	// Different vendor + different day → excluded.
	require.NoError(t, repo.Create(ctx, newOrder(t, uid, other, seedActiveMenuItem(t, pool, other, 12000), day)))
	require.NoError(t, repo.Create(ctx, newOrder(t, uid, vid, iid, day.AddDate(0, 0, 1))))

	// No status filter → both, items hydrated.
	all, err := repo.ListByVendorDay(ctx, vid, day, nil)
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.NotEmpty(t, all[0].Items)

	// Status filter → only placed.
	placed, err := repo.ListByVendorDay(ctx, vid, day, []order.Status{order.StatusPlaced})
	require.NoError(t, err)
	require.Len(t, placed, 1)
	assert.Equal(t, o1.ID, placed[0].ID)
	require.NotEmpty(t, placed[0].Items)

	// Multi-status filter → both.
	both, err := repo.ListByVendorDay(ctx, vid, day, []order.Status{order.StatusPlaced, order.StatusReady})
	require.NoError(t, err)
	assert.Len(t, both, 2)

	// Filter that matches nothing → empty.
	none, err := repo.ListByVendorDay(ctx, vid, day, []order.Status{order.StatusCancelled})
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestOrderRepo_ListReadyOlderThan(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	// A ready order whose ready_at is well in the past.
	old := createOrderWithStatus(t, pool, uid, vid, day, order.StatusReady)
	_, err := pool.Exec(ctx, `UPDATE "order" SET ready_at = now() - interval '2 hours' WHERE id=$1`, old)
	require.NoError(t, err)
	// A ready order with a fresh ready_at → excluded by threshold.
	fresh := createOrderWithStatus(t, pool, uid, vid, day, order.StatusReady)
	_, err = pool.Exec(ctx, `UPDATE "order" SET ready_at = now() WHERE id=$1`, fresh)
	require.NoError(t, err)
	// A placed order → excluded by status.
	createOrderWithStatus(t, pool, uid, vid, day, order.StatusPlaced)

	threshold := time.Now().Add(-1 * time.Hour)
	list, err := repo.ListReadyOlderThan(ctx, threshold)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, old, list[0].ID)
	assert.Equal(t, order.StatusReady, list[0].Status)
}

func TestOrderRepo_ListPickedOrNoShowInPeriod(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	picked := createOrderWithStatus(t, pool, uid, vid, day, order.StatusPickedUp)
	noShow := createOrderWithStatus(t, pool, uid, vid, day.AddDate(0, 0, 1), order.StatusNoShow)
	// placed in period → excluded by status.
	createOrderWithStatus(t, pool, uid, vid, day, order.StatusPlaced)
	// picked_up but outside the [from,to] window → excluded by date.
	createOrderWithStatus(t, pool, uid, vid, day.AddDate(0, 0, 10), order.StatusPickedUp)

	from := day
	to := day.AddDate(0, 0, 2)
	list, err := repo.ListPickedOrNoShowInPeriod(ctx, from, to)
	require.NoError(t, err)
	require.Len(t, list, 2)
	ids := map[string]order.Status{}
	for _, o := range list {
		ids[o.ID] = o.Status
	}
	assert.Equal(t, order.StatusPickedUp, ids[picked])
	assert.Equal(t, order.StatusNoShow, ids[noShow])

	// Empty window → no rows.
	empty, err := repo.ListPickedOrNoShowInPeriod(ctx, day.AddDate(0, 0, 100), day.AddDate(0, 0, 101))
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestOrderRepo_ListByUser_EmptyWhenNone(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	repo := pgrepo.NewOrderRepo(pool)
	list, err := repo.ListByUser(ctx, uid, time.Now().UTC().Truncate(24*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, list)
}
