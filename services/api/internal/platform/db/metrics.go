package db

// pgxpool metrics. Names match vmalert TbiteDbPoolSaturation
// (chart/tbite-platform/templates/vmalert-rules.yaml) so the otel→prom
// collector emits the exact series the alert queries. role labels
// distinguish the rw and ro pools.

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

// RegisterPoolMetrics wires pgxpool.Stat into OTel observable gauges
// (acquired/idle/total/max connections). role tags the series so rw vs
// ro pools are distinguishable. Safe to call multiple times: instruments
// register once on the global meter; additional pools join the observed
// set.
func RegisterPoolMetrics(pool *pgxpool.Pool, role string) error {
	if pool == nil {
		return nil
	}
	poolGauges.once.Do(func() {
		meter := otel.GetMeterProvider().Meter("tbite.api")
		acquired, err := meter.Int64ObservableGauge("tbite_db_pool_acquired_connections",
			metric.WithDescription("pgxpool acquired (in-use) connections."))
		if err != nil {
			poolGauges.initErr = err
			return
		}
		idle, err := meter.Int64ObservableGauge("tbite_db_pool_idle_connections",
			metric.WithDescription("pgxpool idle (available) connections."))
		if err != nil {
			poolGauges.initErr = err
			return
		}
		total, err := meter.Int64ObservableGauge("tbite_db_pool_total_connections",
			metric.WithDescription("pgxpool total (acquired+idle+constructing) connections."))
		if err != nil {
			poolGauges.initErr = err
			return
		}
		maxG, err := meter.Int64ObservableGauge("tbite_db_pool_max_connections",
			metric.WithDescription("pgxpool max connections budget."))
		if err != nil {
			poolGauges.initErr = err
			return
		}
		poolGauges.acquired = acquired
		poolGauges.idle = idle
		poolGauges.total = total
		poolGauges.max = maxG

		_, poolGauges.initErr = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
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
		}, poolGauges.acquired, poolGauges.idle, poolGauges.total, poolGauges.max)
	})
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
