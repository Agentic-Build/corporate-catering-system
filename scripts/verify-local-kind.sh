#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_DEVELOPMENT_FILE="${ROOT_DIR}/.env.development"
ENV_LOCAL_FILE="${ROOT_DIR}/.env.local"
OVERLAY_DIR="${ROOT_DIR}/ops/kubernetes/overlays/local-kind"
CLUSTER_NAME="corporate-catering-local"
KUBE_NAMESPACE="catering-local-kind"
API_LOCAL_PORT="18080"
MCP_LOCAL_PORT="18081"
WEB_LOCAL_PORT="3000"
KIND_CONTEXT="kind-${CLUSTER_NAME}"

usage() {
  cat <<'USAGE'
Usage: scripts/verify-local-kind.sh

Build local runtime/web images, deploy the local-kind Kubernetes overlay onto kind,
and verify the rollout against the OrbStack-backed local dependency stack.
USAGE
}

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
}

ensure_namespace_exists() {
  kubectl --context "${KIND_CONTEXT}" create namespace "${KUBE_NAMESPACE}" --dry-run=client -o yaml | \
    kubectl --context "${KIND_CONTEXT}" apply -f - >/dev/null

  local attempt=0
  until kubectl --context "${KIND_CONTEXT}" get namespace "${KUBE_NAMESPACE}" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [[ "${attempt}" -ge 30 ]]; then
      echo "timed out waiting for namespace ${KUBE_NAMESPACE} to exist" >&2
      exit 1
    fi
    sleep 1
  done
}

load_runtime_env() {
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

  # shellcheck disable=SC1090
  set -a
  source "${ENV_DEVELOPMENT_FILE}"
  # shellcheck disable=SC1090
  source "${ENV_LOCAL_FILE}"
  set +a
}

run_database_migrations() {
  if [[ -z "${DATABASE_RW_URL:-}" ]]; then
    echo "missing required env: DATABASE_RW_URL" >&2
    exit 1
  fi
  (
    cd "${ROOT_DIR}"
    DATABASE_URL="${DATABASE_RW_URL}" sqlx migrate run --source migrations
  )
}

ensure_local_dependencies() {
  "${ROOT_DIR}/scripts/setup-dev.sh" up
  run_database_migrations
}

ensure_kind_cluster() {
  if kind get clusters | rg -xq "${CLUSTER_NAME}"; then
    kind delete cluster --name "${CLUSTER_NAME}"
  fi
  kind create cluster --name "${CLUSTER_NAME}" --wait 120s
}

rewrite_loopback_url() {
  local raw_url="$1"
  RAW_URL="${raw_url}" python3 - <<'PY'
import os
from urllib.parse import urlsplit, urlunsplit

raw = os.environ["RAW_URL"]
parsed = urlsplit(raw)
if parsed.hostname not in {"127.0.0.1", "localhost"}:
    print(raw)
    raise SystemExit(0)

auth = ""
if parsed.username is not None:
    auth = parsed.username
    if parsed.password is not None:
        auth += f":{parsed.password}"
    auth += "@"

port = f":{parsed.port}" if parsed.port is not None else ""
netloc = f"{auth}host.docker.internal{port}"
print(urlunsplit((parsed.scheme, netloc, parsed.path, parsed.query, parsed.fragment)))
PY
}

build_images() {
  local build_database_url
  local mock_auth_signing_secret
  build_database_url="$(rewrite_loopback_url "${DATABASE_RW_URL}")"
  mock_auth_signing_secret="${MOCK_AUTH_SIGNING_SECRET:-clar-002-ci-mock-auth-signing-secret}"

  docker build \
    --build-arg "DATABASE_URL=${build_database_url}" \
    -f "${ROOT_DIR}/Dockerfile.system" \
    -t corporate-catering/system:dev \
    "${ROOT_DIR}"
  docker build \
    --build-arg "MOCK_AUTH_SIGNING_SECRET=${mock_auth_signing_secret}" \
    -f "${ROOT_DIR}/Dockerfile.web" \
    -t corporate-catering/web:dev \
    "${ROOT_DIR}"
  kind load docker-image --name "${CLUSTER_NAME}" corporate-catering/system:dev corporate-catering/web:dev
}

