//go:build perf

package perf_test

import (
	"context"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
)

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/order/perf/pickup_perf_test.go → ../../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
}

type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

func setupPgWithHighConns(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	// Server-side max_connections must exceed pool MaxConns or pgx connects
	// fail with "too many clients already" once the racers fan out.
	bumpMaxConns := testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
		req.Cmd = append(req.Cmd, "-c", "max_connections=300")
		return nil
	})
	c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		bumpMaxConns,
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 250 // keep below server max_connections=300 (reserve headroom for migrate + seed)
	cfg.MinConns = 50
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	return pool, func() { pool.Close(); _ = c.Terminate(ctx) }
}

// seedReadyOrders inserts n orders for a shared (vendor, user) pair, each in
// READY status. The user is the order owner, so each can self-serve pickup.
// Returns orderIDs, the owner userID, and supplyDate.
func seedReadyOrders(t *testing.T, pool *pgxpool.Pool, n int) ([]string, string, time.Time) {
	t.Helper()
	ctx := context.Background()
	var vendorID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO vendor (display_name,legal_name,contact_email,status)
         VALUES ('V', 'V Ltd', 'v-perf-'||gen_random_uuid()||'@x.com', 'approved')
         RETURNING id`).Scan(&vendorID))

	var userID string
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO "user" (primary_email, display_name, role, status, plant)
         VALUES ('u-perf-'||gen_random_uuid()||'@x.com', 'U', 'employee', 'active', 'F12B-3F')
         RETURNING id`).Scan(&userID))

	supplyDate := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	cutoffAt := time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC)

	orderIDs := make([]string, n)
	for i := 0; i < n; i++ {
		var id string
		require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO "order" (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at, ready_at)
VALUES ($1, $2, 'F12B-3F', $3, 'ready', 100, now(), $4, now())
RETURNING id`,
			userID, vendorID, supplyDate, cutoffAt).Scan(&id))
		orderIDs[i] = id
	}
	return orderIDs, userID, supplyDate
}

func TestPickup_1000RacersPercentiles(t *testing.T) {
	if testing.Short() {
		t.Skip("perf test skipped under -short")
	}

	pool, cleanup := setupPgWithHighConns(t)
	defer cleanup()
	ctx := context.Background()

	const N = 1000
	orderIDs, userID, _ := seedReadyOrders(t, pool, N)

	orderRepo := opg.NewOrderRepo(pool)
	stateRepo := opg.NewStateEventRepo(pool)
	auditRepo := opg.NewAuditRepo(pool)
	outboxRepo := opg.NewOutboxRepo(pool)
	svc := &order.Service{
		Pool:     pool,
		Orders:   orderRepo,
		OrdersTx: orderRepo,
		StateTx:  stateRepo,
		AuditTx:  auditRepo,
		OutboxTx: outboxRepo,
		Clock:    fixedClock{T: time.Now()},
	}

	durations := make([]time.Duration, N)
	var wg sync.WaitGroup
	wg.Add(N)
	start := time.Now()
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			t0 := time.Now()
			err := svc.Pickup(ctx, orderIDs[i], userID)
			durations[i] = time.Since(t0)
			if err != nil {
				t.Errorf("pickup [%d] failed: %v", i, err)
			}
		}()
	}
	wg.Wait()
	total := time.Since(start)

	// Percentiles
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p := func(pct float64) time.Duration {
		idx := int(math.Ceil(pct*float64(N))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= N {
			idx = N - 1
		}
		return durations[idx]
	}
	p50, p95, p99, maxD := p(0.50), p(0.95), p(0.99), durations[N-1]

	t.Logf("PERF: N=%d total=%v throughput=%.0f/s p50=%v p95=%v p99=%v max=%v",
		N, total, float64(N)/total.Seconds(), p50, p95, p99, maxD)

	// Design SLO (§9.2) is p95 < 100ms for a SINGLE Pickup. This perf gate fires
	// 1000 simultaneously — under that synthetic stress the per-op numbers are
	// dominated by pool/Postgres queue contention rather than service time.
	// The thresholds below are regression guards for the saturated case, not the
	// design SLO.
	require.Less(t, p50.Milliseconds(), int64(2000), "p50 regressed beyond 2s (got %v)", p50)
	require.Less(t, p95.Milliseconds(), int64(3000), "p95 regressed beyond 3s (got %v)", p95)
	require.Less(t, p99.Milliseconds(), int64(5000), "p99 regressed beyond 5s (got %v)", p99)

	// Confirm all transitioned to picked_up.
	var pickedCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM "order" WHERE status='picked_up'`).Scan(&pickedCount))
	require.Equal(t, N, pickedCount)

	_ = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})) // suppress unused import warning
}
