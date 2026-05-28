package ohttp

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2/sse"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

// streamSSE runs the shared SSE keep-alive loop: it forwards each item from ch
// (mapped to a payload via toPayload) and emits a 20s ping, returning when the
// context is cancelled, the channel closes, or a send fails.
func streamSSE[T any](ctx context.Context, send sse.Sender, ch <-chan T, toPayload func(T) any) {
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if send.Data(order.BoardEvent{Kind: "ping"}) != nil {
				return
			}
		case ev, open := <-ch:
			if !open {
				return
			}
			if send.Data(toPayload(ev)) != nil {
				return
			}
		}
	}
}

// streamMerchantOrderEvents streams live order events for the caller's vendor
// over SSE. The merchant prep board re-fetches its data on each event, so the
// payload stays minimal. A 20s keep-alive ping holds the connection open.
func (a *API) streamMerchantOrderEvents(ctx context.Context, _ *struct{}, send sse.Sender) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok || u.Role != identity.RoleVendorOperator || u.VendorID == nil || *u.VendorID == "" {
		return
	}
	if a.Board == nil {
		<-ctx.Done()
		return
	}
	ch, unsub := a.Board.Subscribe(*u.VendorID)
	defer unsub()
	streamSSE(ctx, send, ch, func(ev order.BoardEvent) any { return ev })
}

// streamEmployeeMenuEvents streams a "menu changed" signal to the employee
// menu view so it refetches when an order shifts available stock.
func (a *API) streamEmployeeMenuEvents(ctx context.Context, _ *struct{}, send sse.Sender) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok || u.Role != identity.RoleEmployee {
		return
	}
	if a.MenuHub == nil {
		<-ctx.Done()
		return
	}
	ch, unsub := a.MenuHub.Subscribe()
	defer unsub()
	streamSSE(ctx, send, ch, func(struct{}) any { return order.BoardEvent{Kind: "changed"} })
}
