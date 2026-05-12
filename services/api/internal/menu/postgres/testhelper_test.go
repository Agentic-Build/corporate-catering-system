package postgres_test

import (
	"context"
	"path/filepath"
	"runtime"
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

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/menu/postgres/testhelper_test.go → ../../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
}

func migrateUp(dsn string) error {
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	if err != nil {
		return err
	}
	return m.Up()
}

// seedApprovedVendor inserts an approved vendor row and returns its UUID.
// Tests pass a unique email suffix to avoid the unique index when called more than once.
func seedApprovedVendor(t *testing.T, pool *pgxpool.Pool, emailSuffix ...string) string {
	t.Helper()
	suffix := "default"
	if len(emailSuffix) > 0 {
		suffix = emailSuffix[0]
	}
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved')
RETURNING id`,
		"vendor-"+suffix, "vendor-"+suffix+" Ltd", "vendor-"+suffix+"@test.com",
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedVendorWithStatus(t *testing.T, pool *pgxpool.Pool, status, emailSuffix string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, $4)
RETURNING id`,
		"vendor-"+emailSuffix, "vendor-"+emailSuffix+" Ltd", "vendor-"+emailSuffix+"@test.com", status,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedPlantMapping(t *testing.T, pool *pgxpool.Pool, vendorID, plant string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO vendor_plant_mapping (vendor_id, plant, active)
VALUES ($1, $2, true)`, vendorID, plant)
	require.NoError(t, err)
}

func seedMealSupply(t *testing.T, pool *pgxpool.Pool, itemID string, day time.Time, capacity, remain int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO meal_supply (menu_item_id, supply_date, capacity, remain, pickup_window, eta_label, cutoff_at)
VALUES ($1, $2, $3, $4, '11:50-12:10', '11:50-12:10', $5)`,
		itemID, day, capacity, remain, day.Add(10*time.Hour))
	require.NoError(t, err)
}
