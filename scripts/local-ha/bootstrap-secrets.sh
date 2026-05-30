#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
state_dir="${LOCAL_HA_STATE_DIR:-.local-ha}"
secrets_file="${state_dir}/secrets.env"

command -v kubectl >/dev/null 2>&1 || {
  echo "kubectl is required" >&2
  exit 1
}
command -v openssl >/dev/null 2>&1 || {
  echo "openssl is required" >&2
  exit 1
}

mkdir -p "${state_dir}"
chmod 700 "${state_dir}"

random_hex() {
  local bytes="$1"
  openssl rand -hex "$bytes"
}

if [[ ! -f "${secrets_file}" ]]; then
  umask 077
  {
    printf 'VALKEY_PW=%s\n' "$(random_hex 24)"
    printf 'MINIO_SECRET=%s\n' "$(random_hex 24)"
    printf 'AUTHENTIK_SECRET_KEY=%s\n' "$(random_hex 48)"
    printf 'AUTHENTIK_BOOTSTRAP_PASSWORD=%s\n' "$(random_hex 18)"
    printf 'AUTHENTIK_BOOTSTRAP_TOKEN=%s\n' "$(random_hex 32)"
    printf 'AUTHENTIK_PG_PW=%s\n' "$(random_hex 24)"
    printf 'HYDRA_PG_PW=%s\n' "$(random_hex 24)"
    printf 'HYDRA_SYSTEM_SECRET=%s\n' "$(random_hex 32)"
    printf 'HYDRA_COOKIE_SECRET=%s\n' "$(random_hex 32)"
    printf 'GRAFANA_PW=%s\n' "$(random_hex 18)"
  } >"${secrets_file}"
fi

# shellcheck disable=SC1090
source "${secrets_file}"

kubectl get namespace "${namespace}" >/dev/null 2>&1 || kubectl create namespace "${namespace}" >/dev/null

apply_secret() {
  local name="$1"
  shift
  kubectl -n "${namespace}" create secret generic "${name}" "$@" --dry-run=client -o yaml |
    kubectl apply -f - >/dev/null
}

apply_secret tbite-valkey \
  --from-literal="password=${VALKEY_PW}" \
  --from-literal="valkey-password=${VALKEY_PW}"

apply_secret tbite-minio-root \
  --from-literal=accessKey=minio \
  --from-literal="secretKey=${MINIO_SECRET}"

apply_secret tbite-s3 \
  --from-literal=accessKeyID=minio \
  --from-literal="secretAccessKey=${MINIO_SECRET}"

apply_secret tbite-nats --from-literal=creds=""

apply_secret tbite-authentik \
  --from-literal="apiToken=${AUTHENTIK_BOOTSTRAP_TOKEN}"

apply_secret tbite-oidc-clients \
  --from-literal=apiClientID=tbite \
  --from-literal=apiClientSecret=local-api-client-secret \
  --from-literal=employeeClientID=tbite-employee \
  --from-literal=employeeClientSecret=local-employee-client-secret \
  --from-literal=merchantClientID=tbite-merchant \
  --from-literal=merchantClientSecret=local-merchant-client-secret \
  --from-literal=adminClientID=tbite-admin \
  --from-literal=adminClientSecret=local-admin-client-secret

apply_secret tbite-grafana-admin \
  --from-literal=admin-user=admin \
  --from-literal="password=${GRAFANA_PW}"

apply_secret tbite-authentik-config \
  --from-literal="AUTHENTIK_SECRET_KEY=${AUTHENTIK_SECRET_KEY}" \
  --from-literal=AUTHENTIK_POSTGRESQL__HOST=tbite-pg-rw.tbite.svc.cluster.local \
  --from-literal=AUTHENTIK_POSTGRESQL__NAME=authentik \
  --from-literal=AUTHENTIK_POSTGRESQL__USER=authentik \
  --from-literal="AUTHENTIK_POSTGRESQL__PASSWORD=${AUTHENTIK_PG_PW}" \
  --from-literal=AUTHENTIK_REDIS__HOST=tbite-valkey-primary.tbite.svc.cluster.local \
  --from-literal=AUTHENTIK_ERROR_REPORTING__ENABLED=false

apply_secret tbite-tbite-platform-authentik-bootstrap \
  --from-literal="AUTHENTIK_BOOTSTRAP_PASSWORD=${AUTHENTIK_BOOTSTRAP_PASSWORD}" \
  --from-literal="AUTHENTIK_BOOTSTRAP_TOKEN=${AUTHENTIK_BOOTSTRAP_TOKEN}" \
  --from-literal=AUTHENTIK_BOOTSTRAP_EMAIL=local-ha-admin@tbite.local

apply_secret tbite-hydra \
  --from-literal="dsn=postgres://hydra:${HYDRA_PG_PW}@tbite-pg-rw.tbite.svc.cluster.local:5432/hydra?sslmode=disable" \
  --from-literal="secretsSystem=${HYDRA_SYSTEM_SECRET}" \
  --from-literal="secretsCookie=${HYDRA_COOKIE_SECRET}" \
  --from-literal="postgresPassword=${HYDRA_PG_PW}"

apply_secret tbite-sops-age --from-literal=pub=local-ha-no-sops

echo "local HA secrets applied to namespace ${namespace}"
echo "secret material is stored in ${secrets_file}"
