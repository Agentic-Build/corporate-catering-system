# ADR 0006 — Secrets and Air-Gapped Deployment Contract

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#53](https://github.com/Agentic-Build/corporate-catering-system/issues/53)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

Enterprise self-hosting frequently entails restricted networks, private
registries, internal certificate authorities, and controlled secret
distribution channels. The corporate catering platform addresses
customers whose security postures range from permissive cloud accounts
to fully air-gapped industrial sites. The deployment architecture must
therefore assume that runtime components may have no outbound internet
access and that secret handling and artifact distribution are part of
the baseline contract rather than an operational afterthought. A design
that depends on live chart downloads from public repositories, live
container image pulls from public registries, or live metadata service
lookups would silently fail in restricted environments and would push
the burden of remediation onto each customer.

The architectural pressure is twofold. First, the secret workflow must
be portable and Git-friendly so that customers can manage encrypted
material alongside the configuration that consumes it. Second, the
distribution mechanism must permit complete vendoring of charts and
images so that an installation can be performed entirely from
locally-staged artifacts.

## Decision

SOPS with age is the canonical secrets workflow. Air-gapped deployment
is a formal product requirement.

## Rationale

SOPS combined with age provides the stack with a portable,
Git-friendly secret mechanism that does not require HashiCorp Vault or
a cloud-managed secret manager to be present in the customer
environment. age keys are simple to generate and distribute, and SOPS
encrypts only the values of YAML or JSON documents (not the keys),
which preserves diff-friendliness in source control. The combination
fits small and medium enterprise installations while still permitting
stronger integrations later: customers who already operate Vault or who
prefer Kubernetes External Secrets Operator may layer those in as
optional integrations without disturbing the canonical SOPS path.

Treating air-gapped deployment as a baseline requirement prevents
hidden dependencies on public registries, live chart downloads, or
external metadata services from accumulating in the chart or in the
application code. Every dependency must be mirrorable into a private
registry, every chart must be pinned and vendorable, and runtime
operation must not require public network egress.

The principal alternative considered and rejected was HashiCorp Vault
as the canonical secret store. Vault is operationally stronger and
provides dynamic credential issuance that SOPS cannot, but installing
and operating Vault is itself a substantial responsibility for the
customer, and Vault is not Git-friendly in the same way that SOPS is.
Adopting Vault as canonical would have imposed a heavyweight
dependency on every installation, including those for which a
filesystem-resident encrypted secret file is entirely sufficient. The
chart therefore admits Vault as an optional integration but does not
require it. A second alternative considered was to rely on cloud-vendor
secret managers (for example AWS Secrets Manager or GCP Secret
Manager). That alternative was rejected because it directly contradicts
the air-gap requirement and would split the production contract into
cloud-specific shapes.

## Design Implications

The decision produces the following operational shape. Encrypted
secret files live in the deployment repository. Decrypted output
becomes ordinary Kubernetes Secret resources consumed by the chart.
All container images required by the chart must be mirrorable into a
private registry; the chart references images by their canonical
identifiers and permits a values-supplied registry prefix to redirect
to a mirror. Helm charts and their dependencies must be pinned in
`Chart.yaml` and vendorable for offline installation; the chart's
dependency lock file is committed. Runtime operation must not require
public network egress; no application service may rely on a public
metadata or licensing endpoint as a precondition for serving traffic.

The chart introduces the following secret-related artifacts:
`.sops.yaml` at the repository root to declare creation rules and
recipients, `ops/secrets/example.sops.yaml` as a reference encrypted
secret, and `docs/deployment/secrets.md` documenting the operator
workflow. Air-gap installation procedures are documented in
`docs/deployment/airgapped.md`. The Makefile exposes `sops-encrypt` and
`sops-decrypt` targets to standardize the operator interface.

## Acceptance Criteria

- Install and upgrade documentation covers image mirroring, chart vendoring, and secret decryption.
- Production values never place secret material in ConfigMaps or unencrypted values files.
- The same Kubernetes Secret contract supports local and production deployments.
- Optional Vault or External Secrets integrations can be added without changing the canonical SOPS path.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Install and upgrade documentation covers image mirroring, chart vendoring, and secret decryption. | `docs/deployment/airgapped.md` covers image mirroring and chart vendoring; `docs/deployment/secrets.md` covers SOPS decryption workflow. |
| Production values never place secret material in ConfigMaps or unencrypted values files. | `chart/tbite-platform/values-prod.yaml` references Kubernetes Secret resources by name; encrypted material is held in SOPS-encrypted files governed by `.sops.yaml` and the example at `ops/secrets/example.sops.yaml`. |
| The same Kubernetes Secret contract supports local and production deployments. | `chart/tbite-platform/values-dev.yaml` and `chart/tbite-platform/values-prod.yaml` reference Kubernetes Secret resources through identical chart-level keys; only the source of secret material differs. |
| Optional Vault or External Secrets integrations can be added without changing the canonical SOPS path. | The chart consumes Kubernetes Secret resources by name and does not depend on the upstream source; an External Secrets integration can populate the same Secret resources without changes to chart templates. **Follow-up** — a reference External Secrets integration may be added if customer demand warrants. |

## Scope Boundary

HashiCorp Vault and cloud-vendor secret managers are optional
integrations. They are not required for the baseline installation.
This ADR does not select an age key distribution policy, an internal
certificate authority topology, or a private registry implementation;
each is environment-specific and supplied through values and operator
documentation.

## References

- Source issue: [Agentic-Build/corporate-catering-system#53](https://github.com/Agentic-Build/corporate-catering-system/issues/53)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0002-helm-umbrella-chart.md`](adr-0002-helm-umbrella-chart.md), [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md)
