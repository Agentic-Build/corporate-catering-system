package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors/postgres"
)

// TestRepos_ClosedPoolPropagatesErrors exercises the query/exec error-return
// branches of every repo method. Closing the pool makes Query/Exec/QueryRow
// fail, which is the same surface those branches guard against in production
// (lost DB connection, etc).
func TestRepos_ClosedPoolPropagatesErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()

	orepo := postgres.NewOperatorRepo(pool)
	vrepo := postgres.NewVendorRepo(pool)
	prepo := postgres.NewPlantMappingRepo(pool)
	ctx := context.Background()

	// Close the pool so all subsequent DB calls error out.
	pool.Close()

	t.Run("operator Get/one/list query error", func(t *testing.T) {
		_, err := orepo.Get(ctx, "v", "o")
		require.Error(t, err)
		assert.NotErrorIs(t, err, vendor.ErrOperatorNotFound)
	})

	t.Run("operator ListByVendor query error", func(t *testing.T) {
		_, err := orepo.ListByVendor(ctx, "v")
		require.Error(t, err)
	})

	t.Run("operator ListByVendorStatus query error", func(t *testing.T) {
		_, err := orepo.ListByVendorStatus(ctx, "v", []vendor.OperatorStatus{vendor.OperatorStatusActive})
		require.Error(t, err)
	})

	t.Run("operator Upsert query error", func(t *testing.T) {
		err := orepo.Upsert(ctx, &vendor.OperatorAccount{VendorID: "v", Email: "e@x.com", Status: vendor.OperatorStatusActive})
		require.Error(t, err)
	})

	t.Run("operator SetStatus exec error", func(t *testing.T) {
		err := orepo.SetStatus(ctx, "v", "o", vendor.OperatorStatusActive)
		require.Error(t, err)
		assert.NotErrorIs(t, err, vendor.ErrOperatorNotFound)
	})

	t.Run("operator SetStatuses exec error", func(t *testing.T) {
		err := orepo.SetStatuses(ctx, "v", []vendor.OperatorStatus{vendor.OperatorStatusActive}, vendor.OperatorStatusVendorSuspended)
		require.Error(t, err)
	})

	t.Run("vendor Create query error", func(t *testing.T) {
		err := vrepo.Create(ctx, &vendor.Vendor{DisplayName: "x", ContactEmail: "z@x.com", Status: vendor.StatusPending})
		require.Error(t, err)
	})

	t.Run("vendor one query error (non-NoRows)", func(t *testing.T) {
		_, err := vrepo.GetByID(ctx, "v")
		require.Error(t, err)
		assert.NotErrorIs(t, err, vendor.ErrVendorNotFound)
	})

	t.Run("vendor UpdateStatus approved exec error", func(t *testing.T) {
		by := "admin"
		err := vrepo.UpdateStatus(ctx, "v", vendor.StatusApproved, &by)
		require.Error(t, err)
	})

	t.Run("vendor UpdateStatus non-approved exec error", func(t *testing.T) {
		err := vrepo.UpdateStatus(ctx, "v", vendor.StatusSuspended, nil)
		require.Error(t, err)
	})

	t.Run("vendor UpdateSettings exec error", func(t *testing.T) {
		err := vrepo.UpdateSettings(ctx, "v", 12, 5)
		require.Error(t, err)
		assert.NotErrorIs(t, err, vendor.ErrVendorNotFound)
	})

	t.Run("vendor UpdateContactEmail exec error", func(t *testing.T) {
		err := vrepo.UpdateContactEmail(ctx, "v", "new@x.com")
		require.Error(t, err)
		assert.NotErrorIs(t, err, vendor.ErrVendorNotFound)
	})

	t.Run("vendor List query error", func(t *testing.T) {
		_, err := vrepo.List(ctx, []vendor.Status{vendor.StatusApproved})
		require.Error(t, err)
	})

	t.Run("plant ListByVendor query error", func(t *testing.T) {
		_, err := prepo.ListByVendor(ctx, "v")
		require.Error(t, err)
	})

	t.Run("plant ListVendorsForPlant query error", func(t *testing.T) {
		_, err := prepo.ListVendorsForPlant(ctx, "F12B-3F")
		require.Error(t, err)
	})

	t.Run("plant SetWindow exec error", func(t *testing.T) {
		err := prepo.SetWindow(ctx, "v", "F12B-3F", "11:00-12:00")
		require.Error(t, err)
		assert.NotErrorIs(t, err, vendor.ErrVendorNotFound)
	})

	t.Run("plant Set begin/exec error", func(t *testing.T) {
		err := prepo.Set(ctx, "v", []string{"F12B-3F"})
		require.Error(t, err)
	})
}
