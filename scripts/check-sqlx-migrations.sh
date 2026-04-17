#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${ROOT_DIR}/ops/local/docker-compose.dev.yml"
ENV_DEVELOPMENT_FILE="${ROOT_DIR}/.env.development"
ENV_LOCAL_FILE="${ROOT_DIR}/.env.local"

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
}

ensure_env_files() {
  if [[ ! -f "${ENV_DEVELOPMENT_FILE}" ]]; then
    echo "missing required env baseline: ${ENV_DEVELOPMENT_FILE}" >&2
    exit 1
  fi
  if [[ ! -f "${ENV_LOCAL_FILE}" ]]; then
    cat >"${ENV_LOCAL_FILE}" <<'LOCAL'
# Local-only overrides for development.
# This file is ignored by git.
LOCAL
  fi
}

load_runtime_env() {
  # shellcheck disable=SC1090
  set -a
  source "${ENV_DEVELOPMENT_FILE}"
  # shellcheck disable=SC1090
  source "${ENV_LOCAL_FILE}"
  set +a
}

compose() {
  docker compose \
    --project-name "${DEV_STACK_PROJECT_NAME}" \
    --env-file "${ENV_DEVELOPMENT_FILE}" \
    --env-file "${ENV_LOCAL_FILE}" \
    -f "${COMPOSE_FILE}" \
    "$@"
}

psql_admin() {
  compose exec -T postgres \
    psql \
    -U "${POSTGRES_USER}" \
    -d postgres \
    -v ON_ERROR_STOP=1 \
    "$@"
}

psql_db() {
  local db_name="$1"
  shift
  compose exec -T postgres \
    psql \
    -U "${POSTGRES_USER}" \
    -d "${db_name}" \
    -v ON_ERROR_STOP=1 \
    "$@"
}

