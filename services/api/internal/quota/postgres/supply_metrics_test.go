package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/takalawang/corporate-catering-system/services/api/internal/quota"
	"github.com/takalawang/corporate-catering-system/services/api/internal/quota/postgres"
)

// gaugePoints returns the int64 gauge data points for the named instrument
// collected by the reader, failing the test if the instrument is missing or is
// not an Int64 gauge.
func gaugePoints(t *testing.T, rm *metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "metric %s is not an Int64 gauge (got %T)", name, m.Data)
			return g.DataPoints
		}
	}
	t.Fatalf("metric %s not found in collected output", name)
	return nil
}

// attrStr extracts a string attribute value from a data point, failing if absent.
func attrStr(t *testing.T, dp metricdata.DataPoint[int64], key string) string {
	t.Helper()
	v, ok := dp.Attributes.Value(attribute.Key(key))
	require.True(t, ok, "attribute %q missing on data point", key)
	return v.AsString()
}

func TestRegisterSupplyGauges(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vendorID := seedApprovedVendor(t, pool)
	itemID := seedActiveItem(t, pool, vendorID)
	repo := postgres.NewSupplyRepo(pool)
	ctx := context.Background()

	today := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, &quota.Supply{
		MenuItemID:   itemID,
		SupplyDate:   today,
		Capacity:     80,
		Remain:       55,
		PickupWindow: "11:50-12:10",
		ETALabel:     "11:50-12:10",
		CutoffAt:     time.Now().Add(24 * time.Hour),
	}))

	// Wire a ManualReader MeterProvider and make it the global provider BEFORE
	// registering the gauges (RegisterSupplyGauges reads otel.GetMeterProvider).
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() {
		_ = mp.Shutdown(context.Background())
		otel.SetMeterProvider(otel.GetMeterProvider()) // best-effort; harmless
	})

	require.NoError(t, postgres.RegisterSupplyGauges(pool))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	wantDate := today.Format("2006-01-02")

	// tbite_supply_capacity — summed per (vendor, date)
	capPts := gaugePoints(t, &rm, "tbite_supply_capacity")
	require.Len(t, capPts, 1)
	assert.Equal(t, int64(80), capPts[0].Value)
	assert.Equal(t, vendorID, attrStr(t, capPts[0], "vendor_id"))
	assert.Equal(t, wantDate, attrStr(t, capPts[0], "supply_date"))

	// tbite_supply_remain — summed per (vendor, date)
	remPts := gaugePoints(t, &rm, "tbite_supply_remain")
	require.Len(t, remPts, 1)
	assert.Equal(t, int64(55), remPts[0].Value)
	assert.Equal(t, vendorID, attrStr(t, remPts[0], "vendor_id"))
	assert.Equal(t, wantDate, attrStr(t, remPts[0], "supply_date"))

	// tbite_item_supply_capacity — per item
	itemPts := gaugePoints(t, &rm, "tbite_item_supply_capacity")
	require.Len(t, itemPts, 1)
	assert.Equal(t, int64(80), itemPts[0].Value)
	assert.Equal(t, itemID, attrStr(t, itemPts[0], "menu_item_id"))
	assert.Equal(t, vendorID, attrStr(t, itemPts[0], "vendor_id"))
	assert.Equal(t, "vendor-default", attrStr(t, itemPts[0], "vendor_name"))
	assert.NotEmpty(t, attrStr(t, itemPts[0], "item_name"))
}
