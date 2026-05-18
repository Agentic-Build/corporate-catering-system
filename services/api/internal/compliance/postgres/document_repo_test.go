package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
)

func TestDocumentRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDocumentRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	uploader := seedAdminUser(t, pool)
	expires := time.Now().UTC().AddDate(0, 6, 0).Truncate(24 * time.Hour)

	d := &compliance.Document{
		VendorID:   vendorID,
		Kind:       compliance.DocKindBusinessLicense,
		BlobURI:    "s3://docs/license.pdf",
		Filename:   "license.pdf",
		UploadedBy: &uploader,
		ExpiresAt:  &expires,
		Status:     compliance.DocStatusPending,
		Notes:      "initial upload",
	}
	require.NoError(t, repo.Create(ctx, d))
	require.NotEmpty(t, d.ID)

	got, err := repo.GetByID(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, vendorID, got.VendorID)
	assert.Equal(t, compliance.DocKindBusinessLicense, got.Kind)
	assert.Equal(t, compliance.DocStatusPending, got.Status)
	assert.Equal(t, "s3://docs/license.pdf", got.BlobURI)
	assert.Equal(t, "license.pdf", got.Filename)
	require.NotNil(t, got.UploadedBy)
	assert.Equal(t, uploader, *got.UploadedBy)
	require.NotNil(t, got.ExpiresAt)
	assert.True(t, got.ExpiresAt.Equal(expires))
	assert.Equal(t, "initial upload", got.Notes)
	assert.Nil(t, got.ReviewedBy)
	assert.Nil(t, got.ReviewedAt)
}

func TestDocumentRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewDocumentRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, compliance.ErrDocumentNotFound)
}

func TestDocumentRepo_ListByVendor_FilterIncludeAll(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDocumentRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	otherVendor := seedApprovedVendor(t, pool)

	pending := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindBusinessLicense,
		BlobURI: "s3://a", Filename: "a.pdf",
	}
	approved := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindFoodSafetyPermit,
		BlobURI: "s3://b", Filename: "b.pdf",
	}
	expired := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindInsurance,
		BlobURI: "s3://c", Filename: "c.pdf",
	}
	// Different vendor doc should never appear.
	other := &compliance.Document{
		VendorID: otherVendor, Kind: compliance.DocKindOther,
		BlobURI: "s3://d", Filename: "d.pdf",
	}
	for _, doc := range []*compliance.Document{pending, approved, expired, other} {
		require.NoError(t, repo.Create(ctx, doc))
	}
	// Approve one, expire one.
	require.NoError(t, repo.UpdateStatus(ctx, approved.ID, compliance.DocStatusApproved, nil, ""))
	require.NoError(t, repo.UpdateStatus(ctx, expired.ID, compliance.DocStatusExpired, nil, ""))

	// Default (non-all) excludes expired.
	active, err := repo.ListByVendor(ctx, vendorID, false)
	require.NoError(t, err)
	require.Len(t, active, 2)
	ids := map[string]bool{active[0].ID: true, active[1].ID: true}
	assert.True(t, ids[pending.ID])
	assert.True(t, ids[approved.ID])
	assert.False(t, ids[expired.ID])

	// includeAll=true returns expired too.
	all, err := repo.ListByVendor(ctx, vendorID, true)
	require.NoError(t, err)
	require.Len(t, all, 3)
}

func TestDocumentRepo_UpdateStatus_PendingToApproved(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDocumentRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	reviewer := seedAdminUser(t, pool)

	d := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindTaxRegistration,
		BlobURI: "s3://x", Filename: "x.pdf",
	}
	require.NoError(t, repo.Create(ctx, d))
	require.Equal(t, compliance.DocStatusPending, d.Status)

	require.NoError(t, repo.UpdateStatus(ctx, d.ID, compliance.DocStatusApproved, &reviewer, "looks good"))

	got, err := repo.GetByID(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, compliance.DocStatusApproved, got.Status)
	require.NotNil(t, got.ReviewedBy)
	assert.Equal(t, reviewer, *got.ReviewedBy)
	require.NotNil(t, got.ReviewedAt)
	assert.Equal(t, "looks good", got.Notes)

	// Unknown id returns ErrDocumentNotFound.
	err = repo.UpdateStatus(ctx, "00000000-0000-0000-0000-000000000000", compliance.DocStatusApproved, &reviewer, "")
	assert.ErrorIs(t, err, compliance.ErrDocumentNotFound)
}

