package db

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var poolGauges struct {
	once     sync.Once
	initErr  error
	acquired metric.Int64ObservableGauge
	idle     metric.Int64ObservableGauge
	total    metric.Int64ObservableGauge
	max      metric.Int64ObservableGauge
	mu       sync.Mutex
	pools    []poolRef
}

type poolRef struct {
	pool *pgxpool.Pool
	role string
}

func RegisterPoolMetrics(pool *pgxpool.Pool, role string) error {
	if pool == nil {
		return nil
	}
	poolGauges.once.Do(initPoolGauges)
	if poolGauges.initErr != nil {
		return poolGauges.initErr
	}

	poolGauges.mu.Lock()
	defer poolGauges.mu.Unlock()
	for _, ref := range poolGauges.pools {
		if ref.pool == pool && ref.role == role {
			return nil
		}
	}
	poolGauges.pools = append(poolGauges.pools, poolRef{pool: pool, role: role})
	return nil
}

func initPoolGauges() {
	meter := otel.GetMeterProvider().Meter("tbite.api")
	gauges := []struct {
		name string
		desc string
		dst  *metric.Int64ObservableGauge
	}{
		{"tbite_db_pool_acquired_connections", "pgxpool acquired (in-use) connections.", &poolGauges.acquired},
		{"tbite_db_pool_idle_connections", "pgxpool idle (available) connections.", &poolGauges.idle},
		{"tbite_db_pool_total_connections", "pgxpool total (acquired+idle+constructing) connections.", &poolGauges.total},
		{"tbite_db_pool_max_connections", "pgxpool max connections budget.", &poolGauges.max},
	}
	for _, g := range gauges {
		gauge, err := meter.Int64ObservableGauge(g.name, metric.WithDescription(g.desc))
		if err != nil {
			poolGauges.initErr = err
			return
		}
		*g.dst = gauge
	}
	_, poolGauges.initErr = meter.RegisterCallback(observePools,
		poolGauges.acquired, poolGauges.idle, poolGauges.total, poolGauges.max)
}

func observePools(_ context.Context, o metric.Observer) error {
	poolGauges.mu.Lock()
	refs := append([]poolRef(nil), poolGauges.pools...)
	poolGauges.mu.Unlock()
	for _, ref := range refs {
		s := ref.pool.Stat()
		attrs := metric.WithAttributes(attribute.String("role", ref.role))
		o.ObserveInt64(poolGauges.acquired, int64(s.AcquiredConns()), attrs)
		o.ObserveInt64(poolGauges.idle, int64(s.IdleConns()), attrs)
		o.ObserveInt64(poolGauges.total, int64(s.TotalConns()), attrs)
		o.ObserveInt64(poolGauges.max, int64(s.MaxConns()), attrs)
	}
	return nil
}
