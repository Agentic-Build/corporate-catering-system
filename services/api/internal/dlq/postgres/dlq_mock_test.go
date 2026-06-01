package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// These white-box tests use a pgxmock pool to drive two error legs that a real
// Postgres cannot reach deterministically without schema gymnastics:
//
//   - dlq_repo.go markTerminal: the EXISTS-probe Scan error leg.
//   - dlq_metrics.go callback: the pending rows.Err() error leg.

// TestMarkTerminal_ExistenceProbeScanError covers the markTerminal branch where
// the UPDATE affects zero rows, the EXISTS probe then runs, and the Scan of the
// boolean result fails. The probe row carries a string where a bool is expected,
// so Scan into *bool fails and the "probe dlq existence" error is returned.
func TestMarkTerminal_ExistenceProbeScanError(t *testing.T) {
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer mock.Close()

	const updateSQL = `
UPDATE dlq_message
SET replayed_at = now(), replayed_by = $2
WHERE id = $1 AND replayed_at IS NULL AND resolved_at IS NULL`

	// UPDATE affects zero rows -> fall through to the EXISTS probe.
	mock.ExpectExec(updateSQL).
		WithArgs("the-id", "admin").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	// EXISTS probe returns a row whose value cannot scan into *bool.
	mock.ExpectQuery(`SELECT EXISTS(SELECT 1 FROM dlq_message WHERE id=$1)`).
		WithArgs("the-id").
		WillReturnRows(pgxmock.NewRows([]string{"exists"}).AddRow("not-a-bool"))

	repo := &DLQRepo{pool: mock}
	err = repo.MarkReplayed(context.Background(), "the-id", "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "probe dlq existence")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRegisterDLQGauges_PendingRowsErr covers the dlq_metrics.go callback leg
// where iteration completes but rows.Err() reports an error. The pending query
// yields one clean row (Scan succeeds), then Rows.Err() returns an error via
// CloseError, surfacing the "dlq pending rows" wrap through reader.Collect.
func TestRegisterDLQGauges_PendingRowsErr(t *testing.T) {
	mock, err := pgxmock.NewPool(pgxmock.QueryMatcherOption(pgxmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(dlqPendingQuery).
		WillReturnRows(
			pgxmock.NewRows([]string{"source_stream", "count"}).
				AddRow("ORDERS_V1", int64(1)).
				CloseError(errors.New("rows iteration boom")),
		)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, RegisterDLQGauges(mock))

	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dlq pending rows")
}
