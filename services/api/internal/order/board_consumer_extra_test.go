package order

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRunBoardConsumer_OnStartedCallbackInvoked verifies the onStarted hook is
// called once the consumer is live (the `if onStarted != nil` branch). The
// existing fanout test passes nil, leaving that branch uncovered.
func TestRunBoardConsumer_OnStartedCallbackInvoked(t *testing.T) {
	js, cleanup := setupJetStream(t)
	defer cleanup()

	started := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	consumerErr := make(chan error, 1)
	go func() {
		consumerErr <- RunBoardConsumer(ctx, js, NewBoardHub(), nil, logger, func() {
			started <- struct{}{}
		})
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("onStarted callback was never invoked")
	}

	cancel()
	select {
	case err := <-consumerErr:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("RunBoardConsumer did not return after ctx cancel")
	}
}
