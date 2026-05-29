# Local HA Behavior Drills

This playbook runs the production chart shape on a local multi-zone kind
cluster. It is for behavior testing: HPA, KEDA, drain, quorum failover, and
observability coverage. It is not a production capacity test.

The harness uses:

- 1 kind control-plane node.
- 6 kind worker nodes.
- 3 modeled zones: `local-a`, `local-b`, `local-c`.
- `values.yaml + values-local-ha.yaml`.
- Full observability: VictoriaMetrics, VictoriaLogs, VictoriaTraces, Grafana,
  OTel Collector, vmagent, alertmanager, kube-state-metrics, node-exporter, and
  Vector log shipping.

The rendered local HA profile is intentionally smaller than
`values-prod-ha.yaml`: it keeps HA topology and signal paths but reduces
Postgres, NATS, MinIO, and observability requests/retention so a 32 GiB
OrbStack allocation can run node and zone drain drills.

## Prerequisites

- OrbStack or Docker with enough memory for kind. Use 32 GiB for routine local
  HA drills; use more if you raise requests or retention.
- `kind`
- `kubectl`
- `helm`
- `go`

HPA CPU scaling requires metrics-server. KEDA scaling uses KEDA's metrics
adapter and event sources.

## Create The Cluster

```bash
make local-ha-cluster
make local-ha-metrics
```

The cluster config is [`ops/local-ha/kind-multiaz.yaml`](../../ops/local-ha/kind-multiaz.yaml).
Verify the modeled zones:

```bash
kubectl get nodes -L topology.kubernetes.io/zone
```

## Deploy The Stack

```bash
make local-ha-bootstrap
make local-ha-deploy
make local-ha-wait
make local-ha-seed
```

`local-ha-deploy` performs the fresh-cluster sequence:

1. Applies chart-rendered dependency CRDs.
2. Installs the release with Helm hooks disabled.
3. Waits for CNPG to create the app connection secret.
4. Creates the chart-level `tbite-db` Secret.
5. Runs a final Helm upgrade with hooks enabled.

Local secret material is generated under `.local-ha/secrets.env`, which is
ignored by Git.

`local-ha-seed` applies the compact TSMC workload seed through the current CNPG
primary pod. It avoids host `psql` and keeps the default dataset small enough
for local behavior drills. Set `SEED_SCALE=true` only when intentionally testing
the larger enterprise demo dataset.

This profile disables Traefik, Gateway API resources, and cert-manager because
the drills use `kubectl port-forward` and service DNS. Set
`INSTALL_GATEWAY_API_CRDS=true` and enable ingress values only when explicitly
testing ingress behavior on a compatible Kubernetes version.

For local platform image testing:

```bash
make local-ha-image
make local-ha-deploy
```

The deploy path defaults the platform images to the `local` tag loaded by
`make local-ha-image`. Use `TAG=... make local-ha-image` together with
`IMAGE_TAG=... make local-ha-deploy` only when intentionally testing a
different tag.

## Access

For dashboards:

```bash
kubectl -n tbite port-forward svc/tbite-grafana 3000:80
```

Then open `http://127.0.0.1:3000`. The admin user is `admin`; the password is
in `.local-ha/secrets.env` as `GRAFANA_PW`.

For API readiness:

```bash
kubectl -n tbite port-forward svc/tbite-tbite-platform-api 8080:80
curl -fsS http://127.0.0.1:8080/readyz
```

## Scenarios

Run evidence collection before and after each drill:

```bash
make local-ha-evidence
```

### API HPA

```bash
make local-ha-seed
make local-ha-autoscale-api
```

This drill runs a local stress workload against the API service, waits for the
CPU-backed HPA to request more than `minReplicas`, waits for the dashboard
PromQL signals to show CPU pressure and API scale pressure, then waits for the
HPA to return to its floor. Override `DURATION`, `TOTAL_RPS`, `CONCURRENCY`,
`SCALE_TIMEOUT_SECONDS`, and `BASELINE_TIMEOUT_SECONDS` when intentionally
changing the drill size. The local HA Redis URL and stress port-forward use the
Valkey primary service so write traffic is not sent to read-only replicas while
Sentinel replication remains enabled. The local HA Postgres stress
port-forward uses the PgBouncer RW pooler service, matching the application
`DATABASE_RW_URL` access layer. The stress runner treats HTTP 5xx and
client-side network errors as failures by default; override `MAX_5XX` or
`MAX_NET_ERRORS` only when intentionally characterizing a degraded run.

