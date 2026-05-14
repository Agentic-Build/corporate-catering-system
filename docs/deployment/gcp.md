# GCP deployment

Targets GKE (Autopilot or Standard) with managed services for state: Cloud SQL for Postgres, Memorystore for Redis, Cloud Storage for objects. NATS stays self-hosted on the cluster. Secrets ride External Secrets Operator + Secret Manager. Ingress is GKE Ingress + ManagedCertificate + Cloud Armor.

The kustomize overlay (`ops/kubernetes/overlays/gcp/`) ships with `PROJECT_ID` and `PLACEHOLDER` markers. **Every `PROJECT_ID` and `PLACEHOLDER` must be replaced before `kubectl apply`** — grep them out first.

## Prerequisites (one-time per environment)

### GCP project

- A project with billing enabled
- APIs: `container.googleapis.com`, `sqladmin.googleapis.com`, `redis.googleapis.com`, `storage.googleapis.com`, `secretmanager.googleapis.com`, `artifactregistry.googleapis.com`, `compute.googleapis.com`

### GKE cluster

- Standard or Autopilot — **Workload Identity must be enabled**
- A node pool (or Autopilot) with at least 4 vCPU available
- Connect: `gcloud container clusters get-credentials tbite-prod --region=us-central1`

### Cloud SQL (Postgres 16)

- An instance reachable from the cluster (private IP recommended)
- A database `tbite` and a user `tbite`
- Password stored in **Secret Manager** as `tbite-postgres-password`
- `cloud-sql-proxy` runs as a sidecar inside the API deployment (already wired in `cloudsql-binding.yaml`); supply the instance connection name in `configmap-patch.yaml`

### Memorystore (Redis 7)

- A standard or basic-tier instance
- Auth token (if enabled) stored in Secret Manager as `tbite-redis-auth`
- Edit `memorystore-binding.yaml` with the instance's primary endpoint

### Cloud Storage

- Buckets: `tbite-prod-payroll-exports` (or your naming) — used by the payroll-settler worker
- An HMAC key for the bound GSA → store the access ID / secret in Secret Manager as part of `tbite-app-secrets`

### Networking

- A reserved global static IP named `tbite-prod-ip` (`gcloud compute addresses create tbite-prod-ip --global`)
- A Cloud Armor security policy named `tbite-prod-armor`
- Cloud DNS records pointing `app.tbite.com` / `merchant.tbite.com` / `admin.tbite.com` / `api.tbite.com` at that IP
- A `ManagedCertificate` resource named `tbite-prod-cert` covering those four hostnames

### Workload Identity bindings

Two GSAs, both `roles/iam.workloadIdentityUser`-bound to their respective KSAs:

| GSA | KSA | Roles |
|---|---|---|
| `tbite-api@PROJECT_ID.iam.gserviceaccount.com` | `tbite/tbite-api` | `roles/cloudsql.client`, `roles/storage.objectAdmin` on the bucket |
| `tbite-external-secrets@PROJECT_ID.iam.gserviceaccount.com` | `tbite/tbite-external-secrets` | `roles/secretmanager.secretAccessor` on each referenced secret |

### External Secrets Operator

Install once per cluster, **before** applying the overlay:

```bash
helm repo add external-secrets https://charts.external-secrets.io
helm install external-secrets external-secrets/external-secrets \
  -n external-secrets-system --create-namespace
```

### Artifact Registry images

Build and push the four images:

```bash
gcloud artifacts repositories create tbite --location=us-central1 --repository-format=docker
docker buildx build --platform=linux/amd64 \
  -t us-central1-docker.pkg.dev/PROJECT_ID/tbite/api:$TAG \
  -f services/api/Dockerfile --push .
# repeat for web-employee, web-merchant, web-admin
```

`kustomize edit set image` is the conventional way to pin the tag at deploy time:

```bash
cd ops/kubernetes/overlays/gcp
kustomize edit set image \
  tbite/api=us-central1-docker.pkg.dev/PROJECT_ID/tbite/api:$TAG \
  tbite/web-employee=us-central1-docker.pkg.dev/PROJECT_ID/tbite/web-employee:$TAG \
  tbite/web-merchant=us-central1-docker.pkg.dev/PROJECT_ID/tbite/web-merchant:$TAG \
  tbite/web-admin=us-central1-docker.pkg.dev/PROJECT_ID/tbite/web-admin:$TAG
```

## Replace placeholders

```bash
grep -R "PROJECT_ID\|PLACEHOLDER" ops/kubernetes/overlays/gcp
```

Substitute every match. Typical fields:

- `PROJECT_ID` → your GCP project ID
- Cloud SQL instance connection name (in `configmap-patch.yaml`)
- Memorystore endpoint (in `memorystore-binding.yaml`)
- Bucket name (in `gcs-binding.yaml`)
- Image tags (handled by `kustomize edit set image` above)

## Deploy

```bash
gcloud container clusters get-credentials tbite-prod --region=us-central1
kubectl config current-context   # confirm before applying
make prod-up env=gcp
```

The Makefile prompts before applying. Status:

```bash
make prod-status env=gcp
kubectl -n tbite get managedcertificate,ingress
```

Certificate provisioning takes ~15-60 minutes the first time. The ingress shows `ADDRESS: <ip>` once the load balancer is live.

## First-deploy migrations

The API container runs migrations on startup is NOT the current behaviour — apply them externally:

```bash
kubectl -n tbite run migrate --rm -i --restart=Never \
  --image=migrate/migrate:v4.18.1 \
  --env=DATABASE_URL="$(kubectl -n tbite get secret tbite-app-secrets -o jsonpath='{.data.DATABASE_RW_URL}' | base64 -d)" \
  --command -- /bin/sh -c 'while read -r line; do echo "$line"; done' \
  < <(cat migrations/*.up.sql)
```

Or attach to the cloud-sql-proxy pod and run `migrate/migrate` with the migrations directory mounted.

## Observability

OTel collector endpoint is read from `OTEL_EXPORTER_OTLP_ENDPOINT` in the ConfigMap. Point it at your collector (Google Cloud Trace via the GCP exporter, or a managed Honeycomb / Grafana Cloud endpoint).

## Tear down

```bash
make prod-down env=gcp
```

Removes the K8s resources but does **not** delete Cloud SQL / Memorystore / GCS / static IP / certificate / Cloud Armor — those are cluster-external and managed via gcloud.
