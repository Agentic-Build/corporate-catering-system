package vhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
	vhttp "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/http"
)

const (
	vendorID   = "11111111-1111-1111-1111-111111111111"
	operatorID = "22222222-2222-2222-2222-222222222222"
)

// Minimal stub session + user store, mirroring the pattern used in idhttp tests.

type stubSessions struct{ sess map[string]*identity.Session }

func (s *stubSessions) Create(_ context.Context, userID string, role identity.Role) (*identity.Session, error) {
	x := &identity.Session{Token: "tok", UserID: userID, Role: role, CreatedAt: time.Now(), LastSeenAt: time.Now()}
	s.sess[x.Token] = x
	return x, nil
}
func (s *stubSessions) Get(_ context.Context, t string) (*identity.Session, error) {
	if v, ok := s.sess[t]; ok {
		return v, nil
	}
	return nil, identity.ErrSessionNotFound
}
func (s *stubSessions) Touch(context.Context, string) error            { return nil }
func (s *stubSessions) Revoke(context.Context, string) error           { return nil }
func (s *stubSessions) RevokeAllForUser(context.Context, string) error { return nil }

type stubUsers struct{ byID map[string]*identity.User }

func (u *stubUsers) GetByID(_ context.Context, id string) (*identity.User, error) {
	if v, ok := u.byID[id]; ok {
		return v, nil
	}
	return nil, identity.ErrUserNotFound
}
func (u *stubUsers) GetByEmail(context.Context, string) (*identity.User, error) {
	return nil, identity.ErrUserNotFound
}
func (u *stubUsers) Create(context.Context, *identity.User) error                { return nil }
func (u *stubUsers) UpdateStatus(context.Context, string, identity.Status) error { return nil }
func (u *stubUsers) UpdateProfile(context.Context, *identity.User) error         { return nil }

