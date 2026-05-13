# T-Bite Security Baseline Checklist

> Documents how each item in the K8s + container security baseline is satisfied
> by the manifests already in this repository. Verified as of P8 completion
> (commit `feat/p8-hardening`).

Each item below cites `file:line` where the setting is enforced. To re-verify
after a future change, run the same `grep` against `ops/kubernetes/`.

---

## 1. Pod / container security

All six application deployments (`api`, `worker`, `scheduler`, `web-employee`,
`web-merchant`, `web-admin`) apply the same `securityContext` template — a
pod-level block plus a container-level block.

| Item | Required | Where enforced |
| --- | --- | --- |
| `runAsNonRoot: true` | yes | `ops/kubernetes/base/deployment-api.yaml:28` + every other `deployment-*.yaml` line 27-28 |
| `runAsUser: 65532` | yes | `ops/kubernetes/base/deployment-api.yaml:29` (and analogous lines in the five sibling deployments) |
| `fsGroup: 65532` | yes | `ops/kubernetes/base/deployment-api.yaml:30` |
| `seccompProfile.type: RuntimeDefault` | yes | `ops/kubernetes/base/deployment-api.yaml:31-32` |
| `allowPrivilegeEscalation: false` | yes | `ops/kubernetes/base/deployment-api.yaml:85` |
| `readOnlyRootFilesystem: true` | yes | `ops/kubernetes/base/deployment-api.yaml:86` |
| `capabilities.drop: [ALL]` | yes | `ops/kubernetes/base/deployment-api.yaml:87-88` |

Sibling deployments enforce the same settings at identical relative offsets:

- `ops/kubernetes/base/deployment-worker.yaml` (pod block L27-32, container block L48-51)
- `ops/kubernetes/base/deployment-scheduler.yaml` (pod block L27-32, container block L59-62)
- `ops/kubernetes/base/deployment-web-employee.yaml` (pod block L27-32, container block L65-68)
- `ops/kubernetes/base/deployment-web-merchant.yaml` (pod block L27-32, container block L65-68)
- `ops/kubernetes/base/deployment-web-admin.yaml` (pod block L27-32, container block L65-68)

Service-account tokens are not auto-mounted into application pods
(`automountServiceAccountToken: false`, e.g.
`ops/kubernetes/base/deployment-api.yaml:26`), removing an unnecessary
in-cluster attack surface.

---

## 2. Network policies

| Item | Where enforced |
| --- | --- |
| Default-deny ingress + egress for the `tbite` namespace | `ops/kubernetes/base/networkpolicy-default-deny.yaml:1-14` (empty `podSelector`, both `policyTypes`) |
| Allow-list app -> Postgres / Redis / NATS / MinIO | Single-node overlay relies on Helm-installed ingress-nginx ingress + namespace-local services; production allow-lists are encoded at the cloud-provider layer (Cloud SQL VPC peering / Memorystore private service connect / GCS over Google APIs). |

Open production hardening item: write explicit `NetworkPolicy` allow-list
manifests for the single-node overlay covering `api -> postgres / redis / nats /
minio`, plus `web-* -> api`, before opening the namespace to multi-tenant
workloads. See "Open items" below.

---

## 3. Secrets

| Item | Where enforced |
| --- | --- |
| No plaintext secrets baked into images | All app images are built from `gcr.io/distroless/static:nonroot` with only the compiled binary copied in — see `services/api/Dockerfile:8-13`. Web images run `node:20-alpine` with only the SvelteKit build output. No `.env` files are copied. |
| App reads every credential from env supplied by `Secret` / `ConfigMap` | `ops/kubernetes/base/deployment-api.yaml:41-64` (`envFrom: configMapRef` + per-key `secretKeyRef` for OIDC credentials) |
| Single-node uses a clearly-labelled DEV secret | `ops/kubernetes/overlays/single-node/secrets-bootstrap.yaml:1-3` (header comment explicitly warns "DO NOT USE OUTSIDE LOCAL DEV"); production overlays must override |
| Production uses External Secrets Operator + GCP Secret Manager | `ops/kubernetes/overlays/gcp/external-secrets.yaml` — declares one `SecretStore` (L36-55) and three `ExternalSecret`s for `tbite-postgres-password` (L58-79), `tbite-redis-auth` (L82-102), and `tbite-app-secrets` (L104-126), all authenticated via Workload Identity. |

