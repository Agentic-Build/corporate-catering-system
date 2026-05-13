package redis_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idredis "github.com/takalawang/corporate-catering-system/services/api/internal/identity/redis"
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

func TestSessionStore_CreateAndGet(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, 7*24*time.Hour)
	sess, err := s.Create(context.Background(), "user-1", identity.RoleEmployee)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(sess.Token, "tb_"))
	assert.Greater(t, len(sess.Token), 16)

	got, err := s.Get(context.Background(), sess.Token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", got.UserID)
	assert.Equal(t, identity.RoleEmployee, got.Role)
}

func TestSessionStore_Get_NotFound(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, time.Hour)
	_, err := s.Get(context.Background(), "tb_nonexistent")
	assert.ErrorIs(t, err, identity.ErrSessionNotFound)
}

func TestSessionStore_Touch(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, time.Hour)
	sess, _ := s.Create(context.Background(), "user-touch", identity.RoleEmployee)
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, s.Touch(context.Background(), sess.Token))
	got, err := s.Get(context.Background(), sess.Token)
	require.NoError(t, err)
	assert.True(t, got.LastSeenAt.After(sess.LastSeenAt))
}

func TestSessionStore_Revoke(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, time.Hour)
	sess, _ := s.Create(context.Background(), "user-revoke", identity.RoleEmployee)
	require.NoError(t, s.Revoke(context.Background(), sess.Token))
	_, err := s.Get(context.Background(), sess.Token)
	assert.ErrorIs(t, err, identity.ErrSessionNotFound)
}

func TestSessionStore_RevokeAllForUser(t *testing.T) {
	rdb, cleanup := setupRedis(t)
	defer cleanup()
	s := idredis.NewSessionStore(rdb, time.Hour)
	s1, _ := s.Create(context.Background(), "user-multi", identity.RoleEmployee)
	s2, _ := s.Create(context.Background(), "user-multi", identity.RoleEmployee)
	require.NoError(t, s.RevokeAllForUser(context.Background(), "user-multi"))
	_, e1 := s.Get(context.Background(), s1.Token)
	_, e2 := s.Get(context.Background(), s2.Token)
	assert.ErrorIs(t, e1, identity.ErrSessionNotFound)
	assert.ErrorIs(t, e2, identity.ErrSessionNotFound)
}
