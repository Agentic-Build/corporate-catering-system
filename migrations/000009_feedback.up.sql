CREATE TYPE meal_complaint_category AS ENUM (
  'wrong_item', 'missing_item', 'quality', 'portion', 'hygiene', 'other'
);
CREATE TYPE meal_complaint_status AS ENUM (
  'open', 'vendor_responded', 'escalated', 'resolved'
);

CREATE TABLE meal_rating (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id    UUID NOT NULL UNIQUE REFERENCES "order"(id) ON DELETE RESTRICT,
  user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  vendor_id   UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  score       SMALLINT NOT NULL CHECK (score BETWEEN 1 AND 5),
  comment     TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX meal_rating_vendor_idx ON meal_rating(vendor_id, created_at DESC);

CREATE TABLE meal_complaint (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id           UUID NOT NULL REFERENCES "order"(id) ON DELETE RESTRICT,
  user_id            UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  vendor_id          UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  category           meal_complaint_category NOT NULL,
  description        TEXT NOT NULL,
  status             meal_complaint_status NOT NULL DEFAULT 'open',
  vendor_response    TEXT NOT NULL DEFAULT '',
  vendor_responded_at TIMESTAMPTZ,
  escalated_at       TIMESTAMPTZ,
  resolution         TEXT NOT NULL DEFAULT '',
  resolved_by        UUID REFERENCES "user"(id),
  resolved_at        TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX meal_complaint_vendor_idx ON meal_complaint(vendor_id, created_at DESC);
CREATE INDEX meal_complaint_user_idx   ON meal_complaint(user_id, created_at DESC);
CREATE INDEX meal_complaint_status_idx ON meal_complaint(status);
-- At most one non-resolved complaint per order.
CREATE UNIQUE INDEX meal_complaint_one_open_idx
  ON meal_complaint(order_id) WHERE status <> 'resolved';
