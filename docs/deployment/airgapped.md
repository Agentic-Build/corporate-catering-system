# Air-gapped baseline

Per architecture issue #53, the tbite-platform runtime **must not require
public network egress**. Every container image, Helm sub-chart, and OCI
artifact the cluster pulls at install or upgrade time has to be reachable
from inside the customer's network. This page is the operator playbook for
mirroring the dependencies and the install-time invariants the runtime
enforces.

The corollary: if a component can only function with a SaaS callback
(public OIDC discovery, telemetry hosted on the public internet, an OCR
API that lives somewhere else), it does not belong in the baseline. Such
features go behind a tag and ship disabled by default.

## What "air-gapped" means here

- The cluster has DNS + L3 reachability to a **private registry** (Harbor,
  registry:2 behind TLS, Artifactory, Nexus, ACR/ECR/GCR with private
  endpoints, …) and an **internal git/HTTP mirror** for Helm charts.
- The cluster has **no route** to `*.docker.io`, `*.ghcr.io`,
  `charts.bitnami.com`, `nats-io.github.io`, etc.
- Operators have a workstation that can reach both the public internet
  (for the initial mirror sync) and the private registry (to push). That
  workstation never becomes a runtime dependency.
- Bootstrap secrets (registry pull secret, age private key, kubeconfig)
  arrive via the customer's normal out-of-band channel.

## Image mirroring (skopeo)

Use [`skopeo copy`][skopeo] to mirror each public image into the private
registry. `skopeo` understands all the registry dialects and supports
multi-arch by default; prefer it over `docker pull && docker push`, which
loses signatures and silently demotes manifests to single-arch.

[skopeo]: https://github.com/containers/skopeo

```bash
# One-off: mirror a single image, all platforms.
skopeo copy --multi-arch=all \
  docker://ghcr.io/agentic-build/tbite-api:sha-766bf49 \
  docker://registry.internal.example.com/agentic-build/tbite-api:sha-766bf49

# Mirror a whole repo at a tag (everything that matches the regex).
skopeo sync --src docker --dest docker --scoped \
  ghcr.io/agentic-build/tbite-api \
  registry.internal.example.com/agentic-build/

# Loop the chart's image-pull set from a manifest.
while read -r ref; do
  dst="registry.internal.example.com/${ref#*/}"
  skopeo copy --multi-arch=all "docker://$ref" "docker://$dst"
done < ops/airgap/images.txt
```

The platform's own images live under `ghcr.io/agentic-build/tbite-*` (see
[`argocd.md`](./argocd.md) for the naming table). Third-party images come
from the sub-charts listed in `chart/tbite-platform/Chart.yaml`; render the
chart once with `helm template` against your values and grep for `image:`
to produce the full set.

After mirroring, point the cluster at the private registry. The cleanest
option is a cluster-wide `images:` rewrite in the overlay (kustomize
`images:` block) or, for ArgoCD installs, a `kustomize.images` override on
the `Application`. Avoid runtime `containerd` rewrite rules unless you're
already running them for other reasons — they're invisible to anyone
reading the manifest.

## Chart vendoring

Helm dependencies in `chart/tbite-platform/Chart.yaml` resolve from public
chart repos by default. For an air-gapped install you must vendor them:

```bash
# Resolve everything Chart.yaml declares and write the .tgz files into
# chart/tbite-platform/charts/. Run from the chart directory.
cd chart/tbite-platform
helm dependency update

# After this, charts/ contains one .tgz per dependency, pinned to the
# version listed in Chart.yaml. Commit charts/ to a private mirror of the
# repo (or to a vendor branch); the runtime install must not re-resolve.
git add charts/ Chart.lock
git commit -m "vendor chart deps @ $(date -u +%Y-%m-%d)"
```

The `charts/` directory is currently empty in this repo because we expect
operators to vendor at install time against their own private mirror. If
you fork the repo into an internal Git, commit `charts/` and `Chart.lock`
to your fork — that is the supported, reproducible install path.

For OCI-distributed charts (e.g. when the upstream switches from a HTTP
chart repo to an OCI registry), `skopeo` works on those too:

```bash
skopeo copy \
  docker://registry-1.docker.io/bitnamicharts/valkey:2.4.1 \
  docker://registry.internal.example.com/bitnamicharts/valkey:2.4.1
```

Then override `repository:` in `Chart.yaml` (or pass `--repo` to
`helm dependency update`) to the private mirror before vendoring.

## Runtime invariants

These are enforced by review, not by code — the platform won't crash if
you break them, but it will start phoning home and that is precisely what
this baseline forbids.

- **No `imagePullPolicy: Always` against a public registry.** Pin
  `imagePullPolicy: IfNotPresent` and immutable `sha-…` tags so a cluster
  restart after the public internet goes away still schedules pods.
- **No public discovery URLs** in chart values. OIDC discovery
  (`/.well-known/openid-configuration`), Hydra issuer URL, the realtime
  gateway's WS endpoint — all of these must resolve to an in-cluster or
  in-VPC hostname. The chart defaults assume Authentik/Hydra are
  co-installed; if you BYO an external IdP, it must live inside the
  air-gapped network.
- **No outbound telemetry by default.** The OpenTelemetry collector ships
  with the in-cluster Victoria-Metrics/Logs/Traces backends as exporters;
  any SaaS exporter (Honeycomb, Datadog, Lightstep, …) is opt-in and goes
  through a separately reviewed egress allowlist.
- **No `helm dependency update` at install time.** The `charts/` tarballs
  must already be present in your fork. The runtime install command is
  `helm install` (or ArgoCD sync) only.
- **No `kubectl apply -f https://…`** in any runbook. Manifests live in
  the repo; URLs are for documentation only.

## Putting it together

A first install against an air-gapped cluster, in rough order:

1. From a connected workstation: `git clone` this repo into your internal
   git mirror; `helm dependency update` inside `chart/tbite-platform/`;
   commit the resulting `charts/`.
2. From the same workstation: `skopeo copy` every public image listed by
   `helm template … | grep image:` plus the
   `ghcr.io/agentic-build/tbite-*` set into your private registry.
3. On the cluster operator workstation: pull the internal mirror, point
   `kubectl` at the air-gapped cluster, generate the SOPS files for that
   environment under `ops/secrets/<env>/` (see
   [`secrets.md`](./secrets.md)), and `make prod-up env=<env>` (or sync
   the ArgoCD `Application`).
4. Verify zero public-internet egress with the cluster's NetworkPolicy
   audit log / egress firewall — the platform should be quiet apart from
   in-cluster traffic and your monitoring backends.
