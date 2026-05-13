CREATE TYPE payroll_batch_status AS ENUM ('draft', 'locked', 'exported', 'closed');
CREATE TYPE payroll_dispute_status AS ENUM ('open', 'resolved_refund', 'resolved_reject', 'cancelled');

CREATE TABLE payroll_batch (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  period_start  DATE NOT NULL,
  period_end    DATE NOT NULL,
  status        payroll_batch_status NOT NULL DEFAULT 'draft',
  locked_at     TIMESTAMPTZ,
  locked_by     UUID REFERENCES "user"(id),
  exported_at   TIMESTAMPTZ,
  export_uri    TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (period_start <= period_end)
);
CREATE UNIQUE INDEX payroll_batch_period_idx ON payroll_batch(period_start, period_end);
CREATE INDEX payroll_batch_status_idx ON payroll_batch(status);

CREATE TABLE payroll_entry (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  batch_id      UUID NOT NULL REFERENCES payroll_batch(id) ON DELETE CASCADE,
  user_id       UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  order_ids     UUID[] NOT NULL,
  amount_minor  BIGINT NOT NULL CHECK (amount_minor >= 0),
  refunded_minor BIGINT NOT NULL DEFAULT 0 CHECK (refunded_minor >= 0),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payroll_entry_batch_idx ON payroll_entry(batch_id);
CREATE INDEX payroll_entry_user_idx ON payroll_entry(user_id);

-- Append-only protection on entries (refunds are tracked via payroll_dispute + entry.refunded_minor UPDATE,
-- so we allow UPDATE here, but NOT DELETE. Simpler than full append-only.)
CREATE OR REPLACE FUNCTION payroll_entry_no_delete() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'payroll_entry is not deletable (op=%)', TG_OP;
END $$ LANGUAGE plpgsql;
CREATE TRIGGER payroll_entry_no_delete_trg BEFORE DELETE ON payroll_entry
  FOR EACH ROW EXECUTE FUNCTION payroll_entry_no_delete();

CREATE TABLE payroll_dispute (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  entry_id      UUID NOT NULL REFERENCES payroll_entry(id) ON DELETE RESTRICT,
  order_id      UUID NOT NULL REFERENCES "order"(id),
  opened_by     UUID NOT NULL REFERENCES "user"(id),
  reason        TEXT NOT NULL,
  status        payroll_dispute_status NOT NULL DEFAULT 'open',
  resolution    TEXT NOT NULL DEFAULT '',
  resolved_by   UUID REFERENCES "user"(id),
  resolved_at   TIMESTAMPTZ,
  refund_minor  BIGINT NOT NULL DEFAULT 0,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payroll_dispute_entry_idx ON payroll_dispute(entry_id);
CREATE INDEX payroll_dispute_status_idx ON payroll_dispute(status);
CREATE INDEX payroll_dispute_opened_by_idx ON payroll_dispute(opened_by);
