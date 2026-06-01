package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors/postgres"
)

func TestVendorRepo_UpdateContactEmail(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := postgres.NewVendorRepo(pool)
	ctx := context.Background()

	v := &vendor.Vendor{DisplayName: "E", LegalName: "E Ltd", ContactEmail: "old@x.com", Status: vendor.StatusApproved}
	require.NoError(t, repo.Create(ctx, v))

	require.NoError(t, repo.UpdateContactEmail(ctx, v.ID, "new@x.com"))
	got, err := repo.GetByID(ctx, v.ID)
	require.NoError(t, err)
	assert.Equal(t, "new@x.com", got.ContactEmail)

	// Unknown (well-formed) vendor id → RowsAffected 0 → ErrVendorNotFound.
	err = repo.UpdateContactEmail(ctx, "00000000-0000-0000-0000-000000000000", "x@x.com")
	assert.ErrorIs(t, err, vendor.ErrVendorNotFound)
}
