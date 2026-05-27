package ohttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	ohttp "github.com/takalawang/corporate-catering-system/services/api/internal/order/http"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

const (
	orderID    = "11111111-1111-1111-1111-111111111111"
	otherOrder = "22222222-2222-2222-2222-222222222222"
	itemID     = "33333333-3333-3333-3333-333333333333"
	testVendor = "v-owner"
	testPlant  = "F12B-3F"
	empUserID  = "u-emp"
)

// ----- Fakes (ohttp_test can't import the order_test fakes) -----

// fakeBeginner stands in for *pgxpool.Pool. It hands the write closure a no-op
// pgx.Tx; the repo fakes ignore the tx, so the place/cancel/modify/etc happy
// paths run without a real DB. Set beginErr to exercise the tx-open failure.
type fakeBeginner struct{ beginErr error }

func (b fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	if b.beginErr != nil {
		return nil, b.beginErr
	}
	return fakeTx{}, nil
}

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeClock returns a fixed "now" so cutoff math is deterministic.
type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

// fakeOrderRepo implements order.Repository. byID seeds Get/cancel/modify reads;
// byUser / byVendorDay feed the list endpoints. *Err fields force the read-path
// error branches.
type fakeOrderRepo struct {
	byID        map[string]*order.Order
	byUser      []*order.Order
	byVendorDay []*order.Order
	listUserErr error
	listVDayErr error
}

func (r *fakeOrderRepo) Create(context.Context, *order.Order) error { return nil }
func (r *fakeOrderRepo) GetByID(_ context.Context, id string) (*order.Order, error) {
	if o, ok := r.byID[id]; ok {
		clone := *o
		return &clone, nil
	}
	return nil, order.ErrOrderNotFound
}
func (r *fakeOrderRepo) UpdateStatus(context.Context, string, order.Status, order.Status, *string, *string, string) error {
	return nil
}
func (r *fakeOrderRepo) ListByUser(_ context.Context, _ string, _ time.Time) ([]*order.Order, error) {
	if r.listUserErr != nil {
		return nil, r.listUserErr
	}
	return r.byUser, nil
}
func (r *fakeOrderRepo) ListPlacedDueForCutoff(context.Context, time.Time) ([]*order.Order, error) {
	return nil, nil
}
func (r *fakeOrderRepo) ListReadyOlderThan(context.Context, time.Time) ([]*order.Order, error) {
	return nil, nil
}
func (r *fakeOrderRepo) ListByVendorDay(_ context.Context, _ string, _ time.Time, _ []order.Status) ([]*order.Order, error) {
	if r.listVDayErr != nil {
		return nil, r.listVDayErr
	}
	return r.byVendorDay, nil
}
func (r *fakeOrderRepo) ListPickedOrNoShowInPeriod(context.Context, time.Time, time.Time) ([]*order.Order, error) {
	return nil, nil
}

// OrderTx methods. CreateTx assigns an ID so Place's downstream steps + DTO have one.
func (r *fakeOrderRepo) CreateTx(_ context.Context, _ pgx.Tx, o *order.Order) error {
	o.ID = orderID
	return nil
}
func (r *fakeOrderRepo) UpdateStatusTx(context.Context, pgx.Tx, string, order.Status, order.Status) error {
	return nil
}
func (r *fakeOrderRepo) ReplaceItemsTx(context.Context, pgx.Tx, string, []order.Item, int64, string) error {
	return nil
}
func (r *fakeOrderRepo) MarkReadyTx(context.Context, pgx.Tx, string) error    { return nil }
func (r *fakeOrderRepo) MarkPickedUpTx(context.Context, pgx.Tx, string) error { return nil }
func (r *fakeOrderRepo) MarkNoShowTx(context.Context, pgx.Tx, string) error   { return nil }

// fakeStateRepo implements order.StateEventRepository + order.StateEventTx.
type fakeStateRepo struct{}

func (fakeStateRepo) Append(context.Context, *order.StateEvent) error { return nil }
func (fakeStateRepo) ListByOrder(context.Context, string) ([]*order.StateEvent, error) {
	return nil, nil
}
func (fakeStateRepo) AppendTx(context.Context, pgx.Tx, *order.StateEvent) error { return nil }

// fakeAuditRepo implements order.AuditRepository + order.AuditTx.
type fakeAuditRepo struct{}

func (fakeAuditRepo) Write(context.Context, *string, *string, string, string, string, map[string]any, string) error {
	return nil
}
func (fakeAuditRepo) WriteTx(context.Context, pgx.Tx, *string, *string, string, string, string, map[string]any, string) error {
	return nil
}

