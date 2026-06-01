package messaging_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
)

func TestWriteDLQ_PayloadMarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	// A channel value cannot be JSON-marshalled → marshal of payload fails.
	err := messaging.WriteDLQ(context.Background(), pool, messaging.DLQEntry{
		Stream:  "ORDERS_V1",
		Payload: map[string]any{"bad": make(chan int)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal dlq payload")
}

func TestWriteDLQ_HeadersMarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	// Valid payload, but unmarshalable headers → marshal of headers fails.
	err := messaging.WriteDLQ(context.Background(), pool, messaging.DLQEntry{
		Stream:  "ORDERS_V1",
		Payload: map[string]any{"ok": 1},
		Headers: map[string]any{"bad": make(chan int)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal dlq headers")
}

func TestWriteDLQ_ExecError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	// Close the pool first so pool.Exec fails inside WriteDLQ.
	cleanup()
	err := messaging.WriteDLQ(context.Background(), pool, messaging.DLQEntry{
		Stream:   "ORDERS_V1",
		Subject:  "order.placed.v1",
		Consumer: "c",
		Payload:  map[string]any{"x": 1},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write dlq")
}
