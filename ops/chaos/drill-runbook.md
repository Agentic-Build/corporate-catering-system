# T-Bite Chaos Drill Runbook — Lunch-Peak API Pod Kill

> **Goal:** Verify the system survives a single API pod being killed during
> peak load without dropping orders.

This runbook is **manual** by design. P8 does not deploy chaos-mesh; we use
`kubectl delete pod` against a 3-replica API deployment while k6 lunch-peak
runs. Automated chaos is a future hardening item.

---

## Pre-conditions

1. A k8s cluster with the `tbite` namespace populated by the
   `ops/kubernetes/overlays/single-node` overlay. Bring it up against your
   current kubectl context (e.g. a k3s box or a throwaway GKE cluster):

   ```bash
   make prod-up env=single-node
   kubectl -n tbite get pods
   ```

2. API deployment scaled to at least 3 replicas (otherwise PDB
   `minAvailable: 1` cannot tolerate a deletion mid-drill).

   ```bash
   kubectl -n tbite scale deployment/api --replicas=3
   kubectl -n tbite get pdb api -o wide   # expect ALLOWED DISRUPTIONS >= 1
   ```

3. k6 installed locally, lunch-peak script ready to run.

4. A second terminal open to watch pod state, plus a third for the audit
   query.

---

## Drill steps

### 1. Start background load

```bash
# Terminal 1 — start the 3-scenario lunch-peak script in the background.
# run-loadtest.sh assumes the API + workers + scheduler are already up
# (via `make prod-up env=single-node` or as separate processes).
ops/load/run-loadtest.sh &
LOAD_PID=$!

# Let traffic ramp.
sleep 30
```

### 2. Kill one random API pod

```bash
# Terminal 2 — pick a random API pod and delete it.
TARGET=$(kubectl -n tbite get pods -l app.kubernetes.io/name=api -o name | shuf | head -1)
echo "Deleting $TARGET at $(date -u +%H:%M:%S)"
kubectl -n tbite delete "$TARGET"
```

### 3. Watch the remaining replicas pick up traffic

```bash
# Terminal 2 — keep watching until a fresh pod is Running + Ready.
kubectl -n tbite get pods -l app.kubernetes.io/name=api -w
```

Expected timeline:
- `0s`: deleted pod enters `Terminating`; remaining 2 replicas continue serving.
- `~5-10s`: new pod scheduled, `Pending` -> `ContainerCreating`.
- `~15-25s`: new pod passes `readinessProbe` and rejoins the Service.

### 4. Verify no order placements were dropped

```bash
# Terminal 3 — query order table for the drill window.
psql "$(kubectl -n tbite get secret tbite-dev-secrets -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d)" \
  -h $(kubectl -n tbite get svc postgres -o jsonpath='{.spec.clusterIP}') \
  -U tbite -d tbite \
  -c "SELECT count(*), max(created_at) FROM \"order\" WHERE created_at > now() - interval '5 minutes';"
```

### 5. Tear down

```bash
wait $LOAD_PID
# Inspect the k6 summary printed at exit.
```

---

## Success criteria

The drill is considered a pass if **all** of the following hold:

- k6 reports `http_req_failed{...} rate < 0.005` (i.e. less than 0.5%) during
  the 30-second pod-recreation window.
- No `place_order` request returns 5xx **without** a corresponding audit
  trail row in `audit_event` (place_order is either fully committed with
  audit, fully aborted, or returns 409 cleanly when quota races).
- `order` row count growth is monotonic and matches k6's reported success
  count for the place_order scenario (modulo conditional-decrement 409s,
  which are by-design and not lost orders).
- `audit_event` trail is intact (no gaps in `order_id` for orders that
  reached `PLACED`).

---

## Failure modes & remediations

| Symptom | Likely cause | Remediation |
| --- | --- | --- |
| k6 error rate > 5% during the kill window | PDB `minAvailable` allowed too aggressive disruption; only 1 replica left under load | Bump `ops/kubernetes/base/pdb-api.yaml` `minAvailable` to 2 (requires `replicas: >= 3`). |
| Errors are 5xx (not 409) | Remaining 2 replicas insufficient for traffic | Bump HPA min replicas in `ops/kubernetes/base/hpa-api.yaml`; re-run drill. |
| Errors persist after new pod is Ready | Service endpoint cache stale on caller side; OR readinessProbe declared Ready before app actually accepts traffic | Tighten `readinessProbe.initialDelaySeconds` / `periodSeconds`; ensure the app delays Ready until pgx pool is healthy. |
| `order` row count regresses | Phantom rollback in place_order transaction; should be impossible given P3 design | Pause drill, capture pg logs, file a P0 bug. |
| `audit_event` row missing for an `order` that reached `PLACED` | Outbox-relay worker lost the event; check `outbox_event` table for stuck rows | Re-run outbox-relay worker; inspect NATS stream consumer lag. |

---

## After-action

1. Save the k6 summary JSON to `ops/load/evidence/<date>-chaos-drill.json`.
2. Note the drill date + outcome in the on-call runbook.
3. If any failure mode triggered, open an issue tagged `chaos-drill-finding`.

---

_This runbook should be exercised at least once per quarter, and after any
change that touches the API deployment, PDB, HPA, or place_order code path._