func TestDocumentRepo_ListExpiringBefore(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDocumentRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Three approved docs at varying expiry; one pending; one no-expiry.
	exp1 := today.AddDate(0, 0, 3)
	exp2 := today.AddDate(0, 0, 7)
	exp3 := today.AddDate(0, 0, 30)

	d1 := &compliance.Document{VendorID: vendorID, Kind: compliance.DocKindBusinessLicense,
		BlobURI: "s3://1", Filename: "1.pdf", ExpiresAt: &exp1}
	d2 := &compliance.Document{VendorID: vendorID, Kind: compliance.DocKindFoodSafetyPermit,
		BlobURI: "s3://2", Filename: "2.pdf", ExpiresAt: &exp2}
	d3 := &compliance.Document{VendorID: vendorID, Kind: compliance.DocKindInsurance,
		BlobURI: "s3://3", Filename: "3.pdf", ExpiresAt: &exp3}
	pendingButExpiring := &compliance.Document{VendorID: vendorID, Kind: compliance.DocKindTaxRegistration,
		BlobURI: "s3://p", Filename: "p.pdf", ExpiresAt: &exp1}
	noExpiry := &compliance.Document{VendorID: vendorID, Kind: compliance.DocKindOther,
		BlobURI: "s3://n", Filename: "n.pdf"}

	for _, doc := range []*compliance.Document{d1, d2, d3, pendingButExpiring, noExpiry} {
		require.NoError(t, repo.Create(ctx, doc))
	}
	for _, id := range []string{d1.ID, d2.ID, d3.ID, noExpiry.ID} {
		require.NoError(t, repo.UpdateStatus(ctx, id, compliance.DocStatusApproved, nil, ""))
	}

	cutoff := today.AddDate(0, 0, 14)
	got, err := repo.ListExpiringBefore(ctx, cutoff)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Sorted by expires_at ascending.
	assert.Equal(t, d1.ID, got[0].ID)
	assert.Equal(t, d2.ID, got[1].ID)
}

func TestDocumentRepo_SupersedesRoundtrip(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDocumentRepo(pool)

	vendorID := seedApprovedVendor(t, pool)

	original := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindBusinessLicense,
		BlobURI: "s3://old", Filename: "old.pdf",
	}
	require.NoError(t, repo.Create(ctx, original))
	assert.Nil(t, original.Supersedes)

	replacement := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindBusinessLicense,
		BlobURI: "s3://new", Filename: "new.pdf",
		Supersedes: &original.ID,
	}
	require.NoError(t, repo.Create(ctx, replacement))

	// The replacement carries the supersedes link; the original does not.
	got, err := repo.GetByID(ctx, replacement.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Supersedes)
	assert.Equal(t, original.ID, *got.Supersedes)

	gotOrig, err := repo.GetByID(ctx, original.ID)
	require.NoError(t, err)
	assert.Nil(t, gotOrig.Supersedes)
}

func TestDocumentRepo_ListPastExpiry(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDocumentRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	today := time.Now().UTC().Truncate(24 * time.Hour)

	past := today.AddDate(0, 0, -2)
	future := today.AddDate(0, 0, 10)

	d1 := &compliance.Document{VendorID: vendorID, Kind: compliance.DocKindBusinessLicense,
		BlobURI: "s3://past", Filename: "past.pdf", ExpiresAt: &past}
	d2 := &compliance.Document{VendorID: vendorID, Kind: compliance.DocKindFoodSafetyPermit,
		BlobURI: "s3://future", Filename: "future.pdf", ExpiresAt: &future}

	require.NoError(t, repo.Create(ctx, d1))
	require.NoError(t, repo.Create(ctx, d2))
	require.NoError(t, repo.UpdateStatus(ctx, d1.ID, compliance.DocStatusApproved, nil, ""))
	require.NoError(t, repo.UpdateStatus(ctx, d2.ID, compliance.DocStatusApproved, nil, ""))

	got, err := repo.ListPastExpiry(ctx, today)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, d1.ID, got[0].ID)
}
