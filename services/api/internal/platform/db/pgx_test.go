package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/db"
)

func TestDefaultPoolConfig(t *testing.T) {
	pc := db.DefaultPoolConfig()
	assert.Equal(t, int32(16), pc.MaxConns)
	assert.Equal(t, int32(2), pc.MinConns)
}

// TestNewPoolWithConfig_ParseError exercises the dsn-parse error branch
// without needing any backend: a syntactically invalid DSN fails in
// pgxpool.ParseConfig before any dial happens.
func TestNewPoolWithConfig_ParseError(t *testing.T) {
	p, err := db.NewPoolWithConfig(context.Background(), "://not a dsn", db.DefaultPoolConfig())
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "parse dsn")
}

// TestNewPoolWithConfig_PingError exercises the ping-failure branch: the DSN
// parses fine but points at a closed port, so Ping fails and the pool is
// closed before the error is returned.
func TestNewPoolWithConfig_PingError(t *testing.T) {
	// 127.0.0.1:1 has no postgres listening; pgxpool.NewWithConfig itself
	// succeeds (lazy connect) and the explicit Ping is what fails.
	p, err := db.NewPoolWithConfig(
		context.Background(),
		"postgres://user:pw@127.0.0.1:1/tbite?sslmode=disable&connect_timeout=2",
		db.DefaultPoolConfig(),
	)
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "ping")
}

// TestNewPoolWithConfig_Success spins up a real postgres and verifies the
// happy path plus the config-clamping branches (MaxConns<=0 -> default,
// MinConns<0 -> 0, MinConns>MaxConns -> MaxConns).
func TestNewPoolWithConfig_Success(t *testing.T) {
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
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	t.Run("explicit budget", func(t *testing.T) {
		p, err := db.NewPoolWithConfig(ctx, dsn, db.PoolConfig{MaxConns: 5, MinConns: 1})
		require.NoError(t, err)
		t.Cleanup(p.Close)
		assert.Equal(t, int32(5), p.Config().MaxConns)
		assert.Equal(t, int32(1), p.Config().MinConns)
		require.NoError(t, p.Ping(ctx))
	})

	t.Run("clamps non-positive MaxConns to default and MinConns>Max to Max", func(t *testing.T) {
		// MaxConns<=0 -> default (16); MinConns(99) > MaxConns(16) -> 16.
		p, err := db.NewPoolWithConfig(ctx, dsn, db.PoolConfig{MaxConns: 0, MinConns: 99})
		require.NoError(t, err)
		t.Cleanup(p.Close)
		assert.Equal(t, int32(16), p.Config().MaxConns)
		assert.Equal(t, int32(16), p.Config().MinConns)
	})

	t.Run("clamps negative MinConns to zero", func(t *testing.T) {
		p, err := db.NewPoolWithConfig(ctx, dsn, db.PoolConfig{MaxConns: 4, MinConns: -3})
		require.NoError(t, err)
		t.Cleanup(p.Close)
		assert.Equal(t, int32(4), p.Config().MaxConns)
		assert.Equal(t, int32(0), p.Config().MinConns)
	})
}
