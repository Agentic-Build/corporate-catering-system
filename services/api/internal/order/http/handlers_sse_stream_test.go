package ohttp_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	ohttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/http"
)

// buildSSEHandler wires an order API with caller-supplied hubs (which may be
// nil) and the given user. It returns the server plus the hubs so a test can
// publish/broadcast into the live stream.
func buildSSEHandler(t *testing.T, user *identity.User, board *order.BoardHub, menuHub *order.MenuHub) *httptest.Server {
	t.Helper()
	api := &ohttp.API{Svc: &order.Service{}, Board: board, MenuHub: menuHub}
	r := chi.NewRouter()
	if user != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(idhttp.ContextWithUser(req.Context(), user)))
			})
		})
	}
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// readSSEUntil opens an SSE GET against url, reads lines until a line contains
// want (or ctx is done), and returns true if want was seen. The request shares
// ctx so the caller can cancel to close the stream and exercise the handler's
// ctx.Done() return branch.
func readSSEUntil(ctx context.Context, t *testing.T, url, want string, gotit chan<- bool) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		gotit <- false
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		gotit <- false
		return
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.Contains(sc.Text(), want) {
			gotit <- true
			return
		}
	}
	gotit <- false
}

func TestStreamMerchantOrderEvents_DeliversEvent(t *testing.T) {
	board := order.NewBoardHub()
	v := testVendor
	user := &identity.User{ID: "u-vendor", Role: identity.RoleVendorOperator, VendorID: &v}
	srv := buildSSEHandler(t, user, board, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotit := make(chan bool, 1)
	go readSSEUntil(ctx, t, srv.URL+"/api/merchant/orders/events", `"kind":"placed"`, gotit)

	// Wait until the handler has subscribed, then keep publishing on a ticker
	// (the first publish may race the SSE writer's initial flush) until the
	// reader reports the event arrived.
	require.Eventually(t, func() bool { return board.SubscriberCount() > 0 }, 5*time.Second, 10*time.Millisecond)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		board.Publish(testVendor, order.BoardEvent{Kind: "placed", OrderID: orderID})
		select {
		case ok := <-gotit:
			assert.True(t, ok)
			cancel()
			return
		case <-tick.C:
		case <-time.After(5 * time.Second):
			t.Fatal("event not delivered over SSE")
		}
	}
}

func TestStreamMerchantOrderEvents_NilBoard(t *testing.T) {
	v := testVendor
	user := &identity.User{ID: "u-vendor", Role: identity.RoleVendorOperator, VendorID: &v}
	srv := buildSSEHandler(t, user, nil, nil) // nil Board → handler blocks on ctx.Done

	ctx, cancel := context.WithCancel(context.Background())
	gotit := make(chan bool, 1)
	go readSSEUntil(ctx, t, srv.URL+"/api/merchant/orders/events", "never", gotit)
	// No board means no events; cancelling closes the stream and the handler
	// returns from its <-ctx.Done() branch.
	time.AfterFunc(200*time.Millisecond, cancel)
	assert.False(t, <-gotit)
}

func TestStreamEmployeeMenuEvents_DeliversEvent(t *testing.T) {
	menuHub := order.NewMenuHub()
	srv := buildSSEHandler(t, employeeUser(), nil, menuHub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotit := make(chan bool, 1)
	go readSSEUntil(ctx, t, srv.URL+"/api/employee/menu/events", `"kind":"changed"`, gotit)

	require.Eventually(t, func() bool { return menuHub.SubscriberCount() > 0 }, 5*time.Second, 10*time.Millisecond)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		menuHub.Broadcast()
		select {
		case ok := <-gotit:
			assert.True(t, ok)
			cancel()
			return
		case <-tick.C:
		case <-time.After(5 * time.Second):
			t.Fatal("event not delivered over SSE")
		}
	}
}

func TestStreamEmployeeMenuEvents_NilMenuHub(t *testing.T) {
	srv := buildSSEHandler(t, employeeUser(), nil, nil) // nil MenuHub → blocks on ctx.Done

	ctx, cancel := context.WithCancel(context.Background())
	gotit := make(chan bool, 1)
	go readSSEUntil(ctx, t, srv.URL+"/api/employee/menu/events", "never", gotit)
	time.AfterFunc(200*time.Millisecond, cancel)
	assert.False(t, <-gotit)
}
