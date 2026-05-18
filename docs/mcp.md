# T-Bite MCP (Model Context Protocol)

The T-Bite platform exposes its business operations via MCP so AI agents can
read + write under the same audit trail and the same business rules as the
REST API. Every MCP tool delegates to the same Service layer the HTTP
handlers use — quota checks, state-machine transitions, outbox writes, and
RBAC are not re-implemented.

## Two transports

### 1. HTTP (SSE) — for remote agents

Endpoint: `https://api.tbite.com/mcp/sse` (production) or
`http://localhost:8080/mcp/sse` (local).

Auth: `Authorization: Bearer <session_token>` — the exact same token the
SvelteKit frontends use. Tokens are issued by the OIDC login flow; there is
no separate MCP credential.

The `/mcp` mount is wrapped by `idhttp.AuthMiddleware`, so an unauthenticated
request gets `401` before reaching any tool handler.

### 2. stdio — for local Claude Code / Cursor / etc.

Run the binary with `--role=mcp-stdio`. Stdio transport is single-client:
the user is resolved once at boot from `MCP_BEARER_TOKEN` and attached to
every request's context via `StdioServer.SetContextFunc`.

```bash
MCP_BEARER_TOKEN=tb_xxxx \
DATABASE_RW_URL=postgres://... \
REDIS_URL=redis://... \
OIDC_CALLBACK_BASE_URL=http://localhost:8080 \
AUTH_PROVIDER_SLUGS=authentik \
AUTH_PROVIDER_AUTHENTIK_ISSUER_URL=http://localhost:9002/application/o/tbite/ \
AUTH_PROVIDER_AUTHENTIK_CLIENT_ID=tbite-local \
AUTH_PROVIDER_AUTHENTIK_CLIENT_SECRET=tbite-local-client-secret \
AUTHENTIK_BASE_URL=http://localhost:9002 \
AUTHENTIK_API_TOKEN=tbite-dev-authentik-api-token \
APP_BASE_URL_EMPLOYEE=... APP_BASE_URL_MERCHANT=... APP_BASE_URL_ADMIN=... \
S3_ACCESS_KEY_ID=x S3_SECRET_ACCESS_KEY=x S3_BUCKET=tbite \
tbite --role=mcp-stdio
```

Configure your MCP-aware client (Claude Code, Cursor) to launch this command
as a stdio child process.

## Tools

21 tools across 7 categories. Read-only: 8. Write: 13 (5 employee, 8 admin).

### Order (6 tools)

| Tool | Role | Description |
|---|---|---|
| `order.list_mine` | employee | List the caller's orders in the last 30 days |
| `order.get` | employee | Fetch one order by id (owner check enforced) |
| `order.get_pickup_code` | employee | Generate the current TOTP for a `READY` order |
| `order.place` | employee | Place a new order (same quota / cutoff rules as HTTP) |
| `order.modify` | employee | Replace a placed order's items before cutoff |
| `order.cancel` | employee | Cancel an own order (only when state allows) |

### Vendor (3 tools, admin)

| Tool | Role | Description |
|---|---|---|
| `vendor.list` | welfare_admin | List vendors with optional status filter |
| `vendor.suspend` | welfare_admin | Suspend an approved vendor (high-risk) |
| `vendor.reinstate` | welfare_admin | Reinstate a suspended vendor |

### Payroll (3 tools, admin)

| Tool | Role | Description |
|---|---|---|
| `payroll.list_batches` | welfare_admin | List batches with optional status filter |
| `payroll.lock_batch` | welfare_admin | Transition `draft → locked` (triggers settler) |
| `payroll.resolve_dispute` | welfare_admin | Resolve a dispute (refund or reject) |

### Audit (1 tool, admin)

| Tool | Role | Description |
|---|---|---|
| `audit.query` | welfare_admin | Filter `audit_event` by target_kind / target_id / since / limit |

### Feedback (2 tools, employee)

| Tool | Role | Description |
|---|---|---|
| `feedback.rate_order` | employee | Submit a 1-5 meal rating for a picked-up order |
| `feedback.file_complaint` | employee | File a complaint for a picked-up order |

### Settlement (1 tool, admin)

| Tool | Role | Description |
|---|---|---|
| `settlement.close_period` | welfare_admin | Close a period: cut one vendor settlement per vendor with orders |

### Compliance (5 tools, admin)

| Tool | Role | Description |
|---|---|---|
| `document.list` | welfare_admin | List a vendor's compliance documents |
| `document.review` | welfare_admin | Approve or reject a pending vendor document |
| `anomaly.list` | welfare_admin | List anomaly alerts filtered by status / severity |
| `anomaly.triage` | welfare_admin | Triage an anomaly, optionally warning or suspending the vendor |
| `anomaly.close` | welfare_admin | Close an open or triaged anomaly |

## Audit trail

Every tool invocation writes an `audit_event` row with:

- `action = "mcp.<toolName>"` (e.g. `mcp.order.place`)
- `request_id = "mcp:<toolName>"`
- `actor_id` = authenticated user
- `actor_role` = user's role at call time

Admins can audit MCP-originated actions by filtering:

```sql
SELECT * FROM audit_event WHERE request_id LIKE 'mcp:%' ORDER BY created_at DESC;
```

## What's deferred (P8+)

- MCP resources (URI-based reads like `vendor://{id}`)
- MCP prompts (server-supplied prompt templates)
- Scoped sub-tokens for high-risk tools (currently any session token can call
  any tool the user's role allows)
