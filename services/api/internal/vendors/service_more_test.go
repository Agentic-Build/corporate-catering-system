package vendor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// fakeUserRepo implements identity.UserRepository; only GetByEmail is exercised
// by vendor.Service.revokeByEmail.
type fakeUserRepo struct {
	byEmail map[string]*identity.User
}

func (r *fakeUserRepo) GetByID(_ context.Context, id string) (*identity.User, error) {
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*identity.User, error) {
	if u, ok := r.byEmail[email]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) Create(_ context.Context, _ *identity.User) error        { return nil }
func (r *fakeUserRepo) UpdateProfile(_ context.Context, _ *identity.User) error { return nil }
func (r *fakeUserRepo) UpdateStatus(_ context.Context, _ string, _ identity.Status) error {
	return nil
}

// fakeSessionStore implements identity.SessionStore; only RevokeAllForUser is
// exercised by revokeByEmail.
type fakeSessionStore struct {
	revoked []string
}

func (s *fakeSessionStore) Create(_ context.Context, _ string, _ identity.Role) (*identity.Session, error) {
	return &identity.Session{}, nil
}
func (s *fakeSessionStore) Get(_ context.Context, _ string) (*identity.Session, error) {
	return nil, nil
}
func (s *fakeSessionStore) Touch(_ context.Context, _ string) error  { return nil }
func (s *fakeSessionStore) Revoke(_ context.Context, _ string) error { return nil }
func (s *fakeSessionStore) RevokeAllForUser(_ context.Context, userID string) error {
	s.revoked = append(s.revoked, userID)
	return nil
}

func TestService_Get(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")

	got, err := svc.Get(ctx, v.ID)
	require.NoError(t, err)
	assert.Equal(t, v.ID, got.ID)

	_, err = svc.Get(ctx, "missing")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}

func TestService_ListPlants_FiltersAndPropagatesError(t *testing.T) {
	svc, _, pr, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, pr.Set(ctx, v.ID, []string{"F12B-3F", "F15-2F"}))

	plants, err := svc.ListPlants(ctx, v.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"F12B-3F", "F15-2F"}, plants)

	// No mappings → empty (non-nil) slice.
	empty, err := svc.ListPlants(ctx, "other")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestService_ListPlantMappings(t *testing.T) {
	svc, _, pr, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, pr.Set(ctx, v.ID, []string{"F12B-3F"}))

	mappings, err := svc.ListPlantMappings(ctx, v.ID)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	assert.Equal(t, "F12B-3F", mappings[0].Plant)
	assert.True(t, mappings[0].Active)
}

func TestService_Approve_Errors(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()

	// Unknown vendor → not found.
	err := svc.Approve(ctx, "missing", "admin", nil)
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)

	// Already approved → ErrAlreadyApproved.
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	err = svc.Approve(ctx, v.ID, "admin", nil)
	assert.ErrorIs(t, err, vendor.ErrAlreadyApproved)
}

func TestService_Approve_InvalidStatus(t *testing.T) {
	svc, vr, _, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, vr.UpdateStatus(ctx, v.ID, vendor.StatusTerminated, nil))

	err := svc.Approve(ctx, v.ID, "admin", nil)
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
}

func TestService_Approve_FromSuspended(t *testing.T) {
	svc, vr, _, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, vr.UpdateStatus(ctx, v.ID, vendor.StatusSuspended, nil))

	require.NoError(t, svc.Approve(ctx, v.ID, "admin", []string{"F12B-3F"}))
	got, _ := vr.GetByID(ctx, v.ID)
	assert.Equal(t, vendor.StatusApproved, got.Status)
}

func TestService_Suspend_Errors(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()

	// Unknown vendor → not found.
	err := svc.Suspend(ctx, "missing", "admin")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)

	// Pending (not approved) → invalid status.
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	err = svc.Suspend(ctx, v.ID, "admin")
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
}

func TestService_Suspend_RevokesSessions(t *testing.T) {
	svc, _, _, _, ss := newSvcWithIdentity("owner@vendor.tw", "user-1")
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	_, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)

	require.NoError(t, svc.Suspend(ctx, v.ID, "admin"))
	assert.Equal(t, []string{"user-1"}, ss.revoked)
}

