# Frontend Design-System Refactor — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan. Phases 1/2/3 are dispatched as parallel subagents.

**Goal:** Refactor the three SvelteKit frontends (employee / merchant / admin) so they match the T-Bite design system — precise recreation of each app's main screen, design-language pass over remaining routes, and four new interaction components wired to the real backend.

**Architecture:** Expand `@tbite/ui` with Svelte ports of the reference primitives (`reference_src/ui.jsx`, `ui_kits/tbite/`), then rebuild each app's layout + main screen + secondary routes on top. Data flow is unchanged: `Go API → +page.server.ts → Svelte page → form actions`. Backend gets one small read-only addition.

**Tech Stack:** SvelteKit (Svelte 5 runes), Tailwind (`@tbite/tokens` preset), Go (Huma/Chi), `openapi-fetch` typed client.

**Companion design doc:** `docs/plans/2026-05-17-frontend-design-system-refactor-design.md` — read it first; it has the full per-app spec.

**Worktree:** `/Users/takala/code/corporate-catering-system/.worktrees/frontend-design-refactor` (branch `frontend-design-refactor`). All commands run from there.

**Reference files (read-only, outside repo):** `~/Downloads/T-Bite Design System/` — `reference_src/*.jsx`, `ui_kits/tbite/*.jsx`, `colors_and_type.css`, `assets/`.

---

## Dependency graph & parallelism

```
Phase 0 (shared @tbite/ui)  ─┐
Phase 4 (backend stats)     ─┤── both first, run in parallel
                             ▼
Phase 1 (employee) ┐
Phase 2 (merchant) ┼── three parallel subagents (each touches only apps/<name>/)
Phase 3 (admin)    ┘
                             ▼
Phase 5 (integration verification)
```

- **Phase 0 blocks 1/2/3** — they import from `@tbite/ui`.
- **Phase 4 blocks Phase 2's meal-library task** — it needs `last_used`/`total_sold`. Phase 4 also touches `packages/api-client`; Phase 2 must start after Phase 4's api-client regen lands.
- **Phases 1/2/3 never touch the same files** — employee/merchant/admin subagents edit only their own `apps/<name>/` tree. Safe to parallelize.

## Conventions for every task

- **Verify after each task:** run the task's verification command; it must pass before committing.
- **Per-app type check:** `pnpm --filter @tbite/<app> exec svelte-kit sync && pnpm --filter @tbite/<app> check` — **0 errors** required (pre-existing `state_referenced_locally` warnings are acceptable; do not introduce new error-level problems).
- **Commit after each task** with a conventional-commit message (`feat:`, `refactor:`, `docs:`). Frequent commits.
- **No placeholder data** — every screen binds to real `+page.server.ts` data. If a reference screen shows a field the API lacks, omit that field (do not fabricate).
- **Match the reference exactly** — port classes verbatim from the `.jsx`, translating `className`→`class`, React handlers→Svelte, `useState`→`$state`, `.map()`→`{#each}`. Keep all Traditional-Chinese copy. Honour the design README: red-600 primary, Noto Sans TC, `·` inline meta, 24-hour time, tabular-nums, informal 你.
- **Icons:** use the `@tbite/ui` `Icon` component (Phase 0). Never emoji-as-icon, never new SVG illustration.

---

## Phase 0 — Shared foundation (`@tbite/ui`, `@tbite/tokens`, assets)

**Owner:** runs first, single agent. Blocks Phases 1/2/3.

### Task 0.1 — Animations & utility CSS into tokens

**Files:** Modify `packages/tokens/src/tokens.css`

Append the keyframes/utilities the reference relies on (source: `colors_and_type.css` in the design system):
- `@keyframes fadeUp` (220ms ease-out) + `.fade-up` class.
- `@keyframes cartBump` (320ms) + `.cart-bump` class.
- `.no-scrollbar` (hide scrollbars on horizontal scrollers).

**Verify:** `pnpm --filter @tbite/tokens check` → Done. **Commit:** `feat(tokens): add fadeUp/cartBump animations and no-scrollbar util`

### Task 0.2 — Icon component

**Files:** Create `packages/ui/src/Icon.svelte`; Modify `packages/ui/src/index.ts`

