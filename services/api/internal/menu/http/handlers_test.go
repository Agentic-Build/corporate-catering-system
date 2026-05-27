package mhttp_test

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	mhttp "github.com/takalawang/corporate-catering-system/services/api/internal/menu/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu/readmodel"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/storage"
)

const (
	vendorID = "v-owner"
	itemID   = "11111111-1111-1111-1111-111111111111"
	plant    = "F12B-3F"
)

// ============================================================================
// Fakes — menu CRUD repositories (menu.Service)
// ============================================================================

type fakeCategoryRepo struct {
	cats      []*menu.Category
	createErr error
	listErr   error
}

func (r *fakeCategoryRepo) Create(_ context.Context, c *menu.Category) error {
	if r.createErr != nil {
		return r.createErr
	}
	c.ID = "cat-1"
	r.cats = append(r.cats, c)
	return nil
}
func (r *fakeCategoryRepo) Update(context.Context, *menu.Category) error { return nil }
func (r *fakeCategoryRepo) Delete(context.Context, string) error         { return nil }
func (r *fakeCategoryRepo) GetByID(context.Context, string) (*menu.Category, error) {
	return nil, menu.ErrCategoryNotFound
}
func (r *fakeCategoryRepo) ListByVendor(_ context.Context, _ string) ([]*menu.Category, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.cats, nil
}

type fakeItemRepo struct {
	byID       map[string]*menu.Item
	rows       []*menu.MerchantItemRow
	active     []*menu.ActiveItemRow
	createErr  error
	updateErr  error
	statusErr  error
	getErr     error
	listErr    error
	activeErr  error
	lastFilter menu.EmployeeMenuFilter
}

func (r *fakeItemRepo) Create(_ context.Context, i *menu.Item) error {
	if r.createErr != nil {
		return r.createErr
	}
	i.ID = itemID
	if r.byID == nil {
		r.byID = map[string]*menu.Item{}
	}
	r.byID[i.ID] = i
	return nil
}
func (r *fakeItemRepo) Update(_ context.Context, i *menu.Item) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.byID[i.ID] = i
	return nil
}
func (r *fakeItemRepo) SetStatus(_ context.Context, id string, s menu.ItemStatus) error {
	if r.statusErr != nil {
		return r.statusErr
	}
	if it, ok := r.byID[id]; ok {
		it.Status = s
	}
	return nil
}
func (r *fakeItemRepo) GetByID(_ context.Context, id string) (*menu.Item, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if it, ok := r.byID[id]; ok {
		clone := *it
		return &clone, nil
	}
	return nil, menu.ErrItemNotFound
}
func (r *fakeItemRepo) ListByVendor(_ context.Context, _ string, _ bool) ([]*menu.MerchantItemRow, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.rows, nil
}
func (r *fakeItemRepo) ListActiveByPlant(_ context.Context, f menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	r.lastFilter = f
	if r.activeErr != nil {
		return nil, r.activeErr
	}
	return r.active, nil
}

type fakeImageRepo struct {
	byItem     map[string][]*menu.Image
	listErr    error
	replaceErr error
}

func (r *fakeImageRepo) Add(context.Context, *menu.Image) error { return nil }
func (r *fakeImageRepo) Remove(context.Context, string) error   { return nil }
func (r *fakeImageRepo) ListByItem(_ context.Context, itemID string) ([]*menu.Image, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.byItem[itemID], nil
}
func (r *fakeImageRepo) ReplaceForItem(context.Context, string, []string) error {
	return r.replaceErr
}

// ============================================================================
// Fakes — favorites
// ============================================================================

type fakeFavoritesRepo struct {
	chips   []menu.FavoriteChip
	next    *time.Time
	addErr  error
	rmErr   error
	listErr error
}

func (r *fakeFavoritesRepo) Add(context.Context, string, string) error    { return r.addErr }
func (r *fakeFavoritesRepo) Remove(context.Context, string, string) error { return r.rmErr }
func (r *fakeFavoritesRepo) ListByUser(_ context.Context, _, _, _ string, _ int, _ *time.Time) ([]menu.FavoriteChip, *time.Time, error) {
	if r.listErr != nil {
		return nil, nil, r.listErr
	}
	return r.chips, r.next, nil
}

// ============================================================================
// Fakes — home service ports
// ============================================================================

type fakeRecentOrders struct {
	recent     []menu.RecentOrderRow
	orderToday *menu.UserOrderToday
	itemNames  map[string][]string
	avail      map[string]bool
	getErr     error
	recentErr  error
	namesErr   error
	availErr   error
}

func (r *fakeRecentOrders) RecentOrdersByUser(_ context.Context, _ string, _, _ int) ([]menu.RecentOrderRow, error) {
	if r.recentErr != nil {
		return nil, r.recentErr
	}
	return r.recent, nil
}
func (r *fakeRecentOrders) GetOrderByUserDate(_ context.Context, _ string, _ time.Time, _ string) (*menu.UserOrderToday, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.orderToday, nil
}
func (r *fakeRecentOrders) ItemNamesByOrderIDs(_ context.Context, _ []string, _ int) (map[string][]string, error) {
	if r.namesErr != nil {
		return nil, r.namesErr
	}
	return r.itemNames, nil
}
func (r *fakeRecentOrders) OrderAvailability(_ context.Context, _ []string, _ time.Time) (map[string]bool, error) {
	if r.availErr != nil {
		return nil, r.availErr
	}
	return r.avail, nil
}

