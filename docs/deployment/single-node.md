# Single-node deployment

Deploys the full T-Bite stack (Postgres, Redis, NATS, MinIO, API, three web apps, ingress-nginx) into one Kubernetes cluster. Suitable for a single-box k3s/k0s install, a throwaway GKE/EKS, or a kind/k3d cluster used as a staging surface.

## Prerequisites

- A reachable Kubernetes cluster (`kubectl cluster-info` succeeds)
- `kubectl` configured against that cluster
- `kustomize` (bundled with recent `kubectl`)
- Container images for `tbite/api`, `tbite/web-employee`, `tbite/web-merchant`, `tbite/web-admin` available to the cluster (build via `cd-publish-images.yml` or `docker buildx`)

## Deploy

```bash
make prod-up env=single-node
```

The Makefile target prints the current `kubectl` context and prompts before applying. It runs `kubectl kustomize ops/kubernetes/overlays/single-node | kubectl apply -f -` against that context.

Watch the rollout:

```bash
make prod-status env=single-node
kubectl -n tbite get pods -w
```

The overlay applies:

- Postgres / Redis / NATS / MinIO as single-replica deployments with `emptyDir` volumes (no PVC). Data is lost on pod restart — for anything beyond a smoke test, swap in a real StorageClass.
- ingress-nginx-class Ingress on `app.tbite.test` / `merchant.tbite.test` / `admin.tbite.test` / `api.tbite.test`. Add those hostnames to your DNS or `/etc/hosts` pointing at the cluster's load balancer IP.
- `secrets-bootstrap.yaml` provides plaintext dev credentials. **Replace these before exposing the cluster to anything that matters** — see `ops/kubernetes/overlays/single-node/secrets-bootstrap.yaml`.

## Verify

```bash
kubectl -n tbite exec deploy/api -- wget -qO- http://localhost:8080/healthz
# {"status":"ok"}
```

Apply the dev seeds if you need test users / vendors:

```bash
kubectl -n tbite exec -i deploy/postgres -- psql -U tbite -d tbite < scripts/dev/seed-e2e.sql
kubectl -n tbite exec -i deploy/postgres -- psql -U tbite -d tbite < scripts/dev/seed-p2.sql
```

## Tear down

```bash
make prod-down env=single-node
```

Removes every resource the overlay declares. `emptyDir` volumes vanish with the pods.

## GitOps (ArgoCD)

To deploy this overlay continuously via ArgoCD + GHCR images instead of `make prod-up`, see [argocd.md](./argocd.md).
