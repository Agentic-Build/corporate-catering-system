#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
chart_dir="${CHART_DIR:-chart/tbite-platform}"
base_values="${BASE_VALUES:-${chart_dir}/values.yaml}"
local_values="${LOCAL_HA_VALUES:-${chart_dir}/values-local-ha.yaml}"
state_dir="${LOCAL_HA_STATE_DIR:-.local-ha}"
secrets_file="${state_dir}/secrets.env"
timeout="${HELM_TIMEOUT:-10m}"
requested_image_tag="${IMAGE_TAG:-}"
gateway_api_version="${GATEWAY_API_VERSION:-v1.2.1}"
install_gateway_api_crds="${INSTALL_GATEWAY_API_CRDS:-false}"

command -v kubectl >/dev/null 2>&1 || {
  echo "kubectl is required" >&2
  exit 1
}
command -v helm >/dev/null 2>&1 || {
  echo "helm is required" >&2
  exit 1
}

current_platform_image_tag() {
  local image
  image="$(
    kubectl -n "${namespace}" get deployment "${release}-tbite-platform-api" \
      -o jsonpath='{.spec.template.spec.containers[?(@.name=="api")].image}' 2>/dev/null || true
  )"
  if [[ -n "${image}" && "${image}" == *:* && "${image}" != *@* ]]; then
    printf '%s\n' "${image##*:}"
  fi
}

image_tag="${requested_image_tag}"
if [[ -z "${image_tag}" ]]; then
  image_tag="$(current_platform_image_tag)"
fi
if [[ -z "${image_tag}" ]]; then
  image_tag="local"
fi

scripts/local-ha/bootstrap-secrets.sh

# shellcheck disable=SC1090
source "${secrets_file}"

helm_args=(
  --namespace "${namespace}"
  --create-namespace
  -f "${base_values}"
  -f "${local_values}"
  --set-string "global.redisURL=redis://:${VALKEY_PW}@tbite-valkey-primary.${namespace}.svc.cluster.local:6379/0"
  --set-string "authentik.authentik.secret_key=${AUTHENTIK_SECRET_KEY}"
  --set-string "authentik.authentik.postgresql.password=${AUTHENTIK_PG_PW}"
  --set-string "hydra.hydra.config.dsn=postgres://hydra:${HYDRA_PG_PW}@tbite-pg-rw.${namespace}.svc.cluster.local:5432/hydra?sslmode=disable"
  --set-string "hydra.hydra.config.secrets.system[0]=${HYDRA_SYSTEM_SECRET}"
  --set-string "minio.tenant.tenant.configSecret.secretKey=${MINIO_SECRET}"
  --set-string "minio-tenant.tenant.configSecret.secretKey=${MINIO_SECRET}"
  --set-string "image.tag=${image_tag}"
)

release_crd_suppression_args=(
  --set cnpg-operator.crds.create=false
  --set keda.crds.install=false
  --set victoria-metrics-k8s-stack.victoria-metrics-operator.crds.enabled=false
  --set victoria-metrics-k8s-stack.victoria-metrics-operator.crds.plain=false
)

helm dependency build "${chart_dir}"

rendered_crd_phase="$(mktemp)"
crd_file="$(mktemp)"
rendered_release_crd_phase="$(mktemp)"
release_crd_file="$(mktemp)"
trap 'rm -f "${rendered_crd_phase}" "${crd_file}" "${rendered_release_crd_phase}" "${release_crd_file}"' EXIT

if [[ "${install_gateway_api_crds}" == "true" ]]; then
  kubectl apply --server-side=true --force-conflicts \
    -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/${gateway_api_version}/standard-install.yaml"
fi

helm template "${release}" "${chart_dir}" "${helm_args[@]}" \
  --include-crds \
  --set crdsReady=false \
  --set hooks.dbMigrate.enabled=false \
  --set hooks.provisionStreams.enabled=false \
  --set hooks.bucketBootstrap.enabled=false \
  --set hooks.createIdentityDatabases.enabled=false \
  >"${rendered_crd_phase}"

python3 - "${rendered_crd_phase}" "${crd_file}" <<'PY'
import sys
import yaml

source, target = sys.argv[1], sys.argv[2]
with open(source) as fh:
    docs = [
        doc
        for doc in yaml.safe_load_all(fh)
        if isinstance(doc, dict) and doc.get("kind") == "CustomResourceDefinition"
    ]
with open(target, "w") as out:
    yaml.safe_dump_all(docs, out, sort_keys=False)
PY

