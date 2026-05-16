CREATE TYPE vendor_settlement_status AS ENUM ('closed', 'void');

CREATE TABLE vendor_settlement (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  vendor_id     UUID NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
  period_start  DATE NOT NULL,
  period_end    DATE NOT NULL,
  order_count   INTEGER NOT NULL CHECK (order_count >= 0),
  portion_count INTEGER NOT NULL CHECK (portion_count >= 0),
  gross_minor   BIGINT NOT NULL CHECK (gross_minor >= 0),
  order_ids     UUID[] NOT NULL,
  status        vendor_settlement_status NOT NULL DEFAULT 'closed',
  closed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  closed_by     UUID REFERENCES "user"(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (period_start <= period_end)
);
-- 同一商家同一期間至多一筆有效結算單
CREATE UNIQUE INDEX vendor_settlement_active_idx
  ON vendor_settlement(vendor_id, period_start, period_end)
  WHERE status = 'closed';
CREATE INDEX vendor_settlement_vendor_idx ON vendor_settlement(vendor_id, period_start DESC);
