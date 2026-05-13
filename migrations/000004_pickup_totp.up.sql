-- Per-order TOTP secret used for pickup verification.
ALTER TABLE "order"
  ADD COLUMN totp_secret BYTEA NOT NULL DEFAULT decode('00', 'hex'),
  ADD COLUMN ready_at TIMESTAMPTZ,
  ADD COLUMN picked_up_at TIMESTAMPTZ,
  ADD COLUMN no_show_at TIMESTAMPTZ;

CREATE INDEX order_ready_idx ON "order"(vendor_id, supply_date) WHERE status = 'ready';
CREATE INDEX order_pickup_pending_idx ON "order"(ready_at) WHERE status = 'ready';
