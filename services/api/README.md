# `services/api/`

Go modular monolith. One binary, eleven roles selected via
`--role=<name>` and dispatched into per-role Kubernetes Deployments (per
[`arch-0001-worker-role-split`](../../docs/architecture/arch-0001-worker-role-split.md)).

## Layout

```
cmd/
  tbite/            # main entry point — implements every --role
  contract-export/  # writes contract/openapi/openapi.yaml from huma routes
  lunch-flow/       # tiny end-to-end smoke against a running API
  stress/           # load generator used by ops/load/

internal/
  config/           # env-driven Config (one struct, validated at boot)
  httpserver/       # huma router, middleware, /healthz, /docs
  platform/         # cross-cutting infra: db, messaging, cache, leader,
                    # storage, observability, clock
  identity/         # SSO (Authentik), Hydra MCP DCR, sessions, RBAC
  menu/             # vendor menus + supply windows
  vendors/          # merchant onboarding + service-area mapping
  quota/            # per-plant / per-day quota gates
  order/            # order lifecycle, board SSE, outbox relay
  payroll/          # batch lock + settler worker + HR CSV export
  feedback/         # ratings + dispute lifecycle
  compliance/       # document expiry + anomaly evaluator/scanner
  settlement/       # vendor month-end statements
  dlq/              # dead-letter queue + replay admin API
  plants/           # plant registry
  mcpserver/        # MCP tool surface (same Services as HTTP)
```

Each domain follows the same shape: `service.go` (business rules) +
`postgres/` (repository) + `http/` (huma handlers). MCP tools call the
same Service layer — RBAC and state machines are never re-implemented.

## Roles dispatched by `--role`

`api`, `realtime-gateway`, `outbox-relay`, `payroll-settler`,
`on-time-evaluator`, `cutoff-sweeper`, `no-show-sweeper`,
`document-expiry-scanner`, `feedback-scanner`, `mcp-stdio`,
`provision-streams`. Scaling signals, idempotency, and DLQ behaviour per
role: [`docs/architecture/worker-roles.md`](../../docs/architecture/worker-roles.md).

## Working in this tree

```bash
go build ./services/api/cmd/tbite           # build the binary
go test ./services/api/internal/...         # unit + integration (testcontainers)
make coverage-go                            # serialized run, writes coverage.out
make contract-sync                          # regenerate OpenAPI + TS client
                                            # (CI fails on drift)
```

Conventions:

- SQL uses raw `pgx` with `$N` parameterized binding. CI rejects
  `fmt.Sprintf("SELECT ...")` (see `scripts/security/check-sql-strings.sh`).
- HTTP handlers go through huma so the OpenAPI doc stays in sync.
- Outbox is the only path for business events to leave a transaction
  (see [`arch-0002`](../../docs/architecture/arch-0002-durable-event-plane-and-outbox.md)).
- Money fields named `*_minor` store **whole NTD**, not cents. Never
  divide by 100.
