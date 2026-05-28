package qhttp_test

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
	qhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota/http"
)

const (
	itemID    = "11111111-1111-1111-1111-111111111111"
	otherItem = "22222222-2222-2222-2222-222222222222"
	vendorID  = "v-owner"
)

// === Fakes (qhttp_test can't import the quota_test package's fakes) ===

type fakeItemRepo struct{ byID map[string]*menu.Item }

func (r *fakeItemRepo) Create(context.Context, *menu.Item) error                 { return nil }
func (r *fakeItemRepo) Update(context.Context, *menu.Item) error                 { return nil }
func (r *fakeItemRepo) SetStatus(context.Context, string, menu.ItemStatus) error { return nil }
func (r *fakeItemRepo) GetByID(_ context.Context, id string) (*menu.Item, error) {
	if i, ok := r.byID[id]; ok {
		return i, nil
	}
	return nil, menu.ErrItemNotFound
}
func (r *fakeItemRepo) ListByVendor(context.Context, string, bool) ([]*menu.MerchantItemRow, error) {
	return nil, nil
}
func (r *fakeItemRepo) ListActiveByPlant(context.Context, menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	return nil, nil
}

type supplyKey struct {
	itemID string
	date   time.Time
}

type fakeSupplyRepo struct {
	byKey     map[supplyKey]*quota.Supply
	vendor    map[string]string // itemID -> vendorID, for ListByVendor
	upsertErr error
}

func newFakeSupplyRepo() *fakeSupplyRepo {
	return &fakeSupplyRepo{byKey: map[supplyKey]*quota.Supply{}, vendor: map[string]string{}}
}

func (r *fakeSupplyRepo) Upsert(_ context.Context, s *quota.Supply) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	s.ID = "sup-1"
	clone := *s
	r.byKey[supplyKey{s.MenuItemID, s.SupplyDate}] = &clone
	return nil
}
func (r *fakeSupplyRepo) Get(_ context.Context, id string, date time.Time) (*quota.Supply, error) {
	if s, ok := r.byKey[supplyKey{id, date}]; ok {
		clone := *s
		return &clone, nil
	}
	return nil, quota.ErrSupplyNotFound
}
func (r *fakeSupplyRepo) ListByVendor(_ context.Context, v string, date time.Time) ([]*quota.Supply, error) {
	var out []*quota.Supply
	for k, s := range r.byKey {
		if s.SupplyDate.Equal(date) && r.vendor[k.itemID] == v {
			clone := *s
			out = append(out, &clone)
		}
	}
	return out, nil
}
func (r *fakeSupplyRepo) Decrement(context.Context, string, time.Time, int) (int, error) {
	return 0, nil
}
func (r *fakeSupplyRepo) Restore(context.Context, string, time.Time, int) error { return nil }
func (r *fakeSupplyRepo) SetSoldOut(_ context.Context, id string, date time.Time, soldOut bool) error {
	if s, ok := r.byKey[supplyKey{id, date}]; ok {
		s.SoldOut = soldOut
		return nil
	}
	return quota.ErrSupplyNotFound
}

// === Harness ===

func vendorUser() *identity.User {
	v := vendorID
	return &identity.User{ID: "u-1", Role: identity.RoleVendorOperator, VendorID: &v}
}

// buildHandler wires the quota API onto a chi router. When user != nil a
// middleware injects it into the request context exactly like AuthMiddleware does.
func buildHandler(t *testing.T, user *identity.User) (*httptest.Server, *fakeSupplyRepo, *fakeItemRepo) {
	t.Helper()
	sr := newFakeSupplyRepo()
	ir := &fakeItemRepo{byID: map[string]*menu.Item{}}
	api := &qhttp.API{Svc: &quota.Service{Supplies: sr, Items: ir}}

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
	return srv, sr, ir
}

func (r *fakeSupplyRepo) seed(s *quota.Supply, owner string) {
	clone := *s
	r.byKey[supplyKey{s.MenuItemID, s.SupplyDate}] = &clone
	r.vendor[s.MenuItemID] = owner
}

