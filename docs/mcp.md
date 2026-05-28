# T-Bite MCP (Model Context Protocol)

The T-Bite platform exposes its business operations via MCP so AI agents can
read + write under the same audit trail and the same business rules as the
REST API. Every MCP tool delegates to the same Service layer the HTTP
handlers use — quota checks, state-machine transitions, outbox writes, and
RBAC are not re-implemented.

## Transports

T-Bite mounts two transports on the same `MCPServer`, so the same tool
inventory is available to remote and local clients:

| Transport | Endpoint | Spec | Use it for |
|---|---|---|---|
| **Streamable HTTP** | `POST/GET/DELETE /mcp` | [2025-03-26](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports#streamable-http) | ChatGPT Custom Connectors, Claude.ai remote MCP, Cursor (remote), Open WebUI, the official MCP SDKs |
| **stdio** | `tbite --role=mcp-stdio` | [2024-11-05](https://modelcontextprotocol.io/specification/2024-11-05/basic/transports#stdio) | Claude Code, Claude Desktop, Cursor, and any other local MCP-aware editor |

CORS is enabled on `/mcp` (allowed origin `*`) so browser-based MCP
playgrounds and the OpenAI connector UI can connect directly.

### Authentication

Every transport uses the same identity model — a session token issued by the
OIDC flow that powers the SvelteKit frontends. There is no separate "MCP
credential" to issue.

- **HTTP transports** read `Authorization: Bearer <session_token>`. The
  global `idhttp.AuthMiddleware` populates `idhttp.UserFromContext(ctx)` so
  every tool handler sees the calling user.
- **stdio** is single-client: the user is resolved once at boot from
  `MCP_BEARER_TOKEN` and attached via `StdioServer.SetContextFunc`.

Unauthenticated calls to `POST /mcp` return `401` with a `WWW-Authenticate`
header that points at the RFC 9728 metadata endpoint:

```
WWW-Authenticate: Bearer realm="https://api.tbite.com/mcp",
                  resource_metadata="https://api.tbite.com/.well-known/oauth-protected-resource"
```

`GET /.well-known/oauth-protected-resource` returns the Authentik issuer URL
so MCP clients can run a standard OAuth 2.0 / PKCE flow before retrying.

## Tools

26 tools across 9 categories. Employee-facing read tools work for the
`employee` and `welfare_admin` roles; write tools are role-gated as marked.
The live, authoritative list is whatever `tools/list` returns.

### Discovery — for "what can I eat today?" prompts

| Tool | Role | Description |
|---|---|---|
| `menu.list_for_day` | employee | List available menu items for the caller's plant on a given date |
| `menu.search` | employee | Keyword + tag + price + in-stock filter against the day's menu |
| `menu.get_item` | employee | Fetch one menu item with images and supply info |
| `vendor.list_open` | employee | List approved vendors serving the caller's plant with cutoff hours |

### Order — for placing / managing orders

| Tool | Role | Description |
|---|---|---|
| `order.list_mine` | employee | List the caller's orders in the last 30 days |
| `order.get` | employee | Fetch one order by id (owner check enforced) |
| `order.place` | employee | Place a new order (same quota / cutoff rules as HTTP) |
| `order.modify` | employee | Replace a placed order's items before cutoff |
| `order.cancel` | employee | Cancel an own order (only when state allows) |

### ChatGPT compatibility

ChatGPT Custom Connectors and the OpenAI Apps SDK require every MCP server
expose two specifically named tools with a strict result shape; we map both
into the same business operations the dedicated tools use:

| Tool | Shape | Description |
|---|---|---|
| `search` | `{ "results": [ {id, title, text, url}, … ] }` | Unified search across menu + own orders |
| `fetch` | `{ id, title, text, url, metadata? }` | Fetch one document by prefixed ID |

IDs returned by `search` are prefixed so `fetch` can route to the right
service: `menu:<uuid>`, `order:<uuid>`, `vendor:<uuid>`.

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

## Client setup

### Claude Desktop / Claude Code (stdio)

```json
{
  "mcpServers": {
    "tbite": {
      "command": "tbite",
      "args": ["--role=mcp-stdio"],
      "env": {
        "MCP_BEARER_TOKEN": "tb_…",
        "DATABASE_RW_URL": "postgres://…",
        "REDIS_URL": "redis://…",
        "OIDC_CALLBACK_BASE_URL": "http://api.tbite.local",
        "AUTH_PROVIDER_SLUGS": "authentik",
        "AUTH_PROVIDER_AUTHENTIK_ISSUER_URL": "http://auth.tbite.local/application/o/tbite/",
        "AUTH_PROVIDER_AUTHENTIK_CLIENT_ID": "tbite-local",
        "AUTH_PROVIDER_AUTHENTIK_CLIENT_SECRET": "change-me",
        "AUTHENTIK_BASE_URL": "http://auth.tbite.local",
        "AUTHENTIK_API_TOKEN": "change-me",
        "APP_BASE_URL_EMPLOYEE": "http://app.tbite.local",
        "APP_BASE_URL_MERCHANT": "http://merchant.tbite.local",
        "APP_BASE_URL_ADMIN": "http://admin.tbite.local",
        "S3_ENDPOINT": "http://minio.tbite.local",
        "S3_ACCESS_KEY_ID": "change-me",
        "S3_SECRET_ACCESS_KEY": "change-me",
        "S3_BUCKET": "tbite-dev"
      }
    }
  }
}
```

Drop in `~/.config/claude-code/config.json` or
`~/Library/Application Support/Claude/claude_desktop_config.json`.

### Claude.ai (remote MCP)

Claude.ai's "Custom integrations" UI accepts the Streamable HTTP endpoint
directly. Use:

- **Server URL**: `https://api.tbite.com/mcp`
- **Auth**: Claude.ai runs an OAuth 2.0 flow against the Authentik issuer
  advertised in `/.well-known/oauth-protected-resource`. If you instead want
  manual Bearer-token mode, paste a session token issued by the regular
  OIDC login flow.

### ChatGPT (Custom Connectors / Apps SDK)

ChatGPT Pro/Team/Enterprise → Settings → Connectors → "Create" → MCP.

- **MCP server URL**: `https://api.tbite.com/mcp`
- **Authentication**: OAuth (auto-discovered via the protected-resource
  metadata endpoint). The advertised tools include `search` and `fetch`
  which ChatGPT uses for citation cards.

### Cursor (remote)

Cursor's MCP config supports HTTP transports natively:

```json
{
  "mcpServers": {
    "tbite": {
      "type": "http",
      "url": "https://api.tbite.com/mcp",
      "headers": { "Authorization": "Bearer tb_…" }
    }
  }
}
```

### Open WebUI / Continue / generic clients

Any client that speaks Streamable HTTP works the same way: point at
`https://api.tbite.com/mcp` and send `Authorization: Bearer <session_token>`.

Generic JSON-RPC test from the command line:

```bash
curl -sN https://api.tbite.com/mcp \
  -H "Authorization: Bearer tb_…" \
  -H "Content-Type: application/json" \
  -H "Accept: application/json, text/event-stream" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

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

## Authorization architecture

```
MCP client (Claude.ai / ChatGPT)
        │  1. POST /mcp without token → 401 + WWW-Authenticate
        ▼
  /.well-known/oauth-protected-resource          (RFC 9728, on T-Bite API)
        │  authorization_servers: [<API base URL>]
        ▼
  /.well-known/openid-configuration              (RFC 8414, served by T-Bite — wraps Hydra
        │  issuer, authorization_endpoint, token_endpoint,    & injects registration_endpoint
        │  registration_endpoint, jwks_uri                    that Hydra v2.2/2.3 omit)
        │
        ├──→ POST /oauth2/register                            (RFC 7591 DCR  →  Ory Hydra sidecar)
        │
        ├──→ GET  /oauth2/auth?…                              (reverse-proxied to Hydra)
        │      ↳ 302 → /oauth/login (T-Bite consent bridge)
        │      ↳ employee pastes session token / logs in via Authentik
        │      ↳ Hydra accepts login → /oauth/consent (auto-approve)
        │      ↳ 302 back to client redirect_uri with `code=…`
        │
        ├──→ POST /oauth2/token (PKCE)                        (reverse-proxied to Hydra)
        │      ↳ returns JWT access token signed by Hydra,
        │        with iss=<API base URL>, sub=<T-Bite user.id>
        │
        ▼
  POST /mcp with `Authorization: Bearer <JWT>`
        ↳ idhttp.AuthMiddleware tries session-token lookup first (web frontends),
          falls through to Hydra JWT verifier (DCR clients).
        ↳ User loaded from sub claim → ctx populated → tool handlers run.
```

### Why Ory Hydra (and not Authentik) for OAuth

Authentik 2026.2 (the OIDC provider that powers the SvelteKit frontends) does
not yet support RFC 7591 Dynamic Client Registration — it is on the roadmap
for 2026.8. Without DCR, Claude.ai and ChatGPT cannot self-register and the
remote MCP flow stalls. Ory Hydra is mounted as a sidecar specifically to
provide the OAuth surface MCP clients require, with Authentik continuing to
authenticate the user via the T-Bite session token the SvelteKit apps mint.

Two known Hydra rough edges that the T-Bite layer smooths over:

1. **Missing `registration_endpoint` in the discovery doc.** Hydra v2.2/v2.3
   exposes `/oauth2/register` for DCR but does not advertise it in
   `/.well-known/openid-configuration`. The T-Bite discovery shim
   (`services/api/internal/identity/hydra/discovery.go`) injects the field.
2. **One-origin guarantee.** Hydra is configured with
   `URLS_SELF_ISSUER=<API base URL>` and the API reverse-proxies every
   `/oauth2/*` and `/.well-known/jwks.json` path back to Hydra. MCP clients
   see a single origin, the `iss` claim in JWTs matches the discovery
   document, and CORS doesn't fragment across two hosts.

## What's deferred (P9+)

- MCP resources (URI-based reads like `vendor://{id}`)
- MCP prompts (server-supplied prompt templates)
- Scoped sub-tokens for high-risk tools — Hydra issues scoped tokens but we
  don't yet narrow tool exposure based on scope claim
- Retiring the Hydra sidecar once Authentik 2026.8 ships native DCR; the
  T-Bite glue (`internal/identity/hydra`) can then be replaced with a direct
  Authentik wiring
