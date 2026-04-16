DROP INDEX IF EXISTS order_state_event_projection_monotonic_idx;

ALTER TABLE order_state_event_projection
DROP COLUMN IF EXISTS occurred_at_epoch_millis;
