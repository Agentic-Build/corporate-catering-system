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
	// services/api/internal/compliance/postgres/testhelper_test.go → ../../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
}

func migrateUp(dsn string) error {
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	if err != nil {
		return err
	}
	return m.Up()
}

var userSeedCounter atomic.Uint64

func seedUserWithRole(t *testing.T, pool *pgxpool.Pool, role string) string {
	t.Helper()
	n := userSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role)
VALUES ($1, $2, $3)
RETURNING id`,
		fmt.Sprintf("compliance-user-%d@test.com", n),
		fmt.Sprintf("compliance-user-%d", n),
		role,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedAdminUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	return seedUserWithRole(t, pool, "welfare_admin")
}

var vendorSeedCounter atomic.Uint64

// seedApprovedVendor inserts an approved vendor and returns its UUID.
func seedApprovedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := vendorSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved')
RETURNING id`,
		fmt.Sprintf("compliance-vendor-%d", n),
		fmt.Sprintf("compliance-vendor-%d Ltd", n),
		fmt.Sprintf("compliance-vendor-%d@test.com", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}
