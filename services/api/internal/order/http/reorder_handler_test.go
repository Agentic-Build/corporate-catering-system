package ohttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	ohttp "github.com/takalawang/corporate-catering-system/services/api/internal/order/http"
)

// buildReorderHandler wires only the reorder endpoint. The ReorderService has a
// concrete *pgxpool.Pool field that cannot be faked without a DB, so these tests
// cover ONLY the auth/validation guards that return before Svc.Reorder is
// reached — the 2xx path is exercised by the testcontainers reorder_service_test.
func buildReorderHandler(t *testing.T, user *identity.User) *httptest.Server {
	t.Helper()
	api := &ohttp.ReorderAPI{} // Svc nil: guard branches never dereference it
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

const reorderBody = `{"source_order_id":"11111111-1111-1111-1111-111111111111","supply_date":"2026-05-14"}`

func TestReorder_Unauthenticated(t *testing.T) {
	srv := buildReorderHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder", reorderBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestReorder_WrongRole(t *testing.T) {
	srv := buildReorderHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder", reorderBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestReorder_NoPlant_400(t *testing.T) {
	// employee with no plant assignment → 400 before Svc.Reorder.
	srv := buildReorderHandler(t, &identity.User{ID: empUserID, Role: identity.RoleEmployee})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder", reorderBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestReorder_MissingSupplyDate_422(t *testing.T) {
	// supply_date is a required (non-omitempty) body field → 422 before handler.
	srv := buildReorderHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder",
		`{"source_order_id":"11111111-1111-1111-1111-111111111111"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestReorder_InvalidSourceUUID_422(t *testing.T) {
	srv := buildReorderHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/reorder",
		`{"source_order_id":"not-a-uuid","supply_date":"2026-05-14"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}