type fakePopularity struct {
	popularity map[string]float64
	meta       []menu.MenuItemMeta
	cutoffs    bool
	popErr     error
	metaErr    error
	cutoffErr  error
}

func (r *fakePopularity) PlantPopularity(_ context.Context, _ string, _ time.Time) (map[string]float64, error) {
	if r.popErr != nil {
		return nil, r.popErr
	}
	return r.popularity, nil
}
func (r *fakePopularity) MetaByIDs(_ context.Context, _ []string) ([]menu.MenuItemMeta, error) {
	if r.metaErr != nil {
		return nil, r.metaErr
	}
	return r.meta, nil
}
func (r *fakePopularity) AllCutoffsPassed(_ context.Context, _ string, _ time.Time, _ time.Time) (bool, error) {
	if r.cutoffErr != nil {
		return false, r.cutoffErr
	}
	return r.cutoffs, nil
}

type fakeAffinity struct {
	aff map[string]float64
	err error
}

func (r *fakeAffinity) UserVendorAffinity(_ context.Context, _ string) (map[string]float64, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.aff, nil
}

type fakeFavForHome struct {
	chips []menu.FavoriteChip
	next  *time.Time
	err   error
}

func (r *fakeFavForHome) ListByUser(_ context.Context, _, _, _ string, _ int, _ *time.Time) ([]menu.FavoriteChip, *time.Time, error) {
	if r.err != nil {
		return nil, nil, r.err
	}
	return r.chips, r.next, nil
}

// fakeCache is a minimal in-memory readmodel.Cache: miss until Set, then hit.
type fakeCache struct{ store map[string][]byte }

func (c *fakeCache) Get(_ context.Context, key string) ([]byte, error) {
	if v, ok := c.store[key]; ok {
		return v, nil
	}
	return nil, readmodel.ErrCacheMiss
}
func (c *fakeCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	c.store[key] = value
	return nil
}
func (c *fakeCache) Invalidate(context.Context, string) error { return nil }

// ============================================================================
// Harness
// ============================================================================

func vendorUser() *identity.User {
	v := vendorID
	return &identity.User{ID: "u-1", Role: identity.RoleVendorOperator, VendorID: &v}
}

func employeeUser() *identity.User {
	p := plant
	return &identity.User{ID: "e-1", Role: identity.RoleEmployee, Plant: &p}
}

// buildMenu wires the menu CRUD API + favorites + home APIs onto a chi router.
// A middleware injects user into the request context (mirrors AuthMiddleware).
func buildMenu(t *testing.T, user *identity.User) (*httptest.Server, *fakeCategoryRepo, *fakeItemRepo, *fakeImageRepo) {
	t.Helper()
	cr := &fakeCategoryRepo{}
	ir := &fakeItemRepo{byID: map[string]*menu.Item{}}
	gr := &fakeImageRepo{byItem: map[string][]*menu.Image{}}
	api := &mhttp.API{Svc: &menu.Service{Categories: cr, Items: ir, Images: gr}}

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
	return srv, cr, ir, gr
}

func buildFavorites(t *testing.T, user *identity.User) (*httptest.Server, *fakeFavoritesRepo) {
	t.Helper()
	fr := &fakeFavoritesRepo{}
	api := &mhttp.FavoritesAPI{Svc: &menu.FavoritesService{Repo: fr}}

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
	return srv, fr
}

func buildHome(t *testing.T, user *identity.User) (*httptest.Server, *fakeRecentOrders, *fakePopularity, *fakeAffinity, *fakeFavForHome, *fakeItemRepo) {
	t.Helper()
	ro := &fakeRecentOrders{}
	pop := &fakePopularity{}
	aff := &fakeAffinity{}
	fav := &fakeFavForHome{}
	ir := &fakeItemRepo{byID: map[string]*menu.Item{}}
	gr := &fakeImageRepo{byItem: map[string][]*menu.Image{}}

	home := &menu.HomeService{
		Clock:         clock.FixedClock{T: time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)},
		ServerTZ:      time.UTC,
		RecentOrders:  ro,
		Popularity:    pop,
		Affinity:      aff,
		FavoritesRepo: fav,
	}
	menuSvc := &menu.Service{Categories: &fakeCategoryRepo{}, Items: ir, Images: gr}
	api := &mhttp.HomeAPI{Home: home, MenuSvc: menuSvc}

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
	return srv, ro, pop, aff, fav, ir
}

