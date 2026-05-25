package phttp

import (
	"context"
	"errors"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/plants"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

type fakePlantRepo struct {
	byCode map[string]*plants.Plant
}

func (f *fakePlantRepo) List(_ context.Context, activeOnly bool) ([]*plants.Plant, error) {
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

func (f *fakePlantRepo) Create(context.Context, *plants.Plant) error { return nil }
func (f *fakePlantRepo) Update(context.Context, *plants.Plant) error { return nil }

type fakeMappingRepo struct {
	byVendor map[string][]string
}

func (f *fakeMappingRepo) ListByVendor(_ context.Context, vendorID string) ([]*vendor.PlantMapping, error) {
	var out []*vendor.PlantMapping
	for _, code := range f.byVendor[vendorID] {
		out = append(out, &vendor.PlantMapping{VendorID: vendorID, Plant: code, Active: true})
	}
	return out, nil
}

func (f *fakeMappingRepo) ListVendorsForPlant(context.Context, string) ([]string, error) {
	return nil, nil
}

func (f *fakeMappingRepo) Set(_ context.Context, vendorID string, codes []string) error {
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