assert_foundation_invariants() {
  local migration_database_name="$1"
  psql_db "${migration_database_name}" <<'SQL'
DO $$
DECLARE
    float_minor_columns INTEGER;
    non_money_minor_columns INTEGER;
    utc_columns_count INTEGER;
    non_utc_timestamptz_columns INTEGER;
    non_global_pk_primary_keys INTEGER;
    projection_pk_columns INTEGER;
    projection_text_pk_columns INTEGER;
    role_enum_count INTEGER;
    authentication_source_enum_count INTEGER;
    vendor_status_enum_count INTEGER;
    service_window_status_enum_count INTEGER;
    order_status_enum_count INTEGER;
    payroll_entry_kind_enum_count INTEGER;
    payroll_source_kind_enum_count INTEGER;
    audit_action_enum_count INTEGER;
BEGIN
    SELECT COUNT(*)
    INTO float_minor_columns
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND column_name LIKE '%minor%'
      AND udt_name IN ('float4', 'float8');
    IF float_minor_columns <> 0 THEN
        RAISE EXCEPTION 'money invariant failed: found % float minor columns', float_minor_columns;
    END IF;

    SELECT COUNT(*)
    INTO non_money_minor_columns
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND (table_name, column_name) IN (
          ('menu_item', 'price_minor'),
          ('employee_order', 'subtotal_minor'),
          ('employee_order', 'discount_minor'),
          ('employee_order', 'total_minor'),
          ('employee_order_line_item', 'unit_price_minor'),
          ('employee_order_line_item', 'line_total_minor'),
          ('payroll_ledger_entry', 'amount_minor')
      )
      AND domain_name <> 'money_minor';
    IF non_money_minor_columns <> 0 THEN
        RAISE EXCEPTION 'money invariant failed: expected money_minor domain on all minor-unit columns';
    END IF;

    SELECT COUNT(*)
    INTO utc_columns_count
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND column_name LIKE '%\_utc' ESCAPE '\';
    IF utc_columns_count = 0 THEN
        RAISE EXCEPTION 'utc invariant failed: no *_utc columns found';
    END IF;

    SELECT COUNT(*)
    INTO non_utc_timestamptz_columns
    FROM information_schema.columns
    WHERE table_schema = 'public'
      AND column_name LIKE '%\_utc' ESCAPE '\'
      AND data_type <> 'timestamp with time zone';
    IF non_utc_timestamptz_columns <> 0 THEN
        RAISE EXCEPTION 'utc invariant failed: *_utc columns must use TIMESTAMPTZ';
    END IF;

    SELECT COUNT(*)
    INTO non_global_pk_primary_keys
    FROM information_schema.table_constraints tc
    JOIN information_schema.key_column_usage kcu
      ON tc.constraint_name = kcu.constraint_name
     AND tc.table_schema = kcu.table_schema
     AND tc.table_name = kcu.table_name
    JOIN information_schema.columns c
      ON c.table_schema = kcu.table_schema
     AND c.table_name = kcu.table_name
     AND c.column_name = kcu.column_name
    WHERE tc.table_schema = 'public'
      AND tc.constraint_type = 'PRIMARY KEY'
      AND tc.table_name <> '_sqlx_migrations'
      AND NOT (
          tc.table_name = 'order_state_event_projection'
          AND kcu.column_name = 'order_id'
      )
      AND (c.domain_name <> 'global_pk' OR c.udt_name <> 'uuid');
    IF non_global_pk_primary_keys <> 0 THEN
        RAISE EXCEPTION 'pk invariant failed: all primary keys must use global_pk/uuid except order_state_event_projection(order_id)';
    END IF;

    SELECT COUNT(*)
    INTO projection_pk_columns
    FROM information_schema.table_constraints tc
    JOIN information_schema.key_column_usage kcu
      ON tc.constraint_name = kcu.constraint_name
     AND tc.table_schema = kcu.table_schema
     AND tc.table_name = kcu.table_name
    WHERE tc.table_schema = 'public'
      AND tc.table_name = 'order_state_event_projection'
      AND tc.constraint_type = 'PRIMARY KEY';
    IF projection_pk_columns <> 1 THEN
        RAISE EXCEPTION 'pk invariant failed: order_state_event_projection must define exactly one primary key column';
    END IF;

    SELECT COUNT(*)
    INTO projection_text_pk_columns
    FROM information_schema.table_constraints tc
    JOIN information_schema.key_column_usage kcu
      ON tc.constraint_name = kcu.constraint_name
     AND tc.table_schema = kcu.table_schema
     AND tc.table_name = kcu.table_name
    JOIN information_schema.columns c
      ON c.table_schema = kcu.table_schema
     AND c.table_name = kcu.table_name
     AND c.column_name = kcu.column_name
    WHERE tc.table_schema = 'public'
      AND tc.table_name = 'order_state_event_projection'
      AND tc.constraint_type = 'PRIMARY KEY'
      AND kcu.column_name = 'order_id'
      AND c.udt_name = 'text';
    IF projection_text_pk_columns <> 1 THEN
        RAISE EXCEPTION 'pk invariant failed: order_state_event_projection primary key must be order_id TEXT';
    END IF;

    SELECT COUNT(*)
    INTO role_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'actor_role'
      AND e.enumlabel IN ('EMPLOYEE', 'VENDOR_OPERATOR', 'COMMITTEE_ADMIN', 'PAYROLL_OPERATOR');
    IF role_enum_count <> 4 THEN
        RAISE EXCEPTION 'enum invariant failed: actor_role enum labels mismatch';
    END IF;

    SELECT COUNT(*)
    INTO authentication_source_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'authentication_source'
      AND e.enumlabel IN ('CORPORATE_SSO', 'VENDOR_ACCOUNT_MFA', 'OAUTH_SERVICE_ACCOUNT');
    IF authentication_source_enum_count <> 3 THEN
        RAISE EXCEPTION 'enum invariant failed: authentication_source enum labels mismatch';
    END IF;

    SELECT COUNT(*)
    INTO vendor_status_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'vendor_status'
      AND e.enumlabel IN ('PENDING', 'ACTIVE', 'SUSPENDED', 'REJECTED');
    IF vendor_status_enum_count <> 4 THEN
        RAISE EXCEPTION 'enum invariant failed: vendor_status enum labels mismatch';
    END IF;

    SELECT COUNT(*)
    INTO service_window_status_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'service_window_status'
      AND e.enumlabel IN ('ACTIVE', 'PAUSED');
    IF service_window_status_enum_count <> 2 THEN
        RAISE EXCEPTION 'enum invariant failed: service_window_status enum labels mismatch';
    END IF;

    SELECT COUNT(*)
    INTO order_status_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'order_status'
      AND e.enumlabel IN ('DRAFT', 'PLACED', 'CANCELLED', 'FULFILLED');
    IF order_status_enum_count <> 4 THEN
        RAISE EXCEPTION 'enum invariant failed: order_status enum labels mismatch';
    END IF;

    SELECT COUNT(*)
    INTO payroll_entry_kind_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'payroll_entry_kind'
      AND e.enumlabel IN ('DEDUCTION', 'ADJUSTMENT_DEBIT', 'ADJUSTMENT_CREDIT', 'REFUND');
    IF payroll_entry_kind_enum_count <> 4 THEN
        RAISE EXCEPTION 'enum invariant failed: payroll_entry_kind enum labels mismatch';
    END IF;

    SELECT COUNT(*)
    INTO payroll_source_kind_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'payroll_source_kind'
      AND e.enumlabel IN ('ORDER_MUTATION', 'DISPUTE_WORKFLOW', 'SFTP_BATCH_EXPORT', 'HR_API_SYNC_ADJUNCT');
    IF payroll_source_kind_enum_count <> 4 THEN
        RAISE EXCEPTION 'enum invariant failed: payroll_source_kind enum labels mismatch';
    END IF;

    SELECT COUNT(*)
    INTO audit_action_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'audit_action'
      AND e.enumlabel IN ('INSERT', 'UPDATE', 'DELETE');
    IF audit_action_enum_count <> 3 THEN
        RAISE EXCEPTION 'enum invariant failed: audit_action enum labels mismatch';
    END IF;
END $$;

INSERT INTO actor_account (actor_external_id, role, authentication_source)
VALUES ('system-auditor', 'COMMITTEE_ADMIN', 'OAUTH_SERVICE_ACCOUNT')
ON CONFLICT (actor_external_id) DO NOTHING;

INSERT INTO actor_account (actor_external_id, role, authentication_source)
VALUES ('employee-payroll-check', 'EMPLOYEE', 'CORPORATE_SSO');

INSERT INTO plant (plant_external_id, name)
VALUES ('plant-payroll-check', 'Plant Payroll Check');

INSERT INTO vendor (vendor_external_id, display_name, status)
VALUES ('vendor-payroll-check', 'Vendor Payroll Check', 'ACTIVE');

INSERT INTO menu_item (
    vendor_id,
    menu_item_external_id,
    name,
    price_minor,
    currency,
    is_active
)
VALUES (
    (SELECT id FROM vendor WHERE vendor_external_id = 'vendor-payroll-check'),
    'menu-payroll-check',
    'Payroll Check Bento',
    100,
    'TWD',
    TRUE
);

INSERT INTO employee_order (
    order_external_id,
    employee_actor_id,
    vendor_id,
    plant_id,
    status,
    subtotal_minor,
    discount_minor,
    total_minor,
    placed_at_utc
)
VALUES (
    'order-payroll-check',
    (SELECT id FROM actor_account WHERE actor_external_id = 'employee-payroll-check'),
    (SELECT id FROM vendor WHERE vendor_external_id = 'vendor-payroll-check'),
    (SELECT id FROM plant WHERE plant_external_id = 'plant-payroll-check'),
    'PLACED',
    100,
    0,
    100,
    CURRENT_TIMESTAMP
);

INSERT INTO payroll_ledger_entry (
    order_id,
    employee_actor_id,
    source_kind,
    entry_kind,
    amount_minor,
    currency,
    metadata
)
VALUES (
    (SELECT id FROM employee_order WHERE order_external_id = 'order-payroll-check'),
    (SELECT id FROM actor_account WHERE actor_external_id = 'employee-payroll-check'),
    'ORDER_MUTATION',
    'DEDUCTION',
    100,
    'TWD',
    '{}'::jsonb
);

INSERT INTO audit_event (table_name, record_id, action, actor_external_id, payload)
VALUES (
    'audit_event',
    (SELECT id FROM actor_account WHERE actor_external_id = 'system-auditor'),
    'INSERT',
    'system-auditor',
    '{}'::jsonb
);

DO $$
BEGIN
    BEGIN
        UPDATE audit_event
        SET actor_external_id = 'tamper'
        WHERE actor_external_id = 'system-auditor';
        RAISE EXCEPTION 'append-only invariant failed: update unexpectedly succeeded';
    EXCEPTION
        WHEN OTHERS THEN
            IF SQLSTATE <> '55000' THEN
                RAISE;
            END IF;
    END;

    BEGIN
        DELETE FROM audit_event
        WHERE actor_external_id = 'system-auditor';
        RAISE EXCEPTION 'append-only invariant failed: delete unexpectedly succeeded';
    EXCEPTION
        WHEN OTHERS THEN
            IF SQLSTATE <> '55000' THEN
                RAISE;
            END IF;
    END;

    BEGIN
        EXECUTE 'TRUNCATE TABLE audit_event';
        RAISE EXCEPTION 'append-only invariant failed: truncate unexpectedly succeeded';
    EXCEPTION
        WHEN OTHERS THEN
            IF SQLSTATE <> '55000' THEN
                RAISE;
            END IF;
    END;
END $$;

DO $$
BEGIN
    BEGIN
        UPDATE payroll_ledger_entry
        SET amount_minor = amount_minor + 1
        WHERE order_id = (SELECT id FROM employee_order WHERE order_external_id = 'order-payroll-check');
        RAISE EXCEPTION 'append-only invariant failed: payroll update unexpectedly succeeded';
    EXCEPTION
        WHEN OTHERS THEN
            IF SQLSTATE <> '55000' THEN
                RAISE;
            END IF;
    END;

    BEGIN
        DELETE FROM payroll_ledger_entry
        WHERE order_id = (SELECT id FROM employee_order WHERE order_external_id = 'order-payroll-check');
        RAISE EXCEPTION 'append-only invariant failed: payroll delete unexpectedly succeeded';
    EXCEPTION
        WHEN OTHERS THEN
            IF SQLSTATE <> '55000' THEN
                RAISE;
            END IF;
    END;

    BEGIN
        EXECUTE 'TRUNCATE TABLE payroll_ledger_entry';
        RAISE EXCEPTION 'append-only invariant failed: payroll truncate unexpectedly succeeded';
    EXCEPTION
        WHEN OTHERS THEN
            IF SQLSTATE <> '55000' THEN
                RAISE;
            END IF;
    END;
END $$;
SQL
}

