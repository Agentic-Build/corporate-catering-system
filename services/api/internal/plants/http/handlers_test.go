package phttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/plants"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

type fakePlantRepo struct {
	byCode    map[string]*plants.Plant
	listErr   error
	createErr error
	updateErr error
}

func (f *fakePlantRepo) List(_ context.Context, activeOnly bool) ([]*plants.Plant, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*plants.Plant
	for _, p := range f.byCode {
		if activeOnly && !p.Active {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (f *fakePlantRepo) Get(_ context.Context, code string) (*plants.Plant, error) {
	if p, ok := f.byCode[code]; ok {
		return p, nil
	}
	return nil, plants.ErrPlantNotFound
}

func (f *fakePlantRepo) Create(_ context.Context, p *plants.Plant) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.byCode[p.Code] = p
	return nil
}

func (f *fakePlantRepo) Update(_ context.Context, p *plants.Plant) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.byCode[p.Code] = p
	return nil
}

type fakeMappingRepo struct {
	byVendor map[string][]string
	inactive map[string]bool // plant codes the repo reports as inactive
	listErr  error
	setErr   error
}

func (f *fakeMappingRepo) ListByVendor(_ context.Context, vendorID string) ([]*vendor.PlantMapping, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*vendor.PlantMapping
	for _, code := range f.byVendor[vendorID] {
		out = append(out, &vendor.PlantMapping{VendorID: vendorID, Plant: code, Active: !f.inactive[code]})
	}
	return out, nil
}

func (f *fakeMappingRepo) ListVendorsForPlant(context.Context, string) ([]string, error) {
	return nil, nil
}

func (f *fakeMappingRepo) Set(_ context.Context, vendorID string, codes []string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.byVendor[vendorID] = codes
	return nil
}

func (f *fakeMappingRepo) SetWindow(context.Context, string, string, string) error { return nil }

func newTestAPI(plantRepo *fakePlantRepo, mappingRepo *fakeMappingRepo) *API {
	return &API{
		Svc:       &plants.Service{Repo: plantRepo},
		VendorSvc: &vendor.Service{Plants: mappingRepo},
	}
}

func vendorCtx(vendorID string) context.Context {
	vid := vendorID
	return idhttp.ContextWithUser(context.Background(), &identity.User{
		ID: "u-vendor", Role: identity.RoleVendorOperator, VendorID: &vid,
	})
}

func statusOf(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	var se huma.StatusError
	require.True(t, errors.As(err, &se), "error is not a huma.StatusError: %v", err)
	return se.GetStatus()
}

func TestMerchantSet_RejectsAnonymous(t *testing.T) {
	a := newTestAPI(&fakePlantRepo{byCode: map[string]*plants.Plant{}}, &fakeMappingRepo{byVendor: map[string][]string{}})
	_, err := a.merchantSet(context.Background(), &setMerchantPlantsInput{})
	assert.Equal(t, 401, statusOf(t, err))
}

func TestMerchantSet_RejectsNonVendor(t *testing.T) {
	a := newTestAPI(&fakePlantRepo{byCode: map[string]*plants.Plant{}}, &fakeMappingRepo{byVendor: map[string][]string{}})
	ctx := idhttp.ContextWithUser(context.Background(), &identity.User{ID: "u-emp", Role: identity.RoleEmployee})
	_, err := a.merchantSet(ctx, &setMerchantPlantsInput{})
	assert.Equal(t, 403, statusOf(t, err))
}

func TestMerchantSet_PersistsSelectedPlants(t *testing.T) {
	plantRepo := &fakePlantRepo{byCode: map[string]*plants.Plant{
		"P1": {Code: "P1", Active: true},
		"P2": {Code: "P2", Active: true},
	}}
	mapping := &fakeMappingRepo{byVendor: map[string][]string{}}
	a := newTestAPI(plantRepo, mapping)

	in := &setMerchantPlantsInput{}
	in.Body.Plants = []string{"P1", "P2"}
	_, err := a.merchantSet(vendorCtx("v1"), in)

	require.NoError(t, err)
	assert.Equal(t, []string{"P1", "P2"}, mapping.byVendor["v1"])
}

func TestMerchantSet_RejectsInactivePlant(t *testing.T) {
	plantRepo := &fakePlantRepo{byCode: map[string]*plants.Plant{
		"P1": {Code: "P1", Active: false},
	}}
	a := newTestAPI(plantRepo, &fakeMappingRepo{byVendor: map[string][]string{}})

	in := &setMerchantPlantsInput{}
	in.Body.Plants = []string{"P1"}
	_, err := a.merchantSet(vendorCtx("v1"), in)

	assert.Equal(t, 400, statusOf(t, err))
}

func TestMerchantSet_EmptyClearsAllMappings(t *testing.T) {
	plantRepo := &fakePlantRepo{byCode: map[string]*plants.Plant{"P1": {Code: "P1", Active: true}}}
	mapping := &fakeMappingRepo{byVendor: map[string][]string{"v1": {"P1"}}}
	a := newTestAPI(plantRepo, mapping)

	in := &setMerchantPlantsInput{}
	in.Body.Plants = []string{}
	_, err := a.merchantSet(vendorCtx("v1"), in)

	require.NoError(t, err)
	assert.Empty(t, mapping.byVendor["v1"])
}

func TestMerchantList_ReturnsEnrichedMappings(t *testing.T) {
	plantRepo := &fakePlantRepo{byCode: map[string]*plants.Plant{
		"P1": {Code: "P1", Label: "廠區一", Address: "台北", Active: true, SortOrder: 1},
	}}
	mapping := &fakeMappingRepo{byVendor: map[string][]string{"v1": {"P1"}}}
	a := newTestAPI(plantRepo, mapping)

	out, err := a.merchantList(vendorCtx("v1"), &struct{}{})

	require.NoError(t, err)
	require.Len(t, out.Body.Items, 1)
	assert.Equal(t, "P1", out.Body.Items[0].Code)
	assert.Equal(t, "廠區一", out.Body.Items[0].Label)
	assert.Equal(t, "台北", out.Body.Items[0].Address)
}

func TestMerchantList_RejectsNonVendor(t *testing.T) {
	a := newTestAPI(&fakePlantRepo{byCode: map[string]*plants.Plant{}}, &fakeMappingRepo{byVendor: map[string][]string{}})
	ctx := idhttp.ContextWithUser(context.Background(), &identity.User{ID: "u-emp", Role: identity.RoleEmployee})
	_, err := a.merchantList(ctx, &struct{}{})
	assert.Equal(t, 403, statusOf(t, err))
}

// ----- HTTP harness (exercises huma validation + the full request path) -----

// huma marks every non-pointer body field without ,omitempty as required, so
// request bodies must carry all of them or validation 422s before the handler.
const (
	createBody = `{"code":"P3","label":"廠區三","address":"台中","sort_order":3}`
	updateBody = `{"label":"x","address":"y","active":true,"sort_order":0}`
)

func adminUser() *identity.User {
	return &identity.User{ID: "a-1", Role: identity.RoleWelfareAdmin}
}

func employeeUser() *identity.User {
	return &identity.User{ID: "e-1", Role: identity.RoleEmployee}
}

func vendorUser(vendorID string) *identity.User {
	v := vendorID
	return &identity.User{ID: "u-vendor", Role: identity.RoleVendorOperator, VendorID: &v}
}

// buildHandler wires the plants API onto a chi router. When user != nil a
// middleware injects it into the request context exactly like AuthMiddleware does.
func buildHandler(t *testing.T, user *identity.User, pr *fakePlantRepo, mr *fakeMappingRepo) *httptest.Server {
	t.Helper()
	api := newTestAPI(pr, mr)

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

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func seededRepos() (*fakePlantRepo, *fakeMappingRepo) {
	return &fakePlantRepo{byCode: map[string]*plants.Plant{
		"P1": {Code: "P1", Label: "廠區一", Address: "台北", Active: true, SortOrder: 1},
		"P2": {Code: "P2", Label: "廠區二", Address: "新竹", Active: false, SortOrder: 2},
	}}, &fakeMappingRepo{byVendor: map[string][]string{}}
}

// =========================================================================
// GET /api/plants  (listActive — any authenticated user)
// =========================================================================

func TestListPlants_Unauthenticated(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, nil, pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListPlants_OK_ActiveOnly(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, employeeUser(), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/plants", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			Code   string `json:"code"`
			Label  string `json:"label"`
			Active bool   `json:"active"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1) // only active P1
	assert.Equal(t, "P1", out.Items[0].Code)
	assert.Equal(t, "廠區一", out.Items[0].Label)
	assert.True(t, out.Items[0].Active)
}

func TestListPlants_RepoError_500(t *testing.T) {
	pr, mr := seededRepos()
	pr.listErr = errors.New("db down")
	srv := buildHandler(t, employeeUser(), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// GET /api/admin/plants  (listAll — welfare_admin only)
// =========================================================================

func TestListAllPlants_Unauthenticated(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, nil, pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListAllPlants_WrongRole(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, employeeUser(), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListAllPlants_OK_IncludesInactive(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/plants", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Len(t, out.Items, 2) // active + inactive
}

func TestListAllPlants_RepoError_500(t *testing.T) {
	pr, mr := seededRepos()
	pr.listErr = errors.New("db down")
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// POST /api/admin/plants  (create — welfare_admin only)
// =========================================================================

func TestCreatePlant_Unauthenticated(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, nil, pr, mr)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/plants", createBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCreatePlant_WrongRole(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, employeeUser(), pr, mr)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/plants", createBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreatePlant_MissingRequiredFields_422(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	// code and label have minLength:"1" → huma rejects empty/missing before handler.
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/plants", `{}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreatePlant_DuplicateCode_409(t *testing.T) {
	pr, mr := seededRepos()
	pr.createErr = plants.ErrDuplicateCode
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/plants",
		`{"code":"P1","label":"dup","address":"x","sort_order":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestCreatePlant_RepoError_500(t *testing.T) {
	pr, mr := seededRepos()
	pr.createErr = errors.New("db down")
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/plants", createBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestCreatePlant_BlankLabel_400(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	// " " passes huma minLength:1 but the service trims it to empty → ErrInvalid → 400.
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/plants",
		`{"code":"P3","label":" ","address":"x","sort_order":0}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreatePlant_OK_201(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/plants",
		`{"code":"P3","label":"廠區三","address":"台中","sort_order":3}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Plant struct {
			Code      string `json:"code"`
			Label     string `json:"label"`
			Address   string `json:"address"`
			Active    bool   `json:"active"`
			SortOrder int    `json:"sort_order"`
		} `json:"plant"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "P3", out.Plant.Code)
	assert.Equal(t, "廠區三", out.Plant.Label)
	assert.Equal(t, "台中", out.Plant.Address)
	assert.True(t, out.Plant.Active) // service defaults new plants active
	assert.Equal(t, 3, out.Plant.SortOrder)
}

// =========================================================================
// PUT /api/admin/plants/{code}  (update — welfare_admin only)
// =========================================================================

func TestUpdatePlant_Unauthenticated(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, nil, pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/plants/P1", updateBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUpdatePlant_WrongRole(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/plants/P1", updateBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestUpdatePlant_MissingLabel_422(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	// label has minLength:"1" → required.
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/plants/P1", `{"active":true}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestUpdatePlant_NotFound_404(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/plants/NOPE", updateBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestUpdatePlant_RepoError_500(t *testing.T) {
	pr, mr := seededRepos()
	pr.updateErr = errors.New("db down")
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/plants/P1", updateBody)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestUpdatePlant_OK(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/plants/P1",
		`{"label":"更名","address":"桃園","active":false,"sort_order":9}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Plant struct {
			Code      string `json:"code"`
			Label     string `json:"label"`
			Address   string `json:"address"`
			Active    bool   `json:"active"`
			SortOrder int    `json:"sort_order"`
		} `json:"plant"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "P1", out.Plant.Code)
	assert.Equal(t, "更名", out.Plant.Label)
	assert.Equal(t, "桃園", out.Plant.Address)
	assert.False(t, out.Plant.Active)
	assert.Equal(t, 9, out.Plant.SortOrder)
}

// =========================================================================
// GET /api/merchant/plants  (merchantList — vendor_operator only)
// =========================================================================

func TestMerchantListHTTP_Unauthenticated(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, nil, pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMerchantListHTTP_NoVendorBinding(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, &identity.User{ID: "u-1", Role: identity.RoleVendorOperator}, pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantListHTTP_OK_Enriched(t *testing.T) {
	pr, mr := seededRepos()
	mr.byVendor["v1"] = []string{"P1"}
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/plants", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			Code    string `json:"code"`
			Label   string `json:"label"`
			Address string `json:"address"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, "P1", out.Items[0].Code)
	assert.Equal(t, "廠區一", out.Items[0].Label)
	assert.Equal(t, "台北", out.Items[0].Address)
}

// =========================================================================
// PUT /api/merchant/plants  (merchantSet — vendor_operator only)
// =========================================================================

func TestMerchantSetHTTP_Unauthenticated(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, nil, pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/plants", `{"plants":["P1"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMerchantSetHTTP_WrongRole(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, adminUser(), pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/plants", `{"plants":["P1"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantSetHTTP_InactivePlant_400(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	// P2 is inactive → ValidateActiveCodes fails → 400.
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/plants", `{"plants":["P2"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMerchantSetHTTP_OK_204(t *testing.T) {
	pr, mr := seededRepos()
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/plants", `{"plants":["P1"]}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, []string{"P1"}, mr.byVendor["v1"])
}

func TestMerchantSetHTTP_SetRepoError_500(t *testing.T) {
	pr, mr := seededRepos()
	mr.setErr = errors.New("db down")
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/plants", `{"plants":["P1"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestMerchantListHTTP_ListRepoError_500(t *testing.T) {
	pr, mr := seededRepos()
	mr.listErr = errors.New("db down")
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/plants", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestMerchantListHTTP_SkipsInactiveMappings(t *testing.T) {
	pr, mr := seededRepos()
	mr.byVendor["v1"] = []string{"P1", "P2"}
	mr.inactive = map[string]bool{"P2": true} // handler drops inactive mappings
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/plants", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, "P1", out.Items[0].Code)
}

func TestMerchantListHTTP_FallsBackToCodeWhenUnknown(t *testing.T) {
	pr, mr := seededRepos()
	mr.byVendor["v1"] = []string{"GHOST"} // not in the plant registry → enrich falls back to code
	srv := buildHandler(t, vendorUser("v1"), pr, mr)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/plants", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			Code  string `json:"code"`
			Label string `json:"label"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, "GHOST", out.Items[0].Code)
	assert.Equal(t, "GHOST", out.Items[0].Label) // label falls back to code
}
