package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/db"
)

// TestRegisterPoolMetrics_NilPool covers the early return when a nil pool is
// passed: it must be a no-op that never registers a ref nor errors.
func TestRegisterPoolMetrics_NilPool(t *testing.T) {
	require.NoError(t, db.RegisterPoolMetrics(nil, "rw"))
}

// TestRegisterPoolMetrics_Idempotent covers the dedup branch: registering the
// same (pool, role) twice returns nil on the second call without appending a
// duplicate ref.
func TestRegisterPoolMetrics_Idempotent(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://user:pw@127.0.0.1:1/tbite?sslmode=disable")
	require.NoError(t, err)
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, db.RegisterPoolMetrics(pool, "ro"))
	// Second identical registration must hit the "already registered" branch.
	require.NoError(t, db.RegisterPoolMetrics(pool, "ro"))
}
