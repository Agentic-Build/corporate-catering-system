# Demo playbook: k3s + Cloudflare Tunnel, single-enterprise prod (TSMC plants)

This playbook is for an operator using the repo for the first time. It
walks from "empty machine + Cloudflare account + a domain" to a running
production-shaped deployment exposing the three SvelteKit apps,
Authentik, Hydra, ArgoCD, and Grafana through one Cloudflare Tunnel.
End state: ~28-30 pods Ready, ~9 GiB memory in use, nine public
hostnames, nineteen TSMC pickup locations across nine fab sites, and
50,000 seeded employee records for the enterprise-scale demo.

Roughly 45 minutes start-to-finish on a fresh box.

## What you end up with

| Public host (you set the zone) | What it is |
| --- | --- |
| `api.tbite.example.com` | Go API + MCP server |
| `app.tbite.example.com` | Employee SvelteKit app (place order, pick up) |
| `merchant.tbite.example.com` | Merchant SvelteKit app (prep board, live SSE) |
| `admin.tbite.example.com` | 福委會 / Welfare admin SvelteKit app |
| `rt.tbite.example.com` | Realtime SSE gateway |
| `auth.tbite.example.com` | Authentik SSO (admin UI + OIDC) |
| `hydra.tbite.example.com` | Ory Hydra (MCP DCR endpoint) |
| `grafana.tbite.example.com` | Grafana dashboards (VictoriaMetrics + alerts) |
| `argocd.tbite.example.com` | ArgoCD UI |

All public hosts terminate TLS at Cloudflare's edge; no inbound port is open
on the cluster.

Nineteen TSMC pickup locations across nine fab/admin sites are seeded:

| Site | Code | Pickup locations |
| --- | --- | --- |
| 新竹總部 (Hsinchu HQ) | `hc-hq` | `r1-b1`, `p5-1f`, `r2-2f` |
| 新竹 Fab 12A | `hc-12a` | `1f`, `3f` |
| 新竹 Fab 12B | `hc-12b` | `1f`, `3f` |
| 中科 Fab 15A | `tc-15a` | `1f`, `3f` |
| 中科 Fab 15B | `tc-15b` | `1f`, `3f` |
| 南科 Fab 14 | `tn-14` | `2f` |
| 南科 Fab 18 P1 | `tn-18p1` | `1f`, `3f`, `b1` |
| 南科 Fab 18 P3 | `tn-18p3` | `1f`, `3f`, `b1` |
| 南科 Fab 18 P7 | `tn-18p7` | `2f` |

Each pickup location is one row in `vendor_plant_mapping.plant` with
its own `service_window` (lunch 11:30-13:00 or break 14:00-17:00).

---

## Prerequisites

### Hardware

One Linux box, 16 cores / 32 GiB RAM / 100 GB SSD. Ubuntu 24.04 LTS
preferred. Works in a Hetzner / Vultr / DigitalOcean VPS or on bare
metal. Apple Silicon via OrbStack works too — see the OrbStack notes
at the end.

### External accounts

- A **Cloudflare** account with a registered domain configured as a
  zone (any plan; Free works).
- A **GitHub** account (only needed if you want to fork; for the
  public chart and image you do not need one).

### Tools to install on the operator's laptop

```bash
# macOS
brew install kubectl helm sops age cloudflared yq jq gh

# Ubuntu / Debian
sudo apt update && sudo apt install -y curl gnupg
# kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -m 0755 kubectl /usr/local/bin/kubectl
# helm
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
# sops + age + yq + jq + cloudflared + gh
# (follow each upstream install doc)
```

Run `kubectl version --client`, `helm version`, `sops --version`,
`age --version`, `cloudflared --version` to confirm.

---

## Step 1 — Clone the repo

```bash
git clone https://github.com/Agentic-Build/corporate-catering-system.git tbite
cd tbite
```

Repo layout the playbook will reference:

```
chart/tbite-platform/           # the Helm umbrella chart
chart/tbite-platform/values.yaml                  # base (single-enterprise prod)
chart/tbite-platform/values-cloudflared.yaml      # ingress overlay you'll use
chart/tbite-platform/blueprints/                  # Authentik blueprints
scripts/dev/seed-tsmc.sql       # TSMC plant + pickup-location seed
scripts/dev/seed-tsmc-scale.sql # 50,000-employee enterprise demo seed
ops/secrets/                    # SOPS-encrypted secret templates
```