// TestRequireAdmin_RejectsEmployee asserts admin-only vendor endpoints return
// 403 when called with an employee session.
func TestRequireAdmin_RejectsEmployee(t *testing.T) {
	user := &identity.User{
		ID: "u-emp", PrimaryEmail: "emp@x.com", DisplayName: "Emp",
		Role: identity.RoleEmployee, Status: identity.StatusActive,
	}
	sessions := &stubSessions{sess: map[string]*identity.Session{
		"tok-emp": {Token: "tok-emp", UserID: "u-emp", Role: identity.RoleEmployee},
	}}
	users := &stubUsers{byID: map[string]*identity.User{"u-emp": user}}

	idAPI := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	vendorAPI := &vhttp.API{Svc: nil} // Svc not reached when guard fails

	r := chi.NewRouter()
	r.Use(idAPI.AuthMiddleware)
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	vendorAPI.Register(h)

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/admin/vendors", nil)
	req.Header.Set("Authorization", "Bearer tok-emp")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestRequireAdmin_RejectsAnonymous asserts the same endpoints return 401 when
// no Authorization header is present.
func TestRequireAdmin_RejectsAnonymous(t *testing.T) {
	sessions := &stubSessions{sess: map[string]*identity.Session{}}
	users := &stubUsers{byID: map[string]*identity.User{}}

	idAPI := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	vendorAPI := &vhttp.API{Svc: nil}

	r := chi.NewRouter()
	r.Use(idAPI.AuthMiddleware)
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	vendorAPI.Register(h)

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/admin/vendors", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// ----- Fakes (vhttp_test can't import the vendor_test fakes) -----

type fakeVendorRepo struct {
	mu      sync.Mutex
	byID    map[string]*vendor.Vendor
	byEmail map[string]*vendor.Vendor
	nextID  int
	getErr  error
	listErr error
}

func newFakeVendorRepo() *fakeVendorRepo {
	return &fakeVendorRepo{byID: map[string]*vendor.Vendor{}, byEmail: map[string]*vendor.Vendor{}}
}

func (r *fakeVendorRepo) Create(_ context.Context, v *vendor.Vendor) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	v.ID = "vendor-" + strconv.Itoa(r.nextID)
	v.CreatedAt = time.Now().UTC()
	v.UpdatedAt = v.CreatedAt
	r.byID[v.ID] = v
	r.byEmail[v.ContactEmail] = v
	return nil
}

func (r *fakeVendorRepo) GetByID(_ context.Context, id string) (*vendor.Vendor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	if v, ok := r.byID[id]; ok {
		clone := *v
		return &clone, nil
	}
	return nil, vendor.ErrVendorNotFound
}

func (r *fakeVendorRepo) GetByEmail(_ context.Context, e string) (*vendor.Vendor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.byEmail[e]; ok {
		return v, nil
	}
	return nil, vendor.ErrVendorNotFound
}

func (r *fakeVendorRepo) UpdateStatus(_ context.Context, id string, s vendor.Status, by *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.byID[id]
	if !ok {
		return vendor.ErrVendorNotFound
	}
	v.Status = s
	if s == vendor.StatusApproved {
		now := time.Now().UTC()
		v.ApprovedAt = &now
		v.ApprovedBy = by
	}
	return nil
}

func (r *fakeVendorRepo) UpdateSettings(_ context.Context, id string, cutoffHour, preorderWindowDays int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.byID[id]
	if !ok {
		return vendor.ErrVendorNotFound
	}
	v.CutoffHour = cutoffHour
	v.PreorderWindowDays = preorderWindowDays
	return nil
}

func (r *fakeVendorRepo) UpdateContactEmail(context.Context, string, string) error { return nil }

func (r *fakeVendorRepo) List(_ context.Context, statuses []vendor.Status) ([]*vendor.Vendor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*vendor.Vendor
	for _, v := range r.byID {
		if len(statuses) == 0 {
			out = append(out, v)
			continue
		}
		for _, s := range statuses {
			if v.Status == s {
				out = append(out, v)
				break
			}
		}
	}
	return out, nil
}

type fakePlantRepo struct {
	mu       sync.Mutex
	byVendor map[string][]*vendor.PlantMapping
	windows  map[string]string // vendorID|plant -> window
	setErr   error
}

func newFakePlantRepo() *fakePlantRepo {
	return &fakePlantRepo{byVendor: map[string][]*vendor.PlantMapping{}, windows: map[string]string{}}
}

func (r *fakePlantRepo) SetWindow(_ context.Context, vendorID, plant, window string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.windows[vendorID+"|"+plant] = window
	return nil
}

func (r *fakePlantRepo) ListByVendor(_ context.Context, id string) ([]*vendor.PlantMapping, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byVendor[id], nil
}

func (r *fakePlantRepo) ListVendorsForPlant(_ context.Context, plant string) ([]string, error) {
	return nil, nil
}

func (r *fakePlantRepo) Set(_ context.Context, id string, plants []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	out := make([]*vendor.PlantMapping, 0, len(plants))
	for _, p := range plants {
		out = append(out, &vendor.PlantMapping{VendorID: id, Plant: p, Active: true})
	}
	r.byVendor[id] = out
	return nil
}

type fakeOperatorRepo struct {
	mu      sync.Mutex
	byID    map[string]*vendor.OperatorAccount
	nextID  int
	listErr error
}

func newFakeOperatorRepo() *fakeOperatorRepo {
	return &fakeOperatorRepo{byID: map[string]*vendor.OperatorAccount{}}
}

func (r *fakeOperatorRepo) Get(_ context.Context, vendorID, operatorID string) (*vendor.OperatorAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.byID[operatorID]
	if !ok || op.VendorID != vendorID {
		return nil, vendor.ErrOperatorNotFound
	}
	return op, nil
}

func (r *fakeOperatorRepo) ListByVendor(_ context.Context, vendorID string) ([]*vendor.OperatorAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*vendor.OperatorAccount
	for _, op := range r.byID {
		if op.VendorID == vendorID {
			out = append(out, op)
		}
	}
	return out, nil
}

func (r *fakeOperatorRepo) ListByVendorStatus(_ context.Context, vendorID string, statuses []vendor.OperatorStatus) ([]*vendor.OperatorAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*vendor.OperatorAccount
	for _, op := range r.byID {
		if op.VendorID != vendorID {
			continue
		}
		if len(statuses) == 0 || hasOperatorStatus(statuses, op.Status) {
			out = append(out, op)
		}
	}
	return out, nil
}

func (r *fakeOperatorRepo) Upsert(_ context.Context, op *vendor.OperatorAccount) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if existing.VendorID == op.VendorID && existing.Email == op.Email {
			op.ID = existing.ID
			op.CreatedAt = existing.CreatedAt
			op.UpdatedAt = time.Now().UTC()
			r.byID[op.ID] = op
			return nil
		}
	}
	r.nextID++
	op.ID = "op-" + strconv.Itoa(r.nextID)
	op.CreatedAt = time.Now().UTC()
	op.UpdatedAt = op.CreatedAt
	r.byID[op.ID] = op
	return nil
}