// fakeOutboxRepo implements order.OutboxRepository + order.OutboxTx.
type fakeOutboxRepo struct{}

func (fakeOutboxRepo) Append(context.Context, order.Tx, string, string, string, map[string]any, map[string]any) error {
	return nil
}
func (fakeOutboxRepo) LockBatch(context.Context, int) ([]*order.OutboxEvent, order.Tx, error) {
	return nil, nil, nil
}
func (fakeOutboxRepo) MarkPublished(context.Context, order.Tx, []int64) error    { return nil }
func (fakeOutboxRepo) MarkFailed(context.Context, order.Tx, int64, string) error { return nil }
func (fakeOutboxRepo) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return nil
}

// fakeQuotaRepo implements order.QuotaTx.
type fakeQuotaRepo struct{}

func (fakeQuotaRepo) DecrementTx(context.Context, pgx.Tx, string, time.Time, int) (int, error) {
	return 0, nil
}
func (fakeQuotaRepo) RestoreTx(context.Context, pgx.Tx, string, time.Time, int) error { return nil }

// fakeItemRepo implements menu.ItemRepository. Only GetByID is exercised.
type fakeItemRepo struct {
	byID map[string]*menu.Item
}

func (r *fakeItemRepo) Create(context.Context, *menu.Item) error                 { return nil }
func (r *fakeItemRepo) Update(context.Context, *menu.Item) error                 { return nil }
func (r *fakeItemRepo) SetStatus(context.Context, string, menu.ItemStatus) error { return nil }
func (r *fakeItemRepo) GetByID(_ context.Context, id string) (*menu.Item, error) {
	if mi, ok := r.byID[id]; ok {
		return mi, nil
	}
	return nil, menu.ErrItemNotFound
}
func (r *fakeItemRepo) ListByVendor(context.Context, string, bool) ([]*menu.MerchantItemRow, error) {
	return nil, nil
}
func (r *fakeItemRepo) ListActiveByPlant(context.Context, menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	return nil, nil
}

// fakePlantRepo implements vendor.PlantMappingRepository.
type fakePlantRepo struct {
	byVendor map[string][]*vendor.PlantMapping
}

func (r *fakePlantRepo) ListByVendor(_ context.Context, id string) ([]*vendor.PlantMapping, error) {
	return r.byVendor[id], nil
}
func (r *fakePlantRepo) ListVendorsForPlant(context.Context, string) ([]string, error) {
	return nil, nil
}
func (r *fakePlantRepo) Set(context.Context, string, []string) error             { return nil }
func (r *fakePlantRepo) SetWindow(context.Context, string, string, string) error { return nil }

// fakeVendorReader implements order.VendorReader: a vendor with a 17:00 cutoff
// and a 7-day preorder window, far enough out that placedOrder passes both
// checks under fakeClock.
type fakeVendorReader struct{}

func (fakeVendorReader) GetByID(context.Context, string) (*vendor.Vendor, error) {
	return &vendor.Vendor{ID: testVendor, CutoffHour: 17, PreorderWindowDays: 7}, nil
}

// ----- Harness -----

func employeeUser() *identity.User {
	p := testPlant
	return &identity.User{ID: empUserID, Role: identity.RoleEmployee, Plant: &p}
}

func vendorUser() *identity.User {
	v := testVendor
	return &identity.User{ID: "u-vendor", Role: identity.RoleVendorOperator, VendorID: &v}
}

func adminUser() *identity.User {
	return &identity.User{ID: "a-1", Role: identity.RoleWelfareAdmin}
}

// fakes bundles the mutable repos a test tweaks before issuing a request, plus
// the live SSE hubs so the streaming tests can publish/broadcast.
type fakes struct {
	orders  *fakeOrderRepo
	items   *fakeItemRepo
	plants  *fakePlantRepo
	vendor  fakeVendorReader
	quota   fakeQuotaRepo
	begin   fakeBeginner
	board   *order.BoardHub
	menuHub *order.MenuHub
}

