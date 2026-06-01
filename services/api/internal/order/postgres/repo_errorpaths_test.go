package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

// unmarshalable returns a payload that json.Marshal cannot encode (channel
// values are not JSON-serialisable), used to exercise the marshal-error
// branches of repositories that persist JSON payloads.
func unmarshalable() map[string]any {
	return map[string]any{"bad": make(chan int)}
}

// TestRepos_ClosedPool_QueryErrors exercises every "pool.Query / QueryRow /
// Begin returned an error" branch across the repos by closing the pool once and
// reusing it for all subtests (one container, many error paths). A closed pool
// fails every operation deterministically.
func TestRepos_ClosedPool_QueryErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	pool.Close()
	ctx := context.Background()

	t.Run("AuditRepo.List", func(t *testing.T) {
		_, err := pgrepo.NewAuditRepo(pool).List(ctx, compliance.AuditFilter{TargetKind: "order"})
		require.Error(t, err)
	})
	t.Run("OrderRepo.GetByID", func(t *testing.T) {
		_, err := pgrepo.NewOrderRepo(pool).GetByID(ctx, "00000000-0000-0000-0000-000000000001")
		require.Error(t, err)
		// Closed pool is not ErrNoRows; it falls to the "get order" wrap.
		assert.Contains(t, err.Error(), "get order")
	})
	t.Run("OrderRepo.ListByUser", func(t *testing.T) {
		_, err := pgrepo.NewOrderRepo(pool).ListByUser(ctx, "u", time.Now())
		require.Error(t, err)
	})
	t.Run("OrderRepo.ListPlacedDueForCutoff", func(t *testing.T) {
		_, err := pgrepo.NewOrderRepo(pool).ListPlacedDueForCutoff(ctx, time.Now())
		require.Error(t, err)
	})
	t.Run("OrderRepo.ListReadyOlderThan", func(t *testing.T) {
		_, err := pgrepo.NewOrderRepo(pool).ListReadyOlderThan(ctx, time.Now())
		require.Error(t, err)
	})
	t.Run("OrderRepo.ListByVendorDay/no-status", func(t *testing.T) {
		_, err := pgrepo.NewOrderRepo(pool).ListByVendorDay(ctx, "v", time.Now(), nil)
		require.Error(t, err)
	})
	t.Run("OrderRepo.ListByVendorDay/with-status", func(t *testing.T) {
		_, err := pgrepo.NewOrderRepo(pool).ListByVendorDay(ctx, "v", time.Now(), []order.Status{order.StatusPlaced})
		require.Error(t, err)
	})
	t.Run("OrderRepo.ListPickedOrNoShowInPeriod", func(t *testing.T) {
		_, err := pgrepo.NewOrderRepo(pool).ListPickedOrNoShowInPeriod(ctx, time.Now(), time.Now())
		require.Error(t, err)
	})
	t.Run("OutboxRepo.LockBatch/begin", func(t *testing.T) {
		_, _, err := pgrepo.NewOutboxRepo(pool).LockBatch(ctx, 10)
		require.Error(t, err)
	})
	t.Run("StateEventRepo.ListByOrder", func(t *testing.T) {
		_, err := pgrepo.NewStateEventRepo(pool).ListByOrder(ctx, "o")
		require.Error(t, err)
	})
	t.Run("RecentOrdersRepo.RecentOrdersByUser", func(t *testing.T) {
		_, err := pgrepo.NewRecentOrdersRepo(pool).RecentOrdersByUser(ctx, "u", 10, 0)
		require.Error(t, err)
	})
	t.Run("RecentOrdersRepo.GetOrderByUserDate", func(t *testing.T) {
		// Closed pool → not ErrNoRows; hits the generic error return.
		_, err := pgrepo.NewRecentOrdersRepo(pool).GetOrderByUserDate(ctx, "u", time.Now(), "F12B-3F")
		require.Error(t, err)
	})
	t.Run("RecentOrdersRepo.ItemNamesByOrderIDs", func(t *testing.T) {
		_, err := pgrepo.NewRecentOrdersRepo(pool).ItemNamesByOrderIDs(ctx, []string{"o"}, 2)
		require.Error(t, err)
	})
	t.Run("RecentOrdersRepo.OrderAvailability", func(t *testing.T) {
		_, err := pgrepo.NewRecentOrdersRepo(pool).OrderAvailability(ctx, []string{"o"}, time.Now())
		require.Error(t, err)
	})
	t.Run("RegisterOutboxGauges/callback-pending-query", func(t *testing.T) {
		// Registration only creates instruments (no DB) → succeeds on a closed
		// pool; the query error surfaces when the callback runs at Collect.
		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		otel.SetMeterProvider(mp)
		t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

		require.NoError(t, pgrepo.RegisterOutboxGauges(pool))
		var rm metricdata.ResourceMetrics
		err := reader.Collect(ctx, &rm)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outbox pending query")
	})
}

