DROP INDEX IF EXISTS order_pickup_pending_idx;
DROP INDEX IF EXISTS order_ready_idx;
ALTER TABLE "order"
  DROP COLUMN IF EXISTS no_show_at,
  DROP COLUMN IF EXISTS picked_up_at,
  DROP COLUMN IF EXISTS ready_at,
  DROP COLUMN IF EXISTS totp_secret;
