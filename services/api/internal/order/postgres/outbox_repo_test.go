package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
)

// newAggregateUUID returns a deterministic UUID for tests (outbox_event.aggregate_id is UUID).
func newAggregateUUID(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}

func TestOutboxRepo_Append_InsertVisibleAfterCommit(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	aggID := newAggregateUUID(1)
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.AppendTx(ctx, tx, "order", aggID, "order.placed.v1",
			map[string]any{"id": aggID, "v": 1},
			map[string]any{"trace_id": "abc"})
	})
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_event WHERE aggregate_id=$1`, aggID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestOutboxRepo_Append_OpaqueTxAcceptsPgxTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)
	aggID := newAggregateUUID(2)

	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		// Use the order.Tx-shaped public Append method.
		return repo.Append(ctx, order.Tx(tx), "order", aggID, "order.placed.v1",
			map[string]any{"id": aggID}, map[string]any{})
	})
	require.NoError(t, err)
}

func TestOutboxRepo_LockBatch_ReturnsUnpublished_AndMarksPublished(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	// Seed 3 events
	for i := 1; i <= 3; i++ {
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.AppendTx(ctx, tx, "order", newAggregateUUID(100+i), "order.placed.v1",
				map[string]any{"n": i}, map[string]any{})
		}))
	}

	events, tx, err := repo.LockBatch(ctx, 10)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.Len(t, events, 3)
	for _, ev := range events {
		assert.Nil(t, ev.PublishedAt)
	}

	ids := []int64{events[0].ID, events[1].ID, events[2].ID}
	require.NoError(t, repo.MarkPublished(ctx, tx, ids))

	// Next lock returns empty
	events2, tx2, err := repo.LockBatch(ctx, 10)
	require.NoError(t, err)
	assert.Nil(t, tx2)
	assert.Empty(t, events2)
}

func TestOutboxRepo_LockBatch_EmptyReturnsNil(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	events, tx, err := repo.LockBatch(ctx, 10)
	require.NoError(t, err)
	assert.Nil(t, tx)
	assert.Empty(t, events)
}

func TestOutboxRepo_MarkFailed_IncrementsAttempts(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.AppendTx(ctx, tx, "order", newAggregateUUID(200), "order.placed.v1",
			map[string]any{}, map[string]any{})
	}))

	events, tx, err := repo.LockBatch(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.Len(t, events, 1)
	id := events[0].ID

	require.NoError(t, repo.MarkFailed(ctx, tx, id, "publish: nats unreachable"))
	// MarkFailed only stages the update; MarkPublished commits the cycle.
	require.NoError(t, repo.MarkPublished(ctx, tx, nil))

	// Re-lock and verify attempts incremented + last_error set
	events2, tx2, err := repo.LockBatch(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, tx2)
	require.Len(t, events2, 1)
	assert.Equal(t, id, events2[0].ID)
	assert.Equal(t, 1, events2[0].Attempts)
	require.NotNil(t, events2[0].LastError)
	assert.Equal(t, "publish: nats unreachable", *events2[0].LastError)

	// Release the lock
	require.NoError(t, repo.MarkPublished(ctx, tx2, nil))
}

// Regression: a mid-batch publish failure must not re-deliver already-published
// events. MarkFailed previously committed the cycle tx, so the cycle-final
// MarkPublished ran on a closed tx, errored, and left the published event's
// published_at NULL — re-publishing it next cycle. The fix makes MarkPublished
// the single commit point covering both the failure and the success.
func TestOutboxRepo_Cycle_PartialFailure_CommitsSuccessAndStagedFailure(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := repo.AppendTx(ctx, tx, "order", newAggregateUUID(210), "order.placed.v1",
			map[string]any{}, map[string]any{}); err != nil {
			return err
		}
		return repo.AppendTx(ctx, tx, "order", newAggregateUUID(211), "order.placed.v1",
			map[string]any{}, map[string]any{})
	}))

	events, tx, err := repo.LockBatch(ctx, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	failedID := events[0].ID
	publishedID := events[1].ID

	// Simulate one event failing to publish mid-batch...
	require.NoError(t, repo.MarkFailed(ctx, tx, failedID, "publish: nats unreachable"))
	// ...then the cycle's single commit for the event that did publish. Before
	// the fix this errored because MarkFailed had already committed/closed tx.
	require.NoError(t, repo.MarkPublished(ctx, tx, []int64{publishedID}))

	// Next cycle: only the failed event remains, with attempts incremented; the
	// published event is gone (not re-delivered).
	events2, tx2, err := repo.LockBatch(ctx, 10)
	require.NoError(t, err)
	require.Len(t, events2, 1)
	assert.Equal(t, failedID, events2[0].ID)
	assert.Equal(t, 1, events2[0].Attempts)
	require.NoError(t, repo.MarkPublished(ctx, tx2, nil))
}

func TestOutboxRepo_LockBatch_Concurrency_DisjointIDSets(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)

	// Seed 100 events
	for i := 1; i <= 100; i++ {
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.AppendTx(ctx, tx, "order", newAggregateUUID(1000+i), "order.placed.v1",
				map[string]any{"n": i}, map[string]any{})
		}))
	}

	// Two goroutines each LockBatch(50) concurrently — must return disjoint sets
	var wg sync.WaitGroup
	results := make([][]int64, 2)
	txs := make([]order.Tx, 2)
	var raceErr atomic.Value

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			events, tx, err := repo.LockBatch(ctx, 50)
			if err != nil {
				raceErr.Store(err)
				return
			}
			ids := make([]int64, 0, len(events))
			for _, ev := range events {
				ids = append(ids, ev.ID)
			}
			results[idx] = ids
			txs[idx] = tx
		}(i)
	}
	wg.Wait()

	if v := raceErr.Load(); v != nil {
		t.Fatalf("concurrent LockBatch error: %v", v)
	}

	// Commit both txs (release locks) to keep the harness clean.
	defer func() {
		for _, tx := range txs {
			if tx != nil {
				_ = repo.MarkPublished(ctx, tx, nil)
			}
		}
	}()

	// Each returned 50 rows (or both summed to 100), and the sets are disjoint.
	total := len(results[0]) + len(results[1])
	assert.Equal(t, 100, total, "two workers must collectively lock all 100 rows once")

	seen := make(map[int64]int)
	for _, id := range results[0] {
		seen[id]++
	}
	for _, id := range results[1] {
		seen[id]++
	}
	for id, c := range seen {
		assert.Equalf(t, 1, c, "id %d locked by both workers", id)
	}
}

func TestOutboxRepo_AppendRollbackOnTxRollback(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewOutboxRepo(pool)
	aggID := newAggregateUUID(300)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	require.NoError(t, repo.AppendTx(ctx, tx, "order", aggID, "order.placed.v1",
		map[string]any{}, map[string]any{}))
	require.NoError(t, tx.Rollback(ctx))

	var count int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_event WHERE aggregate_id=$1`, aggID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "rolled-back outbox insert must not be visible")
}

// Sanity: the LockBatch tx behaves transactionally — committing other rows in
// parallel must not interfere with the locked set.
func TestOutboxRepo_LockBatch_DoesNotBlockNewInserts(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := pgrepo.NewOutboxRepo(pool)

	// Seed 2 events
	for i := 1; i <= 2; i++ {
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.AppendTx(ctx, tx, "order", newAggregateUUID(400+i), "order.placed.v1",
				map[string]any{}, map[string]any{})
		}))
	}

	events, tx, err := repo.LockBatch(ctx, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)

	// While the lock is held, a separate transaction can still insert new events.
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx2 pgx.Tx) error {
		return repo.AppendTx(ctx, tx2, "order", newAggregateUUID(500), "order.placed.v1",
			map[string]any{}, map[string]any{})
	}))

	ids := []int64{events[0].ID, events[1].ID}
	require.NoError(t, repo.MarkPublished(ctx, tx, ids))
}
