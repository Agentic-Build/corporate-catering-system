package oidc_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
)

func setupRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcredis.Run(ctx, "redis:7-alpine")
	require.NoError(t, err)
	addr, err := c.ConnectionString(ctx)
	require.NoError(t, err)
	opt, err := redis.ParseURL(addr)
	require.NoError(t, err)
	rdb := redis.NewClient(opt)
	return rdb, func() {
		_ = rdb.Close()
		_ = c.Terminate(ctx)
	}
}

func TestStateStore_PutGetConsume(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := oidc.NewRedisStateStore(rdb, 5*time.Minute)

	payload := &oidc.StatePayload{
		App:          "employee",
		Provider:     "google",
		ReturnTo:     "/",
		PKCEVerifier: "vvvv",
		Nonce:        "nnnn",
	}
	require.NoError(t, s.Put(context.Background(), "abc123", payload))

	got, err := s.Get(context.Background(), "abc123")
	require.NoError(t, err)
	assert.Equal(t, "employee", got.App)
	assert.Equal(t, "vvvv", got.PKCEVerifier)

	require.NoError(t, s.Consume(context.Background(), "abc123"))
	_, err = s.Get(context.Background(), "abc123")
	assert.ErrorIs(t, err, oidc.ErrStateNotFound)
}

func TestStateStore_Get_NotFound(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := oidc.NewRedisStateStore(rdb, time.Minute)
	_, err := s.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, oidc.ErrStateNotFound)
}
