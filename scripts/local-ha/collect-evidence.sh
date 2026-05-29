#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out_dir="${OUT_DIR:-tmp/local-ha/${stamp}}"

mkdir -p "${out_dir}"

run() {
  local name="$1"
  shift
  {
    printf '$'
    printf ' %q' "$@"
    printf '\n\n'
    "$@" 2>&1 || true
  } >"${out_dir}/${name}.txt"
}

run nodes kubectl get nodes -o wide --show-labels
run pods kubectl -n "${namespace}" get pods -o wide
run deployments kubectl -n "${namespace}" get deploy -o wide
run statefulsets kubectl -n "${namespace}" get statefulset -o wide
run daemonsets kubectl -n "${namespace}" get daemonset -o wide
run hpa kubectl -n "${namespace}" get hpa -o wide
run hpa-yaml kubectl -n "${namespace}" get hpa -o yaml
run metrics-server kubectl -n kube-system get deploy,pod -l k8s-app=metrics-server -o wide
run metrics-apiservice kubectl get apiservice v1beta1.metrics.k8s.io -o yaml
run external-metrics-apiservice kubectl get apiservice v1beta1.external.metrics.k8s.io -o yaml
run minio-pods kubectl -n "${namespace}" get pods -l v1.min.io/tenant=tbite -o wide
run minio-endpoints kubectl -n "${namespace}" get endpoints minio -o yaml
run observability-crs kubectl -n "${namespace}" get vmagent,vmsingle,vmalert,vmalertmanager,vlsingle,vtsingle -o wide
run observability-scrapes kubectl -n "${namespace}" get vmservicescrape,vmpodscrape -o wide
run observability-pods kubectl -n "${namespace}" get pods -l app.kubernetes.io/component=monitoring -o wide
run observability-endpoints kubectl -n "${namespace}" get endpoints "${release}-opentelemetry-collector" "${release}-victoria-logs-single-server" "${release}-vt-single-server" "vmsingle-${release}-victoria-metrics-k8s-stack" -o wide
run observability-otel-config kubectl -n "${namespace}" get configmap "${release}-opentelemetry-collector" -o yaml
run observability-vector kubectl -n "${namespace}" get daemonset,pod -l app.kubernetes.io/name=vector -o wide
run observability-vector-config kubectl -n "${namespace}" get configmap vector-vl-config -o yaml
run scaledobjects kubectl -n "${namespace}" get scaledobject -o wide
run pdb kubectl -n "${namespace}" get pdb -o wide
run cnpg kubectl -n "${namespace}" get cluster,pooler -o wide
run events kubectl -n "${namespace}" get events --sort-by=.lastTimestamp
run top-nodes kubectl top nodes
run top-pods kubectl -n "${namespace}" top pods

echo "evidence written to ${out_dir}"
