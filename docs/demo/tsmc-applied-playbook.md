# TSMC Applied Demo Playbook

This is the shortest operator path for a first-time repository user to
prepare and run the TSMC-style production demo. The detailed cluster build
remains in [`docs/deployment/k3s-cloudflare-tsmc.md`](../deployment/k3s-cloudflare-tsmc.md);
this file is the applied runbook for demo execution.

## Demo target

End state:

- Single-node k3s production-shaped stack on 16 cores / 32 GiB RAM.
- Helm umbrella chart with CloudNativePG, Valkey, NATS JetStream, MinIO,
  Authentik, Hydra, OpenTelemetry, VictoriaMetrics, Grafana, KEDA, and
  cloudflared.
- ArgoCD installed and reachable through the Cloudflare Tunnel.
- TSMC demo data: 50,000 employees, 10 vendors, 150 menu items, 19 pickup
  locations across 9 fab/admin sites, and 45,000 daily menu portions.
- Live SRE demo loop: generate lunch-peak traffic, watch Grafana, delete a
  pod, confirm Kubernetes and the application recover.

## One-time setup

1. Build the cluster and base platform with the detailed k3s playbook:

   ```bash
   less docs/deployment/k3s-cloudflare-tsmc.md
   ```

2. Install ArgoCD after k3s is ready:

   ```bash
   kubectl create namespace argocd
   kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
   kubectl -n argocd rollout status deploy/argocd-server --timeout=5m
   kubectl -n argocd rollout status deploy/argocd-repo-server --timeout=5m
   ```

3. Apply the platform secrets and Helm chart as described in the deployment
   playbook. The demo helper scripts assume the application namespace is
   `tbite` and the chart-created database secret is `tbite-db` with key
   `rwUrl`.

   For GitOps ownership, put the secret-bearing Helm values from the detailed
   playbook into a private SOPS-managed values overlay or ArgoCD SOPS plugin
   flow first. Then replace the hostnames in
   `ops/argocd/values-overrides/tbite-tsmc-demo.yaml`, add the private overlay
   to `ops/argocd/application-tsmc-demo.yaml`, and apply:

   ```bash
   kubectl apply -f ops/argocd/project.yaml
   kubectl apply -f ops/argocd/application-tsmc-demo.yaml
   ```

4. Seed the full TSMC scenario:

   ```bash
   make demo-seed-tsmc
   ```

   This applies, in order:

   - `scripts/dev/seed-p2.sql`
   - `scripts/dev/seed-demo.sql`
   - `scripts/dev/seed-tsmc.sql`
   - `scripts/dev/seed-tsmc-scale.sql`

5. Verify the seeded operating model:

   ```bash
   kubectl -n tbite get pods
   kubectl -n tbite get hpa
   kubectl -n tbite get scaledobject
   ```

## Demo flow

Open these views before starting the drill:

- Employee app: `https://app.<domain>`
- Merchant app: `https://merchant.<domain>`
- Admin app: `https://admin.<domain>`
- Grafana: start with `oncall-overview`, `order-api-slo`,
  `supply-health`, `outbox-and-events`, `sse-gateway`, and
  `role-readiness`.
- ArgoCD: confirm `tbite-tsmc-demo` is synced or, for a direct Helm demo,
  keep ArgoCD open as the cluster control-plane UI.

Generate live lunch traffic:

```bash
DURATION=8m RPS=12 CONCURRENCY=16 EMPLOYEES=800 make demo-load-tsmc
```

Run one crisis drill while traffic is active:

```bash
make demo-crisis component=api
```

Then watch recovery:

```bash
kubectl -n tbite rollout status deploy -l app.kubernetes.io/component=api --timeout=3m
kubectl -n tbite get pods -l app.kubernetes.io/component=api -w
```

Useful variants:

```bash
make demo-crisis component=realtime
make demo-crisis component=worker-outbox-relay
make demo-crisis component=cloudflared
make demo-crisis component=minio
```

## Expected signals

During `api` pod deletion:

- Grafana `role-readiness` shows one API pod drop and recover.
- `order-api-slo` may show a short latency/error bump; 5xx rate should stay
  below the hard drill threshold.
- HPA should keep at least two API pods available in the default prod profile.
- ArgoCD should stay synced after Kubernetes recreates the pod.

During `realtime` pod deletion:

- `sse-gateway` shows active connection churn.
- Employee and merchant UIs should reconnect; new order events continue after
  the replacement pod is ready.

During `worker-outbox-relay` deletion:

- `outbox-and-events` may show unpublished outbox age rising.
- KEDA and the Deployment should bring the worker back.
- Outbox age should return to baseline after the replacement consumes backlog.

During `cloudflared` deletion:

- Cloudflare Tunnel still has at least one connector when the default two
  replicas are running.
- Public endpoints should continue serving through the remaining connector.

## Pass criteria

- Replacement pod is Ready within 3 minutes.
- Employee menu browsing stays available.
- Order placement returns success or expected 409 quota conflicts; sustained
  5xx is a failure.
- Grafana shows the failure and recovery without manual log inspection.
- No manual database repair is required.

Record the exact component, time window, Grafana screenshots, stress summary,
and any remediation in the release notes or incident log.
