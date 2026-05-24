package db

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool = pgxpool.Pool

// PoolConfig carries the per-pool tuning knobs that are surfaced to
// the chart via DB_MAX_CONNS / DB_MIN_CONNS / DB_MAX_CONNS_RO /
// DB_MIN_CONNS_RO so that horizontal scaling never silently exceeds
// the backend connection budget (see ADR-0007 / issue #54).
type PoolConfig struct {
	MaxConns int32
	MinConns int32
}

// DefaultPoolConfig returns the conservative defaults used historically
// before the budget became an explicit chart value. Callers that load
// PoolConfig from environment should clamp to these only when the env
// vars are absent or invalid.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{MaxConns: 16, MinConns: 2}
}

// NewPool creates a pgxpool with the default budget. Retained for tests
// and short-lived tooling. Production roles must use NewPoolWithConfig.
func NewPool(ctx context.Context, dsn string) (*Pool, error) {
	return NewPoolWithConfig(ctx, dsn, DefaultPoolConfig())
}

// NewPoolWithConfig creates a pgxpool with an explicit budget. The
// caller is responsible for picking a budget that fits the database's
// max_connections / PgBouncer pool size when summed across all
// replicas of this role.
func NewPoolWithConfig(ctx context.Context, dsn string, pc PoolConfig) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	if pc.MaxConns <= 0 {
		pc.MaxConns = DefaultPoolConfig().MaxConns
	}
	if pc.MinConns < 0 {
		pc.MinConns = 0
	}
	if pc.MinConns > pc.MaxConns {
		pc.MinConns = pc.MaxConns
	}
	cfg.MaxConns = pc.MaxConns
	cfg.MinConns = pc.MinConns
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()

	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Ping(pingCtx); err != nil {
		p.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return p, nil
}
