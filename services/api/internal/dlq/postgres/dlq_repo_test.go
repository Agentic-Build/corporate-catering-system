package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq/postgres"
)

func newMessage() *dlq.Message {
	return &dlq.Message{
		SourceStream:   "ORDERS_V1",
		SourceSubject:  "order.placed.v1",
		SourceConsumer: "order-projector",
		Payload:        map[string]any{"order_id": "o-1", "qty": float64(3)},
		Headers:        map[string]any{"x-attempt": "5"},
		LastError:      "schema mismatch: unknown field 'foo'",
	}
}

func TestDLQRepo_Write(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := newMessage()
	require.NoError(t, repo.Write(ctx, m))
	require.NotEmpty(t, m.ID)
	require.False(t, m.FirstSeenAt.IsZero())
}

func TestDLQRepo_GetByID(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)

	m := newMessage()
	require.NoError(t, repo.Write(ctx, m))

	got, err := repo.GetByID(ctx, m.ID)
	require.NoError(t, err)
	assert.Equal(t, "ORDERS_V1", got.SourceStream)
	assert.Equal(t, "order.placed.v1", got.SourceSubject)
	assert.Equal(t, "order-projector", got.SourceConsumer)
	assert.Equal(t, "schema mismatch: unknown field 'foo'", got.LastError)
	require.NotNil(t, got.Payload)
	assert.Equal(t, "o-1", got.Payload["order_id"])
	assert.InDelta(t, 3.0, got.Payload["qty"], 0.0001)
	require.NotNil(t, got.Headers)
	assert.Equal(t, "5", got.Headers["x-attempt"])
	assert.Nil(t, got.ReplayedAt)
	assert.Nil(t, got.ResolvedAt)

	_, err = repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, dlq.ErrMessageNotFound)
}

func TestDLQRepo_ListPending(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)
	admin := seedAdminUser(t, pool)

	pendingOrders := newMessage()
	require.NoError(t, repo.Write(ctx, pendingOrders))

	pendingPayroll := newMessage()
	pendingPayroll.SourceStream = "PAYROLL_V1"
	pendingPayroll.SourceSubject = "payroll.batch_locked.v1"
	require.NoError(t, repo.Write(ctx, pendingPayroll))

	replayed := newMessage()
	require.NoError(t, repo.Write(ctx, replayed))
	require.NoError(t, repo.MarkReplayed(ctx, replayed.ID, admin))

	resolved := newMessage()
	require.NoError(t, repo.Write(ctx, resolved))
	require.NoError(t, repo.MarkResolved(ctx, resolved.ID, admin, "garbage"))

	// All streams: only pendingOrders + pendingPayroll.
	all, err := repo.ListPending(ctx, "", 100)
	require.NoError(t, err)
	require.Len(t, all, 2)
	ids := map[string]bool{}
	for _, m := range all {
		ids[m.ID] = true
	}
	assert.True(t, ids[pendingOrders.ID])
	assert.True(t, ids[pendingPayroll.ID])

	// Stream filter narrows to one.
	onlyOrders, err := repo.ListPending(ctx, "ORDERS_V1", 100)
	require.NoError(t, err)
	require.Len(t, onlyOrders, 1)
	assert.Equal(t, pendingOrders.ID, onlyOrders[0].ID)

	// Limit caps the result.
	capped, err := repo.ListPending(ctx, "", 1)
	require.NoError(t, err)
	require.Len(t, capped, 1)
}

func TestDLQRepo_MarkReplayed(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)
	admin := seedAdminUser(t, pool)

	m := newMessage()
	require.NoError(t, repo.Write(ctx, m))
	require.NoError(t, repo.MarkReplayed(ctx, m.ID, admin))

	got, err := repo.GetByID(ctx, m.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ReplayedAt)
	require.NotNil(t, got.ReplayedBy)
	assert.Equal(t, admin, *got.ReplayedBy)

	// Replaying again is rejected.
	err = repo.MarkReplayed(ctx, m.ID, admin)
	assert.ErrorIs(t, err, dlq.ErrAlreadyResolved)

	// Unknown id surfaces a clean not-found.
	err = repo.MarkReplayed(ctx, "00000000-0000-0000-0000-000000000000", admin)
	assert.ErrorIs(t, err, dlq.ErrMessageNotFound)
}

func TestDLQRepo_MarkResolved(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDLQRepo(pool)
	admin := seedAdminUser(t, pool)

	m := newMessage()
	require.NoError(t, repo.Write(ctx, m))
	require.NoError(t, repo.MarkResolved(ctx, m.ID, admin, "discarded: schema drift"))

	got, err := repo.GetByID(ctx, m.ID)
	require.NoError(t, err)
	require.NotNil(t, got.ResolvedAt)
	require.NotNil(t, got.ResolvedBy)
	assert.Equal(t, admin, *got.ResolvedBy)
	assert.Equal(t, "discarded: schema drift", got.ResolvedNotes)

	// Resolving a row that's already resolved is rejected.
	err = repo.MarkResolved(ctx, m.ID, admin, "again")
	assert.ErrorIs(t, err, dlq.ErrAlreadyResolved)

	// A replayed row cannot be marked resolved afterwards.
	other := newMessage()
	require.NoError(t, repo.Write(ctx, other))
	require.NoError(t, repo.MarkReplayed(ctx, other.ID, admin))
	err = repo.MarkResolved(ctx, other.ID, admin, "too late")
	assert.ErrorIs(t, err, dlq.ErrAlreadyResolved)
}

// TestDLQ_MigrationRoundTrip exercises the 000007 down + up cycle to catch
// dangling indexes or FK errors. Runs in its own container.
func TestDLQ_MigrationRoundTrip(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Sanity: table exists after initial up.
	var exists bool
	require.NoError(t, pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='dlq_message')`).Scan(&exists))
	require.True(t, exists, "dlq_message should exist after up")

	dsn := pool.Config().ConnString()
	pool.Close()
	require.NoError(t, migrateDownUp(dsn))

	// Re-open and confirm the table is back and queryable.
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	pool2, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer pool2.Close()
	require.NoError(t, pool2.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='dlq_message')`).Scan(&exists))
	assert.True(t, exists, "dlq_message should exist after down+up")
}
