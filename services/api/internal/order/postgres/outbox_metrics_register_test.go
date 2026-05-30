package postgres_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/embedded"
	"go.opentelemetry.io/otel/metric/noop"

	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
)

var errInstrument = errors.New("instrument boom")

// failingMeter embeds the noop meter (satisfying the full metric.Meter
// interface) and makes the gauge constructors fail after `okBefore` successful
// calls, so each of RegisterOutboxGauges' three instrument-creation error
// returns can be hit in turn.
type failingMeter struct {
	noop.Meter
	intCalls   int
	floatCalls int
	failIntAt  int // fail the Nth (1-based) Int64ObservableGauge call; 0 = never
	failFloat  int // fail the Nth (1-based) Float64ObservableGauge call; 0 = never
}

func (m *failingMeter) Int64ObservableGauge(name string, _ ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	m.intCalls++
	if m.failIntAt != 0 && m.intCalls == m.failIntAt {
		return nil, errInstrument
	}
	return m.Meter.Int64ObservableGauge(name)
}

func (m *failingMeter) Float64ObservableGauge(name string, _ ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	m.floatCalls++
	if m.failFloat != 0 && m.floatCalls == m.failFloat {
		return nil, errInstrument
	}
	return m.Meter.Float64ObservableGauge(name)
}

type failingMeterProvider struct {
	embedded.MeterProvider
	meter metric.Meter
}

func (p *failingMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter { return p.meter }

func TestRegisterOutboxGauges_InstrumentCreationErrors(t *testing.T) {
	cases := []struct {
		name  string
		meter *failingMeter
	}{
		{"pending-gauge", &failingMeter{failIntAt: 1}},
		{"oldest-gauge", &failingMeter{failFloat: 1}},
		{"oldest-unpublished-gauge", &failingMeter{failFloat: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			otel.SetMeterProvider(&failingMeterProvider{meter: tc.meter})
			err := pgrepo.RegisterOutboxGauges(nil) // pool unused: fails before any DB call
			require.Error(t, err)
			assert.ErrorIs(t, err, errInstrument)
		})
	}
}
