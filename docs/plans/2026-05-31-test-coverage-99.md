# Test Coverage → 99%

Date: 2026-05-31
Branch: `test/coverage-99`

## Result

| Surface | Before | After |
|---|---|---|
| **Go** `services/api/internal` (raw) | 79.7% | **98.8%** |
| **Go** effective (excl. K8s leader-election infra) | — | **99.1%** ✅ |
| **Frontend** `.ts` (apps + packages) | ~0% | **100%** |
| Tests passing | — | **all green, 0 FAIL/panic** |

`go vet ./services/api/internal/...` clean. ~28k lines of new test code (Go `*_test.go` + frontend `*.test.ts`); **no production logic changed except one bug fix (below).**

## Scope decisions (user-confirmed)

- **`.svelte` excluded from coverage.** v8 (vitest 4 + Svelte 5) cannot instrument `.svelte` — the rolldown parser fails on Svelte syntax, so components are invisible to coverage. Frontend 99% therefore means **99% of instrumentable `.ts`** (server loads, lib, stores, package logic). `.svelte` components still get behavioral tests where useful but are excluded from the % (`include: ["src/**/*.ts"]`, explicit `*.svelte` exclude in every vitest config). This matches the pre-existing merchant setup.
- **Pragmatic 99% with per-file exemptions** for genuinely-untestable code (no brittle, low-value tests purely to touch a line).

## Bug found & fixed

**`identity/authentik/client.go` — `UpsertVendorOperator` nil-map panic.**
An existing Authentik user whose `attributes` is `null`/omitted decodes to a nil map. The existing-user path did `attrs = user.Attributes` then wrote `attrs["tbite_role"] = …` → `panic: assignment to entry in nil map`. The sibling operator-patch path (same file, ~line 152) already guards this; `UpsertVendorOperator` did not. Fixed with the same nil guard + regression test (`TestUpsertVendorOperatorExistingUserNilAttributes`, verified to panic without the fix). Package now 100%.

This was the **only real implementation bug** surfaced. Everything else flagged by the test pass was unreachable defensive code (see exemptions).

## Frontend infrastructure added

- vitest + `@vitest/coverage-v8` (+ `@testing-library/svelte`/`jest-dom`/`jsdom` where Svelte) added to admin, employee, ui, web-auth, web-shared, api-client (merchant/pickup already had it). Single `pnpm install` (no lockfile race).
- `vitest.config.ts` + `vitest-setup.ts` per package; coverage `include: ["src/**/*.ts"]`, excludes tests/`.d.ts`/`$types`/`.svelte`.
- **`$env/dynamic/private` shim:** the SvelteKit virtual module is unresolvable under plain `svelte()` (no `sveltekit()` plugin), which blocked `hooks.server.ts` / `lib/server/env.ts` / `auth/*/+server.ts`. Added a `resolve.alias` → `vitest.env-private-stub.ts` (process.env-backed) in the three app configs, unblocking those files to 100%.

## Coverage exemptions

### Excluded from the effective % (Makefile `coverage-go` filters it)

- **`platform/leader`** (18 stmts, the in-cluster leader-election path). `RunWithLease` only enters this path when `rest.InClusterConfig()` succeeds — i.e. inside a real K8s pod against a live coordination API. Faking it (`leaderelection.RunOrDie` against a stub API server) would be brittle, low-value test code. The local-fallback path *is* tested. Excluded from the effective number, analogous to how `cmd/` wiring is already excluded.

### Per-file unreachable defensive branches (counted in raw 98.8%, not separately excluded)

~58 statements across repos, each a defensive branch that cannot be driven without adding a production interface seam (which the no-brittle-tests policy rejects). Representative categories:

- **`json.Marshal` on JSON-decoded maps** (hydra `dcr.go`/`discovery.go`) — input is a `map[string]any` from `json.Unmarshal`; Marshal cannot fail.
- **`rows.Scan` error legs** in postgres repos (payroll/compliance/vendors/settlement/quota/dlq) — need an injectable querier returning a row that fails to scan.
- **`crypto/rand.Read` error** (`identity/redis/session_store.go`) — needs an injectable `io.Reader`.
- **`sync.Once`-gated init-error branches** (`platform/db/metrics.go`, `dlq/postgres/dlq_metrics.go`) — the Once fires successfully once per process; no reset seam.
- **Validation guards pre-empted by huma** (`order/http/reorder_handler.go` empty-uuid guard, `quota/http` date-time guard) — huma `format:"uuid"`/`"date-time"` rejects bad input before the handler guard runs.
- **Dead switch `default` arms** (`identity/service.go:237`, after a `validRole` gate) and **S3 presigner error legs** (`menu/http/presign.go` — presigner makes no network call; only an empty key errors, never produced).

Full per-package list: see the workflow report (49 packages, 50 exemptions + 22 needs-seam items), all classified as unreachable-without-prod-change.

`dlq` and `platform/audit` report no coverable statements (interface/type-only packages).

## How it was done

Three multi-agent workflows (one agent per package, agents add only `*_test.go`/`*.test.ts`, never touch production): frontend `.ts` → 99%, frontend SvelteKit wiring → 100%, Go internal (non-Docker phase then testcontainers phase) → 99%. Verified by full `make coverage-go` + per-package `coverage-web`.