Port every icon from `reference_src/ui.jsx` `I.*` and `ui_kits/tbite/components.jsx` `I.*` (union of both): `cart, qr, plus, minus, chevron, filter, search, close, download, check, alert, doc, heart, home, bell, tag, wallet, card, cog, pin`. One `Icon.svelte` with props `{ name: IconName; class?: string }`, 24×24 viewBox, `stroke="currentColor" fill="none"`, stroke-width 1.8 (2.2 for plus/minus), round caps/joins. Export an `IconName` type.

**Verify:** `pnpm --filter @tbite/ui check` → 0 errors. **Commit:** `feat(ui): add Icon component with T-Bite stroke icon set`

### Task 0.3 — Overlay primitives: Modal, Drawer, Toggle

**Files:** Create `packages/ui/src/Modal.svelte`, `Drawer.svelte`, `Toggle.svelte`; Modify `index.ts`

- `Modal.svelte` — port `ui.jsx` `Modal`. Props `{ open, onClose, title, width?, children, footer? }`. Scrim `bg-slate-900/40 backdrop-blur-sm`, `fade-up`, `rounded-2xl` card, close button (`Icon name="close"`), Esc-to-close, click-scrim-to-close, body-scroll-lock while open.
- `Drawer.svelte` — right-slide panel from `EmployeeView`/`MerchantView` drawer shells. Props `{ open, onClose, side?, maxWidth?, children }` with `header`/`footer` snippets. `translate-x-full`→`0` transition, scrim, Esc-to-close.
- `Toggle.svelte` — port `ui.jsx` `Toggle`. Props `{ on, onChange, label? }`, `role="switch"`, keyboard operable.

**Verify:** `pnpm --filter @tbite/ui check` → 0 errors. **Commit:** `feat(ui): add Modal, Drawer, Toggle primitives`

### Task 0.4 — Layout primitives: PageHeader, Tabs, SearchInput, RemainBar, EmptyState

**Files:** Create `packages/ui/src/PageHeader.svelte`, `Tabs.svelte`, `SearchInput.svelte`, `RemainBar.svelte`, `EmptyState.svelte`; Modify `index.ts`

- `PageHeader.svelte` — port `EmployeePages.jsx` `PageHeader`. Props `{ eyebrow, title, subtitle?, actions? }`.
- `Tabs.svelte` — port `EmployeePages.jsx` OrdersPage tab bar. Props `{ tabs: {id,label,count?}[], active, onChange }`, underline style + count pill.
- `SearchInput.svelte` — port `EmployeeView` search box. Props `{ value, onInput, placeholder? }`, pill, left search icon, focus ring.
- `RemainBar.svelte` — port `ui.jsx` `RemainBar`. Props `{ remain, cap }`.
- `EmptyState.svelte` — dashed-border empty card. Props `{ icon?, title, hint? }`.

**Verify:** `pnpm --filter @tbite/ui check` → 0 errors. **Commit:** `feat(ui): add PageHeader, Tabs, SearchInput, RemainBar, EmptyState`

### Task 0.5 — Calibrate existing components to reference

**Files:** Modify `packages/ui/src/Button.svelte`, `Card.svelte`, `MealCard.svelte`, `StateTag.svelte`, `StatCard.svelte`, `LocationBar.svelte`, `TBiteLogo.svelte`

Diff each against `reference_src/ui.jsx` + `ui_kits/tbite/components.jsx` + `ui_kits/tbite/MealCard.jsx`. Align classes exactly: `Button` variants/sizes/focus-ring; `Card` tone variants + `title/description/actions` header; `StateTag` 6 tones incl. `pending`; `StatCard` label/value/delta/hint; `MealCard` hover-lift, low-stock rose pill + pulsing dot, sold-out mask; `TBiteLogo` `small`/`light` props. Fix the 7 `state_referenced_locally` warnings in `MealCard.svelte` while there.

**Verify:** `pnpm --filter @tbite/ui check` → 0 errors, fewer warnings. **Commit:** `refactor(ui): calibrate existing components to design-system reference`

### Task 0.6 — Brand assets

**Files:** Create `apps/employee/static/brand/`, `apps/merchant/static/brand/` (copy from `~/Downloads/T-Bite Design System/assets/`)

Copy `logos/`, `stores/`, `items/`, `categories/` into both. Reference in code as `/brand/logos/r001.png` etc. Use only as fallback when `menu_item_image.blob_uri` is absent.

