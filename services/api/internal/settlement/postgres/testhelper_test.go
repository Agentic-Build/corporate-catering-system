package postgres_test

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
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgres(t *testing.T) (*pgxpool.Pool, func()) {
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
	if err := migrateUp(dsn); err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("migrate: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("pool: %v", err)
	}
	return pool, func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
}

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/settlement/postgres/testhelper_test.go → ../../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
}

func migrateUp(dsn string) error {
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	if err != nil {
		return err
	}
	return m.Up()
}

var (
	userSeedCounter   atomic.Uint64
	vendorSeedCounter atomic.Uint64
	itemSeedCounter   atomic.Uint64
)

func seedAdminUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := userSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role)
VALUES ($1, $2, 'welfare_admin') RETURNING id`,
		fmt.Sprintf("settlement-repo-admin-%d@test.com", n),
		fmt.Sprintf("settlement-repo-admin-%d", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedEmployeeUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := userSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role)
VALUES ($1, $2, 'employee') RETURNING id`,
		fmt.Sprintf("settlement-repo-user-%d@test.com", n),
		fmt.Sprintf("settlement-repo-user-%d", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := vendorSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved') RETURNING id`,
		fmt.Sprintf("settlement-repo-vendor-%d", n),
		fmt.Sprintf("settlement-repo-vendor-%d Ltd", n),
		fmt.Sprintf("settlement-repo-vendor-%d@test.com", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedMenuItem(t *testing.T, pool *pgxpool.Pool, vendorID string, priceMinor int64) string {
	t.Helper()
	n := itemSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO menu_item (vendor_id, name, description, price_minor, status, tags, badges)
VALUES ($1, $2, '', $3, 'active', '{}', '{}') RETURNING id`,
		vendorID, fmt.Sprintf("settlement-repo-item-%d", n), priceMinor,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedOrder inserts an order row directly (bypassing order.Service) so tests can
// land orders in any lifecycle status. When qty > 0 it also inserts a matching
// order_item so portion aggregation has something to sum.
func seedOrder(t *testing.T, pool *pgxpool.Pool, userID, vendorID string, supplyDate time.Time, status string, amount int64, qty int) string {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at, totp_secret)
VALUES ($1, $2, 'F12B-3F', $3, $4::order_status, $5, $6, $7, $8) RETURNING id`,
		userID, vendorID, supplyDate, status, amount,
		supplyDate.Add(-6*time.Hour), supplyDate.Add(-1*time.Hour), secret,
	).Scan(&id)
	require.NoError(t, err)
	if qty > 0 {
		item := seedMenuItem(t, pool, vendorID, amount)
		_, err := pool.Exec(context.Background(), `
INSERT INTO order_item (order_id, menu_item_id, qty, unit_price_minor)
VALUES ($1, $2, $3, $4)`, id, item, qty, amount)
		require.NoError(t, err)
	}
	return id
}
