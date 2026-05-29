package httpserver

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
)

// PostgresChecker pings a pgx pool. The "name" parameter labels the
// pool in the readiness JSON ("postgres-rw" / "postgres-ro").
func PostgresChecker(name string, p *pgxpool.Pool) Checker {
	return CheckerFunc{N: name, F: func(ctx context.Context) error {
		if p == nil {
			return fmt.Errorf("pool not configured")
		}
		return p.Ping(ctx)
	}}
}

// NATSChecker verifies that the NATS connection is in a usable state.
// The check passes when the connection reports CONNECTED — short
// transient disconnects do not yet trip the probe, deliberately, so
// that a single broker rolling restart does not depool every pod.
func NATSChecker(name string, nc *nats.Conn) Checker {
	return CheckerFunc{N: name, F: func(_ context.Context) error {
		if nc == nil {
			return fmt.Errorf("nats not connected")
		}
		if !nc.IsConnected() {
			return fmt.Errorf("nats status %v", nc.Status())
		}
		return nil
	}}
}

// RedisChecker pings a redis/valkey client. Sessions, OIDC state, and
// realtime fanout subscriptions all live here, so any role that
// depends on Valkey must wire this check.
func RedisChecker(name string, c *redis.Client) Checker {
	return CheckerFunc{N: name, F: func(ctx context.Context) error {
		if c == nil {
			return fmt.Errorf("redis not configured")
		}
		return c.Ping(ctx).Err()
	}}
}

// ObjectStorageChecker verifies that the configured object-storage bucket is
// reachable. It uses a read-only bucket HEAD so readiness probes do not mutate
// storage state.
func ObjectStorageChecker(name string, c *storage.S3Client) Checker {
	return CheckerFunc{N: name, F: func(ctx context.Context) error {
		if c == nil {
			return fmt.Errorf("object storage not configured")
		}
		return c.Check(ctx)
	}}
}