// buildHandler wires the order API onto a chi router with the fakes. When
// user != nil a middleware injects it exactly like AuthMiddleware. The returned
// fakes pointer lets a test seed reads / force errors before the request.
func buildHandler(t *testing.T, user *identity.User) (*httptest.Server, *fakes) {
	t.Helper()
	f := &fakes{
		orders:  &fakeOrderRepo{byID: map[string]*order.Order{}},
		items:   &fakeItemRepo{byID: map[string]*menu.Item{}},
		plants:  &fakePlantRepo{byVendor: map[string][]*vendor.PlantMapping{}},
		board:   order.NewBoardHub(),
		menuHub: order.NewMenuHub(),
	}
	// Build the service lazily inside Register via a closure factory so the test
	// can keep mutating f.* up to the request. The pointers are shared, so the
	// service reads the latest state.
	svc := &order.Service{
		Pool:        f.begin,
		Orders:      f.orders,
		OrdersTx:    f.orders,
		StateEvents: fakeStateRepo{},
		StateTx:     fakeStateRepo{},
		Audit:       fakeAuditRepo{},
		AuditTx:     fakeAuditRepo{},
		Outbox:      fakeOutboxRepo{},
		OutboxTx:    fakeOutboxRepo{},
		QuotaTx:     f.quota,
		Items:       f.items,
		Plants:      f.plants,
		Vendors:     f.vendor,
		Clock:       fakeClock{t: time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)},
		Location:    time.UTC,
	}
	api := &ohttp.API{Svc: svc, Board: f.board, MenuHub: f.menuHub}

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
	return srv, f
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// placedOrder builds a PLACED order owned by empUserID with one item, a cutoff
// in the future relative to fakeClock, on the standard test plant/vendor.
func placedOrder(id string, owner string) *order.Order {
	placed := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	return &order.Order{
		ID:              id,
		UserID:          owner,
		VendorID:        testVendor,
		Plant:           testPlant,
		SupplyDate:      time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		Status:          order.StatusPlaced,
		TotalPriceMinor: 220,
		PlacedAt:        &placed,
		CutoffAt:        time.Date(2026, 5, 13, 17, 0, 0, 0, time.UTC),
		Items:           []order.Item{{ID: "i-1", MenuItemID: itemID, Qty: 2, UnitPriceMinor: 110}},
	}
}

func menuItem(id, vendorID string) *menu.Item {
	return &menu.Item{ID: id, VendorID: vendorID, Name: "Dish", PriceMinor: 110, Status: menu.ItemStatusActive}
}

const placeBody = `{"plant":"F12B-3F","supply_date":"2026-05-14","items":[{"menu_item_id":"33333333-3333-3333-3333-333333333333","qty":2}]}`

// =========================================================================
// POST /api/employee/orders  (place)
// =========================================================================

func TestPlaceOrder_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders", placeBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPlaceOrder_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders", placeBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPlaceOrder_MissingItems_422(t *testing.T) {
	// items is a required body field; omitting it → 422 before the handler runs.
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders",
		`{"plant":"F12B-3F","supply_date":"2026-05-14"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestPlaceOrder_EmptyItems_400(t *testing.T) {
	// items present but empty passes validation; handler returns ErrEmptyOrder→400.
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders",
		`{"plant":"F12B-3F","supply_date":"2026-05-14","items":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPlaceOrder_BadSupplyDate_400(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders",
		`{"plant":"F12B-3F","supply_date":"nope","items":[{"menu_item_id":"33333333-3333-3333-3333-333333333333","qty":2}]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPlaceOrder_VendorPlantMismatch_400(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.items.byID[itemID] = menuItem(itemID, testVendor)
	// vendor serves no plants → ErrVendorPlantMismatch → 400.
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders", placeBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPlaceOrder_OK_201(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.items.byID[itemID] = menuItem(itemID, testVendor)
	f.plants.byVendor[testVendor] = []*vendor.PlantMapping{{VendorID: testVendor, Plant: testPlant, Active: true}}
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders", placeBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Order struct {
			ID              string `json:"id"`
			VendorID        string `json:"vendor_id"`
			Plant           string `json:"plant"`
			Status          string `json:"status"`
			TotalPriceMinor int64  `json:"total_price_minor"`
			Items           []struct {
				MenuItemID string `json:"menu_item_id"`
				Qty        int    `json:"qty"`
			} `json:"items"`
		} `json:"order"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, orderID, out.Order.ID)
	assert.Equal(t, testVendor, out.Order.VendorID)
	assert.Equal(t, "placed", out.Order.Status)
	assert.Equal(t, int64(220), out.Order.TotalPriceMinor) // 110 * 2, whole NTD
	require.Len(t, out.Order.Items, 1)
	assert.Equal(t, itemID, out.Order.Items[0].MenuItemID)
	assert.Equal(t, 2, out.Order.Items[0].Qty)
}

// =========================================================================
// GET /api/employee/orders  (list)
// =========================================================================

func TestListMyOrders_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListMyOrders_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListMyOrders_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	// A cancelled order carries CancelledAt so toDTO renders that optional field.
	cancelled := placedOrder(otherOrder, empUserID)
	cancelled.Status = order.StatusCancelled
	cAt := time.Date(2026, 5, 12, 11, 0, 0, 0, time.UTC)
	cancelled.CancelledAt = &cAt
	f.orders.byUser = []*order.Order{placedOrder(orderID, empUserID), cancelled}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID          string  `json:"id"`
			Status      string  `json:"status"`
			CancelledAt *string `json:"cancelled_at"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 2)
	assert.Equal(t, orderID, out.Items[0].ID)
	assert.Equal(t, "placed", out.Items[0].Status)
	assert.Nil(t, out.Items[0].CancelledAt)
	assert.Equal(t, "cancelled", out.Items[1].Status)
	require.NotNil(t, out.Items[1].CancelledAt)
}

func TestListMyOrders_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.listUserErr = assertErr
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// GET /api/employee/orders/{id}  (get)
// =========================================================================

func TestGetMyOrder_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetMyOrder_InvalidUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/not-a-uuid", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestGetMyOrder_NotFound(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetMyOrder_WrongOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = placedOrder(orderID, "someone-else")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetMyOrder_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = placedOrder(orderID, empUserID)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Order struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"order"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, orderID, out.Order.ID)
	assert.Equal(t, "placed", out.Order.Status)
}

// =========================================================================
// PUT /api/employee/orders/{id}  (modify)
// =========================================================================

const modifyBody = `{"items":[{"menu_item_id":"33333333-3333-3333-3333-333333333333","qty":3}]}`

func TestModifyMyOrder_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/"+orderID, modifyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestModifyMyOrder_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/"+orderID, modifyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestModifyMyOrder_InvalidUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/not-a-uuid", modifyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestModifyMyOrder_MissingItems_422(t *testing.T) {
	// items is required → 422 before handler.
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/"+orderID, `{}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestModifyMyOrder_EmptyItems_400(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/"+orderID, `{"items":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestModifyMyOrder_NotFound(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/"+orderID, modifyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestModifyMyOrder_WrongOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = placedOrder(orderID, "someone-else")
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/"+orderID, modifyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestModifyMyOrder_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = placedOrder(orderID, empUserID)
	f.items.byID[itemID] = menuItem(itemID, testVendor)
	resp := do(t, http.MethodPut, srv.URL+"/api/employee/orders/"+orderID, modifyBody)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Order struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"order"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, orderID, out.Order.ID)
}

