# T-Bite Chaos Drill Runbook — TSMC Lunch Peak

## Goal

Verify that the production-shaped single-node deployment detects and recovers
from one service pod being deleted during lunch-peak traffic.

This runbook uses the current Helm/k3s deployment path, the TSMC 50,000-user
seed, Grafana dashboards, and the helper scripts in `ops/demo/`.

## Pre-conditions

1. The k3s + Cloudflare Tunnel deployment is complete:

   ```bash
   kubectl -n tbite get pods
   helm -n tbite status tbite
   ```

2. The TSMC enterprise seed has been applied:

   ```bash
   make demo-seed-tsmc
   ```

3. Grafana is reachable through the public hostname. Open these dashboards:

   - `oncall-overview`
   - `order-api-slo`
   - `role-readiness`
   - `supply-health`
   - `outbox-and-events`
   - `sse-gateway`

4. The operator workstation has `kubectl` and Go available. The load script
   uses `kubectl port-forward` plus `go run ./services/api/cmd/stress`.

## Drill: API Pod Deletion

### 1. Start lunch-peak traffic

```bash
DURATION=8m RPS=12 CONCURRENCY=16 EMPLOYEES=800 make demo-load-tsmc
```

The default scenario is `lunch-crunch`: it focuses order placement on
`hc-12a-1f`, vendor `a1111111-1111-1111-1111-111111111111`, and item
`4f26e612-b35f-5500-8f2a-63eded235675`.

### 2. Delete one API pod

In a second terminal:

```bash
make demo-crisis component=api
kubectl -n tbite rollout status deploy -l app.kubernetes.io/component=api --timeout=3m
```

Watch pods if you want the live timeline:

```bash
kubectl -n tbite get pods -l app.kubernetes.io/component=api -w
```

### 3. Observe expected signals

- `role-readiness`: one API pod becomes unready, then a replacement becomes
  ready.
- `order-api-slo`: latency may bump; sustained 5xx must not appear.
- `supply-health`: the hot item may show quota pressure or 409 conflicts.
- `outbox-and-events`: outbox age should stay bounded.
- ArgoCD: app remains synced; no manual drift repair is required.

## Variants

Run each variant while `make demo-load-tsmc` is still active:

```bash
make demo-crisis component=realtime
make demo-crisis component=worker-outbox-relay
make demo-crisis component=cloudflared
make demo-crisis component=minio
```

Expected variant-specific signals:

| Component | Primary dashboard | Expected recovery |
| --- | --- | --- |
| `realtime` | `sse-gateway` | SSE connections reconnect after replacement pod readiness. |
| `worker-outbox-relay` | `outbox-and-events` | Unpublished outbox age may rise, then drain after worker recovery. |
| `cloudflared` | `infra-health` / Cloudflare dashboard | Remaining connector serves traffic; deleted pod is recreated. |
| `minio` | `object-storage` | Pod restarts with persistent volume; API media paths recover. |

## Pass criteria

The drill passes when all conditions hold:

- Deleted pod's owning controller returns to Ready within 3 minutes.
- Employee menu browsing remains available during the drill.
- Order placement returns success or expected 409 quota conflicts; sustained
  5xx is a failure.
- Grafana shows both the fault and the recovery without relying on log tailing.
- No database mutation or manual data repair is needed.

## Failure modes and remediations

| Symptom | Likely cause | Remediation |
| --- | --- | --- |
| API 5xx continues after replacement pod is Ready | Readiness probe is too weak or downstream dependency is unhealthy | Check `/readyz`, Postgres, Valkey, and NATS dashboards; tighten readiness before retrying. |
| HPA reaches max replicas under normal demo load | API CPU/concurrency target too low for this node | Increase `api.hpa.maxReplicas` or reduce demo `RPS` / `CONCURRENCY`. |
| Outbox age keeps rising after worker recovery | NATS or worker handler failure | Inspect `outbox-and-events`, worker logs, and NATS JetStream consumer lag. |
| Cloudflare public host returns 502 | Tunnel connector or service endpoint unavailable | Check `cloudflared` pods and `kubectl -n tbite get endpoints`. |
| MinIO does not recover | PVC or node disk issue | Check PVC binding, node disk, and MinIO pod events. |

## Evidence to keep

Save the following after the run:

- Component name and deletion timestamp.
- Stress summary printed by `make demo-load-tsmc`.
- Grafana screenshots for the affected dashboards.
- `kubectl -n tbite get pods -o wide` after recovery.
- Any remediation applied before declaring pass/fail.
