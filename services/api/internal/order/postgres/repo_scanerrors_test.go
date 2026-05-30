package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
)

// These tests drive the in-loop rows.Scan(...) error returns and the rows.Err()
// branches. A scan error is forced deterministically by relaxing a column's NOT
// NULL constraint and writing a NULL into a column the repo scans into a
// non-pointer Go type (pgx errors with "cannot scan NULL into *T"). Each test
// owns its container, so the schema edit is isolated.

// seedReadyOrderRaw inserts a ready order directly and returns its id.
func seedReadyOrderRaw(t *testing.T, pool *pgxpool.Pool, uid, vid string, day time.Time) string {
	t.Helper()
	secret := make([]byte, 32)
	var oid string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, cutoff_at, totp_secret, placed_at, ready_at)
VALUES ($1,$2,'F12B-3F',$3,'ready'::order_status,1000,$4,$5,$4,$4) RETURNING id`,
		uid, vid, day, day.Add(10*time.Hour), secret).Scan(&oid))
	return oid
}

// ---- order_repo.go: scanOrder / collectOrders / hydrateItems ----

func TestOrderRepo_ListByUser_ScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	seedReadyOrderRaw(t, pool, uid, vid, day)

	// notes is scanned into a non-pointer string; NULL it to break scanOrder
	// (covers collectOrders' scan-error return and ListByUser's propagation).
	_, err := pool.Exec(ctx, `ALTER TABLE "order" ALTER COLUMN notes DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE "order" SET notes = NULL`)
	require.NoError(t, err)

	_, err = repo.ListByUser(ctx, uid, day.AddDate(0, 0, -1))
	require.Error(t, err)
}

func TestOrderRepo_ListByVendorDay_ScanError_NoStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	seedReadyOrderRaw(t, pool, uid, vid, day)

	_, err := pool.Exec(ctx, `ALTER TABLE "order" ALTER COLUMN notes DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE "order" SET notes = NULL`)
	require.NoError(t, err)

	_, err = repo.ListByVendorDay(ctx, vid, day, nil)
	require.Error(t, err)
}

func TestOrderRepo_ListByUser_HydrateItemsError(t *testing.T) {
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

	// Header rows scan fine, but the hydrateItems query references order_item;
	// rename it so that batched items query fails (covers hydrateItems query
	// error + ListByUser's hydrate-error propagation).
	_, err := pool.Exec(ctx, `ALTER TABLE order_item RENAME TO order_item_hidden`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `ALTER TABLE order_item_hidden RENAME TO order_item`) })

	_, err = repo.ListByUser(ctx, uid, day.AddDate(0, 0, -1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hydrate items")
}

func TestOrderRepo_ListByVendorDay_HydrateItemsError_NoStatus(t *testing.T) {
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

	_, err := pool.Exec(ctx, `ALTER TABLE order_item RENAME TO order_item_hidden`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `ALTER TABLE order_item_hidden RENAME TO order_item`) })

	_, err = repo.ListByVendorDay(ctx, vid, day, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hydrate items")
}

func TestOrderRepo_ListByVendorDay_ScanError_WithStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	seedReadyOrderRaw(t, pool, uid, vid, day)

	_, err := pool.Exec(ctx, `ALTER TABLE "order" ALTER COLUMN notes DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE "order" SET notes = NULL`)
	require.NoError(t, err)

	_, err = repo.ListByVendorDay(ctx, vid, day, []order.Status{order.StatusReady})
	require.Error(t, err)
}

func TestOrderRepo_ListByVendorDay_HydrateItemsError_WithStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	repo := pgrepo.NewOrderRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := newOrder(t, uid, vid, iid, day)
	o.Status = order.StatusReady
	require.NoError(t, repo.Create(ctx, o))

	_, err := pool.Exec(ctx, `ALTER TABLE order_item RENAME TO order_item_hidden`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `ALTER TABLE order_item_hidden RENAME TO order_item`) })

	_, err = repo.ListByVendorDay(ctx, vid, day, []order.Status{order.StatusReady})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hydrate items")
}

func TestOrderRepo_ListByUser_HydrateItemsScanError(t *testing.T) {
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

	// Header scans fine; the batched hydrateItems query succeeds but a NULL qty
	// breaks its per-row scan (qty scans into a non-pointer int) → covers
	// hydrateItems' in-loop scan-error return.
	_, err := pool.Exec(ctx, `ALTER TABLE order_item ALTER COLUMN qty DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE order_item SET qty = NULL WHERE order_id = $1`, o.ID)
	require.NoError(t, err)

	_, err = repo.ListByUser(ctx, uid, day.AddDate(0, 0, -1))
	require.Error(t, err)
}

func TestOrderRepo_GetByID_ItemScanError(t *testing.T) {
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

	// qty is scanned into a non-pointer int; NULL it to break the items loop scan.
	_, err := pool.Exec(ctx, `ALTER TABLE order_item ALTER COLUMN qty DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE order_item SET qty = NULL WHERE order_id = $1`, o.ID)
	require.NoError(t, err)

	_, err = repo.GetByID(ctx, o.ID)
	require.Error(t, err)
}

// ---- audit_repo.go: List scan error ----

func TestAuditRepo_List_ScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAuditRepo(pool)
	role := "welfare_admin"
	uid := seedUser(t, pool, "welfare_admin")
	require.NoError(t, repo.Write(ctx, plaudit.Entry{
		ActorID: &uid, ActorRole: &role, Action: "order.place",
		TargetKind: "order", TargetID: "t", Payload: map[string]any{}, RequestID: "r",
	}))

	// action is scanned into a non-pointer string; NULL it to break the loop scan.
	_, err := pool.Exec(ctx, `ALTER TABLE audit_event ALTER COLUMN action DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE audit_event DISABLE TRIGGER USER`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE audit_event SET action = NULL`)
	require.NoError(t, err)

	_, err = repo.List(ctx, compliance.AuditFilter{})
	require.Error(t, err)
}

