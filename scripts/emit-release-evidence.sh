#!/usr/bin/env bash
set -euo pipefail

OUTPUT_PATH=""
OVERLAY_MANIFEST=""
OVERLAY_DIGEST_FILE=""
LOAD_SLO_REPORT_PATH="ops/observability/load/reports/prelaunch-slo-report.json"
STAGED_CAPACITY_REPORT_PATH="ops/observability/load/reports/staged-capacity-report.json"
DECISION_ISSUE_ID="ISS-005"
GATES_CSV="image-build,migration-check,e2e-smoke,load-gate,deploy"

usage() {
  cat <<'USAGE'
Usage: scripts/emit-release-evidence.sh \
  --overlay-manifest <path> \
  --overlay-digest-file <path> \
  --output <path> \
  [--load-slo-report <path>] \
  [--staged-capacity-report <path>] \
  [--decision-issue-id ISS-005] \
  [--gates gate1,gate2,...]

Emits machine-readable production release evidence.
USAGE
}

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "missing required command: ${cmd}" >&2
    exit 1
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --overlay-manifest)
      shift
      [[ $# -gt 0 ]] || {
        echo "--overlay-manifest requires a path" >&2
        exit 1
      }
      OVERLAY_MANIFEST="$1"
      shift
      ;;
    --overlay-digest-file)
      shift
      [[ $# -gt 0 ]] || {
        echo "--overlay-digest-file requires a path" >&2
        exit 1
      }
      OVERLAY_DIGEST_FILE="$1"
      shift
      ;;
    --output)
      shift
      [[ $# -gt 0 ]] || {
        echo "--output requires a path" >&2
        exit 1
      }
      OUTPUT_PATH="$1"
      shift
      ;;
    --load-slo-report)
      shift
      [[ $# -gt 0 ]] || {
        echo "--load-slo-report requires a path" >&2
        exit 1
      }
      LOAD_SLO_REPORT_PATH="$1"
      shift
      ;;
    --staged-capacity-report)
      shift
      [[ $# -gt 0 ]] || {
        echo "--staged-capacity-report requires a path" >&2
        exit 1
      }
      STAGED_CAPACITY_REPORT_PATH="$1"
      shift
      ;;
    --decision-issue-id)
      shift
      [[ $# -gt 0 ]] || {
        echo "--decision-issue-id requires a value" >&2
        exit 1
      }
      DECISION_ISSUE_ID="$1"
      shift
      ;;
    --gates)
      shift
      [[ $# -gt 0 ]] || {
        echo "--gates requires a CSV value" >&2
        exit 1
      }
      GATES_CSV="$1"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

[[ -n "${OVERLAY_MANIFEST}" ]] || {
  echo "--overlay-manifest is required" >&2
  exit 1
}
[[ -n "${OVERLAY_DIGEST_FILE}" ]] || {
  echo "--overlay-digest-file is required" >&2
  exit 1
}
[[ -n "${OUTPUT_PATH}" ]] || {
  echo "--output is required" >&2
  exit 1
}

[[ -f "${OVERLAY_MANIFEST}" ]] || {
  echo "overlay manifest not found: ${OVERLAY_MANIFEST}" >&2
  exit 1
}
[[ -f "${OVERLAY_DIGEST_FILE}" ]] || {
  echo "overlay digest file not found: ${OVERLAY_DIGEST_FILE}" >&2
  exit 1
}
[[ -f "${LOAD_SLO_REPORT_PATH}" ]] || {
  echo "load SLO report not found: ${LOAD_SLO_REPORT_PATH}" >&2
  exit 1
}
[[ -f "${STAGED_CAPACITY_REPORT_PATH}" ]] || {
  echo "staged capacity report not found: ${STAGED_CAPACITY_REPORT_PATH}" >&2
  exit 1
}

if [[ ! "${DECISION_ISSUE_ID}" =~ ^ISS-[0-9]{3}$ ]]; then
  echo "decision issue id must match ISS-000 format" >&2
  exit 1
fi

require_command sha256sum
require_command node

manifest_digest="$(sha256sum "${OVERLAY_MANIFEST}" | awk '{print $1}')"
digest_file_value="$(awk '{print $1}' "${OVERLAY_DIGEST_FILE}" | head -n 1 | tr -d '[:space:]')"
load_slo_digest="$(sha256sum "${LOAD_SLO_REPORT_PATH}" | awk '{print $1}')"
staged_capacity_digest="$(sha256sum "${STAGED_CAPACITY_REPORT_PATH}" | awk '{print $1}')"

if [[ -z "${digest_file_value}" ]]; then
  echo "overlay digest file is empty: ${OVERLAY_DIGEST_FILE}" >&2
  exit 1
fi
if [[ "${manifest_digest}" != "${digest_file_value}" ]]; then
  echo "overlay digest mismatch: manifest=${manifest_digest} digest_file=${digest_file_value}" >&2
  exit 1
fi

generated_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
commit_sha="${GITHUB_SHA:-$(git rev-parse HEAD)}"
ref_name="${GITHUB_REF_NAME:-$(git rev-parse --abbrev-ref HEAD)}"
run_id="${GITHUB_RUN_ID:-local}"
manifest_size="$(wc -c <"${OVERLAY_MANIFEST}" | tr -d '[:space:]')"
load_slo_report_size="$(wc -c <"${LOAD_SLO_REPORT_PATH}" | tr -d '[:space:]')"
staged_capacity_report_size="$(wc -c <"${STAGED_CAPACITY_REPORT_PATH}" | tr -d '[:space:]')"

mkdir -p "$(dirname "${OUTPUT_PATH}")"

export DECISION_ISSUE_ID
export GATES_CSV
export generated_at
export commit_sha
export ref_name
export run_id
export OVERLAY_MANIFEST
export OVERLAY_DIGEST_FILE
export manifest_digest
export manifest_size
export LOAD_SLO_REPORT_PATH
export STAGED_CAPACITY_REPORT_PATH
export load_slo_digest
export staged_capacity_digest
export load_slo_report_size
export staged_capacity_report_size

node >"${OUTPUT_PATH}" <<'NODE'
const gates = process.env.GATES_CSV.split(",")
  .map((value) => value.trim())
  .filter((value) => value.length > 0)
  .map((name) => ({ name, status: "passed" }));

if (gates.length === 0) {
  throw new Error("at least one gate is required");
}

const evidence = {
  decisionIssueId: process.env.DECISION_ISSUE_ID,
  generatedAt: process.env.generated_at,
  releaseTarget: "production",
  commitSha: process.env.commit_sha,
  refName: process.env.ref_name,
  runId: process.env.run_id,
  consumedArtifacts: {
    productionOverlayManifest: {
      path: process.env.OVERLAY_MANIFEST,
      sha256: process.env.manifest_digest,
      sizeBytes: Number(process.env.manifest_size)
    },
    productionOverlayDigest: {
      path: process.env.OVERLAY_DIGEST_FILE
    },
    loadSloReport: {
      path: process.env.LOAD_SLO_REPORT_PATH,
      sha256: process.env.load_slo_digest,
      sizeBytes: Number(process.env.load_slo_report_size)
    },
    stagedCapacityReport: {
      path: process.env.STAGED_CAPACITY_REPORT_PATH,
      sha256: process.env.staged_capacity_digest,
      sizeBytes: Number(process.env.staged_capacity_report_size)
    }
  },
  gates
};

process.stdout.write(`${JSON.stringify(evidence, null, 2)}\n`);
NODE

echo "release evidence emitted at ${OUTPUT_PATH}"