---

## Step 2 — Install k3s on the cluster box

SSH to the box, then:

```bash
curl -sfL https://get.k3s.io | sh -s - \
  --disable traefik \
  --disable servicelb \
  --write-kubeconfig-mode 644
```

- `--disable traefik` — we use Cloudflare Tunnel, not the bundled
  ingress.
- `--disable servicelb` — no LoadBalancer needed; cloudflared dials
  outbound.
- `--write-kubeconfig-mode 644` — kubectl works without sudo.

Verify:

```bash
sudo k3s kubectl get nodes
# NAME   STATUS   ROLES                  AGE   VERSION
# box    Ready    control-plane,master   30s   v1.31.x+k3s1
```

Copy `/etc/rancher/k3s/k3s.yaml` to your laptop, replace
`server: https://127.0.0.1:6443` with the box's reachable IP, save as
`~/.kube/config-tbite-demo`, and `export KUBECONFIG=~/.kube/config-tbite-demo`.

`kubectl get nodes` from the laptop should now work.

---

## Step 3 — Install the CloudNativePG operator

The chart's default is `postgres.operator.bundled: false` because CNPG
expects to find its own Deployment by a fixed label set; the cleanest
install is operator-cluster-wide:

```bash
helm repo add cnpg https://cloudnative-pg.github.io/charts
helm install cnpg cnpg/cloudnative-pg \
  --version 0.28.2 \
  --namespace cnpg-system \
  --create-namespace \
  --wait --timeout 3m
```

Confirm:

```bash
kubectl -n cnpg-system get pods
# NAME                                READY   STATUS    RESTARTS   AGE
# cnpg-cloudnative-pg-xxx             1/1     Running   0          45s
```

### Step 3b — Install ArgoCD

ArgoCD is installed as a platform control-plane service. The application
can still be bootstrapped by Helm during this first install; ArgoCD gives
operators the UI and GitOps reconciliation path for day-2 changes.

```bash
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
kubectl -n argocd rollout status deploy/argocd-server --timeout=5m
kubectl -n argocd rollout status deploy/argocd-repo-server --timeout=5m
```

If you want ArgoCD to own the chart, first move the secret-bearing Helm
values from Steps 5-8 into a private SOPS-managed values overlay or ArgoCD
SOPS plugin flow. Do not sync the public demo override alone; it intentionally
does not contain `authentik.secret_key`, Hydra system secrets, or bootstrap
passwords. After that private overlay exists, adapt the hostnames in
`ops/argocd/values-overrides/tbite-tsmc-demo.yaml`, add the private overlay
to `ops/argocd/application-tsmc-demo.yaml`, and apply:

```bash
kubectl apply -f ops/argocd/project.yaml
kubectl apply -f ops/argocd/application-tsmc-demo.yaml
```

---

## Step 4 — Create the Cloudflare Tunnel in the dashboard

The chart uses Cloudflare's **remote-managed tunnel** model — the
tunnel and its routing rules are created in the Zero Trust dashboard,
and Cloudflare hands back a single token. No local `cloudflared`
binary is needed for this step.

### 4a. Create the tunnel and copy the token

