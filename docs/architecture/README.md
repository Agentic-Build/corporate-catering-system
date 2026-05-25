# Architecture Decision Records and Architecture Specifications

## Purpose

This directory contains the canonical architecture documentation for the
corporate catering system. Each document records a single, durable
architectural decision or specification governing how the platform is
deployed, packaged, scaled, and operated. The directory is intended to be
the system of record: a code change that contradicts a document recorded
here must either supersede the relevant document in the same change set or
be rejected.

The documents are written in formal academic prose and follow a uniform
structure (Status, Context, Decision, Rationale, Design Implications,
Acceptance Criteria, Compliance Evidence, Scope Boundary, References).
Each document is paired with one or more GitHub issues that captured the
original deliberation.

## Document Categories

Two document types appear in this directory:

- **ADR (Architecture Decision Record)** — a binding choice with at least
  one alternative considered and rejected. ADRs use the file prefix
  `adr-NNNN-*.md`.
- **Architecture Specification** — a binding statement of structure,
  contracts, or boundaries that follows from one or more ADRs. These use
  the file prefix `arch-NNNN-*.md`.

A single baseline document (`00-baseline.md`) sits above both categories
and records the system-level architecture contract from which the ADRs and
specifications derive.

## How to Add a New ADR or Architecture Specification

1. Open a GitHub issue capturing the context, the candidate decision,
   the considered alternatives, and the acceptance criteria. The issue
   number becomes the document's primary reference.
2. Allocate the next sequential `adr-NNNN-` or `arch-NNNN-` identifier.
   ADR numbers and architecture-specification numbers are independent
   sequences.
3. Create the document in this directory using the template implied by
   the existing documents: Status, Context, Decision, Rationale, Design
   Implications/Commitments, Acceptance Criteria, Compliance Evidence,
   Scope Boundary, References.
4. Set Status to `Proposed` while the decision is under review; advance
   to `Accepted` with an explicit calendar date when the decision is
   locked. Use `Superseded by adr-NNNN` when an ADR is replaced; never
   delete a superseded document.
5. Update `compliance-matrix.md` to include the new acceptance criteria
   and the artifacts that satisfy them.
6. Update this README's index to list the new document.

Status transitions are append-only. A document moves through
`Proposed → Accepted → (Superseded | Deprecated)` and never returns to
an earlier state.

## Index

The following sixteen documents constitute the locked architecture
baseline accepted on 2026-05-25.

| Document | One-line summary |
| --- | --- |
| [`00-baseline.md`](00-baseline.md) | System-level architecture contract for a self-hostable, cloud-native, plant-aware single-enterprise stack. |
| [`adr-0001-kubernetes-only-runtime.md`](adr-0001-kubernetes-only-runtime.md) | Kubernetes is the only formal runtime; dev, staging, and production share the same substrate. |
| [`adr-0002-helm-umbrella-chart.md`](adr-0002-helm-umbrella-chart.md) | A Helm umbrella chart is the canonical packaging format with batteries-included and BYO modes. |
| [`adr-0003-traefik-gateway-api-ingress.md`](adr-0003-traefik-gateway-api-ingress.md) | Traefik is the canonical gateway controller, with Gateway API as the primary route model and cert-manager for TLS. |
| [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md) | CloudNativePG, PgBouncer, Valkey HA, NATS JetStream, and MinIO Operator form the canonical data plane. |
| [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md) | VictoriaMetrics, VictoriaLogs, VictoriaTraces, Grafana, and the OpenTelemetry Collector are the canonical observability stack. |
| [`adr-0006-secrets-and-air-gap.md`](adr-0006-secrets-and-air-gap.md) | SOPS plus age is the canonical secrets workflow and air-gapped deployment is a baseline product requirement. |
| [`adr-0007-postgres-connection-and-backup.md`](adr-0007-postgres-connection-and-backup.md) | PgBouncer fronts Postgres, read/write routing is a first-class application contract, and PITR backs production. |
| [`adr-0008-single-enterprise-plant-aware-scaling.md`](adr-0008-single-enterprise-plant-aware-scaling.md) | One enterprise per stack; plant/date is the dominant internal partition key. |
| [`arch-0001-worker-role-split.md`](arch-0001-worker-role-split.md) | Background work is decomposed into role-specific deployments with independent scaling and failure domains. |
| [`arch-0002-durable-event-plane-and-outbox.md`](arch-0002-durable-event-plane-and-outbox.md) | NATS JetStream is the durable event plane and the transactional outbox is the only business-event exit. |
| [`arch-0003-realtime-sse-gateway.md`](arch-0003-realtime-sse-gateway.md) | A dedicated realtime gateway serves topic-scoped SSE fanout, isolated from the API request path. |
| [`arch-0004-read-models-and-caching.md`](arch-0004-read-models-and-caching.md) | Employee home, menu availability, and recommendations are served from event-driven read models with bounded eventual consistency. |
| [`arch-0005-direct-object-storage-path.md`](arch-0005-direct-object-storage-path.md) | Bulk file transfer flows directly to object storage; the API authorizes but does not proxy bytes. |
| [`arch-0006-authentik-hydra-identity-boundary.md`](arch-0006-authentik-hydra-identity-boundary.md) | Authentik is the reference enterprise SSO provider; Hydra retains MCP Dynamic Client Registration responsibility. |
| [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md) | Lifecycle tasks are explicit Kubernetes resources; readiness and autoscaling use workload-aware signals. |

The companion document [`compliance-matrix.md`](compliance-matrix.md)
provides a single mapping from every acceptance criterion across the
sixteen documents to the artifacts that satisfy it.

## Companion operational documents

These supplement the baseline with the per-role and runbook detail the
acceptance criteria call for:

| Document | Covers |
| --- | --- |
| [`worker-roles.md`](worker-roles.md) | Per-role scaling signal, idempotency, retry, and DLQ behaviour (#56, #57). |
| [`../deployment/local-clusters.md`](../deployment/local-clusters.md) | kind / k3d / OrbStack local cluster setup (#48). |
| [`../deployment/backup-restore.md`](../deployment/backup-restore.md) | Data-plane backup, restore, and drill procedure (#51, #54). |
