package vendor_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// ----- Mocks -----

type fakeVendorRepo struct {
	mu      sync.Mutex
	byID    map[string]*vendor.Vendor
	byEmail map[string]*vendor.Vendor
	nextID  int
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
	if v, ok := r.byID[id]; ok {
		return v, nil
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

func (r *fakeVendorRepo) List(_ context.Context, statuses []vendor.Status) ([]*vendor.Vendor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	byVendor map[string][]string
}

func newFakePlantRepo() *fakePlantRepo { return &fakePlantRepo{byVendor: map[string][]string{}} }

func (r *fakePlantRepo) ListByVendor(_ context.Context, id string) ([]*vendor.PlantMapping, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*vendor.PlantMapping
	for _, p := range r.byVendor[id] {
		out = append(out, &vendor.PlantMapping{VendorID: id, Plant: p, Active: true})
	}
	return out, nil
}

func (r *fakePlantRepo) ListVendorsForPlant(_ context.Context, plant string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for vid, plants := range r.byVendor {
		for _, p := range plants {
			if p == plant {
				out = append(out, vid)
				break
			}
		}
	}
	return out, nil
}

func (r *fakePlantRepo) Set(_ context.Context, id string, plants []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byVendor[id] = plants
	return nil
}

type fakeInvites struct {
	mu     sync.Mutex
	byCode map[string]*identity.VendorInvite
}

func newFakeInvites() *fakeInvites {
	return &fakeInvites{byCode: map[string]*identity.VendorInvite{}}
}

func (f *fakeInvites) Get(_ context.Context, code string) (*identity.VendorInvite, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i, ok := f.byCode[code]; ok {
		return i, nil
	}
	return nil, identity.ErrInviteNotFound
}

func (f *fakeInvites) Put(_ context.Context, inv *identity.VendorInvite) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byCode[inv.Code]; !exists {
		f.byCode[inv.Code] = inv
	}
	return nil
}

func (f *fakeInvites) Consume(_ context.Context, code, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	inv, ok := f.byCode[code]
	if !ok {
		return identity.ErrInviteNotFound
	}
	if inv.ConsumedAt != nil {
		return identity.ErrInviteAlreadyUsed
	}
	now := time.Now().UTC()
	inv.ConsumedAt = &now
	inv.ConsumedBy = &userID
	return nil
}

type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

// ----- Tests -----

func newSvc() (*vendor.Service, *fakeVendorRepo, *fakePlantRepo, *fakeInvites) {
	vr := newFakeVendorRepo()
	pr := newFakePlantRepo()
	iv := newFakeInvites()
	svc := &vendor.Service{
		Vendors:   vr,
		Plants:    pr,
		Invites:   iv,
		Clock:     fixedClock{T: time.Now().UTC()},
		InviteTTL: 7 * 24 * time.Hour,
	}
	return svc, vr, pr, iv
}

func TestService_CreatePending(t *testing.T) {
	svc, _, _, _ := newSvc()
	v, err := svc.CreatePending(context.Background(), "稻禾家便當", "稻禾家便當有限公司", "ops@daohe.tw")
	require.NoError(t, err)
	assert.Equal(t, vendor.StatusPending, v.Status)
	assert.NotEmpty(t, v.ID)
}

func TestService_ApproveHappy(t *testing.T) {
	svc, vr, pr, _ := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin-1", []string{"F12B-3F", "F15-2F"}))
	got, _ := vr.GetByID(context.Background(), v.ID)
	assert.Equal(t, vendor.StatusApproved, got.Status)
	require.NotNil(t, got.ApprovedBy)
	assert.Equal(t, "admin-1", *got.ApprovedBy)
	assert.Equal(t, []string{"F12B-3F", "F15-2F"}, pr.byVendor[v.ID])
}

func TestService_ApproveAlreadyApproved(t *testing.T) {
	svc, _, _, _ := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	_ = svc.Approve(context.Background(), v.ID, "admin", nil)
	err := svc.Approve(context.Background(), v.ID, "admin", nil)
	assert.ErrorIs(t, err, vendor.ErrAlreadyApproved)
}

func TestService_SuspendAndReinstate(t *testing.T) {
	svc, _, _, _ := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	_ = svc.Approve(context.Background(), v.ID, "admin", nil)
	require.NoError(t, svc.Suspend(context.Background(), v.ID))
	err := svc.Suspend(context.Background(), v.ID) // already suspended
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
	require.NoError(t, svc.Reinstate(context.Background(), v.ID, "admin"))
}

func TestService_IssueInvite(t *testing.T) {
	svc, _, _, inv := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	code, err := svc.IssueInvite(context.Background(), v.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, code)
	assert.Contains(t, code, "TBI-")
	stored, ok := inv.byCode[code]
	require.True(t, ok)
	assert.Equal(t, v.ID, stored.VendorID)
}

func TestService_IssueInvite_VendorNotFound(t *testing.T) {
	svc, _, _, _ := newSvc()
	_, err := svc.IssueInvite(context.Background(), "nope")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}

func TestService_List(t *testing.T) {
	svc, _, _, _ := newSvc()
	a, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	_, _ = svc.CreatePending(context.Background(), "B", "B Ltd", "b@x.com")
	_ = svc.Approve(context.Background(), a.ID, "admin", nil)

	approved, _ := svc.List(context.Background(), []vendor.Status{vendor.StatusApproved})
	assert.Len(t, approved, 1)
	pending, _ := svc.List(context.Background(), []vendor.Status{vendor.StatusPending})
	assert.Len(t, pending, 1)
	all, _ := svc.List(context.Background(), nil)
	assert.Len(t, all, 2)
}
