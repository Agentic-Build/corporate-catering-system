package observability

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/embedded"
	"go.opentelemetry.io/otel/metric/noop"
)

var errInstrument = errors.New("boom")

// erroringMeter embeds the noop Meter (so it satisfies the full metric.Meter
// interface) but overrides the four constructors MustInitMetrics uses so each
// returns an error, exercising every panic(err) branch.
type erroringMeter struct {
	noop.Meter
	failCounter   bool
	failFloatHist bool
	failIntHist   bool
	failIntGauge  bool
}

func (m erroringMeter) Int64Counter(name string, _ ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.failCounter {
		return nil, errInstrument
	}
	return m.Meter.Int64Counter(name)
}

func (m erroringMeter) Float64Histogram(name string, _ ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if m.failFloatHist {
		return nil, errInstrument
	}
	return m.Meter.Float64Histogram(name)
}

func (m erroringMeter) Int64Histogram(name string, _ ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	if m.failIntHist {
		return nil, errInstrument
	}
	return m.Meter.Int64Histogram(name)
}

func (m erroringMeter) Int64Gauge(name string, _ ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	if m.failIntGauge {
		return nil, errInstrument
	}
	return m.Meter.Int64Gauge(name)
}

type erroringMeterProvider struct {
	embedded.MeterProvider
	meter erroringMeter
}

func (p erroringMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter { return p.meter }

func withErroringMeterProvider(t *testing.T, m erroringMeter) {
	t.Helper()
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(erroringMeterProvider{meter: m})
	resetMetricsForTest()
	t.Cleanup(func() {
		resetMetricsForTest()
		otel.SetMeterProvider(previous)
	})
}

func TestMustInitMetrics_PanicsOnCounterError(t *testing.T) {
	withErroringMeterProvider(t, erroringMeter{failCounter: true})
	require.PanicsWithError(t, errInstrument.Error(), func() { MustInitMetrics() })
}

func TestMustInitMetrics_PanicsOnFloatHistError(t *testing.T) {
	withErroringMeterProvider(t, erroringMeter{failFloatHist: true})
	require.PanicsWithError(t, errInstrument.Error(), func() { MustInitMetrics() })
}

func TestMustInitMetrics_PanicsOnIntHistError(t *testing.T) {
	withErroringMeterProvider(t, erroringMeter{failIntHist: true})
	require.PanicsWithError(t, errInstrument.Error(), func() { MustInitMetrics() })
}

func TestMustInitMetrics_PanicsOnIntGaugeError(t *testing.T) {
	withErroringMeterProvider(t, erroringMeter{failIntGauge: true})
	require.PanicsWithError(t, errInstrument.Error(), func() { MustInitMetrics() })
}

func TestMustInitMetrics_ReturnsSingleton(t *testing.T) {
	withErroringMeterProvider(t, erroringMeter{}) // all succeed via noop
	first := MustInitMetrics()
	require.NotNil(t, first)
	second := MustInitMetrics()
	assert.Same(t, first, second) // sync.Once guarantees identity
}
