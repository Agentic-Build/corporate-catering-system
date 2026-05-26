# Local Kubernetes clusters (kind / k3d / OrbStack)

Per [`adr-0001`](../architecture/adr-0001-kubernetes-only-runtime.md)
(#48), local development uses real Kubernetes — never docker-compose as
a behaviour model. The same umbrella chart deploys locally and in
production; only the values differ (`values-dev.yaml`). This page
covers creating each supported local cluster and pointing the chart at
it.

Prerequisites: `kubectl`, `helm` (≥ 3.14), and one of the cluster tools
below. The dev profile fits **≥ 8 GiB** RAM (~6 GiB of requests).

## Create a cluster

### OrbStack (macOS, simplest)

OrbStack ships a single-node Kubernetes. Enable it in the OrbStack app
(Settings → Kubernetes), then:

```bash
kubectl config use-context orbstack
```

### kind

```bash
cat <<'EOF' | kind create cluster --name tbite --config -
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - { containerPort: 80,  hostPort: 80,  protocol: TCP }
      - { containerPort: 443, hostPort: 443, protocol: TCP }
EOF
```

The port mappings let Traefik serve the `*.tbite.local` hosts on
localhost.

### k3d

```bash
k3d cluster create tbite -p "80:80@loadbalancer" -p "443:443@loadbalancer"
```

> k3s/k3d bundle Traefik and servicelb. The chart installs its own
> Traefik, so either disable the bundled one
> (`k3d cluster create ... --k3s-arg "--disable=traefik@server:0"`) or
> set `traefik.enabled=false` in your local overrides to avoid two
> ingress controllers.

## Install the chart

The chart bundles fourteen CRD-owning subcharts, so a fresh cluster
needs the **two-pass install** documented in the chart README —
phase 1 brings up the operators/CRDs, phase 2 installs the full stack.
See [`chart/tbite-platform/README.md`](../../chart/tbite-platform/README.md#two-pass-install-recommended-for-fresh-clusters)
for the exact commands; the short form is:

```bash
helm dependency build chart/tbite-platform

# phase 1 — operators/CRDs only
helm install tbite chart/tbite-platform \
  -f chart/tbite-platform/values.yaml \
  -f chart/tbite-platform/values-dev.yaml \
  --namespace tbite --create-namespace \
  --set crdsReady=false

# phase 2 — full stack once CRDs are established
helm upgrade tbite chart/tbite-platform \
  -f chart/tbite-platform/values.yaml \
  -f chart/tbite-platform/values-dev.yaml \
  --namespace tbite --set crdsReady=true
```

`values-dev.yaml` drops every role to one replica, disables HPA/KEDA
and PDBs, shrinks storage, and disables Authentik/Hydra (use a stub
OIDC or BYO) — while preserving the same service names, env vars, and
health semantics as production.

## Reach the app

`values-dev.yaml` serves the platform on `*.tbite.local` hosts. Map
them to the cluster ingress:

```bash
# kind/k3d: Traefik is on localhost via the port mappings above
sudo sh -c 'cat >>/etc/hosts <<EOF
127.0.0.1 api.tbite.local app.tbite.local merchant.tbite.local admin.tbite.local auth.tbite.local minio.tbite.local
EOF'
```

OrbStack exposes Services on a routable IP; use that IP instead of
`127.0.0.1`, or port-forward directly.

## Smoke check

```bash
kubectl -n tbite get pods                 # all roles Running/Ready
kubectl -n tbite port-forward svc/tbite-api 8080:80
curl -s localhost:8080/readyz             # {"status":"ready"}
```

Hook Jobs (`db-migrate`, `provision-streams`, `bucket-bootstrap`) run
on install/upgrade; a failed Job surfaces as a failed pod — inspect
with `kubectl -n tbite logs job/<name>`.
