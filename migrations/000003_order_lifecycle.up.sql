CREATE TYPE order_status AS ENUM (
  'draft', 'placed', 'cutoff', 'cancelled', 'ready', 'picked_up', 'no_show', 'refunded'
);

CREATE TABLE "order" (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  vendor_id       UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  plant           TEXT NOT NULL,
  supply_date     DATE NOT NULL,
  status          order_status NOT NULL DEFAULT 'draft',
  total_price_minor BIGINT NOT NULL CHECK (total_price_minor >= 0),
  placed_at       TIMESTAMPTZ,
  cutoff_at       TIMESTAMPTZ NOT NULL,
  cancelled_at    TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_user_date_idx ON "order"(user_id, supply_date DESC);
CREATE INDEX order_vendor_date_idx ON "order"(vendor_id, supply_date);
CREATE INDEX order_status_idx ON "order"(status) WHERE status IN ('placed','cutoff','ready');
CREATE INDEX order_pending_cutoff_idx ON "order"(cutoff_at) WHERE status = 'placed';

CREATE TABLE order_item (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id        UUID NOT NULL REFERENCES "order"(id) ON DELETE CASCADE,
  menu_item_id    UUID NOT NULL REFERENCES menu_item(id) ON DELETE RESTRICT,
  qty             INTEGER NOT NULL CHECK (qty > 0),
  unit_price_minor BIGINT NOT NULL CHECK (unit_price_minor >= 0)
);
CREATE INDEX order_item_order_idx ON order_item(order_id);

CREATE TABLE order_state_event (
  id          BIGSERIAL PRIMARY KEY,
  order_id    UUID NOT NULL REFERENCES "order"(id) ON DELETE CASCADE,
  from_state  order_status,
  to_state    order_status NOT NULL,
  actor_id    UUID REFERENCES "user"(id),
  actor_role  user_role,
  reason      TEXT NOT NULL DEFAULT '',
  payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
  at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX order_state_event_order_idx ON order_state_event(order_id, at DESC);

-- Append-only guard: no update/delete on order_state_event
CREATE OR REPLACE FUNCTION order_state_event_append_only() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'order_state_event is append-only (op=%)', TG_OP;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER order_state_event_no_update BEFORE UPDATE ON order_state_event
  FOR EACH ROW EXECUTE FUNCTION order_state_event_append_only();
CREATE TRIGGER order_state_event_no_delete BEFORE DELETE ON order_state_event
  FOR EACH ROW EXECUTE FUNCTION order_state_event_append_only();

CREATE TABLE audit_event (
  id           BIGSERIAL PRIMARY KEY,
  actor_id     UUID REFERENCES "user"(id),
  actor_role   user_role,
  action       TEXT NOT NULL,
  target_kind  TEXT NOT NULL,
  target_id    TEXT NOT NULL,
  payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
  at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  request_id   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_event_target_idx ON audit_event(target_kind, target_id, at DESC);
CREATE INDEX audit_event_actor_idx ON audit_event(actor_id, at DESC) WHERE actor_id IS NOT NULL;

CREATE OR REPLACE FUNCTION audit_event_append_only() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'audit_event is append-only (op=%)', TG_OP;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER audit_event_no_update BEFORE UPDATE ON audit_event
  FOR EACH ROW EXECUTE FUNCTION audit_event_append_only();
CREATE TRIGGER audit_event_no_delete BEFORE DELETE ON audit_event
  FOR EACH ROW EXECUTE FUNCTION audit_event_append_only();

CREATE TABLE outbox_event (
  id              BIGSERIAL PRIMARY KEY,
  aggregate_type  TEXT NOT NULL,
  aggregate_id    UUID NOT NULL,
  subject         TEXT NOT NULL,
  payload         JSONB NOT NULL,
  headers         JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at    TIMESTAMPTZ,
  attempts        INT NOT NULL DEFAULT 0,
  last_error      TEXT
);
CREATE INDEX outbox_unpublished_idx ON outbox_event(id) WHERE published_at IS NULL;
CREATE INDEX outbox_aggregate_idx ON outbox_event(aggregate_type, aggregate_id);
