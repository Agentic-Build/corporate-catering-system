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
    role_enum_count INTEGER;
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
    INTO role_enum_count
    FROM pg_enum e
    JOIN pg_type t ON t.oid = e.enumtypid
    WHERE t.typname = 'actor_role'
      AND e.enumlabel IN ('EMPLOYEE', 'VENDOR_OPERATOR', 'COMMITTEE_ADMIN', 'PAYROLL_OPERATOR');
    IF role_enum_count <> 4 THEN
        RAISE EXCEPTION 'enum invariant failed: actor_role enum labels mismatch';
    END IF;
END $$;

INSERT INTO actor_account (actor_external_id, role, authentication_source)
VALUES ('system-auditor', 'COMMITTEE_ADMIN', 'OAUTH_SERVICE_ACCOUNT')
ON CONFLICT (actor_external_id) DO NOTHING;

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
