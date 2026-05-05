-- Remove payroll triggers and functions
DROP TRIGGER IF EXISTS payroll_ledger_entry_append_audit_event ON payroll_ledger_entry;
DROP TRIGGER IF EXISTS payroll_ledger_entry_append_only_guard ON payroll_ledger_entry;
DROP TRIGGER IF EXISTS payroll_ledger_entry_append_only_truncate_guard ON payroll_ledger_entry;

-- Drop payroll ledger table
DROP TABLE IF EXISTS payroll_ledger_entry;

-- Drop payroll types
DROP TYPE IF EXISTS payroll_source_kind;
DROP TYPE IF EXISTS payroll_entry_kind;

-- Modify employee_order to add cycle_id
ALTER TABLE employee_order ADD COLUMN cycle_id TEXT;
CREATE INDEX employee_order_cycle_id_idx ON employee_order (cycle_id);

-- Create employee_quota_profile table
CREATE TABLE employee_quota_profile (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    employee_actor_id global_pk NOT NULL REFERENCES actor_account(id) ON DELETE CASCADE,
    weekly_quota_minor money_minor NOT NULL CHECK (weekly_quota_minor >= 0),
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (employee_actor_id)
);

CREATE TRIGGER employee_quota_profile_set_updated_at_utc
BEFORE UPDATE ON employee_quota_profile
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();

CREATE TRIGGER employee_quota_profile_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON employee_quota_profile
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

-- Create quota_ledger_entry table
CREATE TABLE quota_ledger_entry (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id global_pk REFERENCES employee_order(id) ON DELETE SET NULL,
    employee_actor_id global_pk NOT NULL REFERENCES actor_account(id) ON DELETE RESTRICT,
    cycle_id TEXT NOT NULL,
    amount_minor money_minor NOT NULL CHECK (amount_minor <> 0),
    occurred_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX quota_ledger_entry_actor_cycle_idx
    ON quota_ledger_entry (employee_actor_id, cycle_id);

CREATE TRIGGER quota_ledger_entry_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON quota_ledger_entry
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER quota_ledger_entry_append_only_guard
BEFORE UPDATE OR DELETE ON quota_ledger_entry
FOR EACH ROW
EXECUTE FUNCTION enforce_append_only();

CREATE TRIGGER quota_ledger_entry_append_only_truncate_guard
BEFORE TRUNCATE ON quota_ledger_entry
FOR EACH STATEMENT
EXECUTE FUNCTION enforce_append_only();
