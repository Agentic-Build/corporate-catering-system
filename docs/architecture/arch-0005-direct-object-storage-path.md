# Architecture Specification 0005 — Direct Object Storage Path

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#60](https://github.com/Agentic-Build/corporate-catering-system/issues/60)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

Object storage is already part of the corporate catering platform's
data plane and is governed for self-hosted deployments by the MinIO
Operator selection in
[`adr-0004`](adr-0004-self-hosted-ha-data-plane.md). The platform
handles two principal bulk-payload concerns: menu images, which are
displayed to all employees at high read volume, and compliance
documents, which are typically larger but accessed less frequently.
Both can be substantially larger than ordinary JSON request and
response payloads. The architectural pressure is to keep these bulk
payloads off the application's data path so that API and SSR pods are
not consuming CPU and memory budget proxying bytes that an
S3-compatible storage service can serve directly. Permitting API
proxying as the principal mechanism would cause the API to compete
for CPU and memory budget with itself: bulk file transfer would
contend with ordinary business request handling at precisely the
moment when application replicas should be serving business requests.

A related concern is the historical use of base64-encoded JSON payloads
for file uploads. Base64-encoded uploads inflate payload size by
approximately one third, place the entire decoded payload in
application memory before the storage service ever sees it, and
prevent streaming-style upload handling. The architecture should
foreclose this pattern in production.

## Decision

API proxying is not the primary path for bulk file transfer. The API
authorizes file operations and issues presigned URLs (or proxies
through an HTTP-cacheable gateway path) while the actual bytes flow
directly between the client and the object storage service.

## Rationale

The API should authorize and describe file operations, while object
storage and cacheable HTTP infrastructure should carry bytes. This
division of responsibility keeps the API stateless and lightweight,
permits MinIO or S3-compatible storage to serve content efficiently
through its native protocols, and makes large uploads easier to
bound and stream. Authorization, signing, and metadata remain inside
the application boundary; the storage service handles the byte
transfer.

The principal alternative considered and rejected was to retain the
API as the file proxy on the grounds that doing so would simplify
authorization (every byte passes through the API and is therefore
subject to its checks). That alternative was rejected because the
simplification it provides is illusory: bytes that pass through the
API still must be authorized at the entry point, so the API gains
nothing by reading them; meanwhile, the bytes consume API CPU and
memory budget that should be spent on business requests, and the API
becomes a scaling bottleneck for file size and concurrency. A second
alternative considered was to require a managed CDN for file serving
on the grounds that a CDN provides edge caching. That alternative was
rejected because the baseline must work in self-hosted environments
without external CDN integration; a CDN may be added as an
environment-specific enhancement but is not a requirement.

## Design Commitments

- Upload uses multipart streaming or presigned multipart URLs.
- Download uses MinIO/S3-compatible direct URL, reverse proxy, or
  CDN-compatible cache.
- The API owns authorization, signing, and metadata.
- Base64-encoded JSON upload is not a production path.
- Server-side size and content-type limits are enforced.

The application introduces presigned-upload endpoints at
`services/api/internal/menu/http/` that issue signed URLs and record
file metadata in the transactional store. Bucket bootstrap and
validation runs as a Kubernetes Job declared at
`chart/tbite-platform/templates/job-bucket-bootstrap.yaml` per
[`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md), so
that bucket existence and policy configuration are explicit
preconditions rather than implicit application startup behavior.

## Acceptance Criteria

- Menu images and compliance documents avoid API CPU/memory hot paths.
- Direct object paths work in self-hosted and BYO object storage modes.
- Authorization rules remain tied to application metadata.
- Large file uploads cannot bypass server-defined size and type policies.
- Object storage metrics and errors are visible in observability dashboards.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| Menu images and compliance documents avoid API CPU/memory hot paths. | Presigned upload endpoints at `services/api/internal/menu/http/` issue signed URLs; bytes flow directly between client and object storage. |
| Direct object paths work in self-hosted and BYO object storage modes. | The application consumes an S3-compatible endpoint configured through values; the same code path serves the MinIO Operator deployment and any BYO endpoint that honors the S3 contract. |
| Authorization rules remain tied to application metadata. | URL signing and metadata recording are performed inside the application boundary at `services/api/internal/menu/http/`; the storage service never receives anonymous bulk traffic. |
| Large file uploads cannot bypass server-defined size and type policies. | Presigned URLs are scoped with size and content-type constraints encoded in the signing request; rejection of out-of-policy uploads is enforced at the storage boundary. |
| Object storage metrics and errors are visible in observability dashboards. | Metrics flow through the OpenTelemetry Collector under [`adr-0005`](adr-0005-victoria-observability-stack.md); MinIO Operator exposes Prometheus-compatible metrics scraped by the Victoria stack. |

## Scope Boundary

A managed CDN is not required. The baseline must work with
self-hosted MinIO and Kubernetes ingress and reverse-proxy
infrastructure. This specification does not prescribe a specific
presigned-URL TTL, a specific multipart chunk size, or specific
bucket policy contents; those are implementation parameters guided by
this decision.

## References

- Source issue: [Agentic-Build/corporate-catering-system#60](https://github.com/Agentic-Build/corporate-catering-system/issues/60)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0004-self-hosted-ha-data-plane.md`](adr-0004-self-hosted-ha-data-plane.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md)
