package scheduler_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order/scheduler"
)

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
}

type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

func setup(t *testing.T) (*pgxpool.Pool, *scheduler.Cutoff, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	dsn, _ := c.ConnectionString(ctx, "sslmode=disable")
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)

	orderRepo := opg.NewOrderRepo(pool)
	sched := &scheduler.Cutoff{
		Pool:     pool,
		Orders:   orderRepo,
		OrdersTx: orderRepo,
		StateTx:  opg.NewStateEventRepo(pool),
		AuditTx:  opg.NewAuditRepo(pool),
		OutboxTx: opg.NewOutboxRepo(pool),
		Clock:    fixedClock{T: time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)}, // past 17:00
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	cleanup := func() { pool.Close(); _ = c.Terminate(ctx) }
	return pool, sched, cleanup
}

func seedOrder(t *testing.T, pool *pgxpool.Pool, status order.Status, cutoffAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	var vendorID, userID, orderID string
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO vendor (display_name,legal_name,contact_email,status) VALUES ('V', 'V Ltd', 'v-'||gen_random_uuid()||'@x.com', 'approved') RETURNING id`).Scan(&vendorID))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO "user" (primary_email,display_name,role,status) VALUES ('u-'||gen_random_uuid()||'@x.com','U','employee','active') RETURNING id`).Scan(&userID))
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order" (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at)
VALUES ($1, $2, 'F12B-3F', $3, $4, 100, now(), $5)
RETURNING id`,
		userID, vendorID,
		time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		string(status),
		cutoffAt,
	).Scan(&orderID))
	return orderID
}

func TestCutoff_TransitionsPastCutoffOrders(t *testing.T) {
	pool, sched, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	// 1 placed past cutoff, 1 placed future, 1 draft past cutoff
	pastOrderID := seedOrder(t, pool, order.StatusPlaced, time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC))   // exactly at cutoff
	futureOrderID := seedOrder(t, pool, order.StatusPlaced, time.Date(2026, 5, 14, 17, 0, 0, 0, time.UTC)) // future
	draftOrderID := seedOrder(t, pool, order.StatusDraft, time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC))   // not placed

	n, err := sched.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	var ps, fs, ds string
	pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, pastOrderID).Scan(&ps)
	pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, futureOrderID).Scan(&fs)
	pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, draftOrderID).Scan(&ds)
	assert.Equal(t, "cutoff", ps, "past placed must transition")
	assert.Equal(t, "placed", fs, "future placed stays")
	assert.Equal(t, "draft", ds, "draft is not affected")

	// State event + audit + outbox written for the transitioned order
	var seCount, auditCount, outboxCount int
	pool.QueryRow(ctx, `SELECT count(*) FROM order_state_event WHERE order_id=$1 AND to_state='cutoff'`, pastOrderID).Scan(&seCount)
	pool.QueryRow(ctx, `SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='order.cutoff'`, pastOrderID).Scan(&auditCount)
	pool.QueryRow(ctx, `SELECT count(*) FROM outbox_event WHERE aggregate_id::text=$1 AND subject='order.cutoff.v1'`, pastOrderID).Scan(&outboxCount)
	assert.Equal(t, 1, seCount)
	assert.Equal(t, 1, auditCount)
	assert.Equal(t, 1, outboxCount)
}

func TestCutoff_EmptyWhenNoPending(t *testing.T) {
	_, sched, cleanup := setup(t)
	defer cleanup()
	n, err := sched.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestCutoff_RunLoopExitsOnContext(t *testing.T) {
	_, sched, cleanup := setup(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := sched.Run(ctx, 50*time.Millisecond)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
