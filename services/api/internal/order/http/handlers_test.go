package ohttp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ohttp "github.com/takalawang/corporate-catering-system/services/api/internal/order/http"
)

func buildHandler(api *ohttp.API) http.Handler {
	r := chi.NewRouter()
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)
	return r
}

func TestPlaceOrder_Unauthenticated(t *testing.T) {
	api := &ohttp.API{}
	srv := httptest.NewServer(buildHandler(api))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/employee/orders",
		strings.NewReader(`{"plant":"F12B-3F","supply_date":"2026-05-14","items":[{"menu_item_id":"00000000-0000-0000-0000-000000000000","qty":1}]}`))
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListMyOrders_Unauthenticated(t *testing.T) {
	api := &ohttp.API{}
	srv := httptest.NewServer(buildHandler(api))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/employee/orders")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