func (r *fakeOperatorRepo) SetStatus(_ context.Context, vendorID, operatorID string, status vendor.OperatorStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	op, ok := r.byID[operatorID]
	if !ok || op.VendorID != vendorID {
		return vendor.ErrOperatorNotFound
	}
	op.Status = status
	return nil
}

func (r *fakeOperatorRepo) SetStatuses(_ context.Context, vendorID string, from []vendor.OperatorStatus, to vendor.OperatorStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, op := range r.byID {
		if op.VendorID == vendorID && hasOperatorStatus(from, op.Status) {
			op.Status = to
		}
	}
	return nil
}

func hasOperatorStatus(items []vendor.OperatorStatus, status vendor.OperatorStatus) bool {
	for _, item := range items {
		if item == status {
			return true
		}
	}
	return false
}

type fakeProvisioner struct {
	err     error
	upserts int
}

func (p *fakeProvisioner) UpsertVendorOperator(_ context.Context, in identity.VendorOperatorProvisionInput) (*identity.VendorOperatorProvisioned, error) {
	if p.err != nil {
		return nil, p.err
	}
	p.upserts++
	sub := "ak-" + strconv.Itoa(p.upserts)
	return &identity.VendorOperatorProvisioned{Provider: "authentik", ExternalSubject: sub, SetupURL: "http://auth/setup/" + sub}, nil
}

func (p *fakeProvisioner) SuspendVendorOperator(_ context.Context, _ string, _ string) error {
	return p.err
}

func (p *fakeProvisioner) ReinstateVendorOperator(_ context.Context, _ string, _, _ string) error {
	return p.err
}

// ----- Harness -----

func adminUser() *identity.User {
	return &identity.User{ID: "a-1", Role: identity.RoleWelfareAdmin}
}

func vendorUser() *identity.User {
	v := vendorID
	return &identity.User{ID: "u-1", Role: identity.RoleVendorOperator, VendorID: &v}
}

