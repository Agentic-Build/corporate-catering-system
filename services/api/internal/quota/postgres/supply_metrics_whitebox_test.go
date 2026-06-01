package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// failingGaugeMeter embeds the noop meter but returns an error on the failOn-th
// (1-based) Int64ObservableGauge call, letting us exercise each gauge-creation
// error branch in RegisterSupplyGauges.
type failingGaugeMeter struct {
	noop.Meter
	failOn int
	calls  int
}

func (m *failingGaugeMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	m.calls++
	if m.calls == m.failOn {
		return nil, errors.New("gauge create failed")
	}
	return m.Meter.Int64ObservableGauge(name, opts...)
}

type failingGaugeProvider struct {
	noop.MeterProvider
	meter *failingGaugeMeter
}

func (p *failingGaugeProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return p.meter
}

// closedPool returns a pgxpool that has been closed, so any Query/Exec on it
// returns a non-nil error without needing a real database container. pgxpool.New
// connects lazily, so creation against a syntactically valid DSN succeeds.
func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(),
		"postgres://tbite:tbite@127.0.0.1:1/tbite?sslmode=disable")
	require.NoError(t, err)
	p.Close()
	return p
}

// fakeRows is a minimal stand-in for the row interface scanSupplyRows accepts,
// letting us drive its scan-error and Err() branches without a real DB.
type fakeRows struct {
	scanErr error // returned from Scan on the first Next()-true iteration
	rowsErr error // returned from Err()
	emitOne bool  // if true, Next() yields exactly one row
	served  bool
}

func (f *fakeRows) Next() bool {
	if !f.emitOne || f.served {
		return false
	}
	f.served = true
	return true
}

func (f *fakeRows) Scan(dest ...any) error { return f.scanErr }
func (f *fakeRows) Err() error             { return f.rowsErr }

// captureObserver grabs a real metric.Observer from a registered callback so
// scanSupplyRows can be exercised in isolation. The SDK only hands out a valid
// Observer inside a callback, so we trigger one Collect to capture it.
func captureObserver(t *testing.T) (metric.Observer, metric.Int64ObservableGauge, func()) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test.capture")
	g, err := meter.Int64ObservableGauge("capture_gauge")
	require.NoError(t, err)

	obsCh := make(chan metric.Observer, 1)
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		select {
		case obsCh <- o:
		default:
		}
		return nil
	}, g)
	require.NoError(t, err)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	o := <-obsCh
	return o, g, func() { _ = mp.Shutdown(context.Background()) }
}

func TestScanSupplyRows_ScanError(t *testing.T) {
	o, g, done := captureObserver(t)
	defer done()
	_, _, err := scanSupplyRows(&fakeRows{emitOne: true, scanErr: errors.New("boom")}, o, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "supply gauges scan")
}

func TestScanSupplyRows_RowsErr(t *testing.T) {
	o, g, done := captureObserver(t)
	defer done()
	_, _, err := scanSupplyRows(&fakeRows{emitOne: false, rowsErr: errors.New("conn lost")}, o, g)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "supply gauges rows")
}

func TestScanSupplyRows_Empty(t *testing.T) {
	o, g, done := captureObserver(t)
	defer done()
	capByVD, remByVD, err := scanSupplyRows(&fakeRows{}, o, g)
	require.NoError(t, err)
	assert.Empty(t, capByVD)
	assert.Empty(t, remByVD)
}

// TestRegisterSupplyGauges_CallbackQueryError closes the pool so the registered
// callback's query fails on Collect, exercising observeSupplyGauges' query-error
// branch.
func TestRegisterSupplyGauges_CallbackQueryError(t *testing.T) {
	pool := closedPool(t)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, RegisterSupplyGauges(pool))

	var rm metricdata.ResourceMetrics
	// The SDK surfaces the callback error from Collect.
	err := reader.Collect(context.Background(), &rm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "supply gauges query")
}

// TestRegisterSupplyGauges_GaugeCreateErrors exercises each of the three
// Int64ObservableGauge creation error returns by failing the 1st, 2nd, then 3rd
// gauge creation in turn.
func TestRegisterSupplyGauges_GaugeCreateErrors(t *testing.T) {
	pool := closedPool(t) // never queried; creation fails before any DB use
	prev := otel.GetMeterProvider()
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	for _, failOn := range []int{1, 2, 3} {
		otel.SetMeterProvider(&failingGaugeProvider{meter: &failingGaugeMeter{failOn: failOn}})
		err := RegisterSupplyGauges(pool)
		require.Error(t, err, "failOn=%d should bubble the gauge-create error", failOn)
		assert.Contains(t, err.Error(), "gauge create failed")
	}
}