func TestService_Reinstate_Errors(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()

	// Unknown vendor → not found.
	err := svc.Reinstate(ctx, "missing", "admin")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)

	// Approved (not suspended) → invalid status.
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	err = svc.Reinstate(ctx, v.ID, "admin")
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
}

func TestService_UpdateSettings_NotFound(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	_, err := svc.UpdateSettings(context.Background(), "missing", 12, 5)
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}

func TestService_ListOperators_NotFound(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	_, err := svc.ListOperators(context.Background(), "missing")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}

func TestService_CreateOperator_Errors(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()

	// Unknown vendor → not found.
	_, err := svc.CreateOperator(ctx, "missing", "owner@vendor.tw", "Owner")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)

	// Vendor not approved (pending) → invalid status.
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	_, err = svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)

	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))

	// Invalid email → invalid operator.
	_, err = svc.CreateOperator(ctx, v.ID, "not-an-email", "Owner")
	assert.ErrorIs(t, err, vendor.ErrInvalidOperator)

	// Empty email → invalid operator.
	_, err = svc.CreateOperator(ctx, v.ID, "   ", "Owner")
	assert.ErrorIs(t, err, vendor.ErrInvalidOperator)

	// Blank display name → invalid operator.
	_, err = svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "   ")
	assert.ErrorIs(t, err, vendor.ErrInvalidOperator)
}

func TestService_CreateOperator_NoProvisioner(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	svc.Provisioner = nil
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))

	_, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	assert.ErrorIs(t, err, vendor.ErrProvisioningSetup)
}

func TestService_SuspendOperator_Errors(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))

	// Unknown operator → not found.
	err := svc.SuspendOperator(ctx, v.ID, "missing")
	assert.ErrorIs(t, err, vendor.ErrOperatorNotFound)

	// Operator not active → invalid status.
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	require.NoError(t, svc.SuspendOperator(ctx, v.ID, op.ID))
	err = svc.SuspendOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
}

func TestService_SuspendOperator_ProvisioningFailure(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)

	prov.err = errors.New("authentik down")
	err = svc.SuspendOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrProvisioningSetup)
}

func TestService_ReinstateOperator_Errors(t *testing.T) {
	svc, vr, _, _, _ := newSvc()
	ctx := context.Background()

	// Unknown vendor → not found.
	err := svc.ReinstateOperator(ctx, "missing", "op")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)

	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))

	// Vendor not approved → invalid status.
	require.NoError(t, vr.UpdateStatus(ctx, v.ID, vendor.StatusSuspended, nil))
	err = svc.ReinstateOperator(ctx, v.ID, "op")
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
	require.NoError(t, vr.UpdateStatus(ctx, v.ID, vendor.StatusApproved, nil))

	// Unknown operator → not found.
	err = svc.ReinstateOperator(ctx, v.ID, "missing")
	assert.ErrorIs(t, err, vendor.ErrOperatorNotFound)

	// Operator active (not suspended) → invalid status.
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	err = svc.ReinstateOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
}

func TestService_ReinstateOperator_ProvisioningFailure(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	require.NoError(t, svc.SuspendOperator(ctx, v.ID, op.ID))

	prov.err = errors.New("authentik down")
	err = svc.ReinstateOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrProvisioningSetup)
}

func TestService_SuspendRemote_MissingExternalSubject(t *testing.T) {
	svc, _, _, ops, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))

	// Seed an active operator with no external subject, bypassing provisioning.
	op := &vendor.OperatorAccount{
		VendorID:    v.ID,
		Email:       "noaks@vendor.tw",
		DisplayName: "No AK Subject",
		Provider:    "authentik",
		Status:      vendor.OperatorStatusActive,
	}
	require.NoError(t, ops.Upsert(ctx, op))

	err := svc.SuspendOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrInvalidOperator)
}

// emptyProvisioner returns blank Provider/ExternalSubject/SetupURL so the
// stringPtr empty-string branch is exercised on CreateOperator.
type emptyProvisioner struct{}

func (emptyProvisioner) UpsertVendorOperator(_ context.Context, _ identity.VendorOperatorProvisionInput) (*identity.VendorOperatorProvisioned, error) {
	return &identity.VendorOperatorProvisioned{}, nil
}
func (emptyProvisioner) SuspendVendorOperator(_ context.Context, _, _ string) error { return nil }
func (emptyProvisioner) ReinstateVendorOperator(_ context.Context, _, _, _ string) error {
	return nil
}