func (r *fakeItemRepo) seed(id, owner string) {
	r.byID[id] = &menu.Item{ID: id, VendorID: owner, Status: menu.ItemStatusActive}
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req, err := http.NewRequest(method, url, rdr)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// === requireVendor branches ===

func TestSetCapacity_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14",
		`{"capacity":10,"pickup_window":"12:00-12:30","eta_label":"x","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSetCapacity_WrongRole(t *testing.T) {
	srv, _, _ := buildHandler(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14",
		`{"capacity":10,"pickup_window":"12:00-12:30","eta_label":"x","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSetCapacity_NoVendorBinding(t *testing.T) {
	srv, _, _ := buildHandler(t, &identity.User{ID: "u-1", Role: identity.RoleVendorOperator})
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14",
		`{"capacity":10,"pickup_window":"12:00-12:30","eta_label":"x","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// === setCapacity ===

func TestSetCapacity_OK(t *testing.T) {
	srv, _, ir := buildHandler(t, vendorUser())
	ir.seed(itemID, vendorID)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14",
		`{"capacity":80,"pickup_window":"12:00-12:30","eta_label":"10 分鐘","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Supply struct {
			Capacity     int    `json:"capacity"`
			Remain       int    `json:"remain"`
			PickupWindow string `json:"pickup_window"`
		} `json:"supply"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 80, out.Supply.Capacity)
	assert.Equal(t, 80, out.Supply.Remain)
	assert.Equal(t, "12:00-12:30", out.Supply.PickupWindow)
}

func TestSetCapacity_InvalidDate(t *testing.T) {
	srv, _, ir := buildHandler(t, vendorUser())
	ir.seed(itemID, vendorID)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/not-a-date",
		`{"capacity":10,"pickup_window":"12:00-12:30","eta_label":"x","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestSetCapacity_ItemNotFound(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser()) // item not seeded
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14",
		`{"capacity":10,"pickup_window":"12:00-12:30","eta_label":"x","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSetCapacity_WrongVendor_Forbidden(t *testing.T) {
	srv, _, ir := buildHandler(t, vendorUser())
	ir.seed(itemID, "someone-else")
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14",
		`{"capacity":10,"pickup_window":"12:00-12:30","eta_label":"x","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSetCapacity_RepoError_500(t *testing.T) {
	srv, sr, ir := buildHandler(t, vendorUser())
	ir.seed(itemID, vendorID)
	sr.upsertErr = errors.New("db down")
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14",
		`{"capacity":10,"pickup_window":"12:00-12:30","eta_label":"x","cutoff_at":"2026-05-14T10:30:00Z"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === setSoldOut ===

func TestSetSoldOut_OK(t *testing.T) {
	srv, sr, ir := buildHandler(t, vendorUser())
	ir.seed(itemID, vendorID)
	d := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	sr.seed(&quota.Supply{ID: "sup-1", MenuItemID: itemID, SupplyDate: d, Capacity: 50, Remain: 50}, vendorID)

	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14/sold-out",
		`{"sold_out":true}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Supply struct {
			SoldOut bool `json:"sold_out"`
			Remain  int  `json:"remain"`
		} `json:"supply"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.True(t, out.Supply.SoldOut)
	assert.Equal(t, 50, out.Supply.Remain, "remain untouched by sold-out")
}

func TestSetSoldOut_SupplyNotFound_404(t *testing.T) {
	srv, _, ir := buildHandler(t, vendorUser())
	ir.seed(itemID, vendorID) // item exists but no supply row for the date
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/supply/"+itemID+"/2026-05-14/sold-out",
		`{"sold_out":true}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// === list ===

func TestList_WithDate(t *testing.T) {
	srv, sr, ir := buildHandler(t, vendorUser())
	ir.seed(itemID, vendorID)
	ir.seed(otherItem, "someone-else")
	d := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	sr.seed(&quota.Supply{ID: "s1", MenuItemID: itemID, SupplyDate: d, Capacity: 10, Remain: 7}, vendorID)
	sr.seed(&quota.Supply{ID: "s2", MenuItemID: otherItem, SupplyDate: d, Capacity: 5, Remain: 5}, "someone-else")

	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/supply?date=2026-05-14", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Date  string `json:"date"`
		Items []struct {
			MenuItemID string `json:"menu_item_id"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "2026-05-14", out.Date)
	require.Len(t, out.Items, 1, "only the vendor's own supply")
	assert.Equal(t, itemID, out.Items[0].MenuItemID)
}

func TestList_DefaultsToToday(t *testing.T) {
	srv, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/supply", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Date  string `json:"date"`
		Items []any  `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	today := time.Now().UTC().Format("2006-01-02")
	assert.Equal(t, today, out.Date)
	assert.Empty(t, out.Items)
}

func TestList_Unauthenticated(t *testing.T) {
	srv, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/supply", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
