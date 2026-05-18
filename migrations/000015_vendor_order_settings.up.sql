-- Per-vendor ordering settings. cutoff_hour is the local-time hour on the day
-- before supply by which orders must be placed/changed; preorder_window_days
-- is how many days ahead employees may order from this vendor.
ALTER TABLE vendor ADD COLUMN cutoff_hour INT NOT NULL DEFAULT 17
  CHECK (cutoff_hour BETWEEN 0 AND 23);
ALTER TABLE vendor ADD COLUMN preorder_window_days INT NOT NULL DEFAULT 7
  CHECK (preorder_window_days BETWEEN 1 AND 30);
