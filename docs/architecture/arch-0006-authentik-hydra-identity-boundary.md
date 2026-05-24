# Architecture Specification 0006 — Authentik and Hydra Boundary for SSO and MCP DCR

- **Status**: Accepted — 2026-05-25
- **Source issue**: [Agentic-Build/corporate-catering-system#61](https://github.com/Agentic-Build/corporate-catering-system/issues/61)
- **Parent baseline**: [`00-baseline.md`](00-baseline.md)

## Context

The corporate catering platform must support enterprise Single
Sign-On (SSO) for human users and must remain compatible with the
Model Context Protocol (MCP) ecosystem for machine and assistant
clients that consume the system's tools. These two responsibilities
have substantially different operational profiles. Enterprise SSO
benefits from a fully featured identity provider with administrator
user interfaces, group and role management, multi-source user
federation, branding, and policy enforcement; the platform's chosen
provider for this role is Authentik. MCP-compatible OAuth flows, by
contrast, require strict standards compliance and in particular
support for OAuth 2.0 Dynamic Client Registration (DCR) per RFC 7591;
the platform's chosen provider for this role is Hydra.

The architectural pressure is to make these responsibilities explicit
so that business logic does not become coupled to provider-specific
APIs. If application code routinely calls Authentik-specific or
Hydra-specific APIs to perform what should be ordinary identity
operations, then replacing or augmenting either provider becomes a
substantial code change rather than a configuration change. The
architecture should isolate provider-specific code to provisioning
and integration surfaces.

## Decision

Authentik remains the reference self-host SSO provider, and Hydra
remains required for MCP Dynamic Client Registration support.
Application code consumes generic OIDC and OAuth contracts wherever
possible.

## Rationale

Separating provider roles gives the architecture a clearer identity
boundary. Authentik can handle enterprise login, federation, and
user-and-group integration while presenting its administrator
interface to platform operators. Hydra can support standards-oriented
OAuth flows that require DCR, particularly the MCP-facing flows.
Application code depends on generic OIDC and OAuth claims and token
validation contracts; provider-specific API usage is limited to
provisioning surfaces (creating clients, syncing user groups) and to
the integration glue that wires each provider into the chart.

The principal alternative considered and rejected was to consolidate
on a single provider for both responsibilities. That alternative was
rejected because no single provider in the self-hosted ecosystem
currently combines Authentik's enterprise SSO capability with
Hydra-grade DCR support; consolidating on one would force a
compromise on either the SSO administrator experience or on MCP
compatibility. A second alternative considered was to remove MCP DCR
support from the product on the grounds that it could be deferred.
That alternative was rejected because MCP compatibility is a current
product requirement that motivates Hydra's continued presence in the
stack. A third alternative considered was to write a custom DCR
endpoint backed by the same identity provider used for SSO. That
alternative was rejected because building an in-house DCR
implementation would duplicate substantial OAuth 2.0 server
infrastructure that Hydra already provides correctly.

## Design Implications

Authentik owns the reference enterprise SSO and user-federation flows.
Hydra owns OAuth 2.0 and OIDC Dynamic Client Registration for
MCP-facing flows. Application code depends on generic OIDC and OAuth
contracts where possible; specifically, JWT verification, claim
extraction, and token introspection target standard interfaces
configurable through the issuer URL. Provider-specific API usage is
limited to provisioning surfaces and integration glue. The Traefik
gateway governed by [`adr-0003`](adr-0003-traefik-gateway-api-ingress.md)
includes routes for Authentik and Hydra callback and issuer paths,
declared in `chart/tbite-platform/templates/httproute-*.yaml`.

Both providers must satisfy the readiness contract established by
[`arch-0007`](arch-0007-cloud-native-readiness-and-autoscaling.md):
auth-related readiness checks must reflect Authentik and Hydra
availability where the application's authentication flows depend on
them. Both providers must emit telemetry compatible with the
observability stack governed by
[`adr-0005`](adr-0005-victoria-observability-stack.md).

## Acceptance Criteria

- MCP clients requiring DCR can complete registration and OAuth flows.
- Normal app auth remains compatible with generic OIDC provider contracts.
- Dev and production use the same auth topology, scaled down where appropriate.
- Auth-related readiness and observability cover both providers.
- Provider-specific code is isolated from core business workflows.

## Compliance Evidence

| Acceptance criterion | Compliance |
| --- | --- |
| MCP clients requiring DCR can complete registration and OAuth flows. | Hydra is deployed by the chart governed by [`adr-0002`](adr-0002-helm-umbrella-chart.md) and is exposed through HTTPRoutes in `chart/tbite-platform/templates/httproute-*.yaml`; Hydra natively supports DCR. |
| Normal app auth remains compatible with generic OIDC provider contracts. | The application consumes OIDC issuer metadata generically; provider-specific API usage is confined to provisioning code. **Follow-up** at the code boundary as the auth implementation matures. |
| Dev and production use the same auth topology, scaled down where appropriate. | `chart/tbite-platform/values-dev.yaml` and `chart/tbite-platform/values-prod.yaml` deploy the same Authentik and Hydra components with environment-appropriate replica counts and resource requests. |
| Auth-related readiness and observability cover both providers. | Per-role readiness checks at `services/api/internal/httpserver/health.go` include identity-provider dependency checks where applicable; metrics flow through the OpenTelemetry Collector under [`adr-0005`](adr-0005-victoria-observability-stack.md). |
| Provider-specific code is isolated from core business workflows. | Provider-specific API usage is restricted to provisioning surfaces; core order, menu, payroll, and compliance logic depend on generic OIDC and OAuth contracts. **Follow-up** at the code boundary as the auth implementation matures. |

## Scope Boundary

Hydra is not removed while MCP DCR support is a requirement. Core
order, menu, payroll, and compliance logic should not depend directly
on Authentik or Hydra implementation APIs. This specification does
not select a specific user federation backend, a specific token
lifetime, or a specific signing algorithm; those are configuration
concerns guided by this decision.

## References

- Source issue: [Agentic-Build/corporate-catering-system#61](https://github.com/Agentic-Build/corporate-catering-system/issues/61)
- Parent baseline: [`00-baseline.md`](00-baseline.md)
- Related: [`adr-0002-helm-umbrella-chart.md`](adr-0002-helm-umbrella-chart.md), [`adr-0003-traefik-gateway-api-ingress.md`](adr-0003-traefik-gateway-api-ingress.md), [`adr-0005-victoria-observability-stack.md`](adr-0005-victoria-observability-stack.md), [`arch-0007-cloud-native-readiness-and-autoscaling.md`](arch-0007-cloud-native-readiness-and-autoscaling.md)
