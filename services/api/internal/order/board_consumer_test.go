package order

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
)

// setupJetStream spins up a NATS+JS container, provisions the ORDERS_V1 stream
// RunBoardConsumer taps, and returns a ready JetStream handle.
func setupJetStream(t *testing.T) (jetstream.JetStream, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcnats.Run(ctx, "nats:2.10-alpine")
	require.NoError(t, err)
	url, err := c.ConnectionString(ctx)
	require.NoError(t, err)

	nc, err := nats.Connect(url)
	require.NoError(t, err)
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "ORDERS_V1",
		Subjects: []string{"order.>"},
	})
	require.NoError(t, err)

	cleanup := func() {
		nc.Close()
		_ = c.Terminate(ctx)
	}
	return js, cleanup
}

func TestRunBoardConsumer_FansOutToHubs(t *testing.T) {
	js, cleanup := setupJetStream(t)
	defer cleanup()

	hub := NewBoardHub()
	menuHub := NewMenuHub()
	ch, unsub := hub.Subscribe("vendor-1")
	defer unsub()
	menuCh, menuUnsub := menuHub.Subscribe()
	defer menuUnsub()

	consumerErr := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { consumerErr <- RunBoardConsumer(ctx, js, hub, menuHub, logger) }()

	// DeliverNew only delivers messages published after the consumer exists.
	// The consumer is created asynchronously, so publish in a retry loop until
	// the board hub receives the fanned-out event.
	deadline := time.After(10 * time.Second)
	pub := time.NewTicker(150 * time.Millisecond)
	defer pub.Stop()
	var got BoardEvent
loop:
	for {
		select {
		case <-deadline:
			t.Fatal("board hub never received the published event")
		case ev := <-ch:
			got = ev
			break loop
		case <-pub.C:
			_, err := js.Publish(ctx, "order.ready.v1",
				[]byte(`{"order_id":"o-42","vendor_id":"vendor-1"}`))
			require.NoError(t, err)
		}
	}

	require.Equal(t, "ready", got.Kind)
	require.Equal(t, "o-42", got.OrderID)

	// The menu hub is also signalled on every relevant order event.
	select {
	case <-menuCh:
	case <-time.After(2 * time.Second):
		t.Fatal("menu hub was not broadcast to")
	}

	// Events lacking a vendor_id, and malformed JSON, are silently ignored.
	_, err := js.Publish(ctx, "order.placed.v1", []byte(`{"order_id":"no-vendor"}`))
	require.NoError(t, err)
	_, err = js.Publish(ctx, "order.placed.v1", []byte(`{not json`))
	require.NoError(t, err)
	select {
	case ev := <-ch:
		t.Fatalf("expected no fanout for vendor-less / malformed events, got %+v", ev)
	case <-time.After(500 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-consumerErr:
		require.NoError(t, err, "RunBoardConsumer should return nil on ctx cancel")
	case <-time.After(2 * time.Second):
		t.Fatal("RunBoardConsumer did not return after ctx cancel")
	}
}

func TestRunBoardConsumer_StreamMissing(t *testing.T) {
	js, cleanup := setupJetStream(t)
	defer cleanup()

	// Delete the stream RunBoardConsumer taps so js.Stream returns an error the
	// consumer surfaces directly (the early return on line 169-173).
	require.NoError(t, js.DeleteStream(context.Background(), "ORDERS_V1"))
	err := RunBoardConsumer(context.Background(), js, NewBoardHub(), nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Error(t, err, "missing ORDERS_V1 stream must surface an error")
}
