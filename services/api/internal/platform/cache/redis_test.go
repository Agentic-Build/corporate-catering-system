package cache_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/cache"
)

// TestNewClient_ParseError exercises the redis.ParseURL failure branch: a
// syntactically invalid URL fails before any dial happens, so no backend is
// needed.
func TestNewClient_ParseError(t *testing.T) {
	c, err := cache.NewClient(context.Background(), "://not a redis url")
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "parse url")
}

// TestNewClient_PingError exercises the Ping-failure branch: the URL parses
// fine but points at a closed port, so the explicit Ping fails and the client
// is closed before the error is returned.
func TestNewClient_PingError(t *testing.T) {
	// 127.0.0.1:1 has no redis listening; ParseURL succeeds (lazy connect)
	// and the explicit Ping is what fails.
	c, err := cache.NewClient(context.Background(), "redis://127.0.0.1:1/0")
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "ping")
}

// TestNewClient_Success spins up a real redis and verifies the happy path:
// the client connects, the timeouts are applied, and the returned client is
// usable.
func TestNewClient_Success(t *testing.T) {
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	url, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	c, err := cache.NewClient(ctx, url)
	require.NoError(t, err)
	require.NotNil(t, c)
	t.Cleanup(func() { _ = c.Close() })

	// The returned client is live and usable.
	require.NoError(t, c.Ping(ctx).Err())

	// The configured timeouts were applied.
	opt := c.Options()
	assert.Equal(t, 3, int(opt.DialTimeout.Seconds()))
	assert.Equal(t, 2, int(opt.ReadTimeout.Seconds()))
	assert.Equal(t, 2, int(opt.WriteTimeout.Seconds()))
}
