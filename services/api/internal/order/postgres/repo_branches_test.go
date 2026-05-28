package postgres_test

import (
	"context"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
)

func TestAuditRepo_List_FiltersAndCap(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "welfare_admin")
	role := "welfare_admin"
	repo := pgrepo.NewAuditRepo(pool)

	before := time.Now().Add(-time.Hour)
	require.NoError(t, repo.Write(ctx, plaudit.Entry{ActorID: &uid, ActorRole: &role, Action: "order.place", TargetKind: "order", TargetID: "order-1", Payload: map[string]any{"total": 24000}, RequestID: "req-1"}))
	require.NoError(t, repo.Write(ctx, plaudit.Entry{ActorID: &uid, ActorRole: &role, Action: "order.cancel", TargetKind: "order", TargetID: "order-2", Payload: map[string]any{}, RequestID: "req-2"}))
	require.NoError(t, repo.Write(ctx, plaudit.Entry{ActorID: &uid, ActorRole: &role, Action: "vendor.review", TargetKind: "vendor", TargetID: "vendor-1", Payload: map[string]any{}, RequestID: "req-3"}))

	// No filter → all three, payload round-trips.
	all, err := repo.List(ctx, compliance.AuditFilter{})
	require.NoError(t, err)
	require.Len(t, all, 3)
	// DESC by at → most-recent (vendor.review) first.
	assert.Equal(t, "vendor.review", all[0].Action)
	assert.Equal(t, "req-3", all[0].RequestID)
	require.NotNil(t, all[2].ActorID)
	assert.Equal(t, uid, *all[2].ActorID)
	assert.Equal(t, float64(24000), all[2].Payload["total"])

	// TargetKind filter.
	orders, err := repo.List(ctx, compliance.AuditFilter{TargetKind: "order"})
	require.NoError(t, err)
	assert.Len(t, orders, 2)

	// TargetID filter.
	one, err := repo.List(ctx, compliance.AuditFilter{TargetID: "order-1"})
	require.NoError(t, err)
	require.Len(t, one, 1)
	assert.Equal(t, "order.place", one[0].Action)

	// Since filter (everything is after `before`).
	since, err := repo.List(ctx, compliance.AuditFilter{Since: before})
	require.NoError(t, err)
	assert.Len(t, since, 3)
	future, err := repo.List(ctx, compliance.AuditFilter{Since: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	assert.Empty(t, future)

	// Limit clamp: out-of-range limit falls back to default (still returns rows).
	clamped, err := repo.List(ctx, compliance.AuditFilter{Limit: 5000})
	require.NoError(t, err)
	assert.Len(t, clamped, 3)
	// Explicit small limit is honoured.
	limited, err := repo.List(ctx, compliance.AuditFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, limited, 1)
}

func TestOutboxRepo_Append_RejectsNonPgxTx(t *testing.T) {
	repo := pgrepo.NewOutboxRepo(nil)
	err := repo.Append(context.Background(), order.Tx("not-a-tx"), "order", "id", "subj",
		map[string]any{}, map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be pgx.Tx")
}

func TestOutboxRepo_MarkPublished_RejectsNonPgxTx(t *testing.T) {
	repo := pgrepo.NewOutboxRepo(nil)
	err := repo.MarkPublished(context.Background(), order.Tx("not-a-tx"), []int64{1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be pgx.Tx")
}

func TestOutboxRepo_MarkFailed_RejectsNonPgxTx(t *testing.T) {
	repo := pgrepo.NewOutboxRepo(nil)
	err := repo.MarkFailed(context.Background(), order.Tx("not-a-tx"), 1, "boom")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be pgx.Tx")
}

func TestRecentOrdersRepo_EmptyInputs(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewRecentOrdersRepo(pool)

	// ItemNamesByOrderIDs with no ids → empty map, no query.
	names, err := repo.ItemNamesByOrderIDs(ctx, nil, 2)
	require.NoError(t, err)
	assert.Empty(t, names)

	// OrderAvailability with no ids → empty map, no query.
	avail, err := repo.OrderAvailability(ctx, nil, time.Now())
	require.NoError(t, err)
	assert.Empty(t, avail)

	// RecentOrdersByUser for a user with no orders → empty.
	uid := seedUser(t, pool, "employee")
	chips, err := repo.RecentOrdersByUser(ctx, uid, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, chips)
}

func TestRecentOrdersRepo_LimitOffsetClamping(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	v := seedApprovedVendor(t, pool)
	i := seedActiveMenuItem(t, pool, v, 10000)
	_ = i
	today := time.Now().UTC().Truncate(24 * time.Hour)

	secret := make([]byte, 32)
	_, err := pool.Exec(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,'F12B-3F',$3,'ready'::order_status,1000,$4,$5,$4,$4)`,
		uid, v, today.AddDate(0, 0, -1), today.Add(10*time.Hour), secret)
	require.NoError(t, err)

	repo := pgrepo.NewRecentOrdersRepo(pool)
	// limit<1 clamps to 1, offset<0 clamps to 0, limit>50 clamps to 50.
	for _, lim := range []int{0, -5, 999} {
		chips, err := repo.RecentOrdersByUser(ctx, uid, lim, -3)
		require.NoError(t, err)
		require.Len(t, chips, 1)
		assert.Equal(t, v, chips[0].VendorID)
	}
}

func TestRecentOrdersRepo_ItemNamesCapEnforced(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	v := seedApprovedVendor(t, pool)
	i1 := seedActiveMenuItem(t, pool, v, 10000)
	i2 := seedActiveMenuItem(t, pool, v, 11000)
	i3 := seedActiveMenuItem(t, pool, v, 12000)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	secret := make([]byte, 32)
	var oid string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,'F12B-3F',$3,'ready'::order_status,1000,$4,$5,$4,$4) RETURNING id`,
		uid, v, today, today.Add(10*time.Hour), secret).Scan(&oid))
	_, err := pool.Exec(ctx, `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1,$2,1,1000),($1,$3,1,1000),($1,$4,1,1000)`, oid, i1, i2, i3)
	require.NoError(t, err)

	repo := pgrepo.NewRecentOrdersRepo(pool)
	// cap<1 clamps to 1 → at most one name.
	names, err := repo.ItemNamesByOrderIDs(ctx, []string{oid}, 0)
	require.NoError(t, err)
	require.Contains(t, names, oid)
	assert.Len(t, names[oid], 1)

	// cap=2 → exactly two of the three names.
	names2, err := repo.ItemNamesByOrderIDs(ctx, []string{oid}, 2)
	require.NoError(t, err)
	assert.Len(t, names2[oid], 2)
}
