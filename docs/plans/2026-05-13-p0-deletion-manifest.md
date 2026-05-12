# P0 Deletion Manifest

Scratch file for **P0 Task 0**. Lists every path Task 1 (and later, Task 10) will remove from `feat/p0-skeleton`. This file itself is removed by **Task 16** at the end of P0.

- Base commit inspected: `0c74c170` (HEAD on `feat/p0-skeleton` at time of Task 0).
- Working tree confirmation method: `git ls-files <path>` for tracked entries; `ls` for untracked entries.

---

## 1. Tracked files / dirs to delete in Task 1

All paths below exist in HEAD (verified via `git ls-files`). Counts in parentheses are tracked-file counts under that path.

Root files:

- [ ] `Cargo.toml`
- [ ] `Cargo.lock`
- [ ] `Dockerfile.system`
- [ ] `Dockerfile.web`
- [ ] `docker-compose.yml`
- [ ] `package.json`
- [ ] `pnpm-lock.yaml`
- [ ] `tsconfig.contract.json`
- [ ] `contract-client-compile-check.ts`
- [ ] `Makefile`

Tracked directories (delete recursively):

- [ ] `src/**` (27 tracked files — Rust backend)
- [ ] `tests/**` (18 tracked files — Rust integration tests)
- [ ] `.sqlx/**` (7 tracked files — `cargo sqlx prepare` cache)
- [ ] `apps/web/**` (166 tracked files — legacy single SvelteKit SPA)
- [ ] `migrations/**` (10 tracked files — sqlx migrations; will be re-authored for Go)
- [ ] `contract/**` (255 tracked files — generated OpenAPI client + TS bindings; will be regenerated from Go in P1)
- [ ] `scripts/**` (10 tracked files — Rust-era automation; will be re-authored)

Notes:

- After `git rm -r apps/web`, the directory `apps/` becomes empty of tracked files (its only tracked subtree is `apps/web`). Task 1 may leave the empty `apps/` directory in place since Tasks 2/3 will populate it with `apps/employee`, `apps/merchant`, `apps/admin`.
- No paths from the P0 plan's "to delete (tracked)" list were missing in HEAD.

## 2. Untracked files / dirs to clean in Task 1

These are not in git; they are local build artefacts that should be removed via `rm -rf` (not `git rm`):

- [ ] `target/` — Rust build output
- [ ] `node_modules/` — root pnpm install (legacy)

Note: `apps/web/node_modules/` and `apps/web/.svelte-kit/` are not currently present on disk, but if they appear they should also be removed alongside `apps/web/**`.

## 3. Deferred deletion (handled by later tasks — do NOT touch in Task 1)

The following will be deleted later in P0; they remain untouched by Task 1 so we can reference them while building the replacement:

- [ ] `ops/kubernetes/base/**` (20 tracked files) — deleted in **Task 10** when the new dual-overlay K8s layout (`ops/k8s/base`, `ops/k8s/overlays/{single,multi}`) is written.
- [ ] `ops/kubernetes/components/**` (10 tracked files) — deleted in **Task 10**.
- [ ] `ops/kubernetes/overlays/**` (14 tracked files) — deleted in **Task 10**.
- [ ] `docs/plans/2026-05-13-p0-deletion-manifest.md` (this file) — deleted in **Task 16** at P0 end via `git rm`.

## 4. Explicitly kept (do NOT delete at any point in P0)

These paths are preserved through P0. Some are rewritten in place by later tasks (noted inline), but none are bulk-deleted by Task 1.

Documentation & requirements:

- `INITIAL.md` — requirements source of truth.
- `README.md` — rewritten in Task 16, not deleted.
- `docs/**` — design + plan documents (57 tracked files), including:
  - `docs/plans/2026-05-13-tbite-refactor-design.md`
  - `docs/plans/2026-05-13-p0-skeleton.md`
  - `docs/user-journeys.md` + `docs/user-journeys/screenshots/**`

Observability stack (reused as-is):

- `ops/observability/**` (11 tracked files) — OTel collector, load tests, SLO config.

CI:

- `.github/**` (6 workflow files) — workflows are rewritten by **Task 15**, not bulk-deleted in Task 1. Leave them in place until Task 15 rewrites them.

Root config kept:

- `.gitignore`
- `.gitattributes`
- `.dockerignore`
- `.env.development`
- `.env.local` *(not tracked in git; lives only on disk — keep on disk)*

---

## 5. Paths not explicitly addressed by the plan (flagged for Task 1 reviewer)

These are tracked paths the P0 plan did not list under either "delete" or "keep". Default behaviour: **leave untouched in Task 1** unless the reviewer decides otherwise.

- `.agents/build/**` — agent-run scratch artefacts (build run JSON / NDJSON). Tracked but not referenced by either Rust or Go code. Likely safe to keep; not deleted by Task 1.
- `ops/local/docker-compose.dev.yml`, `ops/local/minio-provision.sh`, `ops/local/otel-collector.dev.yaml` — local dev compose stack. Not on the delete list. The top-level `docker-compose.yml` is being deleted; whether the new repo replaces or reuses `ops/local/*` is a question for Task 8 (local stack). **Keep in Task 1.**
- `ops/state/audit-trail.json` — stale audit log dump. Not on either list. **Keep in Task 1**; reviewer can decide later.

## 6. Self-check summary

- All 10 root files in §1 verified present (`TRACKED` from `git ls-files --error-unmatch`).
- All 7 tracked directories in §1 verified non-empty (file counts shown).
- No "to delete (tracked)" entry from the P0 plan was found missing.
- `target/` and `node_modules/` confirmed present on disk and untracked.
- `ops/kubernetes/{base,components,overlays}` correctly deferred to Task 10.
- Keep list explicitly enumerates every path the P0 plan listed as "to KEEP".
