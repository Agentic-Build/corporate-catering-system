# `apps/`

Three SvelteKit 2 + Svelte 5 frontends. Each app is an adapter-node SSR
build deployed as a separate Kubernetes Deployment behind its own host.

| App | Audience | Local host |
| --- | --- | --- |
| `employee/` | 員工：預購、領餐、扣款明細 | `http://app.tbite.local` |
| `merchant/` | 商家：菜單、份數、看板、月結 | `http://merchant.tbite.local` |
| `admin/` | 福委會：商家審核、治理、結算 | `http://admin.tbite.local` |

All three share the workspace packages under [`../packages/`](../packages/):

- `@tbite/ui` — Svelte 5 component library
- `@tbite/tokens` — design tokens (CSS vars + Tailwind preset)
- `@tbite/web-auth` — SvelteKit auth handle (`tbite_sid` cookie → API
  `GET /me` → `event.locals.user`)
- `@tbite/api-client` — typed openapi-fetch client generated from the
  Go API's OpenAPI 3.1 spec
- `@tbite/pickup` — QR payload helpers (`tbite://pickup?order=<id>`)

## Working in one app

```bash
cd apps/employee
pnpm install      # one-time at repo root is enough; pnpm workspace handles it
pnpm dev          # vite dev against $API_BASE_URL
pnpm test         # vitest
pnpm build        # adapter-node SSR build into build/
```

`API_BASE_URL` points at the Go API (`http://api.tbite.local` for the
Helm dev chart, `http://localhost:8080` for a hand-run binary).

## Conventions

- Server-side data fetching uses SvelteKit `load` functions and form
  actions; the API is called through `@tbite/api-client` (typed) — never
  raw `fetch` on a string URL.
- After touching anything that affects the API contract, run
  `make contract-sync` at the repo root so this app's
  `@tbite/api-client` types stay in sync (CI fails on drift).
- Component primitives come from `@tbite/ui` first; reach for ad-hoc
  markup only when the shared component genuinely doesn't fit.
- Auth is wired via `createAuthHandle()` in `src/hooks.server.ts`; see
  [`packages/web-auth/README.md`](../packages/web-auth/README.md).
