package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
)

func TestPostgresChecker(t *testing.T) {
	t.Run("name", func(t *testing.T) {
		assert.Equal(t, "postgres-rw", PostgresChecker("postgres-rw", nil).Name())
	})

	t.Run("nil pool errors", func(t *testing.T) {
		err := PostgresChecker("postgres-rw", nil).Check(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool not configured")
	})

	t.Run("unreachable pool surfaces ping error", func(t *testing.T) {
		// Point at a closed port; ParseConfig must succeed but Ping must fail,
		// exercising the `return p.Ping(ctx)` non-nil branch.
		cfg, err := pgxpool.ParseConfig("postgres://u:p@127.0.0.1:1/db?connect_timeout=1")
		require.NoError(t, err)
		pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
		require.NoError(t, err)
		defer pool.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		assert.Error(t, PostgresChecker("postgres-rw", pool).Check(ctx))
	})
}

func TestRedisChecker(t *testing.T) {
	t.Run("nil client errors", func(t *testing.T) {
		err := RedisChecker("valkey", nil).Check(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "redis not configured")
		assert.Equal(t, "valkey", RedisChecker("valkey", nil).Name())
	})

	t.Run("unreachable client surfaces ping error", func(t *testing.T) {
		c := redis.NewClient(&redis.Options{
			Addr:        "127.0.0.1:1",
			DialTimeout: 500 * time.Millisecond,
		})
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		assert.Error(t, RedisChecker("valkey", c).Check(ctx))
	})
}

func TestObjectStorageChecker(t *testing.T) {
	t.Run("nil client errors", func(t *testing.T) {
		err := ObjectStorageChecker("object-storage", nil).Check(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "object storage not configured")
		assert.Equal(t, "object-storage", ObjectStorageChecker("object-storage", nil).Name())
	})

	t.Run("reachable bucket passes", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		c := newS3(t, srv.URL)
		assert.NoError(t, ObjectStorageChecker("object-storage", c).Check(context.Background()))
	})

	t.Run("missing bucket fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := newS3(t, srv.URL)
		assert.Error(t, ObjectStorageChecker("object-storage", c).Check(context.Background()))
	})
}

func newS3(t *testing.T, endpoint string) *storage.S3Client {
	t.Helper()
	c, err := storage.NewS3(context.Background(), storage.S3Config{
		Endpoint:        endpoint,
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		Bucket:          "tbite",
		UsePathStyle:    true,
	})
	require.NoError(t, err)
	return c
}

func TestNATSChecker(t *testing.T) {
	t.Run("nil conn errors", func(t *testing.T) {
		err := NATSChecker("nats", nil).Check(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nats not connected")
		assert.Equal(t, "nats", NATSChecker("nats", nil).Name())
	})

	t.Run("disconnected conn errors", func(t *testing.T) {
		// A zero-value Conn reports a non-CONNECTED status.
		err := NATSChecker("nats", &nats.Conn{}).Check(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nats status")
	})

	t.Run("connected conn passes", func(t *testing.T) {
		ctx := context.Background()
		c, err := tcnats.Run(ctx, "nats:2.10-alpine")
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Terminate(ctx) })
		url, err := c.ConnectionString(ctx)
		require.NoError(t, err)

		var nc *nats.Conn
		deadline := time.Now().Add(10 * time.Second)
		for {
			nc, err = nats.Connect(url)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				require.NoError(t, err)
			}
			time.Sleep(200 * time.Millisecond)
		}
		defer nc.Close()
		assert.NoError(t, NATSChecker("nats", nc).Check(ctx))
	})
}