Expected:

- `tbite-tbite-platform-api` HPA reports CPU metrics.
- API pods scale above the `minReplicas` floor under enough load.
- API HPA current/desired replicas return to `minReplicas` after the load ends.
- Autoscaler availability remains clean during the intentional CPU scale event:
  `metrics-server missing=0`, `cpu hpas inactive=0`, and
  `bad scale conditions=0` while `api scale event seconds / 10m` records the
  load-driven scale window.
- The `Local HA Drills` dashboard shows the scale event first in
  `Autoscaler activity`, keeps `Autoscaler availability` clean, then shows HPA
  current/desired/min/max, CPU pressure, and HTTP request details in the API
  panels.

### Metrics Server Outage

```bash
make local-ha-fail-metrics-server
```

This drill scales `kube-system/metrics-server` to zero, waits for the
CPU-backed platform HPAs to report `ScalingActive=False` with
`FailedGetResourceMetric`, waits for the dashboard PromQL signals to show the
outage, then restores the original metrics-server replica count.

Expected:

- CPU-backed HPAs show `<unknown>/70%` while metrics-server is unavailable.
- KEDA-backed HPAs stay readable because they use KEDA metrics, not resource
  metrics-server.
- The `Local HA Drills` dashboard shows the issue first in
  `Autoscaler availability` as `metrics-server missing=1`, CPU HPAs inactive,
  and bad scale conditions. `Autoscaler activity` retains
  `metrics-server unavailable seconds / 10m` and `cpu hpa inactive seconds /
  10m` after recovery, then the Autoscaling row shows which CPU HPAs were
  inactive.
- `keda operator missing=0` and `keda metrics apiserver missing=0`, proving the
  outage is scoped to resource metrics and not external metrics.
- Metrics-server and all CPU-backed HPA conditions recover after the script
  restores the deployment.

### KEDA Metrics API Outage

```bash
make local-ha-fail-keda-metrics
```

This drill scales `keda-operator-metrics-apiserver` to zero, waits for the
KEDA-owned HPAs to report `ScalingActive=False` with
`FailedGetExternalMetric`, waits for the dashboard PromQL signals to show the
outage, then restores the original replica count.

Expected:

- KEDA-backed HPAs show `<unknown>` average metrics while
  `external.metrics.k8s.io` has no ready endpoint.
- CPU-backed HPAs remain readable because `metrics-server` is still available.
- The `Local HA Drills` dashboard shows the issue first in
  `Autoscaler availability` as KEDA HPAs inactive, bad scale conditions, and
  `keda metrics apiserver missing=1` while `keda operator missing=0`.
  `Autoscaler activity` retains `keda metrics apiserver unavailable seconds /
  10m` and `keda hpa inactive seconds / 10m` after recovery, then the Outbox
  relay HPA detail shows which KEDA-backed workload was affected.
- KEDA metrics apiserver, `external.metrics.k8s.io`, and all KEDA HPA
  conditions recover after the script restores the deployment.

### Observability Pod Loss

```bash
make local-ha-fail-vmagent
```

This drill temporarily sets the VictoriaMetrics `VMAgent` replica count to zero,
waits for the dashboard to classify the metrics-scrape path as stale, and then
restores the original replica count. Set `FAILURE_MODE=pod` when you only want
to delete the current `vmagent` pod and watch replacement.

Expected:

- While `vmagent` is down, `Observability availability` classifies the outage
  first as `vmagent telemetry stale=1` and `k8s inventory stale=1`, while app
  telemetry stays fresh. Signals sourced from Kubernetes inventory are gated by
  inventory freshness, and kube-state-metrics scrape loss is gated by vmagent
  freshness, so a scraper outage does not masquerade as an OTel, VictoriaLogs,
  VictoriaTraces, or kube-state-metrics backend outage. `log ingest stale` is
  gated by vmagent freshness and a two-minute vmagent availability settle
  window, so a scraper outage or its immediate recovery does not masquerade as
  a VictoriaLogs ingest outage.