func TestService_CreateOperator_EmptyProvisionerFields(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	svc.Provisioner = emptyProvisioner{}
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))

	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	assert.Nil(t, op.ExternalSubject)
	assert.Nil(t, op.SetupURL)
}

func TestService_Approve_NilAudit(t *testing.T) {
	svc, vr, _, _, _ := newSvc()
	svc.Audit = nil
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")

	require.NoError(t, svc.Approve(ctx, v.ID, "admin", []string{"F12B-3F"}))
	got, _ := vr.GetByID(ctx, v.ID)
	assert.Equal(t, vendor.StatusApproved, got.Status)
}

func TestService_SuspendOperator_NoProvisioner(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	svc.Provisioner = nil

	err = svc.SuspendOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrProvisioningSetup)
}

func TestService_ReinstateOperator_NoProvisioner(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	require.NoError(t, svc.SuspendOperator(ctx, v.ID, op.ID))
	svc.Provisioner = nil

	err = svc.ReinstateOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrProvisioningSetup)
}

func TestService_ReinstateRemote_MissingExternalSubject(t *testing.T) {
	svc, _, _, ops, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))

	// Seed a suspended operator with no external subject.
	op := &vendor.OperatorAccount{
		VendorID:    v.ID,
		Email:       "noaks@vendor.tw",
		DisplayName: "No AK Subject",
		Provider:    "authentik",
		Status:      vendor.OperatorStatusSuspended,
	}
	require.NoError(t, ops.Upsert(ctx, op))

	err := svc.ReinstateOperator(ctx, v.ID, op.ID)
	assert.ErrorIs(t, err, vendor.ErrInvalidOperator)
}

func TestService_SuspendOperator_UserNotFoundSkipsRevoke(t *testing.T) {
	// Users/Sessions wired but no matching user → revokeByEmail returns nil.
	svc, _, _, _, ss := newSvcWithIdentity("someone-else@vendor.tw", "user-1")
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)

	require.NoError(t, svc.SuspendOperator(ctx, v.ID, op.ID))
	assert.Empty(t, ss.revoked)
}

var errBoom = errors.New("boom")

func TestService_Suspend_RemoteOperatorError(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	_, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)

	prov.err = errors.New("authentik down")
	assert.ErrorIs(t, svc.Suspend(ctx, v.ID, "admin"), vendor.ErrProvisioningSetup)
}

func TestService_Suspend_RevokeError(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	_, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)

	// Wire identity so revokeByEmail runs; force GetByEmail to error.
	svc.Users = &errUserRepo{}
	svc.Sessions = &fakeSessionStore{}
	assert.ErrorIs(t, svc.Suspend(ctx, v.ID, "admin"), errBoom)
}

func TestService_Reinstate_RemoteOperatorError(t *testing.T) {
	svc, _, _, _, prov := newSvc()
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	_, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	require.NoError(t, svc.Suspend(ctx, v.ID, "admin"))

	prov.err = errors.New("authentik down")
	assert.ErrorIs(t, svc.Reinstate(ctx, v.ID, "admin"), vendor.ErrProvisioningSetup)
}

// errVendorRepo wraps fakeVendorRepo and forces selected methods to fail so the
// service's error-propagation branches are exercised.
type errVendorRepo struct {
	*fakeVendorRepo
	failCreate       bool
	failUpdateStatus bool
}

func (r *errVendorRepo) Create(ctx context.Context, v *vendor.Vendor) error {
	if r.failCreate {
		return errBoom
	}
	return r.fakeVendorRepo.Create(ctx, v)
}

func (r *errVendorRepo) UpdateStatus(ctx context.Context, id string, s vendor.Status, by *string) error {
	if r.failUpdateStatus {
		return errBoom
	}
	return r.fakeVendorRepo.UpdateStatus(ctx, id, s, by)
}

// errPlantRepo forces Set/ListByVendor to fail.
type errPlantRepo struct {
	*fakePlantRepo
	failSet          bool
	failListByVendor bool
}

func (r *errPlantRepo) Set(ctx context.Context, id string, plants []string) error {
	if r.failSet {
		return errBoom
	}
	return r.fakePlantRepo.Set(ctx, id, plants)
}