**Verify:** files present. **Commit:** `chore: add T-Bite brand food photography assets`

### Task 0.7 — Phase 0 gate

**Verify:** `pnpm --filter @tbite/ui check && pnpm --filter @tbite/tokens check` both Done, 0 errors. `index.ts` exports all new components. **This gate must pass before Phases 1/2/3 start.**

---

## Phase 4 — Backend: meal-library stats

**Owner:** single agent, parallel with Phase 0. Blocks Phase 2's meal-library task.

### Task 4.1 — Failing test for menu-item stats

**Files:** Test `services/api/internal/menu/...` (locate the merchant menu-items handler/repo test; create/extend it)

Write a Go test asserting `GET /api/merchant/menu-items` returns, per item, `last_used` (most recent `meal_supply.supply_date`, null if never) and `total_sold` (Σ `order_item.qty` over the item's `picked_up` orders, 0 if none). Seed: one item with two supply dates + some picked-up orders.

**Verify:** `go test ./services/api/internal/menu/...` → FAIL (fields missing). **Commit:** `test(menu): expect last_used/total_sold on merchant menu-items`

### Task 4.2 — Implement stats query + DTO

**Files:** Modify the merchant menu-items repo (`services/api/internal/menu/postgres/...`) + handler DTO (`services/api/internal/menu/http/handlers.go`)

Add a read-only aggregate (single JOIN/lateral query — avoid N+1) computing `last_used` & `total_sold`; add both fields to the menu-item response DTO. No migration, no schema change.

**Verify:** `go test ./services/api/internal/menu/...` → PASS; `go build ./...` clean. **Commit:** `feat(menu): add last_used/total_sold to merchant menu-items`

### Task 4.3 — Regenerate contract & client

**Files:** `contract/openapi/openapi.yaml`, `packages/api-client/src/schema.d.ts` (generated)

Run `make contract-sync`. Confirm the new fields appear in both.

**Verify:** `go test ./...` passes; `pnpm --filter @tbite/api-client check` → Done. **Commit:** `chore(contract): regenerate OpenAPI + TS client for menu-item stats`

---

## Phase 1 — Employee app (`apps/employee`)

**Owner:** subagent. Starts after Phase 0 gate. Touches only `apps/employee/`. Spec: design doc §6.

### Task 1.1 — Layout: sticky header + sidebar

**Files:** Modify `apps/employee/src/routes/+layout.svelte`; Create `apps/employee/src/lib/components/Sidebar.svelte`

Port `EmployeeView` shell **without the role switcher** (header `top-0`, not `top-[52px]`). Header: `TBiteLogo` + `LocationBar` + `SearchInput` + 領餐碼 button (opens TOTP modal) + cart button (badge count) + avatar. Body: `max-w-[1400px] flex gap-6` → `Sidebar` (240px, `sticky top-[100px]`, `hidden lg:block`) + `main`. Sidebar items map to real routes only: 今日首頁 `/`, 我的訂單 `/orders`, 領餐碼 (modal), 我的常點 `/menu/favorites`, 申訴 `/disputes`; active-route highlight; Pro Tip card at bottom. The 領餐碼 modal + cart drawer mount here so they are global; share cart state via a `$lib` store or context.

**Verify:** `svelte-kit sync && pnpm --filter @tbite/employee check` → 0 errors. **Commit:** `feat(employee): rebuild layout with sticky header + sidebar`

### Task 1.2 — Cart store + cart drawer + floating bar

**Files:** Create `apps/employee/src/lib/cart.svelte.ts` (rune-based store), `apps/employee/src/lib/components/CartDrawer.svelte`, `apps/employee/src/lib/components/FloatingCartBar.svelte`

Cart store: `Record<itemId, {qty, name, vendor, price, image}>`, `add/inc/dec/remove/clear`, derived `count`/`total`. `CartDrawer` ports `TbCartDrawer` (item rows, steppers, subtotal, checkout). `FloatingCartBar` ports the dark pill. Checkout submits the existing `?/placeOrder` action (build a form with hidden `item_id`/`qty` fields). No backend change.

**Verify:** check → 0 errors. **Commit:** `feat(employee): add cart store, drawer and floating bar`

### Task 1.3 — TOTP pickup modal

**Files:** Create `apps/employee/src/lib/components/TotpModal.svelte`, `apps/employee/src/lib/components/TotpView.svelte`; Modify `apps/employee/src/routes/+layout.server.ts` (load today's `ready` orders)

`TotpView` ports `TbTotpModal` body (QR built from real code, 6-digit code, countdown, amber note). `TotpModal` wraps it: 0 ready orders → empty state; 1 → show; many → order picker. Fetch code from `/api/employee/orders/{id}/pickup-code`; re-fetch on countdown expiry. Layout loads the ready-order list.

**Verify:** check → 0 errors. **Commit:** `feat(employee): add global TOTP pickup-code modal`

### Task 1.4 — Home page recreation

**Files:** Modify `apps/employee/src/routes/+page.svelte`; Create `apps/employee/src/lib/components/CategoryStrip.svelte`, `FeaturedRow.svelte`

Port `EmployeeView` home: greeting `PageHeader` variant; `CategoryStrip` (chips from distinct `tags` in `day_menu`, client-side filter); three `FeaturedRow` horizontal `MealCard` scrollers fed by `reorder_chips`/`recommend_chips`/`favorite_chips`; full `MealCard` grid from `day_menu` honouring the category filter. Remove `ChipCarousel`/`ReorderChip`/`FavoriteChip`/`RecommendChip` (delete files). Keep all form actions (`placeOrder`, favorites, reorder) + toast logic. Wire add-to-cart into the cart store.

**Verify:** check → 0 errors. **Commit:** `feat(employee): recreate home page per design reference`

### Task 1.5 — Secondary routes design-language pass

**Files:** Modify `apps/employee/src/routes/` — `orders/+page.svelte` (Tabs + `OrderCard` style from `EmployeePages.jsx`), `orders/[id]/+page.svelte`, `orders/[id]/dispute/+page.svelte`, `orders/[id]/pickup/+page.svelte` (reuse `TotpView`), `disputes/+page.svelte`, `menu/favorites/+page.svelte` (FavoritesPage style: No.1 hero + rows), `menu/recommendations/+page.svelte`, `menu/reorders/+page.svelte`, `login/+page.svelte`

Apply `PageHeader`, `Card`, `StateTag`, `MealCard`, tables. Data/actions unchanged. One commit per route or per small group.

**Verify:** `pnpm --filter @tbite/employee check` → 0 errors; `pnpm --filter @tbite/employee build` succeeds. **Commit(s):** `refactor(employee): restyle <route> to design system`

---

## Phase 2 — Merchant app (`apps/merchant`)

**Owner:** subagent. Starts after Phase 0 gate **and** Phase 4 Task 4.3. Touches only `apps/merchant/`. Spec: design doc §7.

### Task 2.1 — Layout

**Files:** Modify `apps/merchant/src/routes/+layout.svelte`

Port `MerchantView` header (logo + 「商家後台 · {vendor}」pill + 通知/設定 buttons + avatar), `bg-slate-50`, single-column, no role switcher.

**Verify:** check → 0 errors. **Commit:** `feat(merchant): rebuild layout per design reference`

### Task 2.2 — Home: today dashboard + plant aggregation

**Files:** Modify `apps/merchant/src/routes/+page.svelte`, `+page.server.ts`; Create `apps/merchant/src/lib/components/PlantAggCard.svelte`

Extend `+page.server.ts` to also load today's orders (`GET /api/merchant/orders?date=today`) grouped by plant. Port `MerchantView` top half: eyebrow + 「今日備餐儀表板」 + `StatCard` row + `PlantAggCard` grid (`TbPlantAggCard`).

**Verify:** check → 0 errors. **Commit:** `feat(merchant): recreate today dashboard with plant aggregation`

### Task 2.3 — 7-day schedule planner (folds in `/supply`)

**Files:** Modify `apps/merchant/src/routes/+page.svelte`, `+page.server.ts`; Create `apps/merchant/src/lib/components/ScheduleDayPicker.svelte`, `ScheduleTable.svelte`, `OrderProgress.svelte`

Load 7-day supply. Port `ScheduleDayPicker` + `ScheduleTable` + `OrderProgress`. Cap edits → `PUT /api/merchant/supply/{itemID}/{date}`; toggle on/off → publish/archive. Today row read-only.

**Verify:** check → 0 errors. **Commit:** `feat(merchant): add 7-day schedule planner`

### Task 2.4 — Meal-library drawer

**Files:** Create `apps/merchant/src/lib/components/MealLibraryDrawer.svelte`; Modify `+page.svelte`, `+page.server.ts`

Load `GET /api/merchant/menu-items` (incl. archived) as the library. Port `MealLibraryDrawer` (search, meal cards with `last_used`/`total_sold` from Phase 4, 加入此日 button). 加入此日 → `PUT supply` (publish first if archived).

**Verify:** check → 0 errors. **Commit:** `feat(merchant): add meal-library drawer`

### Task 2.5 — `/supply` redirect + secondary routes

**Files:** Modify `apps/merchant/src/routes/supply/+page.server.ts` (redirect 301 → `/`); restyle `orders/`, `menus/`, `menus/[id]/`, `menus/new/`, `onboard/`, `login/`

**Verify:** `pnpm --filter @tbite/merchant check` → 0 errors; `build` succeeds. **Commit(s):** `refactor(merchant): redirect /supply and restyle remaining routes`

---

## Phase 3 — Admin app (`apps/admin`)

**Owner:** subagent. Starts after Phase 0 gate. Touches only `apps/admin/`. Spec: design doc §8.

### Task 3.1 — Layout

**Files:** Modify `apps/admin/src/routes/+layout.svelte`

Port `AdminView` header (logo + 「福委會後台 · 管理員」pill + 稽核紀錄/新增邀請 buttons + dark avatar), `bg-slate-50`, single-column, no role switcher.

**Verify:** check → 0 errors. **Commit:** `feat(admin): rebuild layout per design reference`

### Task 3.2 — Home: AdminView recreation

**Files:** Modify `apps/admin/src/routes/+page.svelte`, `+page.server.ts`; Create `apps/admin/src/lib/components/ApprovalCard.svelte`, `AlertList.svelte`

Extend `+page.server.ts` to load in parallel: vendors (pending), anomalies (`GET /api/admin/anomalies`), latest payroll batch (`GET /api/admin/payroll/batches` + its entries). Port `AdminView`: eyebrow + 「福委會後台」 + `StatCard` row + `ApprovalCard` list + `AlertList` + payroll-preview table. Wire approve/notify actions.

**Verify:** check → 0 errors. **Commit:** `feat(admin): recreate governance dashboard`

### Task 3.3 — Secondary routes design-language pass

**Files:** Modify `apps/admin/src/routes/` — `vendors/`, `vendors/[id]/`, `vendors/[id]/documents/`, `payroll/`, `payroll/[id]/`, `payroll/[id]/disputes/`, `payroll/new/`, `anomalies/`, `audit/`, `dlq/`, `login/`

Apply `PageHeader`, `Card`, `StatCard`, `StateTag`, table styling. Data/actions unchanged.

**Verify:** `pnpm --filter @tbite/admin check` → 0 errors; `build` succeeds. **Commit(s):** `refactor(admin): restyle <route> to design system`

---

## Phase 5 — Integration verification

**Owner:** main agent, after Phases 1/2/3 return.

### Task 5.1 — Full build & checks

Run from worktree root:
- `pnpm -r --filter "./apps/*" exec svelte-kit sync`
- `pnpm -r check` → all packages + apps, 0 errors.
- `pnpm build` → all packages + apps build.
- `go build ./... && go test ./...` → pass.
- `pnpm exec prettier --check .` (or `make` equivalent) → formatted.

### Task 5.2 — Visual & smoke review

- Side-by-side each main screen vs `ui_kits/tbite/index.html`.
- Confirm: no role switcher anywhere; employee sidebar has exactly the 5 real routes; cart drawer + TOTP modal + category strip + meal-library drawer all functional against real data.
- Confirm no fabricated/placeholder data.

### Task 5.3 — Finish

Use superpowers:finishing-a-development-branch to open the PR.

---

## Risks (from design doc §13)

- React→Svelte volume (~5,400 JSX lines) — Phase 0 front-loads shared components to cut repetition.
- `day_menu` carries `tags` — **confirmed** (`home_handler.go:203`); category strip needs no backend work.
- Meal-library stats — Phase 4 handles; fallback is to hide the two fields if the query proves costly.
- Multi-order TOTP — modal includes an order picker (a deliberate, reasonable extension over the single-code reference).
- `/supply` consumers — Task 2.5 adds the redirect; update any tests/bookmarks pointing at it.
