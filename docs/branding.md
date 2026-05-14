# T-Bite Branding Policy

The product is **T-Bite**. The brand mark is `T-Bite` — capital T, hyphen,
capital B, lowercase i-t-e, full stop optional. The repo,
Go module, and most technical identifiers use the lowercase form `tbite`
because external systems (npm, Kubernetes, Docker, S3) require it.

This policy exists so future contributors don't introduce typos
(`Tbite`, `t-bite`, `TBite`, etc.) and so the split between brand form and
technical form is intentional rather than accidental.

## The two canonical forms

| Form | Where it appears | Examples |
|------|------------------|----------|
| **`T-Bite`** | Anywhere a human reads it as a brand: README headings, page titles, doc prose, marketing copy, OpenAPI `title`, log banners, error messages | `# T-Bite`, `<title>T-Bite · Employee</title>`, `huma.DefaultConfig("T-Bite API", "0.1.0")` |
| **`tbite`** | Technical identifiers governed by external naming rules | npm namespace `@tbite/*`, K8s namespace `tbite`, Postgres database `tbite`, S3 bucket `tbite`, test fixtures `e2e-employee@tbite.test`, Tailwind CSS prefix `tb-*` (abbreviated further), Docker image tags |

## Why we don't unify to `T-Bite` everywhere

| Constraint | Source | Effect |
|------------|--------|--------|
| npm package name regex | [npm scope rules](https://docs.npmjs.com/cli/v10/configuring-npm/package-json#name) | Must be all-lowercase; `@T-Bite/employee` would be rejected at `npm publish` time |
| Kubernetes namespace | RFC 1123 DNS label (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`) | `kubectl create namespace T-Bite` errors out |
| Docker image tag | Docker reference grammar | Tags must be lowercase |
| S3 bucket name | AWS S3 naming rules | Buckets must be lowercase + DNS-compliant |
| Postgres role / database | SQL identifier rules | `T-Bite` would require quoting in every statement (`"T-Bite"`) — invasive and error-prone |

So `tbite` is forced for technical identifiers and `T-Bite` is preserved for
display. Two forms, each internally consistent.

## Forbidden variants

Never introduce any of these in new code or docs:

- `Tbite`, `TBite`, `T-bite`, `t-Bite`, `t-bite`, `TBITE`, `T-BITE`

If you see one in existing code, treat it as a typo and fix it as part of
the same PR that touched the surrounding lines.

## Authoring checklist for new contributors

1. **Adding a new human-facing string?** Use `T-Bite`. Examples: a new
   admin page header, a new email template, a new docs page.
2. **Adding a new technical identifier** (namespace, package name, env var
   value, DB role, S3 bucket, etc.)? Use `tbite` or a `tb-` shortened
   prefix. Do not invent a third form.
3. **Adding both at once** (e.g., new npm package `@tbite/foo` with a
   user-facing `<title>T-Bite Foo</title>`)? Pair them: technical id is
   `tbite`, display string is `T-Bite`.
4. **Reviewing a PR?** A simple grep can catch most typos:

   ```bash
   grep -rIE '\b(Tbite|TBite|T-bite|t-Bite|TBITE|T-BITE)\b' \
     --include='*.md' --include='*.go' --include='*.ts' \
     --include='*.svelte' --include='*.yaml' --include='*.yml' \
     --include='*.json' --include='*.sh' .
   ```

   Any hit is either a typo (fix it) or a deliberate exception that
   deserves a comment explaining why.

## Logo asset

The brand mark lives in `packages/ui/src/TBiteLogo.svelte`. It is exported
as `TBiteLogo` from `@tbite/ui` and used by:

- `apps/employee/src/routes/+layout.svelte` (app header)
- `apps/employee/src/routes/login/+page.svelte` (login hero)
- `apps/admin/src/routes/+layout.svelte` (app header)
- `apps/admin/src/routes/login/+page.svelte` (login hero)
- `apps/merchant/src/routes/onboard/+page.svelte` (onboarding hero)

The current implementation is an inline SVG placeholder (rounded gradient
tile + bite notch + amber accent dot + bold "T"). Swap the SVG body when
the final brand asset is ready; keep the `Props` interface
(`size?: number; eyebrow?: boolean`) stable so the five callers above stay
unchanged.
