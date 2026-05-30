package evaluator

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newEval(repo compliance.AnomalyRepository) *OnTimeRateEvaluator {
	e := &OnTimeRateEvaluator{
		Anomaly: repo,
		Logger:  discardLogger(),
		// Tight knobs so a handful of events crosses the threshold.
		MinSamples: 4,
		Threshold:  0.95,
		HighThresh: 0.90,
		Window:     time.Hour,
	}
	e.applyDefaults()
	return e
}

// handle: malformed JSON returns a decode error and opens nothing.
func TestHandle_DecodeError(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	e := newEval(repo)
	err := e.handle(context.Background(), "order.picked_up.v1", []byte("{not-json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode payload")
	assert.Zero(t, repo.count())
}

// handle: empty vendor_id is a no-op (returns nil, opens nothing).
func TestHandle_EmptyVendor(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	e := newEval(repo)
	err := e.handle(context.Background(), "order.picked_up.v1", []byte(`{"vendor_id":""}`))
	require.NoError(t, err)
	assert.Zero(t, repo.count())
}

// handle: fewer than MinSamples events → no anomaly even if all no_show.
func TestHandle_BelowMinSamples(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	e := newEval(repo)
	for i := 0; i < 3; i++ { // MinSamples is 4
		require.NoError(t, e.handle(context.Background(), "order.no_show.v1", []byte(`{"vendor_id":"v1"}`)))
	}
	assert.Zero(t, repo.count())
}

// handle: rate at/above Threshold → no anomaly.
func TestHandle_AboveThreshold(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	e := newEval(repo)
	// 4 picked_up, 0 no_show → rate 1.0 >= 0.95.
	for i := 0; i < 4; i++ {
		require.NoError(t, e.handle(context.Background(), "order.picked_up.v1", []byte(`{"vendor_id":"v1"}`)))
	}
	assert.Zero(t, repo.count())
}

// handle: rate between HighThresh and Threshold → medium severity anomaly.
func TestHandle_MediumSeverity(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	// MinSamples=10 so a clean 10-event window: 9 picked_up + 1 no_show = 0.90,
	// which is >= HighThresh(0.90) but < Threshold(0.95) → medium.
	e := &OnTimeRateEvaluator{
		Anomaly:    repo,
		Logger:     discardLogger(),
		MinSamples: 10,
		Threshold:  0.95,
		HighThresh: 0.90,
		Window:     time.Hour,
	}
	e.applyDefaults()
	for i := 0; i < 9; i++ {
		require.NoError(t, e.handle(context.Background(), "order.picked_up.v1", []byte(`{"vendor_id":"v1"}`)))
	}
	require.NoError(t, e.handle(context.Background(), "order.no_show.v1", []byte(`{"vendor_id":"v1"}`)))

	a := repo.last()
	require.NotNil(t, a)
	assert.Equal(t, compliance.SeverityMedium, a.Severity)
	assert.Equal(t, "on_time_rate_drop", a.Kind)
	assert.Equal(t, "vendor", a.TargetKind)
	assert.Equal(t, "v1", a.TargetID)
	assert.InDelta(t, 0.90, a.Payload["rate"], 0.0001)
	assert.Equal(t, 10, a.Payload["total"])
	assert.Equal(t, 9, a.Payload["picked_up"])
}

// handle: rate below HighThresh → high severity anomaly.
func TestHandle_HighSeverity(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	e := newEval(repo) // MinSamples 4
	// 2 picked_up + 2 no_show = 0.5 < HighThresh.
	require.NoError(t, e.handle(context.Background(), "order.picked_up.v1", []byte(`{"vendor_id":"v1"}`)))
	require.NoError(t, e.handle(context.Background(), "order.picked_up.v1", []byte(`{"vendor_id":"v1"}`)))
	require.NoError(t, e.handle(context.Background(), "order.no_show.v1", []byte(`{"vendor_id":"v1"}`)))
	require.NoError(t, e.handle(context.Background(), "order.no_show.v1", []byte(`{"vendor_id":"v1"}`)))

	a := repo.last()
	require.NotNil(t, a)
	assert.Equal(t, compliance.SeverityHigh, a.Severity)
	assert.InDelta(t, 0.5, a.Payload["rate"], 0.0001)
}

// handle: Anomaly.Open failure is wrapped and returned.
func TestHandle_OpenError(t *testing.T) {
	repo := &fakeAnomalyRepo{openErr: errors.New("db down")}
	e := newEval(repo)
	for i := 0; i < 3; i++ {
		require.NoError(t, e.handle(context.Background(), "order.no_show.v1", []byte(`{"vendor_id":"v1"}`)))
	}
	// 4th event crosses MinSamples=4 with rate 0 → tries to Open → error.
	err := e.handle(context.Background(), "order.no_show.v1", []byte(`{"vendor_id":"v1"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open anomaly")
}

// handle: events older than the window are pruned out of the rate calc.
func TestHandle_WindowPrune(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	e := newEval(repo)
	// Inject 5 stale no_show events outside the window directly into state.
	stale := time.Now().Add(-2 * time.Hour) // Window is 1h
	e.mu.Lock()
	for i := 0; i < 5; i++ {
		e.data["v1"] = append(e.data["v1"], onTimeEvent{timestamp: stale, pickedUp: false})
	}
	e.mu.Unlock()
	// One fresh picked_up: stale events get pruned, leaving total=1 < MinSamples.
	require.NoError(t, e.handle(context.Background(), "order.picked_up.v1", []byte(`{"vendor_id":"v1"}`)))
	assert.Zero(t, repo.count())
	e.mu.Lock()
	got := len(e.data["v1"])
	e.mu.Unlock()
	assert.Equal(t, 1, got, "stale events should be pruned from the window")
}

// === Run / setupConsumer / nextMsg ===

// setupConsumer: Stream lookup failure propagates.
func TestSetupConsumer_StreamError(t *testing.T) {
	e := newEval(&fakeAnomalyRepo{})
	e.JS = &fakeJS{streamErr: errors.New("no stream")}
	_, err := e.setupConsumer(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get stream")
}

// setupConsumer: CreateOrUpdateConsumer failure propagates.
func TestSetupConsumer_ConsumerError(t *testing.T) {
	e := newEval(&fakeAnomalyRepo{})
	e.JS = &fakeJS{stream: &fakeStream{consumerErr: errors.New("boom")}}
	_, err := e.setupConsumer(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create consumer")
}

// Run: setupConsumer error short-circuits Run.
func TestRun_SetupConsumerError(t *testing.T) {
	e := newEval(&fakeAnomalyRepo{})
	e.JS = &fakeJS{streamErr: errors.New("no stream")}
	err := e.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get stream")
}

// Run: cons.Messages() error short-circuits Run.
func TestRun_MessagesError(t *testing.T) {
	e := newEval(&fakeAnomalyRepo{})
	e.JS = &fakeJS{stream: &fakeStream{consumer: &fakeConsumer{messagesErr: errors.New("msgs boom")}}}
	err := e.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "messages")
}

// Run: a valid event is processed and acked, then ctx cancel stops the loop.
func TestRun_ProcessesAndAcks(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	good := &fakeMsg{subject: "order.picked_up.v1", data: []byte(`{"vendor_id":"v1"}`)}
	msgs := newFakeMessages(
		nextResult{msg: good},
		nextResult{block: true}, // next call blocks until Stop()
	)
	e := newEval(repo)
	e.JS = &fakeJS{stream: &fakeStream{consumer: &fakeConsumer{msgs: msgs}}}

	started := make(chan struct{})
	e.OnStarted = func() { close(started) }

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- e.Run(ctx) }()

	<-started
	require.Eventually(t, good.wasAcked, 2*time.Second, 10*time.Millisecond)

	cancel()
	select {
	case err := <-runErr:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// Run: a poison event (bad JSON) hits the handle-error path → DLQOnExhaustion
// (nil Pool → Nak) and the loop continues.
func TestRun_HandleErrorNaks(t *testing.T) {
	poison := &fakeMsg{subject: "order.picked_up.v1", data: []byte("{bad")}
	msgs := newFakeMessages(
		nextResult{msg: poison},
		nextResult{block: true},
	)
	e := newEval(&fakeAnomalyRepo{})
	e.JS = &fakeJS{stream: &fakeStream{consumer: &fakeConsumer{msgs: msgs}}}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- e.Run(ctx) }()

	require.Eventually(t, poison.wasNaked, 2*time.Second, 10*time.Millisecond)
	assert.False(t, poison.wasAcked(), "poison event must not be acked")

	cancel()
	select {
	case <-runErr:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// nextMsg: a transient Next() error logs a warning, backs off, and returns
// (nil, true) so the caller continues. We drive it through Run.
func TestRun_TransientNextError(t *testing.T) {
	good := &fakeMsg{subject: "order.no_show.v1", data: []byte(`{"vendor_id":"v1"}`)}
	msgs := newFakeMessages(
		nextResult{err: errors.New("transient")}, // warn + 500ms backoff, continue
		nextResult{msg: good},
		nextResult{block: true},
	)
	e := newEval(&fakeAnomalyRepo{})
	e.JS = &fakeJS{stream: &fakeStream{consumer: &fakeConsumer{msgs: msgs}}}

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- e.Run(ctx) }()

	// The good event after the transient error must eventually be acked.
	require.Eventually(t, good.wasAcked, 3*time.Second, 20*time.Millisecond)

	cancel()
	select {
	case <-runErr:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// nextMsg: when ctx is already cancelled and Next returns a non-iterator-closed
// error path is short-circuited via ctx.Err() → returns (nil, false). Driving
// this through Run: cancel before the transient error backoff completes.
func TestNextMsg_CtxCancelledDuringBackoff(t *testing.T) {
	e := newEval(&fakeAnomalyRepo{})
	// Build an already-cancelled ctx so nextMsg's first branch (ctx.Err()!=nil)
	// returns (nil,false) on a generic error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msgs := newFakeMessages(nextResult{err: errors.New("boom")})
	_, cont := e.nextMsg(ctx, msgs)
	assert.False(t, cont, "cancelled ctx with generic error should not continue")
}

// nextMsg: iterator-closed error returns (nil, false) regardless of ctx.
func TestNextMsg_IteratorClosed(t *testing.T) {
	e := newEval(&fakeAnomalyRepo{})
	msgs := newFakeMessages(nextResult{err: jetstream.ErrMsgIteratorClosed})
	_, cont := e.nextMsg(context.Background(), msgs)
	assert.False(t, cont)
}

// Run: top-of-loop ctx guard. The message's onNext cancels ctx synchronously
// before handle/Ack run, so when the loop returns to the top the ctx.Err()
// guard (line 135) fires and Run returns context.Canceled.
func TestRun_TopOfLoopCtxGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	good := &fakeMsg{subject: "order.picked_up.v1", data: []byte(`{"vendor_id":"v1"}`)}
	msgs := newFakeMessages(
		nextResult{msg: good, onNext: cancel}, // cancel fires as msg is delivered
		nextResult{block: true},               // safety net if guard didn't trip
	)
	e := newEval(&fakeAnomalyRepo{})
	e.JS = &fakeJS{stream: &fakeStream{consumer: &fakeConsumer{msgs: msgs}}}

	runErr := make(chan error, 1)
	go func() { runErr <- e.Run(ctx) }()

	select {
	case err := <-runErr:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after ctx cancel at loop top")
	}
	// The message was still handled+acked before the guard tripped.
	assert.True(t, good.wasAcked())
}

// nextMsg: ctx cancelled while waiting out the post-warn backoff → (nil,false).
func TestNextMsg_CancelDuringBackoffSelect(t *testing.T) {
	e := newEval(&fakeAnomalyRepo{})
	ctx, cancel := context.WithCancel(context.Background())
	msgs := newFakeMessages(nextResult{err: errors.New("boom")})
	// Cancel shortly after the call enters the 500ms backoff select.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, cont := e.nextMsg(ctx, msgs)
	assert.False(t, cont, "cancel during backoff select returns false")
}
