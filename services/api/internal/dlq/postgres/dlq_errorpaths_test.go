package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq/postgres"
)

// unmarshalable returns a payload that json.Marshal cannot encode (channel
// values are not JSON-serialisable), used to exercise the marshal-error
// branches of Write.
func unmarshalable() map[string]any {
	return map[string]any{"bad": make(chan int)}
}

// closedPool spins up a real Postgres, runs migrations, then closes the pool so
// every subsequent pool operation fails deterministically with a "closed pool"
// error. This drives the query/exec error-return branches without flaky timing.
func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, cleanup := setupPostgres(t)
	t.Cleanup(cleanup)
	pool.Close()
	return pool
}

// ---- Write: nil defaulting + marshal/insert errors ----

// TestDLQRepo_Write_NilPayloadHeaders covers the payload==nil and headers==nil
// branches: Write must substitute empty maps and persist them as {} jsonb.
func TestDLQRepo_Write_NilPayloadHeaders(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := &dlq.Message{
		SourceStream:   "ORDERS_V1",
		SourceSubject:  "order.placed.v1",
		SourceConsumer: "order-projector",
		Payload:        nil,
		Headers:        nil,
		LastError:      "boom",
	}
	require.NoError(t, repo.Write(ctx, m))
	// Receiver is back-filled with empty (non-nil) maps.
	require.NotNil(t, m.Payload)
	require.NotNil(t, m.Headers)
	assert.Empty(t, m.Payload)
	assert.Empty(t, m.Headers)

	got, err := repo.GetByID(ctx, m.ID)
	require.NoError(t, err)
	// Persisted as empty JSON objects, read back as empty maps.
	assert.Empty(t, got.Payload)
	assert.Empty(t, got.Headers)
}

func TestDLQRepo_Write_PayloadMarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := newMessage()
	m.Payload = unmarshalable()
	err := repo.Write(ctx, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal dlq payload")
}

func TestDLQRepo_Write_HeadersMarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := newMessage()
	m.Headers = unmarshalable()
	err := repo.Write(ctx, m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal dlq headers")
}

func TestDLQRepo_Write_InsertError(t *testing.T) {
	pool := closedPool(t)
	repo := pgrepo.NewDLQRepo(pool)

	err := repo.Write(context.Background(), newMessage())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insert dlq")
}

// ---- GetByID: non-NoRows scan error ----

func TestDLQRepo_GetByID_ScanError(t *testing.T) {
	pool := closedPool(t)
	repo := pgrepo.NewDLQRepo(pool)

	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	// Closed pool is not ErrNoRows -> generic scan wrap, not ErrMessageNotFound.
	assert.NotErrorIs(t, err, dlq.ErrMessageNotFound)
	assert.Contains(t, err.Error(), "scan dlq")
}

// ---- ListPending: default limit + query error ----

// TestDLQRepo_ListPending_DefaultLimit covers the limit<=0 branch (limit reset
// to 100). With fewer than 100 rows the query still returns all pending rows.
func TestDLQRepo_ListPending_DefaultLimit(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := newMessage()
	require.NoError(t, repo.Write(ctx, m))

	got, err := repo.ListPending(ctx, "", 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, m.ID, got[0].ID)

	gotNeg, err := repo.ListPending(ctx, "", -5)
	require.NoError(t, err)
	require.Len(t, gotNeg, 1)
}

func TestDLQRepo_ListPending_QueryError(t *testing.T) {
	pool := closedPool(t)
	repo := pgrepo.NewDLQRepo(pool)

	_, err := repo.ListPending(context.Background(), "", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query dlq")
}

// ---- markTerminal: exec error + existence-probe error ----

func TestDLQRepo_MarkReplayed_ExecError(t *testing.T) {
	pool := closedPool(t)
	repo := pgrepo.NewDLQRepo(pool)

	err := repo.MarkReplayed(context.Background(), "00000000-0000-0000-0000-000000000000", "admin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update dlq")
}

func TestDLQRepo_MarkResolved_ExecError(t *testing.T) {
	pool := closedPool(t)
	repo := pgrepo.NewDLQRepo(pool)

	err := repo.MarkResolved(context.Background(), "00000000-0000-0000-0000-000000000000", "admin", "n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update dlq")
}

// ---- ListPending: in-loop scan error ----

// TestDLQRepo_ListPending_ScanError forces the per-row rows.Scan error return.
// source_stream is scanned into a non-pointer string; NULLing it makes pgx fail
// with "cannot scan NULL". Each test owns its container, so the schema edit is
// isolated.
func TestDLQRepo_ListPending_ScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := newMessage()
	require.NoError(t, repo.Write(ctx, m))

	_, err := pool.Exec(ctx, `ALTER TABLE dlq_message ALTER COLUMN source_stream DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE dlq_message SET source_stream = NULL WHERE id = $1`, m.ID)
	require.NoError(t, err)

	_, err = repo.ListPending(ctx, "", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan dlq row")
}

// ---- RegisterDLQGauges: callback query error surfaces through Collect ----

func TestRegisterDLQGauges_PendingQueryError(t *testing.T) {
	pool := closedPool(t)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, pgrepo.RegisterDLQGauges(pool))

	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dlq pending query")
}

// TestRegisterDLQGauges_PendingScanError seeds an unresolved row, then NULLs its
// source_stream so the GROUP-BY pending query yields a NULL source_stream that
// fails the non-pointer string scan inside the callback (dlq_metrics.go:66).
func TestRegisterDLQGauges_PendingScanError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := newMessage()
	require.NoError(t, repo.Write(ctx, m))

	_, err := pool.Exec(ctx, `ALTER TABLE dlq_message ALTER COLUMN source_stream DROP NOT NULL`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE dlq_message SET source_stream = NULL WHERE id = $1`, m.ID)
	require.NoError(t, err)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, pgrepo.RegisterDLQGauges(pool))

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dlq pending scan")
}

// TestRegisterDLQGauges_OldestQueryError drops first_seen_at, which the pending
// query never references but the oldest query does. The pending query therefore
// succeeds and the oldest QueryRow errors (dlq_metrics.go:77).
func TestRegisterDLQGauges_OldestQueryError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `ALTER TABLE dlq_message DROP COLUMN first_seen_at`)
	require.NoError(t, err)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	require.NoError(t, pgrepo.RegisterDLQGauges(pool))

	var rm metricdata.ResourceMetrics
	err = reader.Collect(ctx, &rm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dlq oldest query")
}
