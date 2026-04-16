-- Audited relational schema foundation for PostgreSQL 16+.
-- Conventions:
-- - Global primary key standard: UUID (`global_pk`)
-- - Monetary values: BIGINT minor units (`money_minor`)
-- - Timestamps: UTC instants in `TIMESTAMPTZ` columns suffixed with `_utc`

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE DOMAIN global_pk AS UUID;

CREATE DOMAIN money_minor AS BIGINT
    CHECK (VALUE BETWEEN -9000000000000000 AND 9000000000000000);

CREATE DOMAIN currency_code AS CHAR(3)
    CHECK (VALUE ~ '^[A-Z]{3}$');

CREATE TYPE actor_role AS ENUM (
    'EMPLOYEE',
    'VENDOR_OPERATOR',
    'COMMITTEE_ADMIN',
    'PAYROLL_OPERATOR'
);

CREATE TYPE authentication_source AS ENUM (
    'CORPORATE_SSO',
    'VENDOR_ACCOUNT_MFA',
    'OAUTH_SERVICE_ACCOUNT'
);

CREATE TYPE vendor_status AS ENUM (
    'PENDING',
    'ACTIVE',
    'SUSPENDED',
    'REJECTED'
);

CREATE TYPE service_window_status AS ENUM (
    'ACTIVE',
    'PAUSED'
);

CREATE TYPE order_status AS ENUM (
    'DRAFT',
    'PLACED',
    'CANCELLED',
    'FULFILLED'
);

CREATE TYPE payroll_entry_kind AS ENUM (
    'DEDUCTION',
    'ADJUSTMENT_DEBIT',
    'ADJUSTMENT_CREDIT',
    'REFUND'
);

CREATE TYPE payroll_source_kind AS ENUM (
    'ORDER_MUTATION',
    'DISPUTE_WORKFLOW',
    'SFTP_BATCH_EXPORT',
    'HR_API_SYNC_ADJUNCT'
);

CREATE TYPE audit_action AS ENUM (
    'INSERT',
    'UPDATE',
    'DELETE'
);

