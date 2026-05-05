-- Drop quota tables and triggers
DROP TRIGGER IF EXISTS quota_ledger_entry_append_only_truncate_guard ON quota_ledger_entry;
DROP TRIGGER IF EXISTS quota_ledger_entry_append_only_guard ON quota_ledger_entry;
DROP TRIGGER IF EXISTS quota_ledger_entry_append_audit_event ON quota_ledger_entry;
DROP TABLE IF EXISTS quota_ledger_entry;

DROP TRIGGER IF EXISTS employee_quota_profile_append_audit_event ON employee_quota_profile;
DROP TRIGGER IF EXISTS employee_quota_profile_set_updated_at_utc ON employee_quota_profile;
DROP TABLE IF EXISTS employee_quota_profile;

-- Remove cycle_id from employee_order
DROP INDEX IF EXISTS employee_order_cycle_id_idx;
ALTER TABLE employee_order DROP COLUMN cycle_id;

-- Recreate payroll types
CREATE TYPE payroll_source_kind AS ENUM (
    'ORDER_MUTATION',
    'DISPUTE_WORKFLOW',
    'SFTP_BATCH_EXPORT',
    'HR_API_SYNC_ADJUNCT'
);

CREATE TYPE payroll_entry_kind AS ENUM (
    'DEDUCTION',
    'ADJUSTMENT_DEBIT',
    'ADJUSTMENT_CREDIT',
    'REFUND'
);

-- Recreate payroll ledger table
CREATE TABLE payroll_ledger_entry (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id global_pk NOT NULL REFERENCES employee_order(id) ON DELETE RESTRICT,
    employee_actor_id global_pk NOT NULL REFERENCES actor_account(id) ON DELETE RESTRICT,
    source_kind payroll_source_kind NOT NULL,
    entry_kind payroll_entry_kind NOT NULL,
    amount_minor money_minor NOT NULL CHECK (amount_minor <> 0),
    currency currency_code NOT NULL DEFAULT 'TWD',
    occurred_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB
);

CREATE INDEX payroll_ledger_entry_actor_occurred_idx
    ON payroll_ledger_entry (employee_actor_id, occurred_at_utc DESC);

CREATE TRIGGER payroll_ledger_entry_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON payroll_ledger_entry
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER payroll_ledger_entry_append_only_guard
BEFORE UPDATE OR DELETE ON payroll_ledger_entry
FOR EACH ROW
EXECUTE FUNCTION enforce_append_only();

CREATE TRIGGER payroll_ledger_entry_append_only_truncate_guard
BEFORE TRUNCATE ON payroll_ledger_entry
FOR EACH STATEMENT
EXECUTE FUNCTION enforce_append_only();
