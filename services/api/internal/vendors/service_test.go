package vendor_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

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

type fakeOperatorRepo struct {
	mu     sync.Mutex
	byID   map[string]*vendor.OperatorAccount
	nextID int
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
	return r.ListByVendorStatus(context.Background(), vendorID, nil)
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

type fakeProvisioner struct {
	err           error
	upserts       int
	suspended     []string
	reinstated    []string
	subjectByMail map[string]string
}

func newFakeProvisioner() *fakeProvisioner {
	return &fakeProvisioner{subjectByMail: map[string]string{}}
}

func (p *fakeProvisioner) UpsertVendorOperator(_ context.Context, in identity.VendorOperatorProvisionInput) (*identity.VendorOperatorProvisioned, error) {
	if p.err != nil {
		return nil, p.err
	}
	p.upserts++
	sub := p.subjectByMail[in.Email]
	if sub == "" {
		sub = "ak-" + strconv.Itoa(p.upserts)
		p.subjectByMail[in.Email] = sub
	}
	return &identity.VendorOperatorProvisioned{Provider: "authentik", ExternalSubject: sub, SetupURL: "http://auth/setup/" + sub}, nil
}

func (p *fakeProvisioner) SuspendVendorOperator(_ context.Context, _ string, externalSubject string) error {
	if p.err != nil {
		return p.err
	}
	p.suspended = append(p.suspended, externalSubject)
	return nil
}

func (p *fakeProvisioner) ReinstateVendorOperator(_ context.Context, _ string, externalSubject, _ string) error {
	if p.err != nil {
		return p.err
	}
	p.reinstated = append(p.reinstated, externalSubject)
	return nil
}

func newSvc() (*vendor.Service, *fakeVendorRepo, *fakePlantRepo, *fakeOperatorRepo, *fakeProvisioner) {
	vr := newFakeVendorRepo()
	pr := newFakePlantRepo()
	or := newFakeOperatorRepo()
	prov := newFakeProvisioner()
	svc := &vendor.Service{
		Vendors:     vr,
		Plants:      pr,
		Operators:   or,
		Provisioner: prov,
	}
	return svc, vr, pr, or, prov
}

func TestService_CreatePending(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	v, err := svc.CreatePending(context.Background(), "稻禾家便當", "稻禾家便當有限公司", "ops@daohe.tw")
	require.NoError(t, err)
	assert.Equal(t, vendor.StatusPending, v.Status)
	assert.NotEmpty(t, v.ID)
}

func TestService_ApproveHappy(t *testing.T) {
	svc, vr, pr, _, _ := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin-1", []string{"F12B-3F", "F15-2F"}))
	got, _ := vr.GetByID(context.Background(), v.ID)
	assert.Equal(t, vendor.StatusApproved, got.Status)
	require.NotNil(t, got.ApprovedBy)
	assert.Equal(t, "admin-1", *got.ApprovedBy)
	assert.Equal(t, []string{"F12B-3F", "F15-2F"}, pr.byVendor[v.ID])
}

func TestService_CreateOperator_ProvisionsAuthentikThenMirrors(t *testing.T) {
	svc, _, _, ops, prov := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin", nil))

	op, err := svc.CreateOperator(context.Background(), v.ID, "Owner@Vendor.tw", "Owner")
	require.NoError(t, err)
	assert.Equal(t, "owner@vendor.tw", op.Email)
	assert.Equal(t, "authentik", op.Provider)
	require.NotNil(t, op.ExternalSubject)
	assert.Equal(t, "ak-1", *op.ExternalSubject)
	assert.Equal(t, vendor.OperatorStatusActive, op.Status)
	assert.Equal(t, 1, prov.upserts)
	assert.Len(t, ops.byID, 1)
}

func TestService_CreateOperator_ProvisioningFailureLeavesNoLocalOperator(t *testing.T) {
	svc, _, _, ops, prov := newSvc()
	prov.err = errors.New("authentik down")
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin", nil))

	_, err := svc.CreateOperator(context.Background(), v.ID, "owner@vendor.tw", "Owner")
	assert.ErrorIs(t, err, vendor.ErrProvisioningSetup)
	assert.Empty(t, ops.byID)
}

func TestService_SuspendVendor_DisablesActiveOperators(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin", nil))
	op, err := svc.CreateOperator(context.Background(), v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)

	require.NoError(t, svc.Suspend(context.Background(), v.ID))
	assert.Equal(t, []string{*op.ExternalSubject}, prov.suspended)
	got, err := svc.ListOperators(context.Background(), v.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, vendor.OperatorStatusVendorSuspended, got[0].Status)
}

func TestService_ReinstateVendor_RestoresVendorSuspendedOperators(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin", nil))
	op, err := svc.CreateOperator(context.Background(), v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	require.NoError(t, svc.Suspend(context.Background(), v.ID))

	require.NoError(t, svc.Reinstate(context.Background(), v.ID, "admin"))
	assert.Equal(t, []string{*op.ExternalSubject}, prov.reinstated)
	got, err := svc.ListOperators(context.Background(), v.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, vendor.OperatorStatusActive, got[0].Status)
}

func TestService_SuspendOperator(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin", nil))
	op, err := svc.CreateOperator(context.Background(), v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)

	require.NoError(t, svc.SuspendOperator(context.Background(), v.ID, op.ID))
	assert.Equal(t, []string{*op.ExternalSubject}, prov.suspended)
	got, err := svc.ListOperators(context.Background(), v.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, vendor.OperatorStatusSuspended, got[0].Status)
}

func TestService_ReinstateOperator(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	v, _ := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(context.Background(), v.ID, "admin", nil))
	op, err := svc.CreateOperator(context.Background(), v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	require.NoError(t, svc.SuspendOperator(context.Background(), v.ID, op.ID))

	require.NoError(t, svc.ReinstateOperator(context.Background(), v.ID, op.ID))
	assert.Equal(t, []string{*op.ExternalSubject}, prov.reinstated)
	got, err := svc.ListOperators(context.Background(), v.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, vendor.OperatorStatusActive, got[0].Status)
}

func TestService_List(t *testing.T) {
	svc, _, _, _, _ := newSvc()
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

func hasOperatorStatus(items []vendor.OperatorStatus, status vendor.OperatorStatus) bool {
	for _, item := range items {
		if item == status {
			return true
		}
	}
	return false
}