CREATE TABLE actor_account (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_external_id TEXT NOT NULL UNIQUE
        CHECK (actor_external_id <> '' AND actor_external_id = btrim(actor_external_id)),
    role actor_role NOT NULL,
    authentication_source authentication_source NOT NULL,
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE plant (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    plant_external_id TEXT NOT NULL UNIQUE
        CHECK (plant_external_id <> '' AND plant_external_id = btrim(plant_external_id)),
    name TEXT NOT NULL CHECK (name <> '' AND name = btrim(name)),
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE vendor (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_external_id TEXT NOT NULL UNIQUE
        CHECK (vendor_external_id <> '' AND vendor_external_id = btrim(vendor_external_id)),
    display_name TEXT NOT NULL CHECK (display_name <> '' AND display_name = btrim(display_name)),
    status vendor_status NOT NULL DEFAULT 'PENDING',
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE vendor_plant_service_window (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id global_pk NOT NULL REFERENCES vendor(id) ON DELETE CASCADE,
    plant_id global_pk NOT NULL REFERENCES plant(id) ON DELETE CASCADE,
    service_start_at_utc TIMESTAMPTZ NOT NULL,
    service_end_at_utc TIMESTAMPTZ NOT NULL,
    status service_window_status NOT NULL DEFAULT 'ACTIVE',
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (service_end_at_utc > service_start_at_utc),
    UNIQUE (vendor_id, plant_id, service_start_at_utc)
);

CREATE TABLE menu_item (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id global_pk NOT NULL REFERENCES vendor(id) ON DELETE CASCADE,
    menu_item_external_id TEXT NOT NULL
        CHECK (menu_item_external_id <> '' AND menu_item_external_id = btrim(menu_item_external_id)),
    name TEXT NOT NULL CHECK (name <> '' AND name = btrim(name)),
    price_minor money_minor NOT NULL CHECK (price_minor >= 0),
    currency currency_code NOT NULL DEFAULT 'TWD',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (vendor_id, menu_item_external_id)
);

CREATE TABLE employee_order (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    order_external_id TEXT NOT NULL UNIQUE
        CHECK (order_external_id <> '' AND order_external_id = btrim(order_external_id)),
    employee_actor_id global_pk NOT NULL REFERENCES actor_account(id) ON DELETE RESTRICT,
    vendor_id global_pk NOT NULL REFERENCES vendor(id) ON DELETE RESTRICT,
    plant_id global_pk NOT NULL REFERENCES plant(id) ON DELETE RESTRICT,
    status order_status NOT NULL DEFAULT 'DRAFT',
    subtotal_minor money_minor NOT NULL CHECK (subtotal_minor >= 0),
    discount_minor money_minor NOT NULL DEFAULT 0 CHECK (discount_minor >= 0),
    total_minor money_minor NOT NULL CHECK (total_minor >= 0),
    placed_at_utc TIMESTAMPTZ,
    created_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (discount_minor <= subtotal_minor),
    CHECK (total_minor = subtotal_minor - discount_minor)
);

CREATE TABLE employee_order_line_item (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id global_pk NOT NULL REFERENCES employee_order(id) ON DELETE CASCADE,
    menu_item_id global_pk NOT NULL REFERENCES menu_item(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price_minor money_minor NOT NULL CHECK (unit_price_minor >= 0),
    line_total_minor money_minor GENERATED ALWAYS AS (
        unit_price_minor::BIGINT * quantity::BIGINT
    ) STORED
);

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

CREATE TABLE audit_event (
    id global_pk PRIMARY KEY DEFAULT gen_random_uuid(),
    table_name TEXT NOT NULL CHECK (table_name <> '' AND table_name = btrim(table_name)),
    record_id global_pk NOT NULL,
    action audit_action NOT NULL,
    actor_external_id TEXT NOT NULL
        CHECK (actor_external_id <> '' AND actor_external_id = btrim(actor_external_id)),
    occurred_at_utc TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    payload JSONB NOT NULL,
    CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX menu_item_vendor_active_idx
    ON menu_item (vendor_id, is_active);

CREATE INDEX employee_order_employee_created_idx
    ON employee_order (employee_actor_id, created_at_utc DESC);

CREATE INDEX employee_order_vendor_placed_idx
    ON employee_order (vendor_id, placed_at_utc DESC);

CREATE INDEX payroll_ledger_entry_actor_occurred_idx
    ON payroll_ledger_entry (employee_actor_id, occurred_at_utc DESC);

CREATE INDEX audit_event_subject_occurred_idx
    ON audit_event (table_name, record_id, occurred_at_utc DESC);

CREATE OR REPLACE FUNCTION set_updated_at_utc()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at_utc = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$;

CREATE TRIGGER actor_account_set_updated_at_utc
BEFORE UPDATE ON actor_account
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();

CREATE TRIGGER plant_set_updated_at_utc
BEFORE UPDATE ON plant
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();

CREATE TRIGGER vendor_set_updated_at_utc
BEFORE UPDATE ON vendor
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();

CREATE TRIGGER vendor_plant_service_window_set_updated_at_utc
BEFORE UPDATE ON vendor_plant_service_window
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();

CREATE TRIGGER menu_item_set_updated_at_utc
BEFORE UPDATE ON menu_item
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();

CREATE TRIGGER employee_order_set_updated_at_utc
BEFORE UPDATE ON employee_order
FOR EACH ROW
EXECUTE FUNCTION set_updated_at_utc();

CREATE OR REPLACE FUNCTION append_audit_event()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    snapshot JSONB;
    subject_id global_pk;
    subject_action audit_action;
BEGIN
    IF TG_OP = 'DELETE' THEN
        snapshot = to_jsonb(OLD);
        subject_id = OLD.id;
        subject_action = 'DELETE'::audit_action;
    ELSIF TG_OP = 'UPDATE' THEN
        snapshot = jsonb_build_object(
            'before', to_jsonb(OLD),
            'after', to_jsonb(NEW)
        );
        subject_id = NEW.id;
        subject_action = 'UPDATE'::audit_action;
    ELSE
        snapshot = to_jsonb(NEW);
        subject_id = NEW.id;
        subject_action = 'INSERT'::audit_action;
    END IF;

    INSERT INTO audit_event (
        table_name,
        record_id,
        action,
        actor_external_id,
        payload
    )
    VALUES (
        TG_TABLE_NAME,
        subject_id,
        subject_action,
        COALESCE(NULLIF(current_setting('app.actor_external_id', TRUE), ''), 'system'),
        snapshot
    );

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER actor_account_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON actor_account
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER plant_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON plant
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER vendor_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON vendor
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER vendor_plant_service_window_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON vendor_plant_service_window
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER menu_item_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON menu_item
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER employee_order_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON employee_order
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER employee_order_line_item_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON employee_order_line_item
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE TRIGGER payroll_ledger_entry_append_audit_event
AFTER INSERT OR UPDATE OR DELETE ON payroll_ledger_entry
FOR EACH ROW
EXECUTE FUNCTION append_audit_event();

CREATE OR REPLACE FUNCTION enforce_append_only()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '55000',
        MESSAGE = format('table "%s" is append-only; "%s" is not allowed', TG_TABLE_NAME, TG_OP);
    RETURN NULL;
END;
$$;

CREATE TRIGGER audit_event_append_only_guard
BEFORE UPDATE OR DELETE ON audit_event
FOR EACH ROW
EXECUTE FUNCTION enforce_append_only();

CREATE TRIGGER audit_event_append_only_truncate_guard
BEFORE TRUNCATE ON audit_event
FOR EACH STATEMENT
EXECUTE FUNCTION enforce_append_only();

CREATE TRIGGER payroll_ledger_entry_append_only_guard
BEFORE UPDATE OR DELETE ON payroll_ledger_entry
FOR EACH ROW
EXECUTE FUNCTION enforce_append_only();

CREATE TRIGGER payroll_ledger_entry_append_only_truncate_guard
BEFORE TRUNCATE ON payroll_ledger_entry
FOR EACH STATEMENT
EXECUTE FUNCTION enforce_append_only();