create_runtime_secret() {
  local secret_env_file
  secret_env_file="$(mktemp -t corporate-catering-kind-secret.XXXXXX.env)"
  python3 - <<'PY' >"${secret_env_file}"
import os
from urllib.parse import urlsplit, urlunsplit


def rewrite_host_url(raw: str) -> str:
    parsed = urlsplit(raw)
    if parsed.hostname not in {"127.0.0.1", "localhost"}:
        return raw

    auth = ""
    if parsed.username is not None:
        auth = parsed.username
        if parsed.password is not None:
            auth += f":{parsed.password}"
        auth += "@"

    port = f":{parsed.port}" if parsed.port is not None else ""
    netloc = f"{auth}host.docker.internal{port}"
    return urlunsplit((parsed.scheme, netloc, parsed.path, parsed.query, parsed.fragment))


def rewrite_host_endpoint(raw: str) -> str:
    trimmed = raw.strip()
    if trimmed.startswith("127.0.0.1:"):
        return "host.docker.internal:" + trimmed.split(":", 1)[1]
    if trimmed.startswith("localhost:"):
        return "host.docker.internal:" + trimmed.split(":", 1)[1]
    if trimmed.startswith("http://127.0.0.1:") or trimmed.startswith("https://127.0.0.1:"):
        scheme, rest = trimmed.split("://", 1)
        return f"{scheme}://host.docker.internal:{rest.split(':', 1)[1]}"
    if trimmed.startswith("http://localhost:") or trimmed.startswith("https://localhost:"):
        scheme, rest = trimmed.split("://", 1)
        return f"{scheme}://host.docker.internal:{rest.split(':', 1)[1]}"
    return trimmed


env = os.environ
mock_auth_signing_secret = env.get("MOCK_AUTH_SIGNING_SECRET", "").strip() or "clar-002-ci-mock-auth-signing-secret"
entries = {
    "database_rw_url": rewrite_host_url(env["DATABASE_RW_URL"]),
    "database_ro_url": rewrite_host_url(env["DATABASE_RO_URL"]),
    "valkey_url": rewrite_host_url(env["VALKEY_URL"]),
    "nats_url": rewrite_host_url(env["NATS_URL"]),
    "corporate_sso_jwt_issuer": env["CORPORATE_SSO_JWT_ISSUER"],
    "corporate_sso_jwt_audience": env["CORPORATE_SSO_JWT_AUDIENCE"],
    "corporate_sso_jwt_hs256_secret_base64_current": env["CORPORATE_SSO_JWT_HS256_SECRET_BASE64"],
    "corporate_sso_jwt_hs256_secret_base64_next": env["CORPORATE_SSO_JWT_HS256_SECRET_BASE64_NEXT"],
    "vendor_mfa_jwt_issuer": env["VENDOR_MFA_JWT_ISSUER"],
    "vendor_mfa_jwt_audience": env["VENDOR_MFA_JWT_AUDIENCE"],
    "vendor_mfa_jwt_hs256_secret_base64_current": env["VENDOR_MFA_JWT_HS256_SECRET_BASE64"],
    "vendor_mfa_jwt_hs256_secret_base64_next": env["VENDOR_MFA_JWT_HS256_SECRET_BASE64_NEXT"],
    "mcp_oauth_service_account_issuer": env["MCP_OAUTH_SERVICE_ACCOUNT_ISSUER"],
    "mcp_oauth_service_account_audience": env["MCP_OAUTH_SERVICE_ACCOUNT_AUDIENCE"],
    "mcp_oauth_service_account_hs256_secret_base64_current": env["MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64"],
    "mcp_oauth_service_account_hs256_secret_base64_next": env["MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64_NEXT"],
    "mcp_bridge_key_registry_json": env["MCP_BRIDGE_KEY_REGISTRY_JSON"],
    "pickup_totp_secret": env["PRELAUNCH_PICKUP_TOTP_SECRET"],
    "mock_auth_signing_secret": mock_auth_signing_secret,
    "prelaunch_audit_trail_encryption_key_hex": env["PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_HEX"],
    "prelaunch_payroll_export_encryption_key_hex": env["PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_HEX"],
    "object_storage_endpoint": rewrite_host_endpoint(env["MINIO_ENDPOINT"]),
    "object_storage_access_key_id": env["MINIO_ROOT_USER"],
    "object_storage_secret_access_key": env["MINIO_ROOT_PASSWORD"],
    "object_storage_bucket_menu_images": env["MINIO_BUCKET_MENU_IMAGES"],
    "object_storage_bucket_compliance_evidence": env["MINIO_BUCKET_COMPLIANCE_EVIDENCE"],
    "object_storage_bucket_fulfillment_exports": env["MINIO_BUCKET_FULFILLMENT_EXPORTS"],
    "object_storage_region": env["OBJECT_STORAGE_REGION"],
    "object_storage_key_namespace": env["OBJECT_STORAGE_KEY_NAMESPACE"],
    "web_origin": "http://127.0.0.1:3000",
    "web_public_api_base_url": "http://127.0.0.1:18080",
}

for key, value in entries.items():
    print(f"{key}={value}")
PY

  ensure_namespace_exists
  kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" create secret generic corporate-catering-secrets \
    --from-env-file="${secret_env_file}" \
    --dry-run=client \
    -o yaml | kubectl --context "${KIND_CONTEXT}" apply -f -

  rm -f "${secret_env_file}"
}

deploy_overlay() {
  kustomize build "${OVERLAY_DIR}" | kubectl --context "${KIND_CONTEXT}" apply -f -
}

wait_for_rollout() {
  kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" wait --for=condition=complete job/corporate-catering-object-storage-provision --timeout=180s
  for deployment in \
    corporate-catering-api \
    corporate-catering-mcp \
    corporate-catering-compliance-worker \
    corporate-catering-web
  do
    kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" rollout status "deployment/${deployment}" --timeout=300s
  done
}

