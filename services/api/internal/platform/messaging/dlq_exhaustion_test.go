package messaging_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
)

// fakeMsg is a minimal in-process jetstream.Msg used to drive DLQOnExhaustion
// through its branches without a live consumer.
type fakeMsg struct {
	meta    *jetstream.MsgMetadata
	metaErr error
	data    []byte
	subject string

	ackCalled  bool
	nakCalled  bool
	termCalled bool
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return m.meta, m.metaErr }
func (m *fakeMsg) Data() []byte                              { return m.data }
func (m *fakeMsg) Headers() nats.Header                      { return nil }
func (m *fakeMsg) Subject() string                           { return m.subject }
func (m *fakeMsg) Reply() string                             { return "" }
func (m *fakeMsg) Ack() error                                { m.ackCalled = true; return nil }
func (m *fakeMsg) DoubleAck(context.Context) error           { return nil }
func (m *fakeMsg) Nak() error                                { m.nakCalled = true; return nil }
func (m *fakeMsg) NakWithDelay(time.Duration) error          { m.nakCalled = true; return nil }
func (m *fakeMsg) InProgress() error                         { return nil }
func (m *fakeMsg) Term() error                               { m.termCalled = true; return nil }
func (m *fakeMsg) TermWithReason(string) error               { m.termCalled = true; return nil }

func TestDLQOnExhaustion_MetadataError_Naks(t *testing.T) {
	msg := &fakeMsg{metaErr: errors.New("no metadata")}
	got := messaging.DLQOnExhaustion(context.Background(), msg, nil, "c", 3, errors.New("boom"))
	assert.False(t, got)
	assert.True(t, msg.nakCalled, "should Nak when metadata unavailable")
	assert.False(t, msg.termCalled)
}

func TestDLQOnExhaustion_NilPool_Naks(t *testing.T) {
	msg := &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: 5, Stream: "ORDERS_V1"}}
	got := messaging.DLQOnExhaustion(context.Background(), msg, nil, "c", 3, errors.New("boom"))
	assert.False(t, got)
	assert.True(t, msg.nakCalled, "should Nak when pool is nil")
}

func TestDLQOnExhaustion_BeforeExhaustion_Naks(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	// NumDelivered (2) < maxDeliver (3): another attempt, no DLQ write.
	msg := &fakeMsg{meta: &jetstream.MsgMetadata{NumDelivered: 2, Stream: "ORDERS_V1"}}
	got := messaging.DLQOnExhaustion(context.Background(), msg, pool, "c", 3, errors.New("boom"))
	assert.False(t, got)
	assert.True(t, msg.nakCalled)
	assert.False(t, msg.termCalled)
}

func TestDLQOnExhaustion_Exhausted_WritesDLQAndTerms(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	msg := &fakeMsg{
		meta:    &jetstream.MsgMetadata{NumDelivered: 3, Stream: "ORDERS_V1"},
		data:    []byte(`{"order_id":"o-9"}`),
		subject: "order.placed.v1",
	}
	got := messaging.DLQOnExhaustion(ctx, msg, pool, "order-projector", 3, errors.New("kaboom"))
	assert.True(t, got, "should DLQ on exhaustion")
	assert.True(t, msg.termCalled, "should Term after DLQ write")
	assert.False(t, msg.nakCalled)

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM dlq_message WHERE source_stream=$1 AND source_subject=$2 AND last_error=$3`,
		"ORDERS_V1", "order.placed.v1", "kaboom").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestDLQOnExhaustion_Exhausted_UnparseablePayload_StillWrites(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	// Non-JSON data: json.Unmarshal fails (ignored), payload stays nil → WriteDLQ
	// defaults it to {}.
	msg := &fakeMsg{
		meta:    &jetstream.MsgMetadata{NumDelivered: 3, Stream: "ORDERS_V1"},
		data:    []byte("not-json"),
		subject: "order.cancelled.v1",
	}
	got := messaging.DLQOnExhaustion(ctx, msg, pool, "c", 3, errors.New("err"))
	assert.True(t, got)
	assert.True(t, msg.termCalled)

	var payload string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT payload::text FROM dlq_message WHERE source_subject=$1`,
		"order.cancelled.v1").Scan(&payload))
	assert.Equal(t, "{}", payload)
}

func TestDLQOnExhaustion_Exhausted_WriteFails_Naks(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	// Close the pool so the INSERT inside WriteDLQ fails; DLQOnExhaustion must
	// Nak (not silently drop) and report false.
	cleanup()

	msg := &fakeMsg{
		meta:    &jetstream.MsgMetadata{NumDelivered: 4, Stream: "ORDERS_V1"},
		data:    []byte(`{}`),
		subject: "order.placed.v1",
	}
	got := messaging.DLQOnExhaustion(context.Background(), msg, pool, "c", 3, errors.New("boom"))
	assert.False(t, got, "should report false when DLQ write fails")
	assert.True(t, msg.nakCalled, "should Nak when DLQ write fails")
	assert.False(t, msg.termCalled)
}

var _ jetstream.Msg = (*fakeMsg)(nil)
var _ = pgxpool.Pool{}
