package readmodel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// setupRedisCache spins up a throwaway Redis and returns a RedisCache bound to
// it plus a cleanup func. Mirrors identity/redis's testcontainers helper.
func setupRedisCache(t *testing.T) (*RedisCache, *redis.Client, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("run redis container: %v", err)
	}
	addr, err := c.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	opt, err := redis.ParseURL(addr)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	rdb := redis.NewClient(opt)
	rc := &RedisCache{C: rdb, Prefix: "tbite:rm:"}
	return rc, rdb, func() {
		_ = rdb.Close()
		_ = c.Terminate(ctx)
	}
}

func TestRedisCacheGetMiss(t *testing.T) {
	rc, _, cleanup := setupRedisCache(t)
	defer cleanup()

	_, err := rc.Get(context.Background(), "home:absent")
	if !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get on absent key err = %v, want ErrCacheMiss", err)
	}
}

func TestRedisCacheSetGetRoundTrip(t *testing.T) {
	rc, rdb, cleanup := setupRedisCache(t)
	defer cleanup()
	ctx := context.Background()

	if err := rc.Set(ctx, "home:u1", []byte("payload"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := rc.Get(ctx, "home:u1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("Get value = %q, want %q", got, "payload")
	}

	// Stored under the prefixed key so eviction patterns can target the surface.
	raw, err := rdb.Get(ctx, "tbite:rm:home:u1").Bytes()
	if err != nil {
		t.Fatalf("raw Get on prefixed key: %v", err)
	}
	if string(raw) != "payload" {
		t.Fatalf("prefixed value = %q, want %q", raw, "payload")
	}
}

func TestRedisCacheSetTTLApplied(t *testing.T) {
	rc, rdb, cleanup := setupRedisCache(t)
	defer cleanup()
	ctx := context.Background()

	if err := rc.Set(ctx, "home:ttl", []byte("v"), 90*time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ttl, err := rdb.TTL(ctx, "tbite:rm:home:ttl").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > 90*time.Second {
		t.Fatalf("TTL = %v, want (0, 90s]", ttl)
	}
}

func TestRedisCacheInvalidatePattern(t *testing.T) {
	rc, rdb, cleanup := setupRedisCache(t)
	defer cleanup()
	ctx := context.Background()

	// Seed several home keys for plant-a plus an unrelated key.
	for _, k := range []string{"home:u1:plant-a:2026-05-26", "home:u2:plant-a:2026-05-26"} {
		if err := rc.Set(ctx, k, []byte("v"), time.Minute); err != nil {
			t.Fatalf("seed Set %s: %v", k, err)
		}
	}
	if err := rc.Set(ctx, "home:u1:plant-b:2026-05-26", []byte("v"), time.Minute); err != nil {
		t.Fatalf("seed Set other: %v", err)
	}

	// Invalidate every plant-a home key (wildcard user). Pattern is appended
	// to Prefix internally.
	if err := rc.Invalidate(ctx, "home:*:plant-a:2026-05-26"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	for _, k := range []string{"home:u1:plant-a:2026-05-26", "home:u2:plant-a:2026-05-26"} {
		if _, err := rc.Get(ctx, k); !errors.Is(err, ErrCacheMiss) {
			t.Errorf("key %s still present after invalidate (err=%v)", k, err)
		}
	}
	// The plant-b key is untouched.
	if _, err := rc.Get(ctx, "home:u1:plant-b:2026-05-26"); err != nil {
		t.Errorf("plant-b key wrongly invalidated: %v", err)
	}
	_ = rdb
}

func TestRedisCacheInvalidateNoMatch(t *testing.T) {
	rc, _, cleanup := setupRedisCache(t)
	defer cleanup()
	// SCAN over an empty/non-matching keyspace returns nil and never calls Del.
	if err := rc.Invalidate(context.Background(), "home:nomatch:*"); err != nil {
		t.Fatalf("Invalidate (no match): %v", err)
	}
}

func TestRedisCacheInvalidateManyKeysMultipleScanPages(t *testing.T) {
	rc, _, cleanup := setupRedisCache(t)
	defer cleanup()
	ctx := context.Background()

	// Seed more than one SCAN page (count 256) so the cursor loop iterates
	// and the Del branch runs across pages.
	const n = 600
	for i := 0; i < n; i++ {
		key := "pop:plant-a:2026-05-" + pad(i)
		if err := rc.Set(ctx, key, []byte("v"), time.Minute); err != nil {
			t.Fatalf("seed Set %d: %v", i, err)
		}
	}
	if err := rc.Invalidate(ctx, "pop:plant-a:*"); err != nil {
		t.Fatalf("Invalidate many: %v", err)
	}
	// All gone.
	for i := 0; i < n; i++ {
		key := "pop:plant-a:2026-05-" + pad(i)
		if _, err := rc.Get(ctx, key); !errors.Is(err, ErrCacheMiss) {
			t.Fatalf("key %s survived bulk invalidate (err=%v)", key, err)
		}
	}
}

func TestRedisCacheInvalidateScanError(t *testing.T) {
	rc, rdb, cleanup := setupRedisCache(t)
	defer cleanup()
	// Closing the client makes the first SCAN fail, hitting the scan-error
	// wrap branch of Invalidate.
	if err := rdb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err := rc.Invalidate(context.Background(), "home:*")
	if err == nil {
		t.Fatalf("Invalidate on closed client = nil, want scan error")
	}
	if !strings.Contains(err.Error(), "scan ") {
		t.Fatalf("error = %v, want wrapped scan error", err)
	}
}

func TestRedisCacheGetErrorOnClosedClient(t *testing.T) {
	rc, rdb, cleanup := setupRedisCache(t)
	defer cleanup()
	if err := rdb.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Get on a closed client returns a non-Nil error (not ErrCacheMiss).
	_, err := rc.Get(context.Background(), "home:x")
	if err == nil || errors.Is(err, ErrCacheMiss) {
		t.Fatalf("Get on closed client err = %v, want a real (non-miss) error", err)
	}
}

func pad(i int) string {
	s := []byte{'0', '0', '0', '0'}
	for d := 3; d >= 0 && i > 0; d-- {
		s[d] = byte('0' + i%10)
		i /= 10
	}
	return string(s)
}
