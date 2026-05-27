package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
	"github.com/takalawang/corporate-catering-system/services/api/internal/vendors/postgres"
)

func TestPlantMappingRepo_SetAndList(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vrepo := postgres.NewVendorRepo(pool)
	prepo := postgres.NewPlantMappingRepo(pool)
	ctx := context.Background()

	// Pre-populate plant registry (required by FK constraint added in 000018).
	for _, code := range []string{"F12B-3F", "F15-2F", "F18-RF"} {
		_, err := pool.Exec(ctx, `INSERT INTO plant (code, label) VALUES ($1, $1) ON CONFLICT DO NOTHING`, code)
		require.NoError(t, err)
	}

	v := &vendor.Vendor{DisplayName: "V", LegalName: "V Ltd", ContactEmail: "v@x.com", Status: vendor.StatusApproved}
	require.NoError(t, vrepo.Create(ctx, v))

	require.NoError(t, prepo.Set(ctx, v.ID, []string{"F12B-3F", "F15-2F"}))

	list, err := prepo.ListByVendor(ctx, v.ID)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	vs, err := prepo.ListVendorsForPlant(ctx, "F12B-3F")
	require.NoError(t, err)
	assert.Contains(t, vs, v.ID)

	// Re-set replaces existing mappings
	require.NoError(t, prepo.Set(ctx, v.ID, []string{"F18-RF"}))
	list, err = prepo.ListByVendor(ctx, v.ID)
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "F18-RF", list[0].Plant)

	// F12B-3F no longer has this vendor
	vs, err = prepo.ListVendorsForPlant(ctx, "F12B-3F")
	require.NoError(t, err)
	assert.NotContains(t, vs, v.ID)
}

func TestPlantMappingRepo_SetEmpty(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vrepo := postgres.NewVendorRepo(pool)
	prepo := postgres.NewPlantMappingRepo(pool)
	ctx := context.Background()

	// Pre-populate plant registry (required by FK constraint added in 000018).
	_, err := pool.Exec(ctx, `INSERT INTO plant (code, label) VALUES ('F12B-3F', 'F12B-3F') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	v := &vendor.Vendor{DisplayName: "V", LegalName: "V Ltd", ContactEmail: "empty@x.com", Status: vendor.StatusApproved}
	require.NoError(t, vrepo.Create(ctx, v))
	require.NoError(t, prepo.Set(ctx, v.ID, []string{"F12B-3F"}))

	// Reset to empty wipes all mappings
	require.NoError(t, prepo.Set(ctx, v.ID, []string{}))
	list, err2 := prepo.ListByVendor(ctx, v.ID)
	require.NoError(t, err2)
	assert.Len(t, list, 0)
}

func TestPlantMappingRepo_SetWindow(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vrepo := postgres.NewVendorRepo(pool)
	prepo := postgres.NewPlantMappingRepo(pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO plant (code, label) VALUES ('F12B-3F', 'F12B-3F') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)

	v := &vendor.Vendor{DisplayName: "V", LegalName: "V Ltd", ContactEmail: "window@x.com", Status: vendor.StatusApproved}
	require.NoError(t, vrepo.Create(ctx, v))
	require.NoError(t, prepo.Set(ctx, v.ID, []string{"F12B-3F"}))

	require.NoError(t, prepo.SetWindow(ctx, v.ID, "F12B-3F", "11:30-12:30"))
	list, err := prepo.ListByVendor(ctx, v.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "11:30-12:30", list[0].ServiceWindow)

	// No active mapping for that pair → ErrVendorNotFound.
	err = prepo.SetWindow(ctx, v.ID, "F15-2F", "10:00-11:00")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}

func TestPlantMappingRepo_SetUnknownPlant(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	vrepo := postgres.NewVendorRepo(pool)
	prepo := postgres.NewPlantMappingRepo(pool)
	ctx := context.Background()

	v := &vendor.Vendor{DisplayName: "V", LegalName: "V Ltd", ContactEmail: "unknown@x.com", Status: vendor.StatusApproved}
	require.NoError(t, vrepo.Create(ctx, v))

	// Plant code absent from the registry violates the FK and surfaces an error.
	err := prepo.Set(ctx, v.ID, []string{"NOPE-9F"})
	require.Error(t, err)
}