assert_revert_is_clean() {
  local migration_database_name="$1"
  psql_db "${migration_database_name}" <<'SQL'
DO $$
DECLARE
    residual_tables INTEGER;
BEGIN
    SELECT COUNT(*)
    INTO residual_tables
    FROM information_schema.tables
    WHERE table_schema = 'public'
      AND table_type = 'BASE TABLE'
      AND table_name <> '_sqlx_migrations';
    IF residual_tables <> 0 THEN
        RAISE EXCEPTION 'revert left % residual application tables', residual_tables;
    END IF;
END $$;
SQL
}

require_command docker
require_command sqlx

ensure_env_files
load_runtime_env

compose up -d postgres --wait

schema_test_db="${POSTGRES_DB}_schema_check"
migration_database_url="postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${schema_test_db}"

cleanup() {
  psql_admin -c "DROP DATABASE IF EXISTS ${schema_test_db} WITH (FORCE);" >/dev/null
}

trap cleanup EXIT

cleanup
psql_admin -c "CREATE DATABASE ${schema_test_db};" >/dev/null

pushd "${ROOT_DIR}" >/dev/null
DATABASE_URL="${migration_database_url}" sqlx migrate run --source migrations
assert_foundation_invariants "${schema_test_db}"
DATABASE_URL="${migration_database_url}" sqlx migrate revert --source migrations --target-version 0
assert_revert_is_clean "${schema_test_db}"
popd >/dev/null

echo "sqlx migration foundation check passed."
