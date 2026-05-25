-- Plant registry: canonical list of factory/pickup locations.
CREATE TABLE plant (
    code        TEXT PRIMARY KEY,
    label       TEXT NOT NULL,
    address     TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT true,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backfill from existing vendor_plant_mapping (label = code as placeholder).
INSERT INTO plant (code, label)
SELECT DISTINCT plant, plant
  FROM vendor_plant_mapping
ON CONFLICT DO NOTHING;

-- Enforce referential integrity on future mappings.
ALTER TABLE vendor_plant_mapping
    ADD CONSTRAINT vendor_plant_mapping_plant_fk
    FOREIGN KEY (plant) REFERENCES plant(code) ON DELETE RESTRICT;