- After recovery, a replacement `vmagent` pod becomes Ready and Kubernetes
  metrics freshness returns to the normal scrape interval. The script waits for
  dashboard PromQL signals to show low `vmagent` telemetry age, low Kubernetes
  metrics age, vmagent availability restored, zero observability ready gap,
  kube-state-metrics scrape available, and selected-range stale-seconds
  breadcrumbs for both vmagent telemetry and Kubernetes inventory. The
  stale-seconds breadcrumbs are used because vmagent cannot scrape its own
  Deployment availability changes while it is down.
- The `Local HA Drills` dashboard shows the current signal in `Observability
  availability` and keeps selected-range breadcrumbs in `Observability
  activity` through `vmagent stale seconds / 10m`, `k8s inventory stale seconds
  / 10m`, and `observability pod recreations / 10m`.
- The `Observability` row shows component availability, StatefulSet and
  DaemonSet ready gaps, pod recreations, and restarts for the metrics, logs,
  traces, Grafana, OTel Collector, and exporter path.

### Kubernetes Inventory Metrics Outage

```bash
make local-ha-fail-kube-state-metrics
```

This drill scales `tbite-kube-state-metrics` to zero while keeping vmagent,
VictoriaMetrics, OTel, VictoriaLogs, and VictoriaTraces online. It proves that
the dashboard distinguishes Kubernetes object-state scrape failures from app
telemetry and telemetry-backend failures.

Expected:

- `Observability availability` classifies the outage first: `k8s inventory
  stale=1` and `kube-state-metrics scrape missing=1`, while `app telemetry
  stale=0` and `log ingest stale=0`.
- App telemetry age and VictoriaLogs ingest age remain fresh.
- The `Observability` row identifies kube-state-metrics scrape loss directly;
  raw age, `kube-state-metrics missing seconds / 10m`, and `k8s inventory stale
  seconds / 10m` quantify the same split. Deployment availability series sourced
  from kube-state-metrics may be stale during this outage, so scrape `up` and
  metric freshness are the authoritative current-state signals.
- After recovery, kube-state-metrics scrape returns, Kubernetes metrics become
  fresh again, and `make local-ha-wait` succeeds.

### OTel Collector Outage

```bash
make local-ha-fail-otel-collector
```

This drill scales `tbite-opentelemetry-collector` to zero, waits until app
telemetry in VictoriaMetrics becomes stale, then restores the original replica
count and waits for telemetry freshness to recover. It specifically tests the
application telemetry path, which is different from the Kubernetes metrics path
scraped by `vmagent`.

Expected:

- The `Local HA Drills` dashboard classifies the outage first in
  `Observability availability`:
  `otel collector missing=1` and `app telemetry stale=1`, while
  `k8s inventory stale=0` and `log ingest stale=0`.
- The `Observability` row identifies the OTel deployment availability drop,
  accepted metric/span throughput falling, any collector export failures,
  `otel collector missing seconds / 10m`, and `app telemetry stale seconds /
  10m`.
- After recovery, app telemetry age returns to the normal export interval and
  `make local-ha-wait` succeeds.

### VictoriaTraces Backend Outage

```bash
make local-ha-fail-victoria-traces
```

This drill scales `tbite-vt-single-server` to zero while leaving the OTel
Collector and VictoriaMetrics online. It proves that the dashboard distinguishes
a trace-retention backend outage from a collector outage or app metrics outage.
The script generates lightweight API readiness traffic before, during, and
after the outage so trace delivery and recovery signals are not dependent on
incidental background traffic.

Expected:

- App telemetry metrics stay fresh and the OTel Collector remains available.
- `Observability availability` classifies the outage first:
  `traces backend missing=1` and `trace delivery gap active=1`, while
  `logs backend missing=0`, `app telemetry stale=0`, `k8s inventory stale=0`,
  `log ingest stale=0`, and `otel collector missing=0`.
