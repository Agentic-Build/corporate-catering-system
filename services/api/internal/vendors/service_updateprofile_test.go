package vendor_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	audit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

var errInjected = errors.New("injected repo failure")

// stubVendorRepo implements vendor.Repository with a single existing vendor and
// an optional UpdateContactEmail failure, to drive UpdateProfile error paths.
type stubVendorRepo struct {
	v               *vendor.Vendor
	failUpdateEmail bool
}

func (r *stubVendorRepo) GetByID(_ context.Context, id string) (*vendor.Vendor, error) {
	if r.v != nil && r.v.ID == id {
		return r.v, nil
	}
	return nil, vendor.ErrVendorNotFound
}
func (r *stubVendorRepo) GetByEmail(context.Context, string) (*vendor.Vendor, error) {
	return nil, vendor.ErrVendorNotFound
}
func (r *stubVendorRepo) Create(context.Context, *vendor.Vendor) error { return nil }
func (r *stubVendorRepo) UpdateStatus(context.Context, string, vendor.Status, *string) error {
	return nil
}
func (r *stubVendorRepo) UpdateSettings(context.Context, string, int, int) error { return nil }
func (r *stubVendorRepo) UpdateContactEmail(context.Context, string, string) error {
	if r.failUpdateEmail {
		return errInjected
	}
	return nil
}
func (r *stubVendorRepo) List(context.Context, []vendor.Status) ([]*vendor.Vendor, error) {
	return nil, nil
}

// stubPlantRepo implements vendor.PlantMappingRepository with an optional Set failure.
type stubPlantRepo struct {
	failSet bool
}

func (r *stubPlantRepo) ListByVendor(context.Context, string) ([]*vendor.PlantMapping, error) {
	return nil, nil
}
func (r *stubPlantRepo) ListVendorsForPlant(context.Context, string) ([]string, error) {
	return nil, nil
}
func (r *stubPlantRepo) Set(context.Context, string, []string) error {
	if r.failSet {
		return errInjected
	}
	return nil
}
func (r *stubPlantRepo) SetWindow(context.Context, string, string, string) error { return nil }

// failingAuditWriter always fails its Write to drive the audit error path.
type failingAuditWriter struct{}

func (failingAuditWriter) Write(context.Context, audit.Entry) error { return errInjected }

const upVendorID = "vendor-1"

func newStubSvc(vr vendor.Repository, pr vendor.PlantMappingRepository, aw vendor.AuditWriter) *vendor.Service {
	return &vendor.Service{Vendors: vr, Plants: pr, Audit: aw}
}

func TestService_UpdateProfile_UpdateContactEmailError(t *testing.T) {
	vr := &stubVendorRepo{v: &vendor.Vendor{ID: upVendorID}, failUpdateEmail: true}
	svc := newStubSvc(vr, &stubPlantRepo{}, nil)
	_, err := svc.UpdateProfile(context.Background(), upVendorID, "admin-1", strPtr("new@test.com"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errInjected)
}

func TestService_UpdateProfile_PlantsSetError(t *testing.T) {
	vr := &stubVendorRepo{v: &vendor.Vendor{ID: upVendorID}}
	svc := newStubSvc(vr, &stubPlantRepo{failSet: true}, nil)
	_, err := svc.UpdateProfile(context.Background(), upVendorID, "admin-1", nil, &[]string{"F18-1F"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errInjected)
}

func TestService_UpdateProfile_AuditError(t *testing.T) {
	vr := &stubVendorRepo{v: &vendor.Vendor{ID: upVendorID}}
	svc := newStubSvc(vr, &stubPlantRepo{}, failingAuditWriter{})
	_, err := svc.UpdateProfile(context.Background(), upVendorID, "admin-1", strPtr("new@test.com"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errInjected)
}