verify_host_connectivity() {
  ensure_namespace_exists
  kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" run host-connectivity-check \
    --image=alpine:3.20 \
    --restart=Never \
    --overrides='{"spec":{"automountServiceAccountToken":false}}' \
    --command -- sh -lc 'getent hosts host.docker.internal && nc -zvw5 host.docker.internal 5432 && nc -zvw5 host.docker.internal 6379 && nc -zvw5 host.docker.internal 4222 && nc -zvw5 host.docker.internal 9000 && nc -zvw5 host.docker.internal 4317'
  kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" wait --for=condition=Ready pod/host-connectivity-check --timeout=120s >/dev/null 2>&1 || true
  kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" logs pod/host-connectivity-check >/dev/null
  kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" delete pod/host-connectivity-check --ignore-not-found >/dev/null
}

start_port_forward() {
  local target="$1"
  local local_port="$2"
  local remote_port="$3"
  local logfile="$4"

  kubectl --context "${KIND_CONTEXT}" -n "${KUBE_NAMESPACE}" port-forward "${target}" "${local_port}:${remote_port}" >"${logfile}" 2>&1 &
  echo $!
}

wait_for_http() {
  local url="$1"
  local attempt=0
  until curl --fail --silent --show-error "${url}" >/dev/null; do
    attempt=$((attempt + 1))
    if [[ "${attempt}" -ge 30 ]]; then
      echo "timed out waiting for ${url}" >&2
      exit 1
    fi
    sleep 1
  done
}

main() {
  if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
  fi

  require_command docker
  require_command kind
  require_command kubectl
  require_command kustomize
  require_command rg
  require_command curl
  require_command python3
  require_command sqlx

  local previous_context
  previous_context="$(kubectl config current-context 2>/dev/null || true)"
  local api_port_forward_pid=""
  local mcp_port_forward_pid=""
  local web_port_forward_pid=""
  local api_log=""
  local mcp_log=""
  local web_log=""

  cleanup() {
    if [[ -n "${api_port_forward_pid:-}" ]]; then
      kill "${api_port_forward_pid}" >/dev/null 2>&1 || true
    fi
    if [[ -n "${mcp_port_forward_pid:-}" ]]; then
      kill "${mcp_port_forward_pid}" >/dev/null 2>&1 || true
    fi
    if [[ -n "${web_port_forward_pid:-}" ]]; then
      kill "${web_port_forward_pid}" >/dev/null 2>&1 || true
    fi
    if [[ -n "${previous_context:-}" ]] && kubectl config get-contexts -o name | rg -xq "${previous_context}"; then
      kubectl config use-context "${previous_context}" >/dev/null 2>&1 || true
    fi
    rm -f "${api_log:-}" "${mcp_log:-}" "${web_log:-}"
  }
  trap cleanup EXIT

  cd "${ROOT_DIR}"
  load_runtime_env
  ensure_local_dependencies
  ensure_kind_cluster
  build_images
  verify_host_connectivity
  create_runtime_secret
  deploy_overlay
  wait_for_rollout

  api_log="$(mktemp -t corporate-catering-api-port-forward.XXXXXX.log)"
  mcp_log="$(mktemp -t corporate-catering-mcp-port-forward.XXXXXX.log)"
  web_log="$(mktemp -t corporate-catering-web-port-forward.XXXXXX.log)"

  api_port_forward_pid="$(start_port_forward svc/corporate-catering-api "${API_LOCAL_PORT}" 80 "${api_log}")"
  mcp_port_forward_pid="$(start_port_forward svc/corporate-catering-mcp "${MCP_LOCAL_PORT}" 80 "${mcp_log}")"
  web_port_forward_pid="$(start_port_forward svc/corporate-catering-web "${WEB_LOCAL_PORT}" 80 "${web_log}")"

  wait_for_http "http://127.0.0.1:${API_LOCAL_PORT}/health/ready"
  wait_for_http "http://127.0.0.1:${MCP_LOCAL_PORT}/health/ready"
  wait_for_http "http://127.0.0.1:${WEB_LOCAL_PORT}/"

  curl --fail --silent --show-error "http://127.0.0.1:${API_LOCAL_PORT}/health/live" >/dev/null
  curl --fail --silent --show-error "http://127.0.0.1:${MCP_LOCAL_PORT}/health/live" >/dev/null
  curl --fail --silent --show-error "http://127.0.0.1:${WEB_LOCAL_PORT}/" >/dev/null

  cat <<EOF
local-kind verification succeeded
cluster: ${CLUSTER_NAME}
namespace: ${KUBE_NAMESPACE}
api: http://127.0.0.1:${API_LOCAL_PORT}
mcp: http://127.0.0.1:${MCP_LOCAL_PORT}
web: http://127.0.0.1:${WEB_LOCAL_PORT}
EOF
}

main "$@"
