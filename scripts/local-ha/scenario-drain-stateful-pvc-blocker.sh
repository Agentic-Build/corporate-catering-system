#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${NAMESPACE:-tbite}"
release="${RELEASE:-tbite}"
target_statefulset="${TARGET_STATEFULSET:-${release}-nats}"
pods_file="$(mktemp -t tbite-stateful-pvc-pods.XXXXXX.json)"
pvs_file="$(mktemp -t tbite-stateful-pvc-pvs.XXXXXX.json)"

cleanup() {
  rm -f "${pods_file}" "${pvs_file}"
}
trap cleanup EXIT

kubectl -n "${namespace}" get pods -o json >"${pods_file}"
kubectl get pv -o json >"${pvs_file}"

target="$(
  jq -r -n \
    --arg statefulset "${target_statefulset}" \
    --slurpfile podsFile "${pods_file}" \
    --slurpfile pvsFile "${pvs_file}" '
      ($podsFile[0]) as $pods
      | ($pvsFile[0]) as $pvs
      |
      def pv_node($pv):
        [$pv.spec.nodeAffinity.required.nodeSelectorTerms[]?.matchExpressions[]?
         | select(.key == "kubernetes.io/hostname")
         | .values[]?][0] // "";

      ($pvs.items
       | map(select(.spec.claimRef.namespace != null)
             | {
                 namespace: .spec.claimRef.namespace,
                 claim: .spec.claimRef.name,
                 node: pv_node(.)
               })
       | map(select(.node != ""))) as $pinnedClaims
      | ($pinnedClaims | group_by(.node) | map({node: .[0].node, count: length})) as $pinnedByNode
      | [$pods.items[]
         | select(.status.phase == "Running")
         | select(.spec.nodeName != null)
         | select(any(.metadata.ownerReferences[]?; .controller == true and .kind == "StatefulSet" and .name == $statefulset))
         | . as $pod
         | ([.spec.volumes[]?.persistentVolumeClaim.claimName] | map(select(. != null))) as $claims
         | {
             pod: $pod.metadata.name,
             node: $pod.spec.nodeName,
             claims: ($claims
               | map(. as $claim
                     | $pinnedClaims[]
                     | select(.namespace == $pod.metadata.namespace and .claim == $claim and .node == $pod.spec.nodeName)
                     | .claim)),
             pinnedOnNode: (($pinnedByNode[]? | select(.node == $pod.spec.nodeName) | .count) // 0)
           }
         | select(.claims | length > 0)]
      | sort_by(.pinnedOnNode, .pod)
      | .[0] // empty
      | if . == "" then empty else [.pod, .node, (.claims | join(",")), (.pinnedOnNode | tostring)] | @tsv end
    '
)"

if [[ -z "${target}" ]]; then
  echo "no running pod from StatefulSet ${target_statefulset} has a local-path PV pinned to its current node" >&2
  exit 2
fi

IFS=$'\t' read -r target_pod target_node target_claims pinned_on_node <<<"${target}"

printf 'target_statefulset=%s\n' "${target_statefulset}"
printf 'target_pod=%s\n' "${target_pod}"
printf 'target_node=%s\n' "${target_node}"
printf 'target_claims=%s\n' "${target_claims}"
printf 'target_node_pinned_claim_count=%s\n' "${pinned_on_node}"

NAMESPACE="${namespace}" \
  RELEASE="${release}" \
  NODE="${target_node}" \
  EXPECT_BLOCKED=true \
  RESTORE_AFTER_BLOCKER=true \
  ALLOW_PINNED_PVC_DRAIN=true \
  UNCORDON=true \
  scripts/local-ha/scenario-drain-node.sh
