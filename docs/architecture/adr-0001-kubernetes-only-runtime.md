# ADR 0001 — Kubernetes-Only Runtime and Environment Model

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#48](https://github.com/Agentic-Build/corporate-catering-system/issues/48)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

A self-hosted enterprise product requires a single operational model that
is observable end-to-end and reproducible across every environment in
which it runs. When development is conducted on one runtime shape, such
as a docker-compose graph or a hand-curated set of single-binary
processes, and production is conducted on a different runtime shape,
such as Kubernetes, the operational divergence does not remain
contained. It propagates outward and reappears as failures attributed to
application code: differing DNS resolution behavior, divergent storage
semantics, mismatched probe contracts, dissimilar dependency discovery
patterns, and subtle inconsistencies in network policy. Each such
divergence represents environmental drift between development and
production, and each such divergence accumulates a class of defect that
is difficult to reproduce and expensive to investigate.

The architectural pressure is therefore to make local testing smaller in
scale than production while remaining identical in shape. A development
environment must run with reduced replica counts, reduced resource
reservations, and reduced storage sizes. It must not run a different
runtime model that incidentally provides the same business behavior
under benign conditions. The deployment baseline should make local
testing smaller, not semantically different.

## Decision

Kubernetes is the only formal runtime for development, staging, and
production. The supported contract is CNCF-conformant Kubernetes. Local
environments may use kind, k3d, or OrbStack Kubernetes. Production
environments must rely on standard Kubernetes APIs and must avoid
OpenShift-specific assumptions unless a later customer-specific
decision introduces that support explicitly and behind values gates.

## Rationale

The decision elevates Kubernetes from a production wrapper to the shared
substrate for the entire system. The same Helm chart family can render
deployments for local clusters, staging environments, and production
environments, with the only differences appearing in values: replica
counts, resource requests, storage classes, storage sizes, public
domains, certificate issuance modes, and secret sources. Dependency
contracts (PostgreSQL, Valkey, NATS JetStream, MinIO, Traefik, the
observability stack) remain visible during development, and operational
concerns such as readiness probes, liveness probes, pod disruption
budgets, ingress routing, and secret consumption become testable before
release rather than discovered during incident response.

An alternative considered and rejected was to retain a docker-compose
graph as a co-equal development runtime on the grounds that
docker-compose is faster to start and lighter on developer machines. The
alternative was rejected because it would reintroduce precisely the
divergence the decision exists to eliminate. A docker-compose graph
cannot exercise probe contracts, cannot exercise ingress and TLS
behavior, cannot exercise StatefulSet upgrade ordering, cannot exercise
PodDisruptionBudget interactions, and cannot exercise the observability
ingest path; the resulting confidence from passing a compose-based test
is therefore not equivalent to the confidence from passing a
chart-based test. A second alternative considered was to certify
specific distributions (for instance OpenShift) as part of the baseline.
That alternative was rejected because distribution-specific gating
expands the testing matrix without an articulated customer requirement;
provider-specific integrations may be added later behind explicit
values gates.

## Design Implications

Local values files may reduce replicas, resource reservations, and
storage size. Local values files may not replace the architectural role
of PostgreSQL, Valkey, NATS JetStream, MinIO, ingress, or the
observability stack with a substitute that does not honor the same
service contracts. Development shortcuts must preserve the same service
names, environment variables, health semantics, and deployment
boundaries used in production, so that defects observed in either
environment are reproducible in the other.

A practical consequence is that the Helm umbrella chart governed by
[`adr-0002`](adr-0002-helm-umbrella-chart.md) must expose two values
profiles, `values-dev.yaml` and `values-prod.yaml`, that share schema
and structure and that differ only in dimensions sanctioned by this
ADR. Where a value reduction would change a service contract rather
than a size, the chart must refuse the value through `values.schema.json`
validation.

## Acceptance Criteria

- Dev, staging, and production all deploy through the same chart family.
- Runtime differences are confined to values: replicas, resources, storage class, storage size, domains, certificates, and secrets.
- docker-compose and single-node manifests are not production behavior models.
- Local instructions cover kind, k3d, and OrbStack Kubernetes.
- The baseline uses standard Kubernetes APIs unless a provider-specific integration is explicitly guarded behind values.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Dev, staging, and production all deploy through the same chart family. | `chart/tbite-platform/` with shared templates and `chart/tbite-platform/values.yaml` schema; environment-specific overlays in `chart/tbite-platform/values-dev.yaml` and `chart/tbite-platform/values-prod.yaml`. |
| Runtime differences are confined to values: replicas, resources, storage class, storage size, domains, certificates, and secrets. | `chart/tbite-platform/values.schema.json` enforces the permitted value surface; `chart/tbite-platform/values-dev.yaml` and `chart/tbite-platform/values-prod.yaml` differ only along these dimensions. |
| docker-compose and single-node manifests are not production behavior models. | The chart is the only packaging artifact governed by this baseline. Any existing single-node manifest is retained for non-production use only and is not part of the production contract. |
| Local instructions cover kind, k3d, and OrbStack Kubernetes. | **Follow-up** — local cluster instructions for kind, k3d, and OrbStack are to be added to `docs/deployment/` in a subsequent change. |
| The baseline uses standard Kubernetes APIs unless a provider-specific integration is explicitly guarded behind values. | The chart templates use core Kubernetes APIs, Gateway API, and the operators declared in [`adr-0004`](adr-0004-self-hosted-ha-data-plane.md); no OpenShift-specific or provider-specific resources are included unconditionally. |

## Scope Boundary

This ADR does not decide ArgoCD application topology, OpenShift support,
or any customer-specific Kubernetes distribution certification. It does
not select a specific local cluster runtime as canonical among kind,
k3d, and OrbStack; each is supported.

## References

- Source issue: [Agentic-Build/corporate-catering-system#48](https://github.com/Agentic-Build/corporate-catering-system/issues/48)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0002-helm-umbrella-chart.md`](adr-0002-helm-umbrella-chart.md), [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md)
