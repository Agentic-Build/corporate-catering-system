package scheduler_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	opg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/scheduler"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
)

var errBoom = errors.New("boom")

// errListRepo embeds order.Repository and makes ListPlacedDueForCutoff fail,
// so we can drive Cutoff.RunOnce's list-error path without a DB row.
type errListRepo struct{ order.Repository }

func (errListRepo) ListPlacedDueForCutoff(ctx context.Context, before time.Time) ([]*order.Order, error) {
	return nil, errBoom
}

// errUpdateTx embeds OrderStatusUpdater and fails UpdateStatusTx, driving
// transitionOne's error path (and RunOnce's per-order Warn+continue branch).
type errUpdateTx struct{ scheduler.OrderStatusUpdater }

func (errUpdateTx) UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to order.Status) error {
	return errBoom
}

// errStateAppender fails AppendTx, driving transitionOne's StateTx error path.
type errStateAppender struct{ scheduler.StateAppender }

func (errStateAppender) AppendTx(ctx context.Context, tx pgx.Tx, ev *order.StateEvent) error {
	return errBoom
}

// errOutboxAppender fails AppendTx, driving transitionOne's OutboxTx error path.
type errOutboxAppender struct{ scheduler.OutboxAppender }

func (errOutboxAppender) AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error {
	return errBoom
}

// errAuditWriter fails WriteTx, driving transitionOne's AuditTx error path.
type errAuditWriter struct{ scheduler.AuditTxWriter }

func (errAuditWriter) WriteTx(ctx context.Context, tx pgx.Tx, e plaudit.Entry) error {
	return errBoom
}

// TestCutoff_RunOnce_ListError covers cutoff.go:57-58 (ListPlacedDueForCutoff err).
func TestCutoff_RunOnce_ListError(t *testing.T) {
	_, sched, cleanup := setup(t)
	defer cleanup()
	sched.Orders = errListRepo{Repository: sched.Orders}

	n, err := sched.RunOnce(context.Background())
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, errBoom)
}

// TestCutoff_RunOnce_TransitionFailsSkipped covers transitionOne's UpdateStatusTx
// error path and RunOnce's per-order Warn+continue branch. With one due order
// whose transition fails, RunOnce returns 0 with no error and the order is untouched.
func TestCutoff_RunOnce_TransitionFailsSkipped(t *testing.T) {
	pool, sched, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	id := seedOrder(t, pool, order.StatusPlaced, time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC))
	sched.OrdersTx = errUpdateTx{OrderStatusUpdater: sched.OrdersTx}

	n, err := sched.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "transition failed so nothing counted")

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, id).Scan(&status))
	assert.Equal(t, "placed", status, "failed transition leaves order unchanged")
}

// TestCutoff_TransitionOne_StateAppendFails covers transitionOne's StateTx.AppendTx
// error path. The UpdateStatusTx succeeds inside the tx, then StateTx fails, so the
// whole transaction rolls back and the order stays placed.
func TestCutoff_TransitionOne_StateAppendFails(t *testing.T) {
	pool, sched, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	id := seedOrder(t, pool, order.StatusPlaced, time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC))
	sched.StateTx = errStateAppender{StateAppender: sched.StateTx}

	n, err := sched.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, id).Scan(&status))
	assert.Equal(t, "placed", status, "rollback leaves order unchanged")
}

// TestCutoff_TransitionOne_OutboxAppendFails covers transitionOne's OutboxTx.AppendTx
// error path.
func TestCutoff_TransitionOne_OutboxAppendFails(t *testing.T) {
	pool, sched, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	id := seedOrder(t, pool, order.StatusPlaced, time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC))
	sched.OutboxTx = errOutboxAppender{OutboxAppender: sched.OutboxTx}

	n, err := sched.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, id).Scan(&status))
	assert.Equal(t, "placed", status)
}

// TestCutoff_TransitionOne_AuditWriteFails covers transitionOne's AuditTx.WriteTx
// error path.
func TestCutoff_TransitionOne_AuditWriteFails(t *testing.T) {
	pool, sched, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	id := seedOrder(t, pool, order.StatusPlaced, time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC))
	sched.AuditTx = errAuditWriter{AuditTxWriter: sched.AuditTx}

	n, err := sched.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, id).Scan(&status))
	assert.Equal(t, "placed", status)
}

