package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors/postgres"
)

// TestRepos_ScanErrors forces the per-row rows.Scan error branches by relaxing
// a NOT NULL constraint and inserting a NULL into a column the repo scans into a
// non-pointer Go destination. Scanning SQL NULL into a *string fails, which is
// exactly the failure those `if err := rows.Scan(...); err != nil` guards cover
// (e.g. a column dropped/altered out from under the app, corrupt data).
func TestRepos_ScanErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	vrepo := postgres.NewVendorRepo(pool)
	orepo := postgres.NewOperatorRepo(pool)
	prepo := postgres.NewPlantMappingRepo(pool)

	t.Run("vendor List scan error", func(t *testing.T) {
		v := &vendor.Vendor{DisplayName: "S", LegalName: "S Ltd", ContactEmail: "scan-vendor@x.com", Status: vendor.StatusApproved}
		require.NoError(t, vrepo.Create(ctx, v))
		_, err := pool.Exec(ctx, `ALTER TABLE vendor ALTER COLUMN display_name DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE vendor SET display_name = NULL WHERE id = $1`, v.ID)
		require.NoError(t, err)

		_, err = vrepo.List(ctx, nil)
		require.Error(t, err) // NULL -> non-pointer string scan fails

		// Restore so it doesn't poison later subtests.
		_, err = pool.Exec(ctx, `UPDATE vendor SET display_name = '' WHERE id = $1`, v.ID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `ALTER TABLE vendor ALTER COLUMN display_name SET NOT NULL`)
		require.NoError(t, err)
	})

	t.Run("operator list scan error", func(t *testing.T) {
		v := &vendor.Vendor{DisplayName: "OV", LegalName: "OV Ltd", ContactEmail: "scan-op@x.com", Status: vendor.StatusApproved}
		require.NoError(t, vrepo.Create(ctx, v))
		op := &vendor.OperatorAccount{VendorID: v.ID, Email: "scanop@x.com", DisplayName: "Z", Provider: "authentik", Status: vendor.OperatorStatusActive}
		require.NoError(t, orepo.Upsert(ctx, op))

		_, err := pool.Exec(ctx, `ALTER TABLE vendor_operator_account ALTER COLUMN display_name DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE vendor_operator_account SET display_name = NULL WHERE id = $1`, op.ID)
		require.NoError(t, err)

		_, err = orepo.ListByVendor(ctx, v.ID)
		require.Error(t, err)

		_, err = pool.Exec(ctx, `UPDATE vendor_operator_account SET display_name = '' WHERE id = $1`, op.ID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `ALTER TABLE vendor_operator_account ALTER COLUMN display_name SET NOT NULL`)
		require.NoError(t, err)
	})

	t.Run("plant ListByVendor scan error", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO plant (code, label) VALUES ('SCAN-1F','SCAN-1F') ON CONFLICT DO NOTHING`)
		require.NoError(t, err)
		v := &vendor.Vendor{DisplayName: "PV", LegalName: "PV Ltd", ContactEmail: "scan-plant@x.com", Status: vendor.StatusApproved}
		require.NoError(t, vrepo.Create(ctx, v))
		require.NoError(t, prepo.Set(ctx, v.ID, []string{"SCAN-1F"}))

		_, err = pool.Exec(ctx, `ALTER TABLE vendor_plant_mapping ALTER COLUMN service_window DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `UPDATE vendor_plant_mapping SET service_window = NULL WHERE vendor_id = $1`, v.ID)
		require.NoError(t, err)

		_, err = prepo.ListByVendor(ctx, v.ID)
		require.Error(t, err) // NULL service_window -> non-pointer string scan fails

		_, err = pool.Exec(ctx, `UPDATE vendor_plant_mapping SET service_window = '' WHERE vendor_id = $1`, v.ID)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `ALTER TABLE vendor_plant_mapping ALTER COLUMN service_window SET NOT NULL`)
		require.NoError(t, err)
	})

	t.Run("plant Set delete error", func(t *testing.T) {
		v := &vendor.Vendor{DisplayName: "DV", LegalName: "DV Ltd", ContactEmail: "scan-del@x.com", Status: vendor.StatusApproved}
		require.NoError(t, vrepo.Create(ctx, v))

		// Make the in-tx DELETE fail (begin succeeds, delete raises) via a trigger.
		_, err := pool.Exec(ctx, `
CREATE OR REPLACE FUNCTION block_vpm_delete() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'no deletes allowed'; END; $$ LANGUAGE plpgsql`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `CREATE TRIGGER block_vpm_delete_trg BEFORE DELETE ON vendor_plant_mapping FOR EACH ROW EXECUTE FUNCTION block_vpm_delete()`)
		require.NoError(t, err)

		// Seed one row so the DELETE has something to remove and fires the trigger.
		_, err = pool.Exec(ctx, `INSERT INTO plant (code, label) VALUES ('DEL-1F','DEL-1F') ON CONFLICT DO NOTHING`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO vendor_plant_mapping (vendor_id, plant, active) VALUES ($1,'DEL-1F',true)`, v.ID)
		require.NoError(t, err)

		// Set with a different plant set -> DELETE of DEL-1F fires trigger -> error.
		_, err = pool.Exec(ctx, `INSERT INTO plant (code, label) VALUES ('DEL-2F','DEL-2F') ON CONFLICT DO NOTHING`)
		require.NoError(t, err)
		err = prepo.Set(ctx, v.ID, []string{"DEL-2F"})
		require.Error(t, err)

		_, err = pool.Exec(ctx, `DROP TRIGGER block_vpm_delete_trg ON vendor_plant_mapping`)
		require.NoError(t, err)
	})

	t.Run("plant ListVendorsForPlant scan error", func(t *testing.T) {
		_, err := pool.Exec(ctx, `INSERT INTO plant (code, label) VALUES ('SCAN-2F','SCAN-2F') ON CONFLICT DO NOTHING`)
		require.NoError(t, err)
		// Allow a NULL vendor_id row so the vendor_id scan into a string fails.
		_, err = pool.Exec(ctx, `ALTER TABLE vendor_plant_mapping ALTER COLUMN vendor_id DROP NOT NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO vendor_plant_mapping (vendor_id, plant, active) VALUES (NULL, 'SCAN-2F', true)`)
		require.NoError(t, err)

		_, err = prepo.ListVendorsForPlant(ctx, "SCAN-2F")
		require.Error(t, err) // NULL vendor_id -> non-pointer string scan fails

		_, err = pool.Exec(ctx, `DELETE FROM vendor_plant_mapping WHERE plant = 'SCAN-2F' AND vendor_id IS NULL`)
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `ALTER TABLE vendor_plant_mapping ALTER COLUMN vendor_id SET NOT NULL`)
		require.NoError(t, err)
	})
}