SOPS encryption for single-node secrets is **not yet wired up** — the bootstrap
Secret is plaintext on the assumption it's local-only. Adding SOPS for shared
single-node deployments is listed under "Open items" below.

---

## 4. TLS

| Item | Where enforced |
| --- | --- |
| Single-node TLS termination | `ops/kubernetes/overlays/single-node/nginx-ingress.yaml` declares the `IngressClass`; cert-manager + Let's Encrypt is expected to be installed alongside ingress-nginx via Helm by the cluster operator (the overlay does not bundle the controller). |
| GCP TLS termination (Google-managed certs) | `ops/kubernetes/overlays/gcp/gke-ingress.yaml:17-30` (`ManagedCertificate tbite-prod-cert` covering 4 hostnames) plus `FrontendConfig` redirect-to-HTTPS (L33-44). |

---

## 5. Image

| Item | Where enforced |
| --- | --- |
| API built from minimal distroless base | `services/api/Dockerfile:8-13` — `FROM gcr.io/distroless/static:nonroot`, runs as `USER nonroot:nonroot`. Binary built `CGO_ENABLED=0` with `-ldflags="-s -w"`. |
| Web apps built from `node:20-alpine` | `apps/{employee,merchant,admin}/Dockerfile` use the same alpine base; SvelteKit adapter-node output only. |
| HIGH/CRITICAL CVE scan in CI | `.github/workflows/ci-build-images.yml` invokes `aquasecurity/trivy-action` on the API image after each PR build (see Trivy step). |
| Tag with image digest in production | **Deferred** — production deploys still use tag-based image references. See "Open items" below. |

---

## 6. Append-only audit

| Item | Where enforced |
| --- | --- |
| `audit_event` cannot be updated or deleted | `migrations/000003_order_lifecycle.up.sql:70-77` — `audit_event_append_only()` trigger function plus `BEFORE UPDATE` and `BEFORE DELETE` triggers that raise an exception. |
| `payroll_entry` cannot be deleted | `migrations/000005_payroll.up.sql:34-39` — `payroll_entry_no_delete()` trigger function and `BEFORE DELETE` trigger. |

The triggers raise a Postgres exception, so an accidental `DELETE` from an
admin tool fails loudly rather than silently corrupting the trail.

---

## Open items for production hardening

The items below are **not in scope for P8** but should be addressed before
exposing the system to external traffic at scale:

1. **Image signing (cosign / Sigstore)** — sign every image at build time and
   verify signatures via an admission controller (Kyverno or Connaisseur).
2. **Pin images by digest** — replace `tbite/api:dev` references with
   `tbite/api@sha256:...` in production overlays once a registry workflow
   exists.
3. **Allow-list `NetworkPolicy` per workload** — explicit `api -> postgres /
   redis / nats / minio`, `web-* -> api`, `worker -> nats / postgres`
   policies in the single-node overlay.
4. **Secret rotation policy** — document a 90-day rotation cadence for OIDC
   client secrets and DB passwords; automate via Secret Manager rotation
   schedules.
5. **RBAC scope review** — audit the `tbite-api` and `tbite-scheduler`
   ServiceAccount RBAC bindings; ensure scheduler only has the
   `Lease`-related permissions it needs for leader election (added in P8).
6. **SOC2 / ISO27001 audit trail** — wire the `audit_event` table to an
   external WORM store (S3 Object Lock or Cloud Storage Bucket Lock) on a
   nightly export.
7. **Penetration test** — engage an external firm for at minimum OWASP Top-10
   coverage against the three public-facing hostnames.
8. **Runtime security (Falco / GKE Cloud Run Security)** — enable runtime
   threat detection in the production cluster.

---

_Last verified: P8 — see commit `feat(scheduler): K8s Lease leader election`._