- The `Observability` row identifies `tbite-vt-single-server` in the StatefulSet
  ready gap and shows accepted traces outpacing successful trace exports. OTel
  retry-stage outages can keep permanent export-failure counters at zero until
  spans are dropped. The same row retains `VictoriaTraces missing seconds /
  10m` and `trace delivery gap active seconds / 10m` after the backend recovers.
- After recovery, trace exports resume and `make local-ha-wait` succeeds.

### VictoriaLogs Backend Outage

```bash
make local-ha-fail-victoria-logs
```

This drill starts a short local log generator, scales
`tbite-victoria-logs-single-server` to zero, and verifies that VictoriaLogs
ingest becomes stale while Kubernetes metrics and app telemetry remain fresh. It
tests the logs-retention backend path separately from Kubernetes metrics, app
telemetry, and traces.

Expected:

- App telemetry metrics stay fresh, vmagent telemetry stays fresh, and the
  VictoriaLogs scrape is available before and after the outage.
- `Observability availability` classifies the outage first:
  `logs backend missing=1` and `log ingest stale=1`, while
  `traces backend missing=0`, `app telemetry stale=0`, `k8s inventory stale=0`,
  `trace delivery gap active=0`, and `otel collector missing=0`.
  The `log ingest stale` signal is evaluated only after vmagent has been fresh
  and availability-stable for two minutes, keeping scraper recovery distinct
  from a real VictoriaLogs ingest outage.
- The `Observability` row identifies `tbite-victoria-logs-single-server` in the
  StatefulSet ready gap and shows VictoriaLogs ingest rows/s dropping while log
  ingest age rises. The same row retains `VictoriaLogs missing seconds / 10m`
  and `VictoriaLogs ingest stale seconds / 10m` after the backend recovers.
- After recovery, VictoriaLogs ingests rows again, log ingest age returns below
  the normal freshness threshold, and `make local-ha-wait` succeeds.

### Worker KEDA

```bash
make local-ha-autoscale-worker
```

This drill creates a temporary `LOCAL_HA_KEDA` JetStream stream, blocks
`worker-outbox-relay` scheduling with a temporary impossible node selector,
injects publishable outbox rows, waits for the KEDA-owned HPA to request more
than `minReplicas`, then restores scheduling and waits for the backlog to
drain and for the HPA to return to its floor. Override `BACKLOG_ROWS`,
`SCALE_TIMEOUT_SECONDS`, `DRAIN_TIMEOUT_SECONDS`, and
`BASELINE_TIMEOUT_SECONDS` when intentionally changing the drill size.
The chart explicitly sets KEDA-generated HPA scale-down stabilization to match
the worker `cooldownPeriod`, so recovery drills do not wait on Kubernetes'
default 300 second stabilization window after the backlog is already zero.

Expected:

- `worker-outbox-relay` ScaledObject remains readable.
- Worker pods become Pending while scheduling is blocked, then schedule after
  recovery.
- The KEDA HPA desired replicas rises above the `minReplicas` floor while the
  backlog is present.
- Outbox pending returns to baseline after scheduling is restored, and the HPA
  current/desired replicas return to `minReplicas`.
- If HPA scale-down leaves both surviving relay pods in one modeled zone, the
  drill treats that as a fault-domain coverage gap, restores the relay
  deployment's topology spread, and waits for the dashboard coverage signal to
  return to zero.
- Outbox pending dashboard gates are filtered to live pods so stale app metric
  series from deleted worker pods cannot mask recovery.
- KEDA's external metric also appears as an estimated total backlog, so the
  dashboard still shows the queue pressure while all replacement relay pods are
  Pending and app-exported outbox metrics are temporarily absent.
- Autoscaler availability remains clean during the intentional scale event:
  `bad scale conditions=0` and KEDA HPAs stay active while `outbox scale event
  seconds / 10m` records the worker scale window.
- The `Local HA Drills` dashboard keeps `Autoscaler availability` focused on
  current scaler health and moves scale-event breadcrumbs to
  `Autoscaler activity`. It shows the KEDA backlog estimate in `Async backlog`,
  and provides outbox desired/current/pending details in the worker-specific
  autoscaling panels.