func (r *errPlantRepo) ListByVendor(ctx context.Context, id string) ([]*vendor.PlantMapping, error) {
	if r.failListByVendor {
		return nil, errBoom
	}
	return r.fakePlantRepo.ListByVendor(ctx, id)
}

// errOperatorRepo forces selected methods to fail.
type errOperatorRepo struct {
	*fakeOperatorRepo
	failUpsert      bool
	failSetStatus   bool
	failSetStatuses bool
	failList        bool
}

func (r *errOperatorRepo) Upsert(ctx context.Context, op *vendor.OperatorAccount) error {
	if r.failUpsert {
		return errBoom
	}
	return r.fakeOperatorRepo.Upsert(ctx, op)
}

func (r *errOperatorRepo) SetStatus(ctx context.Context, vendorID, operatorID string, s vendor.OperatorStatus) error {
	if r.failSetStatus {
		return errBoom
	}
	return r.fakeOperatorRepo.SetStatus(ctx, vendorID, operatorID, s)
}

func (r *errOperatorRepo) SetStatuses(ctx context.Context, vendorID string, from []vendor.OperatorStatus, to vendor.OperatorStatus) error {
	if r.failSetStatuses {
		return errBoom
	}
	return r.fakeOperatorRepo.SetStatuses(ctx, vendorID, from, to)
}

func (r *errOperatorRepo) ListByVendorStatus(ctx context.Context, vendorID string, statuses []vendor.OperatorStatus) ([]*vendor.OperatorAccount, error) {
	if r.failList {
		return nil, errBoom
	}
	return r.fakeOperatorRepo.ListByVendorStatus(ctx, vendorID, statuses)
}

func TestService_CreatePending_RepoError(t *testing.T) {
	svc, vr, _, _, _ := newSvc()
	svc.Vendors = &errVendorRepo{fakeVendorRepo: vr, failCreate: true}
	_, err := svc.CreatePending(context.Background(), "A", "A Ltd", "a@x.com")
	assert.ErrorIs(t, err, errBoom)
}

func TestService_Approve_RepoErrors(t *testing.T) {
	ctx := context.Background()

	// UpdateStatus failure.
	svc, vr, _, _, _ := newSvc()
	ev := &errVendorRepo{fakeVendorRepo: vr}
	svc.Vendors = ev
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	ev.failUpdateStatus = true
	assert.ErrorIs(t, svc.Approve(ctx, v.ID, "admin", nil), errBoom)

	// Plants.Set failure.
	svc2, _, pr, _, _ := newSvc()
	ep := &errPlantRepo{fakePlantRepo: pr, failSet: true}
	svc2.Plants = ep
	v2, _ := svc2.CreatePending(ctx, "B", "B Ltd", "b@x.com")
	assert.ErrorIs(t, svc2.Approve(ctx, v2.ID, "admin", []string{"F12B-3F"}), errBoom)
}

func TestService_Suspend_RepoErrors(t *testing.T) {
	ctx := context.Background()

	// ListByVendorStatus failure.
	svc, _, _, or, _ := newSvc()
	eo := &errOperatorRepo{fakeOperatorRepo: or, failList: true}
	svc.Operators = eo
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	assert.ErrorIs(t, svc.Suspend(ctx, v.ID, "admin"), errBoom)

	// UpdateStatus failure.
	svc2, vr2, _, _, _ := newSvc()
	ev2 := &errVendorRepo{fakeVendorRepo: vr2}
	svc2.Vendors = ev2
	v2, _ := svc2.CreatePending(ctx, "B", "B Ltd", "b@x.com")
	require.NoError(t, svc2.Approve(ctx, v2.ID, "admin", nil))
	ev2.failUpdateStatus = true
	assert.ErrorIs(t, svc2.Suspend(ctx, v2.ID, "admin"), errBoom)

	// SetStatuses failure.
	svc3, _, _, or3, _ := newSvc()
	eo3 := &errOperatorRepo{fakeOperatorRepo: or3}
	svc3.Operators = eo3
	v3, _ := svc3.CreatePending(ctx, "C", "C Ltd", "c@x.com")
	require.NoError(t, svc3.Approve(ctx, v3.ID, "admin", nil))
	eo3.failSetStatuses = true
	assert.ErrorIs(t, svc3.Suspend(ctx, v3.ID, "admin"), errBoom)
}