// TestRepos_LivePool_TxErrorBranches exercises the marshal-error, FK/constraint,
// and context-cancel exec-error branches that need a working connection. All
// subtests share one container.
func TestRepos_LivePool_TxErrorBranches(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	day := time.Now().UTC().Truncate(24 * time.Hour)

	t.Run("AuditRepo.WriteTx/marshal", func(t *testing.T) {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewAuditRepo(pool).WriteTx(ctx, tx, plaudit.Entry{
				Action: "x", TargetKind: "order", TargetID: "id", Payload: unmarshalable(),
			})
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "marshal payload")
	})

	t.Run("OrderRepo.CreateTx/insert-order-fk", func(t *testing.T) {
		o := newOrder(t, "00000000-0000-0000-0000-000000000000",
			"00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000", day)
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewOrderRepo(pool).CreateTx(ctx, tx, o)
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insert order")
	})

	t.Run("OrderRepo.CreateTx/insert-item-fk", func(t *testing.T) {
		// Valid header, dangling menu_item_id → the order_item INSERT fails.
		o := newOrder(t, uid, vid, "00000000-0000-0000-0000-000000000000", day)
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewOrderRepo(pool).CreateTx(ctx, tx, o)
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insert order_item")
	})

	t.Run("OrderRepo.UpdateStatusTx/bad-enum", func(t *testing.T) {
		// A bogus enum value makes the ::order_status cast fail → exec error
		// (distinct from the 0-rows ErrInvalidTransition branch).
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewOrderRepo(pool).UpdateStatusTx(ctx, tx, "00000000-0000-0000-0000-000000000001",
				order.StatusPlaced, order.Status("not_a_real_status"))
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update status")
	})

	t.Run("OrderRepo.ReplaceItemsTx/insert-item-fk", func(t *testing.T) {
		o := newOrder(t, uid, vid, iid, day)
		require.NoError(t, pgrepo.NewOrderRepo(pool).Create(ctx, o))
		bad := []order.Item{{MenuItemID: "00000000-0000-0000-0000-000000000000", Qty: 1, UnitPriceMinor: 1000}}
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewOrderRepo(pool).ReplaceItemsTx(ctx, tx, o.ID, bad, 1000, "x")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insert order_item")
	})

	t.Run("OrderRepo.ReplaceItemsTx/update-check-violation", func(t *testing.T) {
		o := newOrder(t, uid, vid, iid, day)
		require.NoError(t, pgrepo.NewOrderRepo(pool).Create(ctx, o))
		// Empty items so delete succeeds + no inserts, then a negative total trips
		// the CHECK (total_price_minor >= 0) on the final UPDATE → update-error
		// branch (the order exists, so not the 0-rows ErrOrderNotFound branch).
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewOrderRepo(pool).ReplaceItemsTx(ctx, tx, o.ID, nil, -1, "x")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update order total")
	})

	t.Run("OrderRepo.MarkReadyTx/cancel", func(t *testing.T) {
		ctxCancel, cancel := context.WithCancel(ctx)
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			cancel()
			return pgrepo.NewOrderRepo(pool).MarkReadyTx(ctxCancel, tx, "00000000-0000-0000-0000-000000000001")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mark ready")
	})

	t.Run("OrderRepo.MarkPickedUpTx/cancel", func(t *testing.T) {
		ctxCancel, cancel := context.WithCancel(ctx)
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			cancel()
			return pgrepo.NewOrderRepo(pool).MarkPickedUpTx(ctxCancel, tx, "00000000-0000-0000-0000-000000000001")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mark picked_up")
	})

	t.Run("OrderRepo.MarkNoShowTx/cancel", func(t *testing.T) {
		ctxCancel, cancel := context.WithCancel(ctx)
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			cancel()
			return pgrepo.NewOrderRepo(pool).MarkNoShowTx(ctxCancel, tx, "00000000-0000-0000-0000-000000000001")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mark no_show")
	})

	t.Run("OutboxRepo.AppendTx/marshal-payload", func(t *testing.T) {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewOutboxRepo(pool).AppendTx(ctx, tx, "order", newAggregateUUID(900), "subj",
				unmarshalable(), map[string]any{})
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "marshal payload")
	})

	t.Run("OutboxRepo.AppendTx/marshal-headers", func(t *testing.T) {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewOutboxRepo(pool).AppendTx(ctx, tx, "order", newAggregateUUID(901), "subj",
				map[string]any{}, unmarshalable())
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "marshal headers")
	})

	t.Run("OutboxRepo.MarkPublished/cancel", func(t *testing.T) {
		repo := pgrepo.NewOutboxRepo(pool)
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.AppendTx(ctx, tx, "order", newAggregateUUID(902), "subj",
				map[string]any{}, map[string]any{})
		}))
		ctxCancel, cancel := context.WithCancel(ctx)
		events, tx, err := repo.LockBatch(ctxCancel, 1)
		require.NoError(t, err)
		require.Len(t, events, 1)
		// Cancel so the UPDATE inside MarkPublished fails → exec-error rollback
		// branch (distinct from the empty-ids commit-only branch).
		cancel()
		err = repo.MarkPublished(ctxCancel, tx, []int64{events[0].ID})
		require.Error(t, err)
	})

	t.Run("StateEventRepo.AppendTx/marshal", func(t *testing.T) {
		ev := &order.StateEvent{
			OrderID: "00000000-0000-0000-0000-000000000001",
			ToState: order.StatusPlaced, Payload: unmarshalable(),
		}
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewStateEventRepo(pool).AppendTx(ctx, tx, ev)
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "marshal payload")
	})

	t.Run("StateEventRepo.AppendTx/insert-fk", func(t *testing.T) {
		ev := &order.StateEvent{
			OrderID: "00000000-0000-0000-0000-000000000000",
			ToState: order.StatusPlaced, Payload: map[string]any{},
		}
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return pgrepo.NewStateEventRepo(pool).AppendTx(ctx, tx, ev)
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "append state event")
	})
}