### Drain One Node

```bash
make local-ha-drain-apps
```

Or choose a specific node:

```bash
NODE=tbite-local-ha-worker3 make local-ha-drain-apps
```

When `NODE` is omitted, the script chooses a worker that is currently running
target platform app pods so the drill exercises eviction and replacement.

Expected:

- PDBs prevent unsafe voluntary disruption.
- Replacement pods schedule onto other nodes.
- API readiness recovers without manual repair.
- The `Local HA Drills` dashboard shows the drain first in
  `Fault-domain availability` as current cordoned workers. `Workload
  availability` keeps current app and scheduling symptoms separate from
  `Workload activity`, which retains `unavailable app seconds / 10m`,
  `pending pod seconds / 10m`, `unschedulable pod seconds / 10m`, and restart
  breadcrumbs after recovery. `Recent disruption activity` remains the
  per-component drill-down.

`local-ha-drain-apps` cordons a worker node and evicts only the platform app
components: API, realtime, web, worker, and scheduler pods. This is the routine
local drain drill because kind's default
`standard` storage class is `rancher.io/local-path`: StatefulSet PVCs are pinned
to their first node and cannot move while that node is cordoned. After uncordon,
the script restarts platform app deployments to restore topology spread and
waits for the dashboard fault-domain signals to return to baseline.

For an intentional full-node local-path blocker drill:

```bash
ALLOW_PINNED_PVC_DRAIN=true UNCORDON=false make local-ha-drain-node
make local-ha-evidence
kubectl uncordon <node>
make local-ha-wait
```

When `ALLOW_PINNED_PVC_DRAIN=true` and `UNCORDON=false` are used together, the
script waits for the expected blocker instead of waiting for full readiness.
The expected symptom is `Pending` StatefulSet pods with PVCs whose PV
`nodeAffinity` points at the cordoned node. The `Local HA Drills` dashboard
surfaces this from the overall row through `Workload availability` current
unschedulable pods, `stateful scheduling blockers`, unavailable work, stateful
ready gap, and `Fault-domain availability` signals. It keeps `unavailable app
seconds / 10m`, `unschedulable pod seconds / 10m`, `stateful scheduling
blocker seconds / 10m`, `stateful ready gap seconds / 10m`, and `cordoned
worker seconds / 10m` as activity breadcrumbs after recovery. `Scheduler And
Storage` then identifies the blocked pod name, owning StatefulSet or CNPG
Cluster, and affected StatefulSet ready gap. Platform replacement pods may
become unready while their data-plane dependency is degraded, but they must not
enter `CrashLoopBackOff`; the script observes the retained blocker window and
fails the drill if any platform app pod crashes.

### Drain One Modeled Zone

```bash
ZONE=local-a make local-ha-drain-zone-apps
```

Expected:

- The script cordons every node in the selected zone before eviction, so
  replacement app pods cannot land in the zone that is being drained.
- Remaining zones carry the app replicas.
- CNPG, NATS, and Valkey retain quorum. App-only drains do not evict CNPG pods,
  but CNPG intentionally switches the primary away from a cordoned node. When
  the current primary node is in the drained zone, the script expects the
  dashboard to keep `cnpg unhealthy=0` after recovery and to show a CNPG
  role-change breadcrumb.
- Observability keeps reporting during the event.
- The `Local HA Drills` dashboard shows current drain status in
  `Fault-domain availability`: `zone capacity depleted`, `cordoned workers`,
  and `zone coverage gaps`. `Fault-domain activity` retains recent cordoned
  zones, `cordoned worker seconds / 10m`, and `zone coverage gap seconds /
  10m` during and after the drain window. `Workload availability` should
  return to zero after app pods recover, while `Workload activity` keeps
  `unavailable app seconds / 10m` as the breadcrumb for the disruption window.
- The script fails if any selected app pod remains assigned to the drained zone
  after readiness recovery.
- The script observes the drained window and fails if any platform app pod
  enters `CrashLoopBackOff`.
- The script uncordons the zone, restarts platform app deployments to restore
  topology spread, and waits for `zone coverage gaps=0` so the next drill starts
  from a clean baseline.

