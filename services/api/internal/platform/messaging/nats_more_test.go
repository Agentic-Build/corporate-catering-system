package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
)

// TestNew_ConnectFails_CancelledCtx drives the connect-failure path where the
// caller's context is already done, so New returns immediately rather than
// blocking on the 10s deadline.
func TestNew_ConnectFails_CancelledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Port 1 has no NATS server: nats.Connect returns an error; with ctx already
	// cancelled, New gives up on the first iteration.
	cl, err := messaging.New(ctx, "nats://127.0.0.1:1")
	require.Error(t, err)
	assert.Nil(t, cl)
	assert.Contains(t, err.Error(), "nats connect")
}

// TestNew_ConnectFails_CancelDuringBackoff exercises the inner select's
// ctx.Done() branch: the first connect fails, ctx is still live so we enter the
// backoff select, then ctx cancels mid-backoff.
func TestNew_ConnectFails_CancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	cl, err := messaging.New(ctx, "nats://127.0.0.1:1")
	require.Error(t, err)
	assert.Nil(t, cl)
	assert.Contains(t, err.Error(), "nats connect")
}

func TestPublishTraced_Success(t *testing.T) {
	url, cleanup := setupNATS(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	defer cl.Close()
	require.NoError(t, cl.ProvisionStreams(ctx))

	require.NoError(t, cl.PublishTraced(ctx, "order.placed.v1", []byte(`{"order_id":"p-1"}`), "dedup-1"))

	// Re-publishing the same dedup ID collapses to a single stream message.
	require.NoError(t, cl.PublishTraced(ctx, "order.placed.v1", []byte(`{"order_id":"p-1"}`), "dedup-1"))

	info, err := cl.JS.Stream(ctx, "ORDERS_V1")
	require.NoError(t, err)
	state, err := info.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), state.State.Msgs, "dedup ID should collapse re-publish")
}

func TestPublishTraced_NoDedup(t *testing.T) {
	url, cleanup := setupNATS(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	defer cl.Close()
	require.NoError(t, cl.ProvisionStreams(ctx))

	// Empty dedupID → no Nats-Msg-Id header; two publishes = two messages.
	require.NoError(t, cl.PublishTraced(ctx, "order.placed.v1", []byte(`{"a":1}`), ""))
	require.NoError(t, cl.PublishTraced(ctx, "order.placed.v1", []byte(`{"a":2}`), ""))

	info, err := cl.JS.Stream(ctx, "ORDERS_V1")
	require.NoError(t, err)
	state, err := info.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), state.State.Msgs)
}

func TestProvisionStreams_ConnClosed_Errors(t *testing.T) {
	url, cleanup := setupNATS(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	// Close the underlying connection so CreateOrUpdateStream (ORDERS_V1) fails.
	cl.Close()

	err = cl.ProvisionStreams(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provision ORDERS_V1")
}

func TestProvisionStreams_PayrollConflict_Errors(t *testing.T) {
	url, cleanup := setupNATS(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	defer cl.Close()

	// Pre-create a stream that claims the "payroll.>" subjects. ORDERS_V1 then
	// provisions fine, but PAYROLL_V1's CreateOrUpdate fails on subject overlap,
	// exercising the PAYROLL_V1 error branch.
	_, err = cl.JS.CreateStream(ctx, jetstream.StreamConfig{
		Name:     "PAYROLL_SQUATTER",
		Subjects: []string{"payroll.>"},
		Storage:  jetstream.FileStorage,
	})
	require.NoError(t, err)

	err = cl.ProvisionStreams(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provision PAYROLL_V1")
}

func TestPublishTraced_NoStream_Errors(t *testing.T) {
	url, cleanup := setupNATS(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	defer cl.Close()
	// No ProvisionStreams: publishing to a subject with no bound stream fails;
	// PublishTraced records the error on the span and returns it.
	err = cl.PublishTraced(ctx, "no.such.subject", []byte(`{}`), "")
	require.Error(t, err)
}