// buildPresign mounts only the presigned upload/download routes. storage is
// the *storage.S3Client to wire (nil exercises the 503 path; a non-nil empty
// client lets the auth/validation branches run before any storage call).
func buildPresign(t *testing.T, user *identity.User, storageClient *storage.S3Client) *httptest.Server {
	t.Helper()
	api := &mhttp.API{Svc: &menu.Service{}, Storage: storageClient}

	r := chi.NewRouter()
	if user != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(idhttp.ContextWithUser(req.Context(), user)))
			})
		})
	}
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.RegisterPresigned(h)

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

func fkError() *pgconn.PgError { return &pgconn.PgError{Code: "23503"} }

// ============================================================================
// GET /api/merchant/categories
// ============================================================================

func TestListCategories_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/categories", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListCategories_WrongRole(t *testing.T) {
	srv, _, _, _ := buildMenu(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/categories", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListCategories_NoVendorBinding(t *testing.T) {
	srv, _, _, _ := buildMenu(t, &identity.User{ID: "u-1", Role: identity.RoleVendorOperator})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/categories", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListCategories_OK(t *testing.T) {
	srv, cr, _, _ := buildMenu(t, vendorUser())
	cr.cats = []*menu.Category{{ID: "cat-1", VendorID: vendorID, Name: "主食", SortOrder: 2}}
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/categories", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			SortOrder int    `json:"sort_order"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, "cat-1", out.Items[0].ID)
	assert.Equal(t, "主食", out.Items[0].Name)
	assert.Equal(t, 2, out.Items[0].SortOrder)
}

func TestListCategories_RepoError_500(t *testing.T) {
	srv, cr, _, _ := buildMenu(t, vendorUser())
	cr.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/categories", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// POST /api/merchant/categories
// ============================================================================

func TestCreateCategory_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/categories", `{"name":"主食","sort_order":1}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCreateCategory_MissingName_422(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/categories", `{"name":"","sort_order":1}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreateCategory_OK_201(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/categories", `{"name":"主食","sort_order":3}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Category struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			SortOrder int    `json:"sort_order"`
		} `json:"category"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "cat-1", out.Category.ID)
	assert.Equal(t, "主食", out.Category.Name)
	assert.Equal(t, 3, out.Category.SortOrder)
}

func TestCreateCategory_RepoError_500(t *testing.T) {
	srv, cr, _, _ := buildMenu(t, vendorUser())
	cr.createErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/categories", `{"name":"主食","sort_order":1}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// GET /api/merchant/menu-items
// ============================================================================