func TestService_Reinstate_RepoErrors(t *testing.T) {
	ctx := context.Background()

	suspended := func(svc *vendor.Service, vr *fakeVendorRepo) *vendor.Vendor {
		v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
		require.NoError(t, vr.UpdateStatus(ctx, v.ID, vendor.StatusSuspended, nil))
		return v
	}

	// ListByVendorStatus failure.
	svc, vr, _, or, _ := newSvc()
	eo := &errOperatorRepo{fakeOperatorRepo: or, failList: true}
	svc.Operators = eo
	v := suspended(svc, vr)
	assert.ErrorIs(t, svc.Reinstate(ctx, v.ID, "admin"), errBoom)

	// UpdateStatus failure.
	svc2, vr2, _, _, _ := newSvc()
	ev2 := &errVendorRepo{fakeVendorRepo: vr2}
	svc2.Vendors = ev2
	v2, _ := svc2.CreatePending(ctx, "B", "B Ltd", "b@x.com")
	require.NoError(t, vr2.UpdateStatus(ctx, v2.ID, vendor.StatusSuspended, nil))
	ev2.failUpdateStatus = true
	assert.ErrorIs(t, svc2.Reinstate(ctx, v2.ID, "admin"), errBoom)

	// SetStatuses failure.
	svc3, vr3, _, or3, _ := newSvc()
	eo3 := &errOperatorRepo{fakeOperatorRepo: or3}
	svc3.Operators = eo3
	v3 := suspended(svc3, vr3)
	eo3.failSetStatuses = true
	assert.ErrorIs(t, svc3.Reinstate(ctx, v3.ID, "admin"), errBoom)
}

func TestService_ListPlants_RepoError(t *testing.T) {
	svc, _, pr, _, _ := newSvc()
	svc.Plants = &errPlantRepo{fakePlantRepo: pr, failListByVendor: true}
	_, err := svc.ListPlants(context.Background(), "any")
	assert.ErrorIs(t, err, errBoom)
}

func TestService_CreateOperator_UpsertError(t *testing.T) {
	svc, _, _, or, _ := newSvc()
	svc.Operators = &errOperatorRepo{fakeOperatorRepo: or, failUpsert: true}
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	_, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	assert.ErrorIs(t, err, errBoom)
}

func TestService_SuspendOperator_SetStatusError(t *testing.T) {
	svc, _, _, or, _ := newSvc()
	eo := &errOperatorRepo{fakeOperatorRepo: or}
	svc.Operators = eo
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	eo.failSetStatus = true
	assert.ErrorIs(t, svc.SuspendOperator(ctx, v.ID, op.ID), errBoom)
}

func TestService_RevokeByEmail_RepoError(t *testing.T) {
	svc, _, _, _, _ := newSvc()
	svc.Users = &errUserRepo{}
	svc.Sessions = &fakeSessionStore{}
	ctx := context.Background()
	v, _ := svc.CreatePending(ctx, "A", "A Ltd", "a@x.com")
	require.NoError(t, svc.Approve(ctx, v.ID, "admin", nil))
	op, err := svc.CreateOperator(ctx, v.ID, "owner@vendor.tw", "Owner")
	require.NoError(t, err)
	assert.ErrorIs(t, svc.SuspendOperator(ctx, v.ID, op.ID), errBoom)
}

// errUserRepo returns a non-NotFound error from GetByEmail.
type errUserRepo struct{ *fakeUserRepo }

func (errUserRepo) GetByEmail(_ context.Context, _ string) (*identity.User, error) {
	return nil, errBoom
}

// newSvcWithIdentity builds a service whose revokeByEmail path is wired with a
// user (matching opEmail → userID) plus a session store, so suspend/reinstate
// flows can exercise session revocation.
func newSvcWithIdentity(opEmail, userID string) (*vendor.Service, *fakeVendorRepo, *fakePlantRepo, *fakeOperatorRepo, *fakeSessionStore) {
	svc, vr, pr, or, _ := newSvc()
	ss := &fakeSessionStore{}
	svc.Users = &fakeUserRepo{byEmail: map[string]*identity.User{
		opEmail: {ID: userID, PrimaryEmail: opEmail},
	}}
	svc.Sessions = ss
	return svc, vr, pr, or, ss
}
