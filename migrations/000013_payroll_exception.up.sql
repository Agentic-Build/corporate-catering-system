-- Payroll settlement exception list: entries that need manual handling before
-- the HR deduction file goes out — a departed/suspended employee (auto-detected)
-- or a deduction the welfare admin has flagged as failed.
CREATE TYPE payroll_exception_kind AS ENUM ('employee_departed', 'deduction_failed');
CREATE TYPE payroll_exception_status AS ENUM ('open', 'resolved', 'excluded');

CREATE TABLE payroll_exception (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  batch_id    UUID NOT NULL REFERENCES payroll_batch(id) ON DELETE CASCADE,
  entry_id    UUID NOT NULL REFERENCES payroll_entry(id) ON DELETE CASCADE,
  user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
  kind        payroll_exception_kind NOT NULL,
  status      payroll_exception_status NOT NULL DEFAULT 'open',
  detail      TEXT NOT NULL DEFAULT '',
  resolution  TEXT NOT NULL DEFAULT '',
  resolved_by UUID REFERENCES "user"(id),
  resolved_at TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one exception of a given kind per (batch, entry) — lets detection
-- re-run idempotently via ON CONFLICT DO NOTHING.
CREATE UNIQUE INDEX payroll_exception_dedup_idx ON payroll_exception(batch_id, entry_id, kind);
CREATE INDEX payroll_exception_batch_idx ON payroll_exception(batch_id);