// ---- state_event_repo.go: ListByOrder scan error ----

func TestStateEventRepo_ListByOrder_ScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	orderRepo := pgrepo.NewOrderRepo(pool)
	eventRepo := pgrepo.NewStateEventRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := newOrder(t, uid, vid, iid, day)
	require.NoError(t, orderRepo.Create(ctx, o))

	ev := &order.StateEvent{OrderID: o.ID, ToState: order.StatusPlaced, Reason: "x", Payload: map[string]any{}}
	require.NoError(t, eventRepo.Append(ctx, ev))

	// to_state is scanned into a non-pointer string; NULL it to break the scan.
	_, err := pool.Exec(ctx, `ALTER TABLE order_state_event ALTER COLUMN to_state DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE order_state_event DISABLE TRIGGER USER`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE order_state_event SET to_state = NULL WHERE id = $1`, ev.ID)
	require.NoError(t, err)

	_, err = eventRepo.ListByOrder(ctx, o.ID)
	require.Error(t, err)
}

// ---- recent_orders_repo.go: three loop scan errors ----

func TestRecentOrdersRepo_RecentOrdersByUser_ScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	repo := pgrepo.NewRecentOrdersRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	seedReadyOrderRaw(t, pool, uid, vid, day)

	// total_price_minor is scanned into a non-pointer int64; NULL it (drop CHECK
	// + NOT NULL) to break the chips loop scan.
	_, err := pool.Exec(ctx, `ALTER TABLE "order" ALTER COLUMN total_price_minor DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE "order" SET total_price_minor = NULL`)
	require.NoError(t, err)

	_, err = repo.RecentOrdersByUser(ctx, uid, 10, 0)
	require.Error(t, err)
}

func TestRecentOrdersRepo_ItemNamesByOrderIDs_ScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	repo := pgrepo.NewRecentOrdersRepo(pool)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	oid := seedReadyOrderRaw(t, pool, uid, vid, day)
	_, err := pool.Exec(ctx, `INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor) VALUES ($1,$2,1,1000)`, oid, iid)
	require.NoError(t, err)

	// mi.name is scanned into a non-pointer string; NULL it to break the loop scan.
	_, err = pool.Exec(ctx, `ALTER TABLE menu_item ALTER COLUMN name DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE menu_item SET name = NULL WHERE id = $1`, iid)
	require.NoError(t, err)

	_, err = repo.ItemNamesByOrderIDs(ctx, []string{oid}, 2)
	require.Error(t, err)
}

// OrderAvailability's in-loop scan error (recent_orders_repo.go:148) is
// unreachable: the only scanned value is `DISTINCT oi.order_id`, which is both
// the JOIN key and the `= ANY($1)` filter, so it can never be NULL in the result
// and always fits the Go string target. Documented as an exemption rather than
// asserting an impossible error.

// ---- outbox_repo.go: LockBatch scan error ----

func TestOutboxRepo_LockBatch_ScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	// aggregate_type is scanned into a non-pointer string; an unpublished row
	// with a NULL aggregate_type breaks LockBatch's per-row scan.
	_, err := pool.Exec(ctx, `ALTER TABLE outbox_event ALTER COLUMN aggregate_type DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO outbox_event (aggregate_type, aggregate_id, subject, payload, headers)
VALUES (NULL, gen_random_uuid(), 's', '{}'::jsonb, '{}'::jsonb)`)
	require.NoError(t, err)

	_, _, err = repo.LockBatch(ctx, 10)
	require.Error(t, err)
}

// ---- outbox_metrics.go: pending scan error + rows.Err ----

func TestRegisterOutboxGauges_PendingScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Seed an unpublished row, then NULL its aggregate_type so the GROUP-BY
	// pending query yields a NULL aggregate_type that fails the string scan.
	_, err := pool.Exec(ctx, `ALTER TABLE outbox_event ALTER COLUMN aggregate_type DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
INSERT INTO outbox_event (aggregate_type, aggregate_id, subject, payload, headers)
VALUES (NULL, gen_random_uuid(), 's', '{}'::jsonb, '{}'::jsonb)`)
	require.NoError(t, err)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, pgrepo.RegisterOutboxGauges(pool))

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outbox pending scan")
}