// buildHandler wires the vendor API onto a chi router. When user != nil a
// middleware injects it into the request context exactly like AuthMiddleware does.
func buildHandler(t *testing.T, user *identity.User) (*httptest.Server, *fakeVendorRepo, *fakePlantRepo, *fakeOperatorRepo, *fakeProvisioner) {
	t.Helper()
	vr := newFakeVendorRepo()
	pr := newFakePlantRepo()
	or := newFakeOperatorRepo()
	prov := &fakeProvisioner{}
	api := &vhttp.API{Svc: &vendor.Service{
		Vendors:     vr,
		Plants:      pr,
		Operators:   or,
		Provisioner: prov,
	}}

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
	return srv, vr, pr, or, prov
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

// seedVendor inserts a vendor with a fixed id + status directly into the repo.
func seedVendor(vr *fakeVendorRepo, id string, status vendor.Status) *vendor.Vendor {
	v := &vendor.Vendor{
		ID:                 id,
		DisplayName:        "稻禾家便當",
		LegalName:          "稻禾家便當有限公司",
		ContactEmail:       "ops@daohe.tw",
		Status:             status,
		CutoffHour:         16,
		PreorderWindowDays: 7,
	}
	vr.byID[id] = v
	vr.byEmail[v.ContactEmail] = v
	return v
}

// =========================================================================
// GET /api/admin/vendors
// =========================================================================

func TestListVendors_Unauthenticated(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListVendors_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListVendors_OK_WithPlantMappings(t *testing.T) {
	srv, vr, pr, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	pr.byVendor[vendorID] = []*vendor.PlantMapping{
		{VendorID: vendorID, Plant: "F12B-3F", Active: true, ServiceWindow: "11:30-13:00"},
		{VendorID: vendorID, Plant: "F15-2F", Active: false, ServiceWindow: "12:00-13:00"}, // inactive → skipped
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID            string   `json:"id"`
			DisplayName   string   `json:"display_name"`
			Status        string   `json:"status"`
			Plants        []string `json:"plants"`
			PlantMappings []struct {
				Plant         string `json:"plant"`
				ServiceWindow string `json:"service_window"`
			} `json:"plant_mappings"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, vendorID, out.Items[0].ID)
	assert.Equal(t, "approved", out.Items[0].Status)
	assert.Equal(t, []string{"F12B-3F"}, out.Items[0].Plants)
	require.Len(t, out.Items[0].PlantMappings, 1)
	assert.Equal(t, "11:30-13:00", out.Items[0].PlantMappings[0].ServiceWindow)
}

func TestListVendors_StatusFilter(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	seedVendor(vr, operatorID, vendor.StatusPending)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors?status=pending", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, operatorID, out.Items[0].ID)
}

func TestListVendors_RepoError_500(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	vr.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =========================================================================
// POST /api/admin/vendors
// =========================================================================

func TestCreateVendor_Unauthenticated(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors",
		`{"display_name":"稻禾","legal_name":"稻禾股份","contact_email":"ops@daohe.tw"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCreateVendor_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors",
		`{"display_name":"稻禾","legal_name":"稻禾股份","contact_email":"ops@daohe.tw"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreateVendor_MissingFields_422(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors", `{}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreateVendor_BadEmail_422(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors",
		`{"display_name":"稻禾","legal_name":"稻禾股份","contact_email":"not-an-email"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreateVendor_OK_201(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors",
		`{"display_name":"稻禾","legal_name":"稻禾股份","contact_email":"ops@daohe.tw"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Vendor struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Status      string `json:"status"`
		} `json:"vendor"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.NotEmpty(t, out.Vendor.ID)
	assert.Equal(t, "稻禾", out.Vendor.DisplayName)
	assert.Equal(t, "pending", out.Vendor.Status)
}

// =========================================================================
// POST /api/admin/vendors/{id}/approve
// =========================================================================

func TestApproveVendor_Unauthenticated(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/approve", `{"plants":["F12B-3F"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestApproveVendor_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/approve", `{"plants":["F12B-3F"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestApproveVendor_InvalidUUID_422(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/not-a-uuid/approve", `{"plants":["F12B-3F"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestApproveVendor_NotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/approve", `{"plants":["F12B-3F"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestApproveVendor_AlreadyApproved_409(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/approve", `{"plants":["F12B-3F"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestApproveVendor_InvalidStatus_409(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusTerminated)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/approve", `{"plants":["F12B-3F"]}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestApproveVendor_OK_204(t *testing.T) {
	srv, vr, pr, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusPending)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/approve", `{"plants":["F12B-3F","F15-2F"]}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, vendor.StatusApproved, vr.byID[vendorID].Status)
	require.Len(t, pr.byVendor[vendorID], 2)
}

// =========================================================================
// POST /api/admin/vendors/{id}/suspend
// =========================================================================

func TestSuspendVendor_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/suspend", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSuspendVendor_NotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/suspend", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSuspendVendor_NotApproved_409(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusPending)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/suspend", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestSuspendVendor_OK_204(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/suspend", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, vendor.StatusSuspended, vr.byID[vendorID].Status)
}

// =========================================================================
// POST /api/admin/vendors/{id}/reinstate
// =========================================================================

func TestReinstateVendor_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/reinstate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestReinstateVendor_NotSuspended_409(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/reinstate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestReinstateVendor_OK_204(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusSuspended)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/reinstate", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, vendor.StatusApproved, vr.byID[vendorID].Status)
}

// =========================================================================
// GET /api/admin/vendors/{id}/operators
// =========================================================================

func TestListOperators_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/operators", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListOperators_VendorNotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/operators", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestListOperators_OK(t *testing.T) {
	srv, vr, _, or, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	sub := "ak-7"
	now := time.Now().UTC()
	or.byID[operatorID] = &vendor.OperatorAccount{
		ID: operatorID, VendorID: vendorID, Email: "owner@vendor.tw", DisplayName: "Owner",
		Provider: "authentik", ExternalSubject: &sub, Status: vendor.OperatorStatusActive, LastSyncedAt: &now,
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/operators", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID              string `json:"id"`
			Email           string `json:"email"`
			Provider        string `json:"provider"`
			ExternalSubject string `json:"external_subject"`
			Status          string `json:"status"`
			LastSyncedAt    string `json:"last_synced_at"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, operatorID, out.Items[0].ID)
	assert.Equal(t, "owner@vendor.tw", out.Items[0].Email)
	assert.Equal(t, "authentik", out.Items[0].Provider)
	assert.Equal(t, "ak-7", out.Items[0].ExternalSubject)
	assert.Equal(t, "active", out.Items[0].Status)
	assert.NotEmpty(t, out.Items[0].LastSyncedAt)
}

// =========================================================================
// POST /api/admin/vendors/{id}/operators
// =========================================================================

func TestCreateOperator_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators",
		`{"email":"owner@vendor.tw","display_name":"Owner"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreateOperator_MissingFields_422(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators", `{}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestCreateOperator_VendorNotApproved_409(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusPending)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators",
		`{"email":"owner@vendor.tw","display_name":"Owner"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestCreateOperator_ProvisionerFails_502(t *testing.T) {
	srv, vr, _, _, prov := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	prov.err = errors.New("authentik down")
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators",
		`{"email":"owner@vendor.tw","display_name":"Owner"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

func TestCreateOperator_OK_201(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators",
		`{"email":"Owner@Vendor.tw","display_name":"Owner"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Operator struct {
			ID          string `json:"id"`
			VendorID    string `json:"vendor_id"`
			Email       string `json:"email"`
			Provider    string `json:"provider"`
			Status      string `json:"status"`
			SetupURL    string `json:"setup_url"`
			DisplayName string `json:"display_name"`
		} `json:"operator"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.NotEmpty(t, out.Operator.ID)
	assert.Equal(t, vendorID, out.Operator.VendorID)
	assert.Equal(t, "owner@vendor.tw", out.Operator.Email) // normalized lower-case
	assert.Equal(t, "authentik", out.Operator.Provider)
	assert.Equal(t, "active", out.Operator.Status)
	assert.NotEmpty(t, out.Operator.SetupURL)
}

// =========================================================================
// POST /api/admin/vendors/{id}/operators/{operator_id}/suspend
// =========================================================================

func TestSuspendOperator_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/"+operatorID+"/suspend", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSuspendOperator_InvalidUUID_422(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/not-a-uuid/suspend", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestSuspendOperator_NotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/"+operatorID+"/suspend", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSuspendOperator_NotActive_409(t *testing.T) {
	srv, _, _, or, _ := buildHandler(t, adminUser())
	sub := "ak-1"
	or.byID[operatorID] = &vendor.OperatorAccount{
		ID: operatorID, VendorID: vendorID, ExternalSubject: &sub, Status: vendor.OperatorStatusSuspended,
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/"+operatorID+"/suspend", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestSuspendOperator_OK_204(t *testing.T) {
	srv, _, _, or, _ := buildHandler(t, adminUser())
	sub := "ak-1"
	or.byID[operatorID] = &vendor.OperatorAccount{
		ID: operatorID, VendorID: vendorID, Provider: "authentik", ExternalSubject: &sub, Status: vendor.OperatorStatusActive,
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/"+operatorID+"/suspend", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, vendor.OperatorStatusSuspended, or.byID[operatorID].Status)
}

// =========================================================================
// POST /api/admin/vendors/{id}/operators/{operator_id}/reinstate
// =========================================================================

func TestReinstateOperator_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/"+operatorID+"/reinstate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestReinstateOperator_VendorNotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/"+operatorID+"/reinstate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestReinstateOperator_OK_204(t *testing.T) {
	srv, vr, _, or, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	sub := "ak-1"
	or.byID[operatorID] = &vendor.OperatorAccount{
		ID: operatorID, VendorID: vendorID, Provider: "authentik", ExternalSubject: &sub, Status: vendor.OperatorStatusSuspended,
	}
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/operators/"+operatorID+"/reinstate", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, vendor.OperatorStatusActive, or.byID[operatorID].Status)
}

// =========================================================================
// PUT /api/admin/vendors/{id}/plants/{plant}/window
// =========================================================================

func TestSetPlantWindow_Unauthenticated(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/vendors/"+vendorID+"/plants/F12B-3F/window",
		`{"service_window":"11:30-13:00"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSetPlantWindow_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/vendors/"+vendorID+"/plants/F12B-3F/window",
		`{"service_window":"11:30-13:00"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestSetPlantWindow_VendorNotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/vendors/"+vendorID+"/plants/F12B-3F/window",
		`{"service_window":"11:30-13:00"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestSetPlantWindow_OK_204(t *testing.T) {
	srv, vr, pr, _, _ := buildHandler(t, adminUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	resp := do(t, http.MethodPut, srv.URL+"/api/admin/vendors/"+vendorID+"/plants/F12B-3F/window",
		`{"service_window":"11:30-13:00"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "11:30-13:00", pr.windows[vendorID+"|F12B-3F"])
}

// =========================================================================
// GET /api/merchant/settings
// =========================================================================

func TestMerchantGetSettings_Unauthenticated(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settings", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMerchantGetSettings_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser()) // admin is not a vendor operator
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settings", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantGetSettings_NoVendorBinding(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, &identity.User{ID: "u-1", Role: identity.RoleVendorOperator})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settings", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantGetSettings_VendorNotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser()) // vendor not seeded
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settings", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMerchantGetSettings_OK(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, vendorUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/settings", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Settings struct {
			CutoffHour         int `json:"cutoff_hour"`
			PreorderWindowDays int `json:"preorder_window_days"`
		} `json:"settings"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 16, out.Settings.CutoffHour)
	assert.Equal(t, 7, out.Settings.PreorderWindowDays)
}

// =========================================================================
// PUT /api/merchant/settings
// =========================================================================

func TestMerchantUpdateSettings_Unauthenticated(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/settings",
		`{"cutoff_hour":15,"preorder_window_days":5}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMerchantUpdateSettings_WrongRole(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/settings",
		`{"cutoff_hour":15,"preorder_window_days":5}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantUpdateSettings_OutOfRange_422(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, vendorUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	// cutoff_hour 24 exceeds the schema maximum:23 → huma 422 before the handler.
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/settings",
		`{"cutoff_hour":24,"preorder_window_days":5}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestMerchantUpdateSettings_OK(t *testing.T) {
	srv, vr, _, _, _ := buildHandler(t, vendorUser())
	seedVendor(vr, vendorID, vendor.StatusApproved)
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/settings",
		`{"cutoff_hour":15,"preorder_window_days":5}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Settings struct {
			CutoffHour         int `json:"cutoff_hour"`
			PreorderWindowDays int `json:"preorder_window_days"`
		} `json:"settings"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, 15, out.Settings.CutoffHour)
	assert.Equal(t, 5, out.Settings.PreorderWindowDays)
}

func TestMerchantUpdateSettings_VendorNotFound(t *testing.T) {
	srv, _, _, _, _ := buildHandler(t, vendorUser())
	// UpdateSettings on a missing vendor → repo returns ErrVendorNotFound (404).
	resp := do(t, http.MethodPut, srv.URL+"/api/merchant/settings",
		`{"cutoff_hour":15,"preorder_window_days":5}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