// TestCutoff_Run_DefaultInterval covers Run's interval<=0 default branch
// (cutoff.go:108-110). With interval 0 it defaults to 60s; the ticker never
// fires before the context is cancelled. The initial run transitions the order.
func TestCutoff_Run_DefaultInterval(t *testing.T) {
	pool, sched, cleanup := setup(t)
	defer cleanup()

	id := seedOrder(t, pool, order.StatusPlaced, time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC))

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := sched.Run(ctx, 0)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	var status string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT status FROM "order" WHERE id=$1`, id).Scan(&status))
	assert.Equal(t, "cutoff", status, "initial run transitioned the order")
}

// TestCutoff_Run_InitialRunError covers Run's "cutoff initial run" error branch
// (cutoff.go:115). The list error makes the initial RunOnce fail; Run then exits
// on the cancelled context.
func TestCutoff_Run_InitialRunError(t *testing.T) {
	_, sched, cleanup := setup(t)
	defer cleanup()
	sched.Orders = errListRepo{Repository: sched.Orders}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := sched.Run(ctx, 50*time.Millisecond)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestNoShow_RunOnce_DefaultMaxAge covers no_show.go:24-26 (MaxAge<=0 default to 2h).
// With MaxAge 0, RunOnce sets it to 2h and runs MarkNoShow. We seed a ready order
// older than 2h (and the fixed clock at 18:00) so it transitions.
func TestNoShow_RunOnce_DefaultMaxAge(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendorID := seedVendor(t, pool)
	userID := seedEmployee(t, pool)
	// ready_at 14:00 is 4h before the fixed clock 18:00 → older than the 2h default.
	old := seedReadyOrder(t, pool, vendorID, userID, time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC))

	orderRepo := opg.NewOrderRepo(pool)
	svc := &order.Service{
		Pool:     pool,
		Orders:   orderRepo,
		OrdersTx: orderRepo,
		StateTx:  opg.NewStateEventRepo(pool),
		AuditTx:  opg.NewAuditRepo(pool),
		OutboxTx: opg.NewOutboxRepo(pool),
		Clock:    fixedClock{T: time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)},
	}
	sweep := &scheduler.NoShowSweep{
		Svc:    svc,
		MaxAge: 0, // forces the default-2h branch
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	n, err := sweep.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 2*time.Hour, sweep.MaxAge, "MaxAge defaulted to 2h")

	var status string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, old).Scan(&status))
	assert.Equal(t, "no_show", status)
}

// TestNoShow_Run_DefaultInterval covers no_show.go Run's Interval<=0 default
// branch, the initial run's "transitioned count>0" Info branch, and clean exit
// on context cancellation. The seeded ready order is transitioned by the initial
// run; the 5m default ticker never fires before the context is cancelled.
func TestNoShow_Run_DefaultInterval(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()

	vendorID := seedVendor(t, pool)
	userID := seedEmployee(t, pool)
	seedReadyOrder(t, pool, vendorID, userID, time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC))

	orderRepo := opg.NewOrderRepo(pool)
	svc := &order.Service{
		Pool:     pool,
		Orders:   orderRepo,
		OrdersTx: orderRepo,
		StateTx:  opg.NewStateEventRepo(pool),
		AuditTx:  opg.NewAuditRepo(pool),
		OutboxTx: opg.NewOutboxRepo(pool),
		Clock:    fixedClock{T: time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)},
	}
	sweep := &scheduler.NoShowSweep{
		Svc:      svc,
		Interval: 0, // forces the default 5m interval branch (ticker won't fire before cancel)
		MaxAge:   1 * time.Hour,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := sweep.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 5*time.Minute, sweep.Interval, "Interval defaulted to 5m")
}

// TestNoShow_Run_TickerTransition covers no_show.go Run's ticker-branch
// "transitioned count>0" Info path (no_show.go:51-53). The initial run finds
// nothing; a ready order is inserted after the first tick so a later tick
// transitions it.
func TestNoShow_Run_TickerTransition(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()

	vendorID := seedVendor(t, pool)
	userID := seedEmployee(t, pool)

	orderRepo := opg.NewOrderRepo(pool)
	svc := &order.Service{
		Pool:     pool,
		Orders:   orderRepo,
		OrdersTx: orderRepo,
		StateTx:  opg.NewStateEventRepo(pool),
		AuditTx:  opg.NewAuditRepo(pool),
		OutboxTx: opg.NewOutboxRepo(pool),
		Clock:    fixedClock{T: time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)},
	}
	sweep := &scheduler.NoShowSweep{
		Svc:      svc,
		Interval: 30 * time.Millisecond,
		MaxAge:   1 * time.Hour,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}

	// Insert a ready order after the initial run (which finds nothing) so a
	// subsequent ticker tick performs the transition.
	go func() {
		time.Sleep(60 * time.Millisecond)
		seedReadyOrder(t, pool, vendorID, userID, time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err := sweep.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM "order" WHERE status='no_show'`).Scan(&n))
	assert.Equal(t, 1, n, "ticker tick transitioned the late-inserted order")
}

// TestNoShow_Run_InitialRunError covers no_show.go Run's initial-run error branch.
func TestNoShow_Run_InitialRunError(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()

	orderRepo := opg.NewOrderRepo(pool)
	svc := &order.Service{
		Pool:     pool,
		Orders:   errReadyListRepo{Repository: orderRepo},
		OrdersTx: orderRepo,
		StateTx:  opg.NewStateEventRepo(pool),
		AuditTx:  opg.NewAuditRepo(pool),
		OutboxTx: opg.NewOutboxRepo(pool),
		Clock:    fixedClock{T: time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)},
	}
	sweep := &scheduler.NoShowSweep{
		Svc:      svc,
		Interval: 50 * time.Millisecond,
		MaxAge:   1 * time.Hour,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := sweep.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// errReadyListRepo makes ListReadyOlderThan fail so MarkNoShow (and thus the
// NoShowSweep initial run) returns an error.
type errReadyListRepo struct{ order.Repository }

func (errReadyListRepo) ListReadyOlderThan(ctx context.Context, threshold time.Time) ([]*order.Order, error) {
	return nil, errBoom
}
