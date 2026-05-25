package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

type PlantMappingRepo struct{ pool *pgxpool.Pool }

func NewPlantMappingRepo(p *pgxpool.Pool) *PlantMappingRepo { return &PlantMappingRepo{pool: p} }

func (r *PlantMappingRepo) ListByVendor(ctx context.Context, vendorID string) ([]*vendor.PlantMapping, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, vendor_id, plant, active, service_window, created_at
  FROM vendor_plant_mapping
 WHERE vendor_id = $1 AND active = true
 ORDER BY plant`, vendorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*vendor.PlantMapping
	for rows.Next() {
		var p vendor.PlantMapping
		if err := rows.Scan(&p.ID, &p.VendorID, &p.Plant, &p.Active, &p.ServiceWindow, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (r *PlantMappingRepo) ListVendorsForPlant(ctx context.Context, plant string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT vendor_id FROM vendor_plant_mapping WHERE plant = $1 AND active = true`, plant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetWindow sets the service window for one vendor×plant mapping. Returns
// ErrVendorNotFound when no active mapping exists for that pair.
func (r *PlantMappingRepo) SetWindow(ctx context.Context, vendorID, plant, window string) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vendor_plant_mapping SET service_window=$3
 WHERE vendor_id=$1 AND plant=$2 AND active=true`, vendorID, plant, window)
	if err != nil {
		return fmt.Errorf("set service window: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return vendor.ErrVendorNotFound
	}
	return nil
}

func (r *PlantMappingRepo) Set(ctx context.Context, vendorID string, plants []string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		// Drop only mappings no longer in the desired set; empty set clears all.
		if _, err := tx.Exec(ctx, `
DELETE FROM vendor_plant_mapping
 WHERE vendor_id = $1 AND NOT (plant = ANY($2))`, vendorID, plants); err != nil {
			return fmt.Errorf("delete removed mappings: %w", err)
		}
		// Insert new mappings; reactivate retained ones without touching
		// service_window (admin-managed, set via SetWindow).
		for _, p := range plants {
			if _, err := tx.Exec(ctx, `
INSERT INTO vendor_plant_mapping (vendor_id, plant, active)
VALUES ($1, $2, true)
ON CONFLICT (vendor_id, plant) DO UPDATE SET active = true`, vendorID, p); err != nil {
				return fmt.Errorf("insert mapping %q: %w", p, err)
			}
		}
		return nil
	})
}
