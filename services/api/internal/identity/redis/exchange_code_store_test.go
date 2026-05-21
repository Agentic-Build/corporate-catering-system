package redis_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
)

func TestExchangeCodeStore_PutAndConsume(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewExchangeCodeStore(rdb, 60*time.Second)

	require.NoError(t, s.Put(context.Background(), "code-abc", "tb_secret"))

	tok, ok, err := s.Consume(context.Background(), "code-abc")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "tb_secret", tok)
}

func TestExchangeCodeStore_Consume_OneTime(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewExchangeCodeStore(rdb, 60*time.Second)

	require.NoError(t, s.Put(context.Background(), "code-once", "tb_once"))
	_, ok, err := s.Consume(context.Background(), "code-once")
	require.NoError(t, err)
	assert.True(t, ok)

	// Second consume must fail (code is one-time-use).
	_, ok, err = s.Consume(context.Background(), "code-once")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestExchangeCodeStore_Consume_Unknown(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewExchangeCodeStore(rdb, 60*time.Second)

	_, ok, err := s.Consume(context.Background(), "code-missing")
	require.NoError(t, err)
	assert.False(t, ok)
}
