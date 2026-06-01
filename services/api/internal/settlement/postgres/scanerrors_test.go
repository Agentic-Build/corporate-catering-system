package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	spg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement/postgres"
)

// TestSettlementRepo_ScanErrors drives the per-row in-loop rows.Scan(...) error
// returns that the happy paths never reach. Technique (mirrors the menu/order
// repos): seed a real row so rows.Next() returns true, then NULL a value-typed
// column that scans into a non-pointer destination — NULL cannot scan into a
// non-pointer Go value, so the in-loop Scan fails after iteration has started.
// Schema mutations are reverted in t.Cleanup so subtests stay independent on the
// shared pool.
func TestSettlementRepo_ScanErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	dropNotNullThenNil := func(t *testing.T, table, col, where string) {
		t.Helper()
		_, err := pool.Exec(ctx, `ALTER TABLE `+table+` ALTER COLUMN `+col+` DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE `+table+` SET `+col+` = NULL `+where)
		require.NoError(t, err)
		t.Cleanup(func() {
			// Best-effort restore so other subtests sharing the pool are unaffected.
			_, _ = pool.Exec(ctx, `DELETE FROM `+table+` WHERE `+col+` IS NULL`)
			_, _ = pool.Exec(ctx, `ALTER TABLE `+table+` ALTER COLUMN `+col+` SET NOT NULL`)
		})
	}

	t.Run("collectSettlements_ScanError", func(t *testing.T) {
		start, end := aprilPeriod()
		vendor := seedVendor(t, pool)
		admin := seedAdminUser(t, pool)
		st := newClosed(vendor, admin, start, end, []string{})
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			return repo.CreateTx(ctx, tx, st)
		}))
		// order_count scans into int; NULL breaks the in-loop Scan after Next().
		dropNotNullThenNil(t, "vendor_settlement", "order_count", `WHERE id = '`+st.ID+`'`)

		_, err := repo.ListByVendor(ctx, vendor)
		require.Error(t, err)
	})

	t.Run("AggregateByVendor_ScanError", func(t *testing.T) {
		start, end := aprilPeriod()
		vendor := seedVendor(t, pool)
		user := seedEmployeeUser(t, pool)
		oid := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 12000, 1)
		// vendor_id is the GROUP BY key scanned into a.VendorID (string). NULLing
		// it makes a NULL-key group whose scan into a non-pointer string fails.
		dropNotNullThenNil(t, `"order"`, "vendor_id", `WHERE id = '`+oid+`'`)

		_, err := repo.AggregateByVendor(ctx, start, end)
		require.Error(t, err)
	})

	t.Run("OrderLinesByIDs_ScanError", func(t *testing.T) {
		start, _ := aprilPeriod()
		vendor := seedVendor(t, pool)
		user := seedEmployeeUser(t, pool)
		oid := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 3), "picked_up", 9000, 1)
		// status scans into l.Status (string); NULL breaks the in-loop Scan.
		dropNotNullThenNil(t, `"order"`, "status", `WHERE id = '`+oid+`'`)

		_, err := repo.OrderLinesByIDs(ctx, []string{oid})
		require.Error(t, err)
	})
}