As with node drains, full-zone drains against kind local-path PVCs are a
separate blocker drill. Use `ALLOW_PINNED_PVC_DRAIN=true` only when you
intentionally want to observe pinned StatefulSet PVC behavior. Use
`ALLOW_PINNED_PVC_DRAIN=true UNCORDON=false make local-ha-drain-zone` to keep
the blocked state observable for evidence collection; when `ZONE` is omitted,
the script chooses a pinned-PVC zone that avoids observability backend PVCs when
possible. The script then waits for the expected pinned-PVC blocker instead of
waiting for full readiness. In the dashboard, the first row should show current
unschedulable pods, `stateful scheduling blockers`, StatefulSet ready gap, and
fault-domain availability. `Workload activity` and `Fault-domain activity`
retain `unavailable app seconds / 10m`, `unschedulable pod seconds / 10m`,
`stateful scheduling blocker seconds / 10m`, `stateful ready gap seconds /
10m`, and `cordoned worker seconds / 10m` as breadcrumbs. `Scheduler And
Storage` identifies the blocked pod names, their StatefulSet or CNPG Cluster
owners, and StatefulSet ready gaps.

Kubernetes does not automatically rebalance healthy pods after a node or zone is
uncordoned. The app drain targets perform this recovery automatically. If a
manual drain or interrupted run leaves `zone coverage gaps`, restore the app
layer explicitly:

```bash
make local-ha-rebalance-apps
```

The rebalance target restarts the platform app deployments, waits for readiness,
and fails if any deployment still runs in fewer zones than its current replica
count can cover.

The HA overlays keep both zone and hostname spread soft with `ScheduleAnyway`.
That lets local nodes in the remaining zones absorb replicas during a drain
while the dashboard reports any reduced zone coverage explicitly. The topology
constraints also match `pod-template-hash`, so rolling updates spread the new
ReplicaSet instead of being skewed by old pods that are about to terminate.

### CNPG Primary Failover

```bash
make local-ha-fail-cnpg
```

The default `FAILURE_MODE=abrupt` force-deletes the current primary pod. Use
`FAILURE_MODE=graceful` only when you intentionally want to observe a slow
controlled shutdown path.

The local HA values shorten CNPG shutdown timings
(`smartShutdownTimeout=15`, `stopDelay=60`, `switchoverDelay=60`) so the drill
models operator response without waiting several minutes for production-grade
graceful shutdown defaults.

Expected:

- `status.currentPrimary` changes.
- The CNPG cluster returns to a healthy phase.
- The script waits for the `Local HA Drills` dashboard PromQL signals to show
  CNPG collectors and ready pods recovered, exactly one primary, bounded
  replication lag, exactly one `tbite-pg-rw` endpoint pointing at the current
  primary, `cnpg unhealthy=0`, a visible role-change breadcrumb, and a visible
  CNPG pod-recreation breadcrumb. It also performs a temporary-table write/read
  through the application `DATABASE_RW_URL` before and after failover.
- The dashboard shows the primary move, CNPG instance health, dependency
  readiness changes, RW service routing, pod recreation versus container
  restart distinction, and the PostgreSQL-backed KEDA scaler recovering.

### NATS, Valkey, And MinIO Pod Loss

```bash
make local-ha-fail-nats
make local-ha-fail-valkey
make local-ha-fail-minio
```

`local-ha-fail-valkey` deletes the current Valkey master by default
(`TARGET_ROLE=master`). Set `TARGET_ROLE=slave` to test replica loss, or set
`POD=tbite-valkey-node-N` when you need to target a specific pod.

`local-ha-fail-minio` deletes one MinIO tenant pool pod by default. Set
`POD=tbite-pool-0-N` when you need to target a specific pod.

Expected:

