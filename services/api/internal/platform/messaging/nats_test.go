package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"

	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/messaging"
)

func setupNATS(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()
	// The testcontainers nats module already includes "-js" in the default Cmd,
	// so JetStream is enabled out of the box.
	c, err := tcnats.Run(ctx, "nats:2.10-alpine")
	require.NoError(t, err)
	url, err := c.ConnectionString(ctx)
	require.NoError(t, err)
	return url, func() { _ = c.Terminate(ctx) }
}

func TestClient_ProvisionStreams_ReplicasFromField(t *testing.T) {
	url, cleanup := setupNATS(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	defer cl.Close()

	// default replicas = 1
	if cl.StreamReplicas != 1 {
		t.Fatalf("StreamReplicas default = %d, want 1", cl.StreamReplicas)
	}

	require.NoError(t, cl.ProvisionStreams(ctx))

	info, err := cl.JS.Stream(ctx, "ORDERS_V1")
	require.NoError(t, err)
	state, err := info.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, state.Config.Replicas)
}

func TestClient_ProvisionAndPublish(t *testing.T) {
	url, cleanup := setupNATS(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	defer cl.Close()

	require.NoError(t, cl.ProvisionStreams(ctx))

	// publish + consume
	_, err = cl.JS.Publish(ctx, "order.placed.v1", []byte(`{"order_id":"test-1"}`))
	require.NoError(t, err)

	info, err := cl.JS.Stream(ctx, "ORDERS_V1")
	require.NoError(t, err)
	state, err := info.Info(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), state.State.Msgs)

	// Confirm stream is idempotent (re-provision)
	require.NoError(t, cl.ProvisionStreams(ctx))

	// Create consumer + pull
	cons, err := info.CreateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "test-consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: "order.>",
	})
	require.NoError(t, err)
	msgs, err := cons.FetchNoWait(10)
	require.NoError(t, err)
	var count int
	for m := range msgs.Messages() {
		_ = m.Ack()
		count++
	}
	assert.Equal(t, 1, count)
}