1. Open [Cloudflare Zero Trust dashboard](https://one.dash.cloudflare.com/)
   → **Networks** → **Tunnels** → **Create a tunnel**.
2. Pick the **Cloudflared** connector type, name the tunnel
   `tbite-demo` (or anything), **Save**.
3. The dashboard shows install instructions for various platforms;
   ignore them — we run cloudflared in-cluster. **Copy the long
   base64 token string** (the part after `--token` in the example
   command). You will paste it into a Secret in Step 5.

### 4b. Add the nine public hostnames

Still in the dashboard, under the tunnel's **Public Hostnames** tab,
**Add a public hostname** for each entry below. Cloudflare creates the
matching DNS CNAME automatically the moment you save each one.

| Subdomain | Domain | Service type | URL |
| --- | --- | --- | --- |
| `api` | `tbite.example.com` | HTTP | `tbite-api.tbite.svc.cluster.local:80` |
| `app` | `tbite.example.com` | HTTP | `tbite-web-employee.tbite.svc.cluster.local:80` |
| `merchant` | `tbite.example.com` | HTTP | `tbite-web-merchant.tbite.svc.cluster.local:80` |
| `admin` | `tbite.example.com` | HTTP | `tbite-web-admin.tbite.svc.cluster.local:80` |
| `rt` | `tbite.example.com` | HTTP | `tbite-realtime.tbite.svc.cluster.local:80` |
| `auth` | `tbite.example.com` | HTTP | `tbite-authentik-server.tbite.svc.cluster.local:80` |
| `hydra` | `tbite.example.com` | HTTP | `tbite-hydra-public.tbite.svc.cluster.local:4444` |
| `grafana` | `tbite.example.com` | HTTP | `tbite-grafana.tbite.svc.cluster.local:80` |
| `argocd` | `tbite.example.com` | HTTP | `argocd-server.argocd.svc.cluster.local:80` |

Replace `tbite.example.com` with your actual zone. All chart-managed
Service ports default to `80` (mapped to the per-role container port
via the Service's `targetPort` — e.g. api Service port 80 → api
container port 8080). The two exceptions are Hydra's public port
(4444, owned by the upstream subchart) and Grafana / ArgoCD (80,
also subchart-managed). For the realtime SSE hostname, expand
**Additional application settings → TLS** and set **HTTP2 connection**
off and **Disable Chunked Encoding** off — the application emits a
20s heartbeat so Cloudflare's 100s idle timeout does not drop SSE
streams.

### 4c. (Optional) Add Cloudflare Access policies

For `grafana`, `admin`, and `argocd` you typically also add a Zero
Trust Access application in the dashboard (Access → Applications)
requiring Google / Okta / WebAuthn SSO before the request even
reaches the cluster. This is independent of Authentik (which gates
the employee / merchant flows).

---

## Step 5 — Generate operator secrets

Generate the age key the cluster will decrypt SOPS payloads with:

```bash
mkdir -p ~/.config/sops/age
age-keygen -o ~/.config/sops/age/keys.txt
export SOPS_AGE_KEY_FILE=~/.config/sops/age/keys.txt
AGE_PUB=$(grep -oE 'age1[0-9a-z]+' ~/.config/sops/age/keys.txt)
echo "age public key: $AGE_PUB"
```

Update `.sops.yaml` in the repo root with this public key (replace the
placeholder recipient), then prepare the namespace and secrets:

```bash
kubectl create namespace tbite

# Apply the age private key as a Secret so SOPS-aware tooling in-cluster
# can decrypt. For raw helm + kubectl the operator decrypts locally and
# `kubectl create secret` plain values, which is what we do below.

# 1. Cloudflare tunnel token — the long base64 string you copied
#    from the dashboard in Step 4a.
TUNNEL_TOKEN="<paste-token-from-cloudflare-dashboard>"
kubectl -n tbite create secret generic tbite-cloudflared \
  --from-literal=token="$TUNNEL_TOKEN"

# 2. Strong random credentials
VALKEY_PW=$(openssl rand -base64 24)
AUTHENTIK_SECRET_KEY=$(openssl rand -base64 60)
HYDRA_SYSTEM_SECRET=$(openssl rand -hex 32)
GRAFANA_PW=$(openssl rand -base64 18)
AUTHENTIK_PG_PW=$(openssl rand -hex 24)
HYDRA_PG_PW=$(openssl rand -hex 24)

# Stash these somewhere safe — you will need AUTHENTIK_PG_PW and
# HYDRA_PG_PW again in Step 8 when you create the matching DB roles.
cat > demo-secrets.local <<EOF
VALKEY_PW=$VALKEY_PW
AUTHENTIK_SECRET_KEY=$AUTHENTIK_SECRET_KEY
HYDRA_SYSTEM_SECRET=$HYDRA_SYSTEM_SECRET
GRAFANA_PW=$GRAFANA_PW
AUTHENTIK_PG_PW=$AUTHENTIK_PG_PW
HYDRA_PG_PW=$HYDRA_PG_PW
EOF
chmod 600 demo-secrets.local

# 3. Application secrets (placeholders for OIDC + S3 + Authentik token;
#    rotated in Step 10 after Authentik is up).
kubectl -n tbite create secret generic tbite-valkey \
  --from-literal=password="$VALKEY_PW" \
  --from-literal=valkey-password="$VALKEY_PW"
kubectl -n tbite create secret generic tbite-minio-root \
  --from-literal=accessKey=minio \
  --from-literal=secretKey="$(openssl rand -hex 16)"
kubectl -n tbite create secret generic tbite-s3 \
  --from-literal=accessKeyID=minio \
  --from-literal=secretAccessKey="$(kubectl -n tbite get secret tbite-minio-root -o jsonpath='{.data.secretKey}' | base64 -d)"
kubectl -n tbite create secret generic tbite-nats --from-literal=creds=""
kubectl -n tbite create secret generic tbite-authentik \
  --from-literal=apiToken="will-rotate-after-authentik-up"
kubectl -n tbite create secret generic tbite-oidc-clients \
  --from-literal=apiClientID=tbite \
  --from-literal=apiClientSecret="REPLACE-WITH-SOPS-ENCRYPTED-VALUE" \
  --from-literal=employeeClientID=tbite-employee \
  --from-literal=employeeClientSecret=placeholder \
  --from-literal=merchantClientID=tbite-merchant \
  --from-literal=merchantClientSecret=placeholder \
  --from-literal=adminClientID=tbite-admin \
  --from-literal=adminClientSecret=placeholder
kubectl -n tbite create secret generic tbite-grafana-admin \
  --from-literal=password="$GRAFANA_PW"
```

For a real prod deploy these would all live in `ops/secrets/*.sops.yaml`
encrypted by SOPS + age, with ArgoCD's SOPS plugin decrypting at sync
time. The plain-`kubectl create secret` form above is the smallest
demo path.

---

## Step 6 — Resolve chart dependencies and install (phase 1)

The chart pulls 14 subcharts; resolve them once:

```bash
make chart-deps
# … 14 subchart tarballs land in chart/tbite-platform/charts/
```

Install with the cloudflared overlay. Phase 1 keeps `crdsReady: false`
so subchart CRDs land before any CR is submitted:

```bash
helm install tbite chart/tbite-platform/ \
  -f chart/tbite-platform/values.yaml \
  -f chart/tbite-platform/values-cloudflared.yaml \
  --namespace tbite \
  --set crdsReady=false \
  --set "global.domain=tbite.example.com" \
  --set "global.baseURL.api=https://api.tbite.example.com" \
  --set "global.baseURL.employee=https://app.tbite.example.com" \
  --set "global.baseURL.merchant=https://merchant.tbite.example.com" \
  --set "global.baseURL.admin=https://admin.tbite.example.com" \
  --set "global.oidcIssuerURL=https://auth.tbite.example.com/application/o/tbite/" \
  --set "global.authentik.baseURL=http://tbite-authentik-server.tbite.svc.cluster.local:80" \
  --set "global.s3.endpoint=http://minio.tbite.svc.cluster.local" \
  --set "global.nats.url=nats://tbite-nats.tbite.svc.cluster.local:4222" \
  --set "global.redisURL=redis://:${VALKEY_PW}@tbite-valkey-primary.tbite.svc.cluster.local:6379/0" \
  --set "authentik.authentik.secret_key=${AUTHENTIK_SECRET_KEY}" \
  --set "authentik.authentik.postgresql.password=${AUTHENTIK_PG_PW}" \
  --set "hydra.hydra.config.dsn=postgres://hydra:${HYDRA_PG_PW}@tbite-pg-rw.tbite.svc.cluster.local:5432/hydra?sslmode=disable" \
  --set "hydra.hydra.config.urls.self.issuer=https://hydra.tbite.example.com" \
  --set "observability.grafana.ingressHost=grafana.tbite.example.com" \
  --set authentikBlueprints.devUsers.enabled=true \
  --set hooks.dbMigrate.enabled=false \
  --set hooks.provisionStreams.enabled=false \
  --set hooks.createIdentityDatabases.enabled=false \
  --wait=false --timeout=4m
```

Watch pods come up:

```bash
kubectl -n tbite get pods -w
```

You will see NATS, Valkey, MinIO, Authentik, Hydra, and the
`cloudflared` Deployment reach Running within ~3 minutes. The Go
application pods (api / realtime / workers / schedulers) will be
`CreateContainerConfigError` waiting for the `tbite-db` secret that
only exists after CNPG provisions Postgres in phase 2.

---

## Step 7 — Phase 2: turn on CRDs and let CNPG build Postgres

```bash
helm upgrade tbite chart/tbite-platform/ \
  -f chart/tbite-platform/values.yaml \
  -f chart/tbite-platform/values-cloudflared.yaml \
  --namespace tbite \
  --reuse-values \
  --set crdsReady=true \
  --set postgres.cluster.resources.requests.memory=2Gi \
  --set postgres.cluster.resources.limits.memory=2Gi \
  --set postgres.cluster.monitoring.enabled=false \
  --wait=false --timeout=4m
```

Wait for the CNPG Cluster to converge:

```bash
until kubectl -n tbite get cluster tbite-pg -o jsonpath='{.status.phase}' \
      2>/dev/null | grep -q "healthy"; do
  sleep 5
  kubectl -n tbite get cluster tbite-pg 2>/dev/null | tail -1
done
echo "✓ Postgres ready"
```

Capture the CNPG-generated app DSN and turn it into the `tbite-db`
secret the application pods expect:

```bash
PG_URL=$(kubectl -n tbite get secret tbite-pg-app -o jsonpath='{.data.uri}' | base64 -d)
RO_URL=$(echo "$PG_URL" | sed 's|tbite-pg-rw|tbite-pg-ro|')
kubectl -n tbite create secret generic tbite-db \
  --from-literal=rwUrl="$PG_URL" \
  --from-literal=roUrl="$RO_URL"
```

---

## Step 8 — Create identity databases + run migrations

Create the Authentik and Hydra databases (CNPG defaults to no
superuser-from-network so we open a local psql in the primary pod):

```bash
. ./demo-secrets.local

kubectl -n tbite exec tbite-pg-1 -c postgres -- \
  psql -U postgres -d postgres -c \
  "CREATE ROLE authentik LOGIN PASSWORD '${AUTHENTIK_PG_PW}';"
kubectl -n tbite exec tbite-pg-1 -c postgres -- \
  psql -U postgres -d postgres -c \
  "CREATE DATABASE authentik OWNER authentik;"
kubectl -n tbite exec tbite-pg-1 -c postgres -- \
  psql -U postgres -d postgres -c \
  "CREATE ROLE hydra LOGIN PASSWORD '${HYDRA_PG_PW}';"
kubectl -n tbite exec tbite-pg-1 -c postgres -- \
  psql -U postgres -d postgres -c \
  "CREATE DATABASE hydra OWNER hydra;"
```

Run the application schema migrations:

```bash
kubectl -n tbite run db-migrate --rm -i \
  --image=migrate/migrate:v4.18.1 \
  --restart=Never -- \
  -source "github://Agentic-Build/corporate-catering-system/migrations#main" \
  -database "$PG_URL" up
```

Run the Hydra schema migrations:

```bash
kubectl -n tbite run hydra-mig --rm -i \
  --image=oryd/hydra:v26.2.0 \
  --restart=Never \
  --env="DSN=postgres://hydra:${HYDRA_PG_PW}@tbite-pg-rw.tbite.svc.cluster.local:5432/hydra?sslmode=disable" \
  --command -- hydra migrate sql up -e --yes
```

Provision the JetStream streams the workers consume:

```bash
PLATFORM_IMAGE="ghcr.io/agentic-build/tbite-api:$(yq -r '.image.tag' chart/tbite-platform/values.yaml)"

kubectl -n tbite run prov --rm -i \
  --image="$PLATFORM_IMAGE" \
  --restart=Never \
  --env="NATS_URL=nats://tbite-nats.tbite.svc.cluster.local:4222" \
  --env="DATABASE_RW_URL=$PG_URL" \
  --env="REDIS_URL=redis://:${VALKEY_PW}@tbite-valkey-primary.tbite.svc.cluster.local:6379/0" \
  --command -- /usr/local/bin/tbite --role=provision-streams
```

Restart the Go application Deployments so they pick up the freshly
created `tbite-db` secret:

```bash
kubectl -n tbite rollout restart deploy \
  -l app.kubernetes.io/part-of=tbite-platform
```

Within ~30 seconds the api, realtime, worker, and scheduler pods
should all reach `Running 1/1`. Verify:

```bash
kubectl -n tbite get pods
# 22 pods in Running 1/1 (or 2/2 / 3/3 for the multi-container ones)
```

---

## Step 9 — Seed the TSMC enterprise demo

The application catalog (10 vendors + 150 menu items + meal supply for
7 days) lives in `scripts/dev/seed-p2.sql`. The full TSMC demo then
layers canonical demo orders, pickup-location remapping, and 50,000
synthetic employees:

```bash
make demo-seed-tsmc
```

`ops/demo/seed-tsmc-enterprise.sh` reads the chart contract Secret
`tbite-db` key `rwUrl`, starts short-lived psql pods, and applies:

1. `scripts/dev/seed-p2.sql` — vendors, menu, image rows, 7 days of supply.
2. `scripts/dev/seed-demo.sql` — canonical demo users and visible orders.
3. `scripts/dev/seed-tsmc.sql` — 19 pickup locations and service windows.
4. `scripts/dev/seed-tsmc-scale.sql` — 50,000 employees and scaled supply.

Confirm:

```bash
kubectl -n tbite run tsmc-seed-check --rm -i --restart=Never \
  --image=ghcr.io/cloudnative-pg/postgresql:17.2 \
  --env="DATABASE_RW_URL=$(kubectl -n tbite get secret tbite-db -o jsonpath='{.data.rwUrl}' | base64 -d)" \
  --command -- sh -ec 'psql "$DATABASE_RW_URL" -tAc "
    SELECT plant, count(*)
    FROM \"user\"
    WHERE primary_email ~ '\''^tsmc[0-9]{5}@tbite\.test$'\''
    GROUP BY plant
    ORDER BY plant;
  "'
# hc-12a-1f|3600
# ...
# tn-18p7-2f|4000
```

---

## Step 10 — First-time Authentik login + rotate the OIDC client secret

The Authentik blueprint mounted by the chart created the `tbite`
OAuth2 application with a seed `client_secret` that you MUST rotate.

1. Open `https://auth.tbite.example.com/if/admin/` in a browser.
2. Bootstrap the admin: Authentik prints the initial password to the
   server pod log on first boot.
   ```bash
   kubectl -n tbite logs deploy/tbite-authentik-server | grep -i "bootstrap password" | head -1
   ```
3. Log in as `akadmin` with that password. Change it immediately in
   `Admin → Users → akadmin → Set password`.
4. Generate an API token: `Admin → Directory → Tokens → Create →
   Intent: API`. Copy the token.
5. Rotate the application `client_secret`: `Admin → Applications →
   Providers → T-Bite OIDC → Edit → Client Secret → Regenerate`. Copy
   the new value.
6. Push the rotated values back into Kubernetes:
   ```bash
   AUTHENTIK_API_TOKEN="<paste-token>"
   NEW_OIDC_SECRET="<paste-client-secret>"
   kubectl -n tbite patch secret tbite-authentik --type=merge \
     -p "$(printf '{"data":{"apiToken":"%s"}}' "$(printf %s "$AUTHENTIK_API_TOKEN" | base64)")"
   kubectl -n tbite patch secret tbite-oidc-clients --type=merge \
     -p "$(printf '{"data":{"apiClientSecret":"%s"}}' "$(printf %s "$NEW_OIDC_SECRET" | base64)")"
   kubectl -n tbite rollout restart deploy/tbite-api
   ```

The api role's OIDC discovery against
`https://auth.tbite.example.com/application/o/tbite/.well-known/openid-configuration`
will now succeed; check the log:

```bash
kubectl -n tbite logs deploy/tbite-api --tail=10
# … "msg":"http listening","addr":":8080"
# … "msg":"board consumer started, tapping ORDERS_V1"
# … "msg":"readmodel invalidator started"
# … "msg":"mcp server mounted","oauth_metadata":true
```

---

## Step 11 — Smoke test the five UIs through Cloudflare

| Open | Login | Expect to see |
| --- | --- | --- |
| `https://app.tbite.example.com` | `e2e-employee@tbite.test` / `tbite-dev-pass` (the dev-users blueprint seeded this) | Home page picking lunch for `hc-12a-1f`; three carousels (再點一次 / 我的最愛 / 推薦你今天); 10 vendors, 150 menu items |
| `https://merchant.tbite.example.com` | `e2e-merchant@tbite.test` / `tbite-dev-pass` (vendor `r001 阿城炙燒便當`) | Prep board; place an order from the employee app and watch it appear live via SSE (no refresh) |
| `https://admin.tbite.example.com` | `e2e-admin@tbite.test` / `tbite-dev-pass` | Welfare admin view: order board across all 19 pickup locations, vendor approval queue, payroll cycle, compliance docs |
| `https://grafana.tbite.example.com` | `admin` / value of `$GRAFANA_PW` from `demo-secrets.local` | 19 dashboards grouped by domain tags such as `overview`, `platform`, `infra`, `domain`, and `slo`; start with **outbox-and-events**, **pg-routing**, **sse-gateway**, and **role-readiness** for operational checks |
| `https://argocd.tbite.example.com` | `admin` / `kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' \| base64 -d` | Cluster sync status and day-2 reconciliation |

End-to-end SSE check from the employee side: place an order in
employee app → merchant app instantly shows new order on the prep
board → mark it `ready` → employee app instantly shows pickup TOTP.

---

## Step 12 — Verify the architecture contracts

The split worker / scheduler / realtime roles each declare their own
dependency list at `/readyz`:

```bash
NB=$(kubectl -n tbite get pod tbite-nats-box -o jsonpath='{.items[0].metadata.name}' 2>/dev/null \
  || kubectl -n tbite get pod -l app.kubernetes.io/name=nats-box -o jsonpath='{.items[0].metadata.name}')

probe() {
  local label=$1 port=$2
  local pod=$(kubectl -n tbite get pod -l app.kubernetes.io/component="$label" -o jsonpath='{.items[0].metadata.name}')
  local ip=$(kubectl -n tbite get pod "$pod" -o jsonpath='{.status.podIP}')
  echo "=== $label /readyz ==="
  kubectl -n tbite exec "$NB" -- wget -qO- "http://${ip}:${port}/readyz"
  echo
}

probe realtime 8081
probe worker-outbox-relay 2112
probe scheduler-cutoff 2112
```

Expected output:

```
=== realtime /readyz ===
{"deps":[{"name":"postgres-rw","ok":true},{"name":"valkey","ok":true},{"name":"nats","ok":true}],"status":"ready"}
=== worker-outbox-relay /readyz ===
{"deps":[{"name":"postgres-rw","ok":true},{"name":"nats","ok":true}],"status":"ready"}
=== scheduler-cutoff /readyz ===
{"deps":[{"name":"postgres-rw","ok":true}],"status":"ready"}
```

Total memory budget — should fit comfortably in 32 GiB:

```bash
kubectl -n tbite get pods -o json | \
  jq -r '.items[] | .spec.containers[] | .resources.requests.memory // "0"' | \
  awk 'function b(v){if(v~/Gi$/)return substr(v,1,length(v)-2)*1024;if(v~/Mi$/)return substr(v,1,length(v)-2);return 0}{m+=b($0)}END{printf "%.1f GiB across %d containers\n",m/1024,NR}'
# 8.9 GiB across 31 containers
```

---

## Step 13 — Run the applied SRE demo

The applied demo is intentionally short:

```bash
# Generate lunch-peak traffic through local port-forwards.
DURATION=8m RPS=12 CONCURRENCY=16 EMPLOYEES=800 make demo-load-tsmc

# In another terminal, delete one pod and watch self-healing.
make demo-crisis component=api
kubectl -n tbite rollout status deploy -l app.kubernetes.io/component=api --timeout=3m
```

Grafana views to keep open:

- `oncall-overview`
- `order-api-slo`
- `role-readiness`
- `supply-health`
- `outbox-and-events`
- `sse-gateway`

Repeat with `component=realtime`, `component=worker-outbox-relay`, or
`component=cloudflared` to demonstrate different failure surfaces. The
full demo checklist lives in
[`docs/demo/tsmc-applied-playbook.md`](../demo/tsmc-applied-playbook.md).

---

## Adding more pickup locations later

The chart does not need to be touched to add a new pickup point — it
is one row in `vendor_plant_mapping` per vendor that should serve
the new location:

```sql
INSERT INTO vendor_plant_mapping (vendor_id, plant, service_window)
VALUES
  ('a1111111-1111-1111-1111-111111111111', 'tn-22-1f', '11:30-13:00'),
  ('a2222222-2222-2222-2222-222222222222', 'tn-22-1f', '11:30-13:00');
```

Employees and operators see the new pickup location appear in their
plant picker on next page load; no Helm release rev needed.

To remove a pickup location, soft-disable rather than delete (orders
referencing the plant carry a foreign-key relationship to the string,
not the row):

```sql
UPDATE vendor_plant_mapping SET active = false WHERE plant = 'tn-22-1f';
```

## Day-2 operations

### See what is running

```bash
kubectl -n tbite get pods,svc,pvc
helm -n tbite status tbite
kubectl -n tbite logs deploy/tbite-cloudflared
# Zero Trust dashboard → Networks → Tunnels → tbite-demo → Connectors
# shows live cloudflared replicas + edge connections.
```

### Logs

```bash
kubectl -n tbite logs -l app.kubernetes.io/component=api -f
kubectl -n tbite logs -l app.kubernetes.io/component=realtime -f
kubectl -n tbite logs -l app.kubernetes.io/component=worker-outbox-relay -f
```

### Upgrade the application image (after a git push to main)

CI builds and publishes `ghcr.io/agentic-build/tbite-api:sha-<short>`
and writes the new tag into `chart/tbite-platform/values.yaml`. To
roll out:

```bash
git pull --ff-only
helm upgrade tbite chart/tbite-platform/ \
  -f chart/tbite-platform/values.yaml \
  -f chart/tbite-platform/values-cloudflared.yaml \
  --namespace tbite \
  --reuse-values
```

### Stop the cluster, keep data

```bash
helm -n tbite uninstall tbite
# Postgres + NATS + Valkey + MinIO PVCs persist; re-install picks them up.
```

### Fully tear down

```bash
helm -n tbite uninstall tbite
helm -n cnpg-system uninstall cnpg
kubectl delete namespace tbite cnpg-system
# Cloudflare Zero Trust dashboard → Networks → Tunnels → tbite-demo
# → "..." → Delete tunnel
sudo /usr/local/bin/k3s-uninstall.sh
```

---

## Troubleshooting

**`helm dependency update` errors with `can't get a valid version`** —
the upstream subchart repository moved or yanked the pinned version.
Run `helm search repo <name> --versions` to find a current one and
update `chart/tbite-platform/Chart.yaml` accordingly. The chart was
last pinned 2026-05; subchart releases shift every few weeks.

**Pods stuck `CreateContainerConfigError: secret "X" not found`** —
re-check Step 5 for `X`. The cloudflared and identity-DB secrets must
all exist before the helm upgrade in Step 7.

**`kubelet: Failed to pull image ... no matching manifest for linux/arm64`** —
your cluster is Apple Silicon (kind / k3d / OrbStack). Build a local
arm64 image and override:
```bash
make image-build-local TAG=local-arm64
# k3s: import into containerd
docker save ghcr.io/agentic-build/tbite-api:local-arm64 | sudo k3s ctr images import -
# helm upgrade with --set image.tag=local-arm64 --set image.pullPolicy=Never
```

**Authentik returns 500** — first boot has not finished blueprint
reconciliation. Wait for `kubectl -n tbite logs deploy/tbite-authentik-worker`
to print `Task finished … apply_blueprint`, then try again.

**Cloudflare returns 502** — the tunnel is up but the in-cluster
Service for the hostname's public-hostname mapping returns no
endpoints. Cross-check (a) the Service URL you entered in Step 4b
matches `kubectl -n tbite get svc` exactly, and (b) the Service has
endpoints (`kubectl -n tbite get endpoints <name>` — usually a pod
is in CrashLoop).

**SSE drops at 100s** — Cloudflare's edge enforces a 100s idle
timeout. The application's SSE handler emits a 20s heartbeat so the
connection should not idle out. If it still does, double-check the
realtime public-hostname's "TLS → Disable Chunked Encoding" setting
(off) in the dashboard.

**Public hostname returns Cloudflare error 1033 ("tunnel error")** —
the cloudflared pods cannot reach Cloudflare's edge. Check the
cloudflared pod log (`kubectl -n tbite logs deploy/...-cloudflared`)
and verify the cluster has outbound network access on TCP 7844 / UDP
7844 (QUIC).

---

## Notes for Apple Silicon (OrbStack)

`k3s` itself does not install on macOS. The supported local k8s on
Apple Silicon are OrbStack Kubernetes, kind, or k3d. The playbook
otherwise applies verbatim; substitute Step 2 with:

```bash
# OrbStack: just enable Kubernetes in the OrbStack UI; nothing to install.
kubectl config use-context orbstack
```

Image pulls need a multi-arch (`linux/amd64,linux/arm64`) image. The
repo's `cd-publish-images` workflow publishes both architectures on
every main merge; for development off-main, run `make image-build-local`
and override `image.tag` / `image.pullPolicy` per the troubleshooting
note above.
