package main

// Plain net/http SSE handlers used by the realtime-gateway role. They
// mirror the huma-based handlers under services/api/internal/order/http
// without pulling in the rest of the huma router — the realtime
// Deployment must stay small, with no business-write endpoints
// mounted. Topic scoping is by vendor (board) and by global menu
// broadcast (menu), matching the current client contract; plant/date
// scoping is exposed via the hubs' subscriber keys and can be
// extended without changing the URL shape.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

// boardSSEHandler returns the merchant prep-board SSE endpoint. The
// caller must be authenticated as a vendor operator with VendorID set;
// the AuthMiddleware in roles.go performs that check.
func boardSSEHandler(hub *order.BoardHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := idhttp.UserFromContext(r.Context())
		if !ok || u.Role != identity.RoleVendorOperator || u.VendorID == nil || *u.VendorID == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		writeSSEHeaders(w)
		flusher.Flush()

		ch, unsub := hub.Subscribe(*u.VendorID)
		defer unsub()

		sseOnConnect(r.Context(), "board")
		defer sseOnDisconnect(r.Context(), "board")

		hb := time.NewTicker(20 * time.Second)
		defer hb.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-hb.C:
				if err := writeSSE(w, order.BoardEvent{Kind: "ping"}); err != nil {
					return
				}
				flusher.Flush()
			case ev, open := <-ch:
				if !open {
					return
				}
				start := time.Now()
				if err := writeSSE(w, ev); err != nil {
					return
				}
				flusher.Flush()
				sseRecordFanoutLag(r.Context(), "board", time.Since(start))
			}
		}
	}
}

// menuSSEHandler returns the employee menu-changed SSE endpoint.
// Authentication is enforced upstream; this handler only checks role.
func menuSSEHandler(hub *order.MenuHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := idhttp.UserFromContext(r.Context())
		if !ok || u.Role != identity.RoleEmployee {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		writeSSEHeaders(w)
		flusher.Flush()

		ch, unsub := hub.Subscribe()
		defer unsub()

		sseOnConnect(r.Context(), "menu")
		defer sseOnDisconnect(r.Context(), "menu")

		hb := time.NewTicker(20 * time.Second)
		defer hb.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-hb.C:
				if err := writeSSE(w, order.BoardEvent{Kind: "ping"}); err != nil {
					return
				}
				flusher.Flush()
			case _, open := <-ch:
				if !open {
					return
				}
				start := time.Now()
				if err := writeSSE(w, order.BoardEvent{Kind: "changed"}); err != nil {
					return
				}
				flusher.Flush()
				sseRecordFanoutLag(r.Context(), "menu", time.Since(start))
			}
		}
	}
}

func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering: Traefik HTTPRoute does not need this
	// header but conservative L7 proxies (corporate egress, NGINX
	// fronts) do, so it costs nothing to emit.
	w.Header().Set("X-Accel-Buffering", "no")
}

func writeSSE(w http.ResponseWriter, payload any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", buf)
	return err
}
