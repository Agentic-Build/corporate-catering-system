#!/usr/bin/env bash
# security-scan.sh — runs Trivy on built images and kubesec on K8s manifests.
# Outputs: ops/security/scan-report.txt
set -euo pipefail

REPORT="ops/security/scan-report.txt"
mkdir -p ops/security
echo "T-Bite security scan @ $(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$REPORT"

# Trivy on built images (requires built tbite/* images locally)
if command -v trivy >/dev/null 2>&1; then
  for img in tbite/api:dev tbite/web-employee:dev tbite/web-merchant:dev tbite/web-admin:dev; do
    echo "=== trivy $img ===" >> "$REPORT"
    trivy image --severity HIGH,CRITICAL --no-progress "$img" >> "$REPORT" 2>&1 || true
  done
else
  echo "trivy not installed; skipping image scan" >> "$REPORT"
fi

# kubesec on rendered single-node manifests
if command -v kubesec >/dev/null 2>&1; then
  kubectl kustomize ops/kubernetes/overlays/single-node > /tmp/single-node.yaml
  echo "=== kubesec ===" >> "$REPORT"
  kubesec scan /tmp/single-node.yaml >> "$REPORT" 2>&1 || true
else
  echo "kubesec not installed; skipping manifest scan" >> "$REPORT"
fi

echo "Report at: $REPORT"
