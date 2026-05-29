# Local Kubernetes clusters (kind / k3d / OrbStack)

Per [`adr-0001`](../architecture/adr-0001-kubernetes-only-runtime.md)
(#48), local development uses real Kubernetes — never docker-compose as
a behaviour model. The same umbrella chart deploys locally and in
production; only the values differ (`values-dev.yaml`). This page
covers creating each supported local cluster and pointing the chart at
it.

Prerequisites: `kubectl`, `helm` (>= 3.14), and one of the cluster tools
below. The dev profile fits **>= 8 GiB** RAM (~6 GiB of requests).
For local multi-zone HA behavior drills, use
[`local-ha.md`](./local-ha.md) instead of the single-node dev profile.

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

The chart owns the local development runtime. The normal upgrade path is:

```bash
make dev
```

That target runs `helm dependency build` and installs/upgrades
`chart/tbite-platform` with `values-dev.yaml` in the current Kubernetes
context.

A fresh cluster may still need a CRD bootstrap pass because several
subcharts own CustomResourceDefinitions. Use the two-pass install in
[`chart/tbite-platform/README.md`](../../chart/tbite-platform/README.md#two-pass-install-recommended-for-fresh-clusters),
then use `make dev` for routine upgrades.

The direct Helm form is:

```bash
helm dependency build chart/tbite-platform

helm upgrade --install tbite chart/tbite-platform \
  -f chart/tbite-platform/values-dev.yaml \
  --namespace tbite --create-namespace
```

`values-dev.yaml` bundles the CNPG operator for local clusters, drops
every role to one replica, disables HPA/KEDA and PDBs, shrinks storage,
and disables Authentik/Hydra by default (use a stub OIDC or BYO) while
preserving the same service names, env vars, and health semantics as
production.

## Reach the app

`values-dev.yaml` serves the platform on `*.tbite.local` hosts. Map
them to the cluster ingress:

```bash
# kind/k3d: Traefik is on localhost via the port mappings above
sudo sh -c 'cat >>/etc/hosts <<EOF
127.0.0.1 api.tbite.local app.tbite.local merchant.tbite.local admin.tbite.local auth.tbite.local hydra.tbite.local minio.tbite.local grafana.tbite.local
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

## Seed data

Seed scripts require explicit host-side connectivity and do not start
containers. Port-forward the chart-managed services or use routable
cluster endpoints, then export the concrete values:

```bash
export DATABASE_URL='postgres://...'
export S3_ENDPOINT='http://minio.tbite.local'
export S3_ACCESS_KEY_ID='...'
export S3_SECRET_ACCESS_KEY='...'
export S3_BUCKET='tbite-dev'
export S3_PUBLIC_BASE_URL='http://minio.tbite.local'

make seed
```