// TestOrderRepo_NilTxAndSchemaErrors covers the remaining query-error branches
// that need a live pool with the schema mutated mid-test (table renamed so a
// specific statement fails). Shares one container.
func TestOrderRepo_SchemaMutationErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	repo := pgrepo.NewOrderRepo(pool)

	o := newOrder(t, uid, vid, iid, day)
	require.NoError(t, repo.Create(ctx, o))

	// Rename order_item so the items-related statements fail; restore afterwards.
	_, err := pool.Exec(ctx, `ALTER TABLE order_item RENAME TO order_item_hidden`)
	require.NoError(t, err)
	defer func() { _, _ = pool.Exec(ctx, `ALTER TABLE order_item_hidden RENAME TO order_item`) }()

	t.Run("GetByID/items-query", func(t *testing.T) {
		_, err := repo.GetByID(ctx, o.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "get items")
	})

	t.Run("ReplaceItemsTx/delete", func(t *testing.T) {
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.ReplaceItemsTx(ctx, tx, "00000000-0000-0000-0000-000000000001", nil, 1000, "x")
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete order_items")
	})
}

// TestOutboxRepo_LockBatch_QueryError renames outbox_event so the SELECT inside
// LockBatch fails after Begin succeeds (the query-error rollback branch).
func TestOutboxRepo_LockBatch_QueryError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	_, err := pool.Exec(ctx, `ALTER TABLE outbox_event RENAME TO outbox_event_hidden`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `ALTER TABLE outbox_event_hidden RENAME TO outbox_event`) })

	_, _, err = repo.LockBatch(ctx, 10)
	require.Error(t, err)
}

// TestRegisterOutboxGauges_CallbackOldestQueryError breaks only the oldest query
// (renames created_at) so the pending query still succeeds and the callback
// reaches the oldest-query error branch.
func TestRegisterOutboxGauges_CallbackOldestQueryError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, pgrepo.RegisterOutboxGauges(pool))

	_, err := pool.Exec(ctx, `ALTER TABLE outbox_event RENAME COLUMN created_at TO created_at_hidden`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `ALTER TABLE outbox_event RENAME COLUMN created_at_hidden TO created_at`)
	})

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outbox oldest query")
}