// =========================================================================
// POST /api/employee/orders/{id}/cancel
// =========================================================================

func TestCancelMyOrder_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/cancel", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCancelMyOrder_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/cancel", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCancelMyOrder_InvalidUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/not-a-uuid/cancel", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCancelMyOrder_NotFound(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/cancel", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCancelMyOrder_WrongOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = placedOrder(orderID, "someone-else")
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/cancel", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCancelMyOrder_NotPlaced_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	o := placedOrder(orderID, empUserID)
	o.Status = order.StatusReady // not PLACED → ErrInvalidTransition → 409
	f.orders.byID[orderID] = o
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/cancel", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestCancelMyOrder_OK_204(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = placedOrder(orderID, empUserID)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/cancel", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// =========================================================================
// POST /api/employee/orders/{id}/pickup
// =========================================================================

func TestPickupOrder_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/pickup", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPickupOrder_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/pickup", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPickupOrder_NotFound(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/pickup", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPickupOrder_WrongOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	o := placedOrder(orderID, "someone-else")
	o.Status = order.StatusReady
	f.orders.byID[orderID] = o
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/pickup", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPickupOrder_NotReady_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = placedOrder(orderID, empUserID) // PLACED, not READY → 409
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/pickup", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestPickupOrder_OK_204(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	o := placedOrder(orderID, empUserID)
	o.Status = order.StatusReady
	f.orders.byID[orderID] = o
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/pickup", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// =========================================================================
// POST /api/merchant/orders/mark-ready
// =========================================================================

const markReadyBody = `{"order_ids":["11111111-1111-1111-1111-111111111111"]}`

func TestMarkOrdersReady_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/orders/mark-ready", markReadyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMarkOrdersReady_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/orders/mark-ready", markReadyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMarkOrdersReady_NoVendorBinding_403(t *testing.T) {
	srv, _ := buildHandler(t, &identity.User{ID: "u-vendor", Role: identity.RoleVendorOperator})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/orders/mark-ready", markReadyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMarkOrdersReady_EmptyList_422(t *testing.T) {
	// order_ids has minItems:1 → empty array fails validation → 422.
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/orders/mark-ready", `{"order_ids":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestMarkOrdersReady_WrongVendor_403(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	o := placedOrder(orderID, empUserID)
	o.VendorID = "other-vendor" // not the caller's vendor → ErrForbidden → 403
	f.orders.byID[orderID] = o
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/orders/mark-ready", markReadyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMarkOrdersReady_NotFound(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser()) // order not seeded → ErrOrderNotFound → 404
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/orders/mark-ready", markReadyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMarkOrdersReady_OK_204(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.orders.byID[orderID] = placedOrder(orderID, empUserID) // PLACED→READY allowed
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/orders/mark-ready", markReadyBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// =========================================================================
// GET /api/merchant/orders  (listMerchantOrders)
// =========================================================================

func TestListMerchantOrders_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListMerchantOrders_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListMerchantOrders_BadDate_400(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders?date=nope", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListMerchantOrders_OK(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	// A picked-up order carries ReadyAt + PickedUpAt so toMerchantDTO renders
	// both optional timestamps.
	o := placedOrder(orderID, empUserID)
	o.Status = order.StatusPickedUp
	rAt := time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC)
	pAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	o.ReadyAt = &rAt
	o.PickedUpAt = &pAt
	f.orders.byVendorDay = []*order.Order{o}
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders?date=2026-05-14", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Date  string `json:"date"`
		Items []struct {
			ID         string  `json:"id"`
			Plant      string  `json:"plant"`
			ReadyAt    *string `json:"ready_at"`
			PickedUpAt *string `json:"picked_up_at"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "2026-05-14", out.Date)
	require.Len(t, out.Items, 1)
	assert.Equal(t, orderID, out.Items[0].ID)
	require.NotNil(t, out.Items[0].ReadyAt)
	require.NotNil(t, out.Items[0].PickedUpAt)
}

func TestListMerchantOrders_PlantFilter(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.orders.byVendorDay = []*order.Order{placedOrder(orderID, empUserID)}
	// filter for a different plant → the only order is excluded.
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders?date=2026-05-14&plant=OTHER", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Items)
}

func TestListMerchantOrders_DefaultDateAndStatusFilter(t *testing.T) {
	// No date query → handler defaults to today UTC; status filter is forwarded.
	srv, f := buildHandler(t, vendorUser())
	f.orders.byVendorDay = []*order.Order{placedOrder(orderID, empUserID)}
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders?status=placed", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Date  string `json:"date"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.NotEmpty(t, out.Date) // today, formatted
	require.Len(t, out.Items, 1)
}

func TestListMerchantOrders_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.orders.listVDayErr = assertErr
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders?date=2026-05-14", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// GET /api/merchant/prep-sheet
// =========================================================================

func TestPrepSheet_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/prep-sheet", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPrepSheet_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/prep-sheet", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPrepSheet_BadDate_400(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/prep-sheet?date=nope", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPrepSheet_OK(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.orders.byVendorDay = []*order.Order{placedOrder(orderID, empUserID)}
	f.items.byID[itemID] = menuItem(itemID, testVendor)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/prep-sheet?date=2026-05-14", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Date          string `json:"date"`
		TotalOrders   int    `json:"total_orders"`
		TotalPortions int    `json:"total_portions"`
		Plants        []struct {
			Plant        string `json:"plant"`
			OrderCount   int    `json:"order_count"`
			PortionCount int    `json:"portion_count"`
		} `json:"plants"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "2026-05-14", out.Date)
	assert.Equal(t, 1, out.TotalOrders)
	assert.Equal(t, 2, out.TotalPortions) // one item, qty 2
	require.Len(t, out.Plants, 1)
	assert.Equal(t, testPlant, out.Plants[0].Plant)
	assert.Equal(t, 1, out.Plants[0].OrderCount)
}

func TestPrepSheet_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.orders.listVDayErr = assertErr
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/prep-sheet?date=2026-05-14", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// SSE endpoints — auth guard only. The stream body cannot be asserted via a
// normal request (huma's SSE writer flushes lazily and the loop blocks on
// ctx.Done / the 20s heartbeat), so per the slice plan we cover only the
// return-immediately guard for an unauthenticated / wrong-role caller and
// skip the stream body itself.
// =========================================================================

func TestStreamMerchantOrderEvents_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders/events", "")
	defer resp.Body.Close()
	// SSE handler returns immediately (no user) → connection closes cleanly.
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStreamMerchantOrderEvents_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser()) // employee is not a vendor operator
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/orders/events", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStreamEmployeeMenuEvents_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu/events", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStreamEmployeeMenuEvents_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser()) // vendor is not an employee
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu/events", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// assertErr is a sentinel non-domain error → mapErr falls through to 500.
var assertErr = &genericErr{}

type genericErr struct{}

func (*genericErr) Error() string { return "db down" }
