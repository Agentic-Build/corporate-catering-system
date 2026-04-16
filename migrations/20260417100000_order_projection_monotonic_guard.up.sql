ALTER TABLE order_state_event_projection
ADD COLUMN occurred_at_epoch_millis BIGINT NOT NULL DEFAULT 0;

CREATE INDEX order_state_event_projection_monotonic_idx
    ON order_state_event_projection (order_id, occurred_at_epoch_millis DESC, event_id DESC);
