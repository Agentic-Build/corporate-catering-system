package relay_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/relay"
)

// fakeOutbox is an in-process order.OutboxRepository for driving the relay's
// control flow (defaults, error branches) without a database. LockBatch returns
// a scripted batch + error; MarkPublished / MarkFailed return scripted errors
// and record their calls so tests can assert the relay's behaviour.
type fakeOutbox struct {
	mu sync.Mutex

	lockEvents  []*order.OutboxEvent
	lockErr     error
	lockCalls   int32
	lockErrOnce bool // when set, lockErr only applies to the first call

	markPublishedErr   error
	markPublishedIDs   [][]int64
	markPublishedCalls int32

	markFailedErr   error
	markFailedIDs   []int64
	markFailedCalls int32
}

func (f *fakeOutbox) Append(ctx context.Context, tx order.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error {
	return nil
}

func (f *fakeOutbox) LockBatch(ctx context.Context, limit int) ([]*order.OutboxEvent, order.Tx, error) {
	n := atomic.AddInt32(&f.lockCalls, 1)
	if f.lockErr != nil && (!f.lockErrOnce || n == 1) {
		return nil, nil, f.lockErr
	}
	return f.lockEvents, struct{}{}, nil
}

func (f *fakeOutbox) MarkPublished(ctx context.Context, tx order.Tx, ids []int64) error {
	atomic.AddInt32(&f.markPublishedCalls, 1)
	f.mu.Lock()
	f.markPublishedIDs = append(f.markPublishedIDs, ids)
	f.mu.Unlock()
	return f.markPublishedErr
}

func (f *fakeOutbox) MarkFailed(ctx context.Context, tx order.Tx, id int64, lastError string) error {
	atomic.AddInt32(&f.markFailedCalls, 1)
	f.mu.Lock()
	f.markFailedIDs = append(f.markFailedIDs, id)
	f.mu.Unlock()
	return f.markFailedErr
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// TestRun_AppliesBatchDefault covers the `if r.Batch <= 0 { r.Batch = 100 }`
// default: a relay constructed with Batch unset must request 100 from LockBatch.
func TestRun_AppliesBatchDefault(t *testing.T) {
	var seenLimit int32
	f := &fakeOutbox{}
	// Wrap LockBatch to capture the limit by using a closure-backed fake.
	lf := &limitCapturingOutbox{fakeOutbox: f, seen: &seenLimit}

	r := &relay.Relay{
		Outbox: lf,
		NATS:   nil, // never used: LockBatch returns empty, no publish path
		Logger: quietLogger(),
		// Batch unset (0) -> default 100; Sleep unset (0) -> default 500ms.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := r.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int32(100), atomic.LoadInt32(&seenLimit), "Batch default of 100 must reach LockBatch")
}

// limitCapturingOutbox records the limit passed to LockBatch, then returns an
// empty batch (so the relay sleeps and never touches NATS).
type limitCapturingOutbox struct {
	*fakeOutbox
	seen *int32
}

func (l *limitCapturingOutbox) LockBatch(ctx context.Context, limit int) ([]*order.OutboxEvent, order.Tx, error) {
	atomic.StoreInt32(l.seen, int32(limit))
	return nil, struct{}{}, nil
}

// TestRun_StopsImmediatelyWhenCtxAlreadyCancelled covers the top-of-loop
// `case <-ctx.Done(): return ctx.Err()` branch: an already-cancelled context
// must make Run return without ever calling LockBatch.
func TestRun_StopsImmediatelyWhenCtxAlreadyCancelled(t *testing.T) {
	f := &fakeOutbox{}
	r := &relay.Relay{
		Outbox: f,
		NATS:   nil,
		Logger: quietLogger(),
		Batch:  10,
		Sleep:  time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done before Run starts

	err := r.Run(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(0), atomic.LoadInt32(&f.lockCalls), "cancelled-at-top must skip the cycle entirely")
}

// TestRun_LogsNonCanceledCycleError covers `if err != nil && !errors.Is(err,
// context.Canceled)` plus the LockBatch error return in cycle. cycle returns a
// non-Canceled error; Run must log it and keep looping (it does NOT exit on a
// cycle error), so we cancel after the first failing cycle.
func TestRun_LogsNonCanceledCycleError(t *testing.T) {
	boom := errors.New("lockbatch boom")
	f := &fakeOutbox{lockErr: boom}

	r := &relay.Relay{
		Outbox: f,
		NATS:   nil,
		Logger: quietLogger(),
		Batch:  10,
		Sleep:  5 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := r.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	// cycle was attempted at least once and errored each time.
	assert.GreaterOrEqual(t, atomic.LoadInt32(&f.lockCalls), int32(1), "cycle must run and hit the LockBatch error path")
}

// TestCycle_MarkPublishedError covers the `if err := MarkPublished(...); err
// != nil { return len(events), err }` branch via Run: when every publish fails
// (no matching stream) AND MarkPublished errors, cycle returns that error and
// Run logs it. We use a real NATS client with a subject that has no stream so
// PublishTraced fails, exercising MarkFailed too.
func TestCycle_MarkPublishedError(t *testing.T) {
	nats, cleanupNATS := setupNATS(t)
	defer cleanupNATS()

	markPubErr := errors.New("mark published boom")
	f := &fakeOutbox{
		lockEvents: []*order.OutboxEvent{
			{ID: 1, AggregateType: "order", Subject: "nostream.foo.v1", Payload: map[string]any{"k": "v"}},
		},
		markPublishedErr: markPubErr,
		lockErrOnce:      false,
	}

	r := &relay.Relay{
		Outbox: f,
		NATS:   nats,
		Logger: quietLogger(),
		Batch:  10,
		Sleep:  5 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := r.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&f.markFailedCalls), int32(1), "failed publish must stage MarkFailed")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&f.markPublishedCalls), int32(1), "cycle must reach the single commit point")
	require.NotEmpty(t, f.markFailedIDs)
	assert.Equal(t, int64(1), f.markFailedIDs[0], "the unpublished event id must be marked failed")
	// successIDs stays empty because the publish failed.
	require.NotEmpty(t, f.markPublishedIDs)
	assert.Empty(t, f.markPublishedIDs[0], "no event published successfully -> empty success id slice")
}

// TestCycle_MarkFailedError covers the nested `if err2 := MarkFailed(...); err2
// != nil { Logger.Error(...) }` branch: publish fails (no stream) and the
// MarkFailed staging itself errors; the relay logs and continues. With
// MarkFailed erroring but MarkPublished succeeding, cycle returns nil and Run
// loops until the deadline.
func TestCycle_MarkFailedError(t *testing.T) {
	nats, cleanupNATS := setupNATS(t)
	defer cleanupNATS()

	f := &fakeOutbox{
		lockEvents: []*order.OutboxEvent{
			{ID: 7, AggregateType: "order", Subject: "nostream.bar.v1", Payload: map[string]any{"k": "v"}},
		},
		markFailedErr:    errors.New("mark failed boom"),
		markPublishedErr: nil,
	}

	r := &relay.Relay{
		Outbox: f,
		NATS:   nats,
		Logger: quietLogger(),
		Batch:  10,
		Sleep:  5 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err := r.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	assert.GreaterOrEqual(t, atomic.LoadInt32(&f.markFailedCalls), int32(1), "MarkFailed must be attempted on publish failure")
	assert.GreaterOrEqual(t, atomic.LoadInt32(&f.markPublishedCalls), int32(1), "cycle still commits after a failed MarkFailed")
}