func TestListItems_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/menu-items", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListItems_OK(t *testing.T) {
	srv, _, ir, gr := buildMenu(t, vendorUser())
	last := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	ir.rows = []*menu.MerchantItemRow{
		{
			Item: menu.Item{
				ID: itemID, VendorID: vendorID, Name: "雞腿便當",
				PriceMinor: 11000, Status: menu.ItemStatusActive,
				Tags: []string{"招牌"},
			},
			LastUsed:  &last,
			TotalSold: 42,
		},
	}
	gr.byItem[itemID] = []*menu.Image{{BlobURI: "s3://b/1.jpg"}}

	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/menu-items", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID         string   `json:"id"`
			PriceMinor int64    `json:"price_minor"`
			Status     string   `json:"status"`
			Images     []string `json:"images"`
			LastUsed   *string  `json:"last_used"`
			TotalSold  int      `json:"total_sold"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, itemID, out.Items[0].ID)
	assert.Equal(t, int64(11000), out.Items[0].PriceMinor) // whole NTD
	assert.Equal(t, "active", out.Items[0].Status)
	assert.Equal(t, []string{"s3://b/1.jpg"}, out.Items[0].Images)
	require.NotNil(t, out.Items[0].LastUsed)
	assert.Equal(t, "2026-05-10", *out.Items[0].LastUsed)
	assert.Equal(t, 42, out.Items[0].TotalSold)
}

func TestListItems_ListRepoError_500(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/menu-items", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestListItems_ImagesRepoError_500(t *testing.T) {
	srv, _, ir, gr := buildMenu(t, vendorUser())
	ir.rows = []*menu.MerchantItemRow{{Item: menu.Item{ID: itemID, VendorID: vendorID, Name: "X", Status: menu.ItemStatusDraft}}}
	gr.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/menu-items", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// POST /api/merchant/menu-items
// ============================================================================

func TestCreateItem_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items",
		`{"name":"雞腿便當","description":"經典","price_minor":11000,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCreateItem_MissingName_422(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items",
		`{"name":"","description":"x","price_minor":11000,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreateItem_NegativePrice_422(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items",
		`{"name":"X","description":"x","price_minor":-1,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreateItem_OK_201(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items",
		`{"name":"雞腿便當","description":"經典","price_minor":11000,"tags":["招牌"],"images":["s3://b/1.jpg"]}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Item struct {
			ID         string   `json:"id"`
			VendorID   string   `json:"vendor_id"`
			Name       string   `json:"name"`
			PriceMinor int64    `json:"price_minor"`
			Status     string   `json:"status"`
			Tags       []string `json:"tags"`
			Images     []string `json:"images"`
		} `json:"item"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, itemID, out.Item.ID)
	assert.Equal(t, vendorID, out.Item.VendorID)
	assert.Equal(t, int64(11000), out.Item.PriceMinor) // whole NTD
	assert.Equal(t, "active", out.Item.Status)
	assert.Equal(t, []string{"招牌"}, out.Item.Tags)
	assert.Equal(t, []string{"s3://b/1.jpg"}, out.Item.Images)
}

func TestCreateItem_RepoError_500(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.createErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items",
		`{"name":"X","description":"x","price_minor":11000,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// PATCH /api/merchant/menu-items/{id}
// ============================================================================

func TestUpdateItem_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodPatch, srv.URL+"/api/merchant/menu-items/"+itemID,
		`{"name":"X","description":"x","price_minor":12000,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUpdateItem_InvalidUUID_422(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPatch, srv.URL+"/api/merchant/menu-items/not-a-uuid",
		`{"name":"X","description":"x","price_minor":12000,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestUpdateItem_NotFound(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser()) // item not seeded
	resp := do(t, http.MethodPatch, srv.URL+"/api/merchant/menu-items/"+itemID,
		`{"name":"X","description":"x","price_minor":12000,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestUpdateItem_WrongVendor_403(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: "someone-else", Name: "X", Status: menu.ItemStatusDraft}
	resp := do(t, http.MethodPatch, srv.URL+"/api/merchant/menu-items/"+itemID,
		`{"name":"X","description":"x","price_minor":12000,"tags":[]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestUpdateItem_OK(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: vendorID, Name: "old", PriceMinor: 9000, Status: menu.ItemStatusDraft}
	resp := do(t, http.MethodPatch, srv.URL+"/api/merchant/menu-items/"+itemID,
		`{"name":"new","description":"d","price_minor":12000,"tags":["a"],"images":["s3://b/x.jpg"]}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Item struct {
			ID         string   `json:"id"`
			Name       string   `json:"name"`
			PriceMinor int64    `json:"price_minor"`
			Images     []string `json:"images"`
		} `json:"item"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, itemID, out.Item.ID)
	assert.Equal(t, "new", out.Item.Name)
	assert.Equal(t, int64(12000), out.Item.PriceMinor)
	assert.Equal(t, []string{"s3://b/x.jpg"}, out.Item.Images)
}

// ============================================================================
// POST /api/merchant/menu-items/{id}/publish
// ============================================================================

func TestPublishItem_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/publish", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPublishItem_InvalidUUID_422(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/not-a-uuid/publish", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestPublishItem_NotFound(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/publish", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestPublishItem_WrongVendor_403(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: "someone-else", Status: menu.ItemStatusDraft}
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/publish", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPublishItem_OK_204(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: vendorID, Status: menu.ItemStatusDraft}
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/publish", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// ============================================================================
// POST /api/merchant/menu-items/{id}/archive
// ============================================================================

func TestArchiveItem_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/archive", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestArchiveItem_NotFound(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/archive", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestArchiveItem_WrongVendor_403(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: "someone-else", Status: menu.ItemStatusActive}
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/archive", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestArchiveItem_OK_204(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: vendorID, Status: menu.ItemStatusActive}
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/archive", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// ============================================================================
// POST /api/merchant/menu-items/{id}/copy
// ============================================================================

func TestCopyItem_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/copy", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCopyItem_NotFound(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/copy", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCopyItem_WrongVendor_403(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: "someone-else", Name: "X", Status: menu.ItemStatusActive}
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/copy", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCopyItem_OK_201(t *testing.T) {
	srv, _, ir, gr := buildMenu(t, vendorUser())
	ir.byID[itemID] = &menu.Item{ID: itemID, VendorID: vendorID, Name: "雞腿便當", PriceMinor: 11000, Status: menu.ItemStatusActive}
	gr.byItem[itemID] = []*menu.Image{{BlobURI: "s3://b/a.jpg"}}
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/menu-items/"+itemID+"/copy", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Item struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			PriceMinor int64  `json:"price_minor"`
		} `json:"item"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "雞腿便當（複製）", out.Item.Name)
	assert.Equal(t, "active", out.Item.Status)
	assert.Equal(t, int64(11000), out.Item.PriceMinor)
}

// ============================================================================
// GET /api/employee/menu
// ============================================================================

func TestListEmployeeMenu_Unauthenticated(t *testing.T) {
	srv, _, _, _ := buildMenu(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListEmployeeMenu_WrongRole(t *testing.T) {
	srv, _, _, _ := buildMenu(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListEmployeeMenu_NoPlant_400(t *testing.T) {
	srv, _, _, _ := buildMenu(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListEmployeeMenu_BadDay_400(t *testing.T) {
	srv, _, _, _ := buildMenu(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu?day=nope", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListEmployeeMenu_OK(t *testing.T) {
	srv, _, ir, gr := buildMenu(t, employeeUser())
	day := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	ir.active = []*menu.ActiveItemRow{
		{
			Item: menu.Item{
				ID: itemID, VendorID: vendorID, Name: "排骨便當", PriceMinor: 12000,
				Status: menu.ItemStatusActive, Tags: []string{"招牌"},
			},
			VendorName: "稻禾家", SupplyDate: day, Capacity: 50, Remain: 30,
			PickupWindow: "12:00-12:30", ETALabel: "10 分鐘後",
		},
	}
	gr.byItem[itemID] = []*menu.Image{{BlobURI: "blob://1"}}

	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu?day=2026-05-14", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID         string   `json:"id"`
			Vendor     string   `json:"vendor"`
			PriceMinor int64    `json:"price_minor"`
			Remain     int      `json:"remain"`
			Capacity   int      `json:"capacity"`
			SoldOut    bool     `json:"sold_out"`
			Images     []string `json:"images"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, itemID, out.Items[0].ID)
	assert.Equal(t, "稻禾家", out.Items[0].Vendor)
	assert.Equal(t, int64(12000), out.Items[0].PriceMinor) // whole NTD
	assert.Equal(t, 30, out.Items[0].Remain)
	assert.Equal(t, 50, out.Items[0].Capacity)
	assert.False(t, out.Items[0].SoldOut)
	assert.Equal(t, []string{"blob://1"}, out.Items[0].Images)
}

func TestListEmployeeMenu_PlantFromQueryWhenUserUnset(t *testing.T) {
	// user has employee role but no plant; ?plant= supplies it.
	srv, _, ir, _ := buildMenu(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu?plant=Z9&in_stock=true&price_min=5000&price_max=20000&sort=price_asc&q=雞&tags=低卡", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Z9", ir.lastFilter.Plant)
	assert.Equal(t, "雞", ir.lastFilter.Q)
	require.NotNil(t, ir.lastFilter.PriceMin)
	assert.Equal(t, int64(5000), *ir.lastFilter.PriceMin)
	require.NotNil(t, ir.lastFilter.PriceMax)
	assert.Equal(t, int64(20000), *ir.lastFilter.PriceMax)
	require.NotNil(t, ir.lastFilter.InStock)
	assert.True(t, *ir.lastFilter.InStock)
	assert.Equal(t, menu.EmployeeMenuSortPriceAsc, ir.lastFilter.Sort)
}

func TestListEmployeeMenu_RepoError_500(t *testing.T) {
	srv, _, ir, _ := buildMenu(t, employeeUser())
	ir.activeErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/menu", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// POST /api/employee/favorites
// ============================================================================

func TestAddFavorite_Unauthenticated(t *testing.T) {
	srv, _ := buildFavorites(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/favorites", `{"menu_item_id":"`+itemID+`"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAddFavorite_WrongRole(t *testing.T) {
	srv, _ := buildFavorites(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/favorites", `{"menu_item_id":"`+itemID+`"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAddFavorite_InvalidUUID_422(t *testing.T) {
	srv, _ := buildFavorites(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/favorites", `{"menu_item_id":"not-a-uuid"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestAddFavorite_OK_201(t *testing.T) {
	srv, _ := buildFavorites(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/favorites", `{"menu_item_id":"`+itemID+`"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestAddFavorite_MenuItemNotFound_404(t *testing.T) {
	srv, fr := buildFavorites(t, employeeUser())
	fr.addErr = fkError() // FK violation → 404
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/favorites", `{"menu_item_id":"`+itemID+`"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAddFavorite_RepoError_500(t *testing.T) {
	srv, fr := buildFavorites(t, employeeUser())
	fr.addErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/favorites", `{"menu_item_id":"`+itemID+`"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// DELETE /api/employee/favorites/{menu_item_id}
// ============================================================================

func TestRemoveFavorite_Unauthenticated(t *testing.T) {
	srv, _ := buildFavorites(t, nil)
	resp := do(t, http.MethodDelete, srv.URL+"/api/employee/favorites/"+itemID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestRemoveFavorite_InvalidUUID_422(t *testing.T) {
	srv, _ := buildFavorites(t, employeeUser())
	resp := do(t, http.MethodDelete, srv.URL+"/api/employee/favorites/not-a-uuid", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestRemoveFavorite_OK_204(t *testing.T) {
	srv, _ := buildFavorites(t, employeeUser())
	resp := do(t, http.MethodDelete, srv.URL+"/api/employee/favorites/"+itemID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestRemoveFavorite_RepoError_500(t *testing.T) {
	srv, fr := buildFavorites(t, employeeUser())
	fr.rmErr = errors.New("db down")
	resp := do(t, http.MethodDelete, srv.URL+"/api/employee/favorites/"+itemID, "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// GET /api/employee/favorites
// ============================================================================

func TestListFavorites_Unauthenticated(t *testing.T) {
	srv, _ := buildFavorites(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/favorites?day=2026-05-14", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListFavorites_NoPlant_400(t *testing.T) {
	srv, _ := buildFavorites(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/favorites?day=2026-05-14", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListFavorites_MissingDay_400(t *testing.T) {
	srv, _ := buildFavorites(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/favorites", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListFavorites_BadDay_400(t *testing.T) {
	srv, _ := buildFavorites(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/favorites?day=nope", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListFavorites_BadCursor_400(t *testing.T) {
	srv, _ := buildFavorites(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/favorites?day=2026-05-14&cursor=nope", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListFavorites_OK(t *testing.T) {
	srv, fr := buildFavorites(t, employeeUser())
	next := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	fr.chips = []menu.FavoriteChip{
		{MenuItemID: itemID, Name: "雞腿便當", UnitPrice: 11000, VendorID: vendorID, AvailableToday: true},
	}
	fr.next = &next
	cursor := time.Date(2026, 5, 14, 7, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/favorites?day=2026-05-14&limit=5&cursor="+cursor, "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Chips []struct {
			MenuItemID     string `json:"menu_item_id"`
			Name           string `json:"name"`
			UnitPrice      int64  `json:"unit_price"`
			VendorID       string `json:"vendor_id"`
			AvailableToday bool   `json:"available_today"`
		} `json:"chips"`
		NextCursor *string `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Chips, 1)
	assert.Equal(t, itemID, out.Chips[0].MenuItemID)
	assert.Equal(t, int64(11000), out.Chips[0].UnitPrice) // whole NTD
	assert.True(t, out.Chips[0].AvailableToday)
	require.NotNil(t, out.NextCursor)
}

func TestListFavorites_RepoError_500(t *testing.T) {
	srv, fr := buildFavorites(t, employeeUser())
	fr.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/favorites?day=2026-05-14", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// GET /api/employee/home
// ============================================================================

func TestGetHome_Unauthenticated(t *testing.T) {
	srv, _, _, _, _, _ := buildHome(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetHome_NoPlant_400(t *testing.T) {
	srv, _, _, _, _, _ := buildHome(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGetHome_OK(t *testing.T) {
	srv, ro, pop, aff, fav, ir := buildHome(t, employeeUser())
	// Order already placed today (status still open → HasOrdered, stays on today).
	ro.orderToday = &menu.UserOrderToday{
		OrderID: "o-1", VendorID: vendorID, Status: "submitted",
		TotalPriceMinor: 13000, CutoffAt: time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC),
	}
	ro.recent = []menu.RecentOrderRow{
		{OrderID: "o-1", VendorID: vendorID, TotalPriceMinor: 13000, Freq: 3},
	}
	ro.itemNames = map[string][]string{"o-1": {"雞腿便當"}}
	ro.avail = map[string]bool{"o-1": true}
	pop.popularity = map[string]float64{"m-1": 5}
	pop.meta = []menu.MenuItemMeta{{ID: "m-1", Name: "炸雞", UnitPrice: 8000, VendorID: vendorID}}
	aff.aff = map[string]float64{vendorID: 1}
	fav.chips = []menu.FavoriteChip{{MenuItemID: "m-1", Name: "炸雞", UnitPrice: 8000, VendorID: vendorID}}
	ir.active = []*menu.ActiveItemRow{
		{Item: menu.Item{ID: itemID, VendorID: vendorID, Name: "排骨便當", PriceMinor: 12000, Status: menu.ItemStatusActive}, VendorName: "稻禾家", Capacity: 50, Remain: 20},
	}

	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		TargetDay    string `json:"target_day"`
		HasOrdered   bool   `json:"has_ordered"`
		OrderSummary *struct {
			OrderID         string `json:"order_id"`
			TotalPriceMinor int64  `json:"total_price_minor"`
		} `json:"order_summary"`
		ReorderChips  []map[string]any `json:"reorder_chips"`
		FavoriteChips []map[string]any `json:"favorite_chips"`
		DayMenu       []struct {
			ID         string `json:"id"`
			PriceMinor int64  `json:"price_minor"`
		} `json:"day_menu"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "2026-05-14", out.TargetDay)
	assert.True(t, out.HasOrdered)
	require.NotNil(t, out.OrderSummary)
	assert.Equal(t, "o-1", out.OrderSummary.OrderID)
	assert.Equal(t, int64(13000), out.OrderSummary.TotalPriceMinor) // whole NTD
	require.Len(t, out.ReorderChips, 1)
	require.Len(t, out.DayMenu, 1)
	assert.Equal(t, int64(12000), out.DayMenu[0].PriceMinor)
}

func TestGetHome_NoOrderTomorrowWhenCutoffsPassed(t *testing.T) {
	srv, ro, pop, _, _, _ := buildHome(t, employeeUser())
	ro.orderToday = nil
	pop.cutoffs = true // all cutoffs passed → target_day rolls to tomorrow
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		TargetDay  string `json:"target_day"`
		HasOrdered bool   `json:"has_ordered"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "2026-05-15", out.TargetDay)
	assert.False(t, out.HasOrdered)
}

func TestGetHome_ComputeError_400(t *testing.T) {
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.getErr = errors.New("db down") // Compute() surfaces error → 400
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestGetHome_ReorderError_500(t *testing.T) {
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.recentErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestGetHome_FavoriteChipsError_500(t *testing.T) {
	srv, _, _, _, fav, _ := buildHome(t, employeeUser())
	fav.err = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestGetHome_RecommendChipsError_500(t *testing.T) {
	srv, _, pop, _, _, _ := buildHome(t, employeeUser())
	pop.popErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestGetHome_DayMenuError_500(t *testing.T) {
	srv, _, _, _, _, ir := buildHome(t, employeeUser())
	ir.activeErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// TestGetHome_WithCache exercises the cached computeHome wrapper: first request
// is a cache miss (recompute + store), second is a hit. Both must return 200.
func TestGetHome_WithCache(t *testing.T) {
	ro := &fakeRecentOrders{}
	pop := &fakePopularity{}
	home := &menu.HomeService{
		Clock:         clock.FixedClock{T: time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)},
		ServerTZ:      time.UTC,
		RecentOrders:  ro,
		Popularity:    pop,
		Affinity:      &fakeAffinity{},
		FavoritesRepo: &fakeFavForHome{},
	}
	menuSvc := &menu.Service{
		Categories: &fakeCategoryRepo{},
		Items:      &fakeItemRepo{byID: map[string]*menu.Item{}},
		Images:     &fakeImageRepo{byItem: map[string][]*menu.Image{}},
	}
	api := &mhttp.HomeAPI{
		Home:    home,
		MenuSvc: menuSvc,
		Cache:   &fakeCache{store: map[string][]byte{}},
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(idhttp.ContextWithUser(req.Context(), employeeUser())))
		})
	})
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	for i := 0; i < 2; i++ { // miss then hit
		resp := do(t, http.MethodGet, srv.URL+"/api/employee/home?day=2026-05-14", "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()
	}
}

// ============================================================================
// GET /api/employee/reorders
// ============================================================================

func TestListReorders_Unauthenticated(t *testing.T) {
	srv, _, _, _, _, _ := buildHome(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/reorders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListReorders_NoPlant_400(t *testing.T) {
	srv, _, _, _, _, _ := buildHome(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/reorders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListReorders_OK(t *testing.T) {
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.recent = []menu.RecentOrderRow{
		{OrderID: "o-1", VendorID: vendorID, TotalPriceMinor: 13000, Freq: 3},
	}
	ro.itemNames = map[string][]string{"o-1": {"雞腿便當", "綠茶"}}
	ro.avail = map[string]bool{"o-1": true}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/reorders?limit=5", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Chips []struct {
			SourceOrderID   string   `json:"source_order_id"`
			TotalPriceMinor int64    `json:"total_price_minor"`
			ItemsPreview    []string `json:"items_preview"`
			Freq            int      `json:"freq"`
			AvailableToday  bool     `json:"available_today"`
		} `json:"chips"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Chips, 1)
	assert.Equal(t, "o-1", out.Chips[0].SourceOrderID)
	assert.Equal(t, int64(13000), out.Chips[0].TotalPriceMinor) // whole NTD
	assert.Equal(t, []string{"雞腿便當", "綠茶"}, out.Chips[0].ItemsPreview)
	assert.Equal(t, 3, out.Chips[0].Freq)
	assert.True(t, out.Chips[0].AvailableToday)
}

func TestListReorders_RepoError_500(t *testing.T) {
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.recentErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/reorders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestListReorders_NextCursor(t *testing.T) {
	// A full page (rows == limit) yields a next_cursor.
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.recent = []menu.RecentOrderRow{{OrderID: "o-1", VendorID: vendorID, TotalPriceMinor: 9000, Freq: 1}}
	ro.itemNames = map[string][]string{"o-1": {"X"}}
	ro.avail = map[string]bool{"o-1": true}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/reorders?limit=1&cursor=0", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		NextCursor *int `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotNil(t, out.NextCursor)
	assert.Equal(t, 1, *out.NextCursor)
}

func TestListReorders_NilPreviewNormalised(t *testing.T) {
	// No item names for the order → ItemsPreview nil → DTO emits []  not null.
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.recent = []menu.RecentOrderRow{{OrderID: "o-1", VendorID: vendorID, TotalPriceMinor: 9000, Freq: 1}}
	ro.itemNames = nil
	ro.avail = nil
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/reorders", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Chips []struct {
			ItemsPreview []string `json:"items_preview"`
		} `json:"chips"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Chips, 1)
	assert.Equal(t, []string{}, out.Chips[0].ItemsPreview)
}

// ============================================================================
// GET /api/employee/recommendations
// ============================================================================

func TestListRecommendations_Unauthenticated(t *testing.T) {
	srv, _, _, _, _, _ := buildHome(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListRecommendations_NoPlant_400(t *testing.T) {
	srv, _, _, _, _, _ := buildHome(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee})
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListRecommendations_BadDay_400(t *testing.T) {
	srv, _, _, _, _, _ := buildHome(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations?day=nope", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListRecommendations_OK(t *testing.T) {
	srv, _, pop, aff, fav, _ := buildHome(t, employeeUser())
	pop.popularity = map[string]float64{"m-1": 10, "m-2": 5}
	pop.meta = []menu.MenuItemMeta{
		{ID: "m-1", Name: "炸雞", UnitPrice: 8000, VendorID: vendorID},
		{ID: "m-2", Name: "牛肉麵", UnitPrice: 14000, VendorID: "v-other"},
	}
	aff.aff = map[string]float64{vendorID: 3}
	fav.chips = nil // nothing excluded
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations?day=2026-05-14&limit=5", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Chips []struct {
			MenuItemID string  `json:"menu_item_id"`
			UnitPrice  int64   `json:"unit_price"`
			Score      float64 `json:"score"`
		} `json:"chips"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.Chips)
	assert.Equal(t, int64(8000), out.Chips[0].UnitPrice) // whole NTD
}

func TestListRecommendations_RepoError_500(t *testing.T) {
	srv, _, pop, _, _, _ := buildHome(t, employeeUser())
	pop.popErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations?day=2026-05-14", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestListRecommendations_NoDayUsesComputedTargetDay(t *testing.T) {
	// Omitting ?day= forces computeHome() to derive target_day first.
	srv, ro, pop, aff, fav, _ := buildHome(t, employeeUser())
	ro.orderToday = nil // stay on today (cutoffs not passed)
	pop.popularity = map[string]float64{"m-1": 10}
	pop.meta = []menu.MenuItemMeta{{ID: "m-1", Name: "炸雞", UnitPrice: 8000, VendorID: vendorID}}
	aff.aff = map[string]float64{vendorID: 1}
	fav.chips = nil
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Chips []struct {
			MenuItemID string `json:"menu_item_id"`
		} `json:"chips"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.Chips)
	assert.Equal(t, "m-1", out.Chips[0].MenuItemID)
}

func TestListRecommendations_NextCursor(t *testing.T) {
	// Two candidates with limit=1 → a second page → next_cursor set.
	srv, _, pop, aff, fav, _ := buildHome(t, employeeUser())
	pop.popularity = map[string]float64{"m-1": 10, "m-2": 5}
	pop.meta = []menu.MenuItemMeta{
		{ID: "m-1", Name: "炸雞", UnitPrice: 8000, VendorID: vendorID},
		{ID: "m-2", Name: "牛肉麵", UnitPrice: 14000, VendorID: "v-other"},
	}
	aff.aff = map[string]float64{vendorID: 3}
	fav.chips = nil
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations?day=2026-05-14&limit=1&cursor=0", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var out struct {
		Chips      []map[string]any `json:"chips"`
		NextCursor *int             `json:"next_cursor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Chips, 1)
	require.NotNil(t, out.NextCursor)
}

func TestListRecommendations_MetaError_500(t *testing.T) {
	srv, _, pop, _, _, _ := buildHome(t, employeeUser())
	pop.popularity = map[string]float64{"m-1": 10}
	pop.metaErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations?day=2026-05-14", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// ============================================================================
// POST /api/merchant/uploads/presigned  (non-storage branches only)
// ============================================================================

func TestPresignUpload_Unauthenticated(t *testing.T) {
	srv := buildPresign(t, nil, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/uploads/presigned",
		`{"content_type":"image/jpeg","size":1024}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPresignUpload_WrongRole(t *testing.T) {
	srv := buildPresign(t, employeeUser(), nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/uploads/presigned",
		`{"content_type":"image/jpeg","size":1024}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPresignUpload_MissingBodyFields_422(t *testing.T) {
	srv := buildPresign(t, vendorUser(), nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/uploads/presigned", `{}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestPresignUpload_UnsupportedContentType_400(t *testing.T) {
	srv := buildPresign(t, vendorUser(), nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/uploads/presigned",
		`{"content_type":"application/pdf","size":1024}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPresignUpload_NoStorage_503(t *testing.T) {
	// Auth + validation pass; nil Storage short-circuits with 503.
	srv := buildPresign(t, vendorUser(), nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/uploads/presigned",
		`{"content_type":"image/jpeg","size":1024}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// ============================================================================
// GET /api/menu/uploads/presigned  (non-storage branches only)
// ============================================================================

func TestPresignDownload_NoStorage_503(t *testing.T) {
	srv := buildPresign(t, employeeUser(), nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/menu/uploads/presigned?key=menu-images/v1/a.jpg", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestPresignDownload_Unauthenticated_401(t *testing.T) {
	// Non-nil storage so the nil-check passes; no user → requireAuthed 401.
	srv := buildPresign(t, nil, &storage.S3Client{})
	resp := do(t, http.MethodGet, srv.URL+"/api/menu/uploads/presigned?key=menu-images/v1/a.jpg", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPresignDownload_BadKey_403(t *testing.T) {
	// Key outside menu-images/ is rejected before any storage call.
	srv := buildPresign(t, employeeUser(), &storage.S3Client{})
	resp := do(t, http.MethodGet, srv.URL+"/api/menu/uploads/presigned?key=payroll/batch.csv", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPresignDownload_Traversal_400(t *testing.T) {
	srv := buildPresign(t, employeeUser(), &storage.S3Client{})
	resp := do(t, http.MethodGet, srv.URL+"/api/menu/uploads/presigned?key=menu-images/../payroll/x.csv", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
