package oidc_test

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

// closedRedis returns a client pointed at a closed server so every command
// errors with a non-redis.Nil error, exercising the wrapped-error paths.
func closedRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listening
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestPut_RedisError(t *testing.T) {
	s := oidc.NewRedisStateStore(closedRedis(t), time.Minute)
	err := s.Put(context.Background(), "k", &oidc.StatePayload{App: "employee"})
	require.Error(t, err)
}

func TestGet_RedisError(t *testing.T) {
	s := oidc.NewRedisStateStore(closedRedis(t), time.Minute)
	_, err := s.Get(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis get")
}

func TestConsume_RedisError(t *testing.T) {
	s := oidc.NewRedisStateStore(closedRedis(t), time.Minute)
	err := s.Consume(context.Background(), "k")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis get state for consume")
}

// TestGet_ConsumedKeyDecodeError stores a corrupt value under the consumed key
// (with the live key absent) so Get hits decodeStatePayload's unmarshal error.
func TestGet_ConsumedKeyDecodeError(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := oidc.NewRedisStateStore(rdb, time.Minute)

	ctx := context.Background()
	// Write garbage directly under the consumed key namespace.
	require.NoError(t, rdb.Set(ctx, "oidc-consumed:badstate", "{not-json", time.Minute).Err())

	_, err := s.Get(ctx, "badstate")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode state")
}

// TestGet_LiveKeyDecodeError writes garbage under the live key so the final
// decodeStatePayload (non-consumed branch) returns its unmarshal error.
func TestGet_LiveKeyDecodeError(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := oidc.NewRedisStateStore(rdb, time.Minute)

	ctx := context.Background()
	require.NoError(t, rdb.Set(ctx, "oidc:livebad", "{nope", time.Minute).Err())

	_, err := s.Get(ctx, "livebad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode state")
}

// TestConsume_NoLiveKey covers the redis.Nil early-return (state already gone).
func TestConsume_NoLiveKey(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := oidc.NewRedisStateStore(rdb, time.Minute)
	require.NoError(t, s.Consume(context.Background(), "never-existed"))
}
