#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

namespace="${namespace:-tbite}"
image="${image:-tbite-lunch-flow:cluster-local}"
build="${build:-true}"
run_id="${run_id:-cluster-$(date +%Y%m%d-%H%M%S)}"
job="${job:-lunch-flow-${run_id}}"
mode="${mode:-all}"
cleanup="${cleanup:-delete}"
replace="${replace:-true}"
employees="${employees:-100000}"
merchants="${merchants:-200}"
pickup_points="${pickup_points:-200}"
pickup_sigma="${pickup_sigma:-50}"
merchant_sigma="${merchant_sigma:-6}"
stage1_rps="${stage1_rps:-20}"
stage1_concurrency="${stage1_concurrency:-20}"
stage1_batch_size="${stage1_batch_size:-100}"
stage2_rps="${stage2_rps:-0}"
stage2_concurrency="${stage2_concurrency:-1000}"
http_timeout="${http_timeout:-30s}"
base_url="${base_url:-http://tbite-tbite-platform-api}"
database_secret="${database_secret:-tbite-db}"
database_secret_key="${database_secret_key:-rwUrl}"
redis_url="${redis_url:-redis://:devpassword@tbite-valkey-primary:6379/0}"
report="${report:-/tmp/lunch-flow-${run_id}.json}"
timeout_seconds="${timeout_seconds:-1800}"
keep_job="${keep_job:-false}"

if [[ "${build}" == "true" ]]; then
	docker build -t "${image}" -f - . <<'DOCKERFILE'
FROM golang:1.26.3-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/api ./services/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/lunch-flow ./services/api/cmd/lunch-flow

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/lunch-flow /usr/local/bin/lunch-flow
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/lunch-flow"]
DOCKERFILE
fi

kubectl -n "${namespace}" delete job "${job}" --ignore-not-found=true >/dev/null

cat <<EOF | kubectl -n "${namespace}" apply -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: ${job}
  labels:
    app.kubernetes.io/name: lunch-flow
    app.kubernetes.io/part-of: tbite-load-test
spec:
  backoffLimit: 0
  template:
    metadata:
      labels:
        app.kubernetes.io/name: lunch-flow
        app.kubernetes.io/part-of: tbite-load-test
    spec:
      restartPolicy: Never
      containers:
        - name: lunch-flow
          image: ${image}
          imagePullPolicy: IfNotPresent
          env:
            - name: LUNCH_FLOW_BASE_URL
              value: "${base_url}"
            - name: DATABASE_RW_URL
              valueFrom:
                secretKeyRef:
                  name: ${database_secret}
                  key: ${database_secret_key}
            - name: REDIS_URL
              value: "${redis_url}"
          volumeMounts:
            - name: reports
              mountPath: /reports
          args:
            - --mode=${mode}
            - --cleanup=${cleanup}
            - --replace=${replace}
            - --run-id=${run_id}
            - --employees=${employees}
            - --merchants=${merchants}
            - --pickup-points=${pickup_points}
            - --pickup-sigma=${pickup_sigma}
            - --merchant-sigma=${merchant_sigma}
            - --stage1-rps=${stage1_rps}
            - --stage1-concurrency=${stage1_concurrency}
            - --stage1-batch-size=${stage1_batch_size}
            - --stage2-rps=${stage2_rps}
            - --stage2-concurrency=${stage2_concurrency}
            - --http-timeout=${http_timeout}
            - --report-file=/reports/report.json
        - name: report-holder
          image: busybox:1.37
          imagePullPolicy: IfNotPresent
          command:
            - sh
            - -c
            - trap 'exit 0' TERM INT; while true; do sleep 3600 & wait \$!; done
          volumeMounts:
            - name: reports
              mountPath: /reports
      volumes:
        - name: reports
          emptyDir: {}
EOF

pod=""
deadline=$((SECONDS + timeout_seconds))
while [[ -z "${pod}" ]]; do
	if (( SECONDS > deadline )); then
		echo "timed out waiting for job pod" >&2
		exit 1
	fi
	pod="$(kubectl -n "${namespace}" get pod -l job-name="${job}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
	sleep 1
done

echo "run_id=${run_id}"
echo "job=${job}"
echo "pod=${pod}"
echo "report=${report}"

exit_code=""
while [[ -z "${exit_code}" ]]; do
	if (( SECONDS > deadline )); then
		kubectl -n "${namespace}" describe pod "${pod}" >&2 || true
		echo "timed out waiting for lunch-flow container" >&2
		exit 1
	fi
	exit_code="$(kubectl -n "${namespace}" get pod "${pod}" -o jsonpath='{.status.containerStatuses[?(@.name=="lunch-flow")].state.terminated.exitCode}' 2>/dev/null || true)"
	sleep 5
done

kubectl -n "${namespace}" logs "${pod}" -c lunch-flow
kubectl -n "${namespace}" cp -c report-holder "${pod}:/reports/report.json" "${report}"

if [[ "${keep_job}" != "true" ]]; then
	kubectl -n "${namespace}" delete job "${job}" --ignore-not-found=true >/dev/null
fi

echo "report=${report}"

if [[ "${exit_code}" != "0" ]]; then
	exit "${exit_code}"
fi
