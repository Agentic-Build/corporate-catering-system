package ohttp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/sse"
	"github.com/stretchr/testify/assert"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

// These white-box unit tests drive streamSSE directly so every loop branch
// (event delivered, send error on an event, channel closed, context cancelled)
// is exercised deterministically without a real SSE socket or a 20s wait.

// TestStreamSSE_EventThenChannelClose: one event is forwarded successfully,
// then the channel closes and the loop returns via the !open branch.
func TestStreamSSE_EventThenChannelClose(t *testing.T) {
	ch := make(chan order.BoardEvent, 1)
	ch <- order.BoardEvent{Kind: "placed", OrderID: "o1"}
	close(ch)

	var sent []any
	send := sse.Sender(func(m sse.Message) error {
		sent = append(sent, m.Data)
		return nil
	})

	done := make(chan struct{})
	go func() {
		streamSSE(context.Background(), send, ch, func(ev order.BoardEvent) any { return ev })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamSSE did not return after channel close")
	}
	require := assert.New(t)
	require.Len(sent, 1)
	require.Equal(order.BoardEvent{Kind: "placed", OrderID: "o1"}, sent[0])
}

// TestStreamSSE_SendErrorOnEvent: send.Data fails for an event → loop returns.
func TestStreamSSE_SendErrorOnEvent(t *testing.T) {
	ch := make(chan order.BoardEvent, 1)
	ch <- order.BoardEvent{Kind: "ready"}

	send := sse.Sender(func(sse.Message) error { return errors.New("broken pipe") })

	done := make(chan struct{})
	go func() {
		streamSSE(context.Background(), send, ch, func(ev order.BoardEvent) any { return ev })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamSSE did not return after send error")
	}
}

// TestStreamSSE_ContextCancelled: an empty, open channel + a cancelled context
// returns via the ctx.Done() branch.
func TestStreamSSE_ContextCancelled(t *testing.T) {
	ch := make(chan order.BoardEvent) // open, never delivers
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		streamSSE(ctx, send2noop(), ch, func(ev order.BoardEvent) any { return ev })
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamSSE did not return after context cancel")
	}
}

func send2noop() sse.Sender { return func(sse.Message) error { return nil } }

// TestStreamSSE_Heartbeat covers the 20s keep-alive ping branch. The ticker
// interval is hardcoded inside streamSSE (no injectable clock seam), so the
// only caller-reachable way to reach the heartbeat case is to wait it out.
// The ping send is wired to fail so the loop returns promptly once it fires,
// covering both the ping-send and its error-return statements.
func TestStreamSSE_Heartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 20s heartbeat wait in -short mode")
	}
	ch := make(chan order.BoardEvent) // open, never delivers → only the heartbeat fires
	pinged := make(chan struct{}, 1)
	send := sse.Sender(func(m sse.Message) error {
		if ev, ok := m.Data.(order.BoardEvent); ok && ev.Kind == "ping" {
			select {
			case pinged <- struct{}{}:
			default:
			}
		}
		return errors.New("client gone") // force the heartbeat-send error return
	})

	done := make(chan struct{})
	go func() {
		streamSSE(context.Background(), send, ch, func(ev order.BoardEvent) any { return ev })
		close(done)
	}()
	select {
	case <-pinged:
	case <-time.After(25 * time.Second):
		t.Fatal("heartbeat ping never fired")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamSSE did not return after heartbeat send error")
	}
}
