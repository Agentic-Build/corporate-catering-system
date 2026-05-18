-- Service window per vendor×plant — the time band a vendor delivers to a
-- plant (e.g. "11:30-13:00"), shown to employees and managed by 福委會.
ALTER TABLE vendor_plant_mapping ADD COLUMN service_window TEXT NOT NULL DEFAULT '';