- StatefulSets recreate the deleted pods.
- NATS quorum remains available after one pod loss. The NATS script waits for
  dashboard PromQL signals to show `nats unhealthy=0`, ready pods, exporter
  scrapes, cluster routes, a single JetStream meta leader, full meta cluster
  size, zero meta pending work, no new JetStream API errors above the pre-drill
  baseline, no consumer backlog, no remaining not-ready NATS client pods, a new
  pod-recreation breadcrumb, and at least `MIN_RANGE_SIGNAL_SECONDS` of `nats
  server degraded seconds / 10m` in the selected range.
  It also performs a core NATS publish/subscribe probe through the `tbite-nats`
  service before and after pod replacement so service routing cannot pass
  silently on metrics alone, and it fails if any platform app pod enters
  `CrashLoopBackOff`.
- Valkey Sentinel mode keeps the service reachable. The Valkey script waits for
  dashboard PromQL signals to show `valkey unhealthy=0`, ready pods, exporter
  scrapes, exactly one master, the expected replica count, connected replicas,
  healthy replica links, exactly one `tbite-valkey-primary` endpoint pointing
  at the current master, no remaining not-ready cache client pods, zero
  exporter scrape errors, a pod-recreation breadcrumb, and a role-change
  breadcrumb when the deleted pod was the master. The script also requires at
  least `MIN_RANGE_SIGNAL_SECONDS` of `valkey degraded seconds / 10m` in the
  selected range. The script performs a
  write/read through the primary service after failover so read-only replica
  routing cannot pass silently, and it fails if any platform app pod enters
  `CrashLoopBackOff`.
- MinIO tenant returns to the expected pool replica count after one pod loss.
  The MinIO script waits for dashboard PromQL signals to show `minio
  unhealthy=0`, ready pods at the expected count, zero StatefulSet ready gap,
  zero not-ready tenant pods, MinIO API scrape availability, cluster health,
  all tenant nodes online, no offline nodes/drives, no remaining not-ready
  object-storage client pods, and the recreated pod's `kube_pod_created`
  timestamp moving forward. The dashboard also exposes `minio degraded seconds
  / 10m` when the scrape cadence catches a degraded interval, but the script
  does not hard-fail on that duration because a single local MinIO pod can be
  replaced between scrapes. It performs a write/read/delete through the
  in-cluster S3 service before and after pod replacement, and it fails if any
  platform app pod enters `CrashLoopBackOff`.
- The `Messaging, Cache, And Object Storage` dashboard row keeps NATS current
  health and history separate: `NATS availability` shows pod readiness,
  exporter scrape health, cluster routes, JetStream metadata state, consumer
  backlog, and NATS client not-ready impact, while `NATS activity` keeps pod
  recreations, degraded seconds, API errors, restarts, and client readiness
  changes. The same row also shows Valkey role/replication state and cache
  client not-ready/readiness-change impact, plus object-storage client
  not-ready impact. The `Object Storage` row shows
  MinIO tenant readiness, StatefulSet ready gap, MinIO API scrape health,
  cluster health, online/offline nodes and drives, quorum, S3 traffic/errors,
  object-storage client not-ready/readiness-change impact, pod recreations, and
  restarts. The overall row separates data-service current state from recovered
  incident breadcrumbs: `Data service availability` keeps structural health and
  client-not-ready signals, while `Data service activity` keeps recent
  NATS/Valkey/MinIO pod recreations, degraded seconds, dependency readiness
  changes, Valkey/CNPG role changes, NATS API errors, and MinIO S3 5xx. The
  Valkey detail panel additionally separates
  topology health from primary-service routing by showing primary endpoint
  count and whether that endpoint is the master. Scrape health may lag pod
  readiness briefly after replacement.

## Pass Criteria

A local HA drill passes when:

- The target fault is visible in Kubernetes events and Grafana.
- Recovered drain/disruption events remain visible in Grafana over the selected
  dashboard time range.
- The owning controller recreates or reschedules affected pods.
- PDBs block unsafe voluntary disruption.
- HPA/KEDA status remains readable and reacts under load.
- CNPG/NATS/Valkey maintain or regain quorum without manual data repair.
- The observability pipeline is currently healthy: metrics freshness is low,
  observability ready gaps are zero, and recovered observability pod loss remains
  visible over the selected dashboard range.
- `make local-ha-wait` returns successfully after recovery.

## Notes

This harness intentionally tests behavior, not full production capacity.
Use `values-prod-ha.yaml` and a larger cluster for capacity validation.
