package ohttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	ohttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/http"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// === reorder-specific fakes ===
// ReorderService talks to a narrower repo surface than Service, so these mirror
// only what the no-survivor / validation-error paths touch. The pool-backed
// persist path (201) is covered by the package's testcontainers service test.

type fakeReorderOrderRepo struct {
	src    *order.Order
	getErr error
}

func (r *fakeReorderOrderRepo) GetByID(_ context.Context, _ string) (*order.Order, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.src, nil
}
func (r *fakeReorderOrderRepo) CreateTx(context.Context, pgx.Tx, *order.Order) error { return nil }

// fakeReorderItems reports every item as archived → zero survivors.
type fakeReorderItems struct{}

func (fakeReorderItems) GetByID(_ context.Context, id string) (*order.ReorderMenuItem, error) {
	return &order.ReorderMenuItem{ID: id, Name: "Gone Dish", PriceMinor: 110, Archived: true}, nil
}

type fakeReorderSupply struct{}

func (fakeReorderSupply) Get(context.Context, string, time.Time) (*order.ReorderSupply, error) {
	return &order.ReorderSupply{Remain: 10, CutoffAt: time.Now().Add(24 * time.Hour)}, nil
}
func (fakeReorderSupply) DecrementTx(context.Context, pgx.Tx, string, time.Time, int) (int, error) {
	return 0, nil
}

type fakeReorderState struct{}

func (fakeReorderState) AppendTx(context.Context, pgx.Tx, *order.StateEvent) error { return nil }

type fakeReorderAudit struct{}

func (fakeReorderAudit) WriteTx(context.Context, pgx.Tx, plaudit.Entry) error { return nil }

type fakeReorderOutbox struct{}

func (fakeReorderOutbox) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return nil
}

// buildReorderHandlerSvc wires the reorder endpoint with a real ReorderService
// backed by the supplied order repo. plantsServe controls whether the source
// vendor still serves the caller's plant (false → ErrVendorPlantMismatch).
func buildReorderHandlerSvc(t *testing.T, user *identity.User, orders order.Service, ro *fakeReorderOrderRepo, plantsServe bool) *httptest.Server {
	t.Helper()
	_ = orders
	plants := &fakePlantRepo{byVendor: map[string][]*vendor.PlantMapping{}}
	if plantsServe {
		plants.byVendor[testVendor] = []*vendor.PlantMapping{{VendorID: testVendor, Plant: testPlant, Active: true}}
	}
	svc := order.NewReorderService(order.ReorderDeps{
		Pool:     nil, // never reached on the no-survivor / validation-error paths
		Orders:   ro,
		Supply:   fakeReorderSupply{},
		Items:    fakeReorderItems{},
		Vendors:  fakeVendorReader{},
		Plants:   plants,
		State:    fakeReorderState{},
		Audit:    fakeReorderAudit{},
		Outbox:   fakeReorderOutbox{},
		Clock:    fakeClock{t: time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)},
		Location: time.UTC,
	})
	api := &ohttp.ReorderAPI{Svc: svc}
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

func reorderSrc() *order.Order {
	return &order.Order{
		ID:       orderID,
		UserID:   empUserID,
		VendorID: testVendor,
		Plant:    testPlant,
		Items:    []order.Item{{ID: "i-1", MenuItemID: itemID, Qty: 2, UnitPriceMinor: 110}},
	}
}

func TestReorder_AllUnavailable_409(t *testing.T) {
	// Every source item is archived → zero survivors → 409 with the unavailable
	// list rendered through allUnavailableDetail.ErrorDetail / toUnavailableDTOs.
	srv := buildReorderHandlerSvc(t, employeeUser(), order.Service{}, &fakeReorderOrderRepo{src: reorderSrc()}, true)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder", reorderBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	var out struct {
		Detail string `json:"detail"`
		Errors []struct {
			Message string `json:"message"`
			Value   []struct {
				MenuItemID string `json:"menu_item_id"`
				Name       string `json:"name"`
				Reason     string `json:"reason"`
			} `json:"value"`
		} `json:"errors"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "all_items_unavailable", out.Detail)
	require.Len(t, out.Errors, 1)
	require.Len(t, out.Errors[0].Value, 1)
	assert.Equal(t, itemID, out.Errors[0].Value[0].MenuItemID)
	assert.Equal(t, "archived", out.Errors[0].Value[0].Reason)
}

func TestReorder_WrongOwner_403(t *testing.T) {
	// Source order belongs to someone else → ErrForbidden → reorderMapErr → 403.
	src := reorderSrc()
	src.UserID = "someone-else"
	srv := buildReorderHandlerSvc(t, employeeUser(), order.Service{}, &fakeReorderOrderRepo{src: src}, true)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder", reorderBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestReorder_SourceNotFound_404(t *testing.T) {
	srv := buildReorderHandlerSvc(t, employeeUser(), order.Service{}, &fakeReorderOrderRepo{getErr: order.ErrOrderNotFound}, true)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder", reorderBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestReorder_EmptySupplyDate_400(t *testing.T) {
	// supply_date present-but-empty satisfies the "required" key check (it has no
	// format), so it reaches the handler's explicit empty-string guard → 400.
	srv := buildReorderHandlerSvc(t, employeeUser(), order.Service{}, &fakeReorderOrderRepo{src: reorderSrc()}, true)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder",
		`{"source_order_id":"11111111-1111-1111-1111-111111111111","supply_date":""}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestReorder_VendorPlantMismatch_400(t *testing.T) {
	// Source vendor no longer serves the caller's plant → ErrVendorPlantMismatch → 400.
	srv := buildReorderHandlerSvc(t, employeeUser(), order.Service{}, &fakeReorderOrderRepo{src: reorderSrc()}, false)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder", reorderBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