if grep -q '^kind: CustomResourceDefinition' "${crd_file}"; then
  kubectl apply --server-side=true --force-conflicts -f "${crd_file}"
fi

kubectl wait --for=condition=Established crd --all --timeout=180s

helm template "${release}" "${chart_dir}" "${helm_args[@]}" "${release_crd_suppression_args[@]}" \
  --skip-crds \
  --set crdsReady=true \
  --set hooks.dbMigrate.enabled=false \
  --set hooks.provisionStreams.enabled=false \
  --set hooks.bucketBootstrap.enabled=false \
  --set hooks.createIdentityDatabases.enabled=false \
  >"${rendered_release_crd_phase}"

python3 - "${rendered_release_crd_phase}" "${release_crd_file}" <<'PY'
import sys
import yaml

source, target = sys.argv[1], sys.argv[2]
with open(source) as fh:
    names = sorted(
        doc["metadata"]["name"]
        for doc in yaml.safe_load_all(fh)
        if isinstance(doc, dict)
        and doc.get("kind") == "CustomResourceDefinition"
        and isinstance(doc.get("metadata"), dict)
        and doc["metadata"].get("name")
    )
with open(target, "w") as out:
    if names:
        out.write("\n".join(names) + "\n")
PY

while IFS= read -r crd_name || [[ -n "${crd_name}" ]]; do
  [[ -n "${crd_name}" ]] || continue
  kubectl label crd "${crd_name}" app.kubernetes.io/managed-by=Helm --overwrite
  kubectl annotate crd "${crd_name}" \
    meta.helm.sh/release-name="${release}" \
    meta.helm.sh/release-namespace="${namespace}" \
    --overwrite
done <"${release_crd_file}"

echo "==> installing local HA release without hooks"
helm upgrade --install "${release}" "${chart_dir}" "${helm_args[@]}" "${release_crd_suppression_args[@]}" \
  --skip-crds \
  --set crdsReady=true \
  --set hooks.dbMigrate.enabled=false \
  --set hooks.provisionStreams.enabled=false \
  --set hooks.bucketBootstrap.enabled=false \
  --set hooks.createIdentityDatabases.enabled=false \
  --wait=false \
  --timeout="${timeout}"

echo "==> waiting for CNPG cluster"
deadline=$((SECONDS + 900))
while true; do
  phase="$(kubectl -n "${namespace}" get cluster tbite-pg -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  if [[ "${phase}" == *"healthy"* || "${phase}" == *"Healthy"* ]]; then
    break
  fi
  if (( SECONDS > deadline )); then
    kubectl -n "${namespace}" describe cluster tbite-pg >&2 || true
    echo "timed out waiting for tbite-pg to become healthy; phase=${phase}" >&2
    exit 1
  fi
  sleep 5
done

pg_url="$(kubectl -n "${namespace}" get secret tbite-pg-app -o jsonpath='{.data.uri}' | base64 -d)"
pooler_service="tbite-pg-pooler-rw"
pooler_deployment="tbite-pg-pooler-rw"
pooler_url="${pg_url/tbite-pg-rw/${pooler_service}}"
ro_url="${pg_url/tbite-pg-rw/tbite-pg-ro}"

echo "==> waiting for CNPG pooler"
kubectl -n "${namespace}" rollout status "deployment/${pooler_deployment}" --timeout="${timeout}"
deadline=$((SECONDS + 180))
while true; do
  pooler_ready_endpoints="$(
    kubectl -n "${namespace}" get endpointslice \
      -l "kubernetes.io/service-name=${pooler_service}" \
      -o json \
      | jq '[.items[].endpoints[]? | select(.conditions.ready == true)] | length'
  )"
  if (( pooler_ready_endpoints > 0 )); then
    break
  fi
  if (( SECONDS > deadline )); then
    kubectl -n "${namespace}" get endpointslice -l "kubernetes.io/service-name=${pooler_service}" -o wide >&2 || true
    echo "timed out waiting for ready endpoints on ${pooler_service}" >&2
    exit 1
  fi
  sleep 5
done

kubectl -n "${namespace}" create secret generic tbite-db \
  --from-literal="rwUrl=${pooler_url}" \
  --from-literal="roUrl=${ro_url}" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "==> enabling hooks and completing upgrade"
helm upgrade --install "${release}" "${chart_dir}" "${helm_args[@]}" "${release_crd_suppression_args[@]}" \
  --skip-crds \
  --set crdsReady=true \
  --wait=false \
  --timeout="${timeout}"

echo "local HA release submitted. Run scripts/local-ha/wait-ready.sh to wait for rollouts."
