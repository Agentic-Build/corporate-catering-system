# P9 Employee Personalization (再點 / 最愛 / 推薦) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (or superpowers:subagent-driven-development) to implement this plan task-by-task.

**Goal**：員工首頁加入三類 chip 快速入口 — 「再點一次」（過去 30 天訂單 frequency）、「我的最愛」（手動收藏品項）、「推薦你今天」（同 plant 同事熱門 + 個人 vendor 偏好加權）— 配合「目標日」狀態機（已取餐 / 過 cutoff → 跳明天），讓員工 1-2 個 chip 就能下單。

**Architecture**：純新增 `favorite_item` 一張表；其餘三類 chip 都從現有 `order` / `order_item` / `meal_supply` 即時計算。「目標日」邏輯在 server-side `HomeService.Compute`；「推薦」拆 3 個小 SQL + Go 端加權排序（α env-tunable）；「整單再點」 server-side clone source order + partial fallback。

**Tech Stack**：沿用 — pgx + huma + chi + SvelteKit。不新增依賴。

**Branch**：`feat/p9-personalization`

**Scope boundary**：
- P9 **做**：`favorite_item` schema + 7 個 employee endpoints (home aggregate / 3 list / fav add+del / reorder) + 員工首頁三 chip carousel + 「看更多」3 個分頁 + MealCard ⭐ 按鈕 + 整合測試
- P9 **不做**：collaborative filtering KNN（保留至 P10）、商家 scorecard 公開化、訂單分析儀表板、預約週期單、發票自動化、MCP 暴露 favorites/reorder tool

---

## Task 1：Migration — `favorite_item` schema

**Files**:
- Create: `migrations/000008_employee_personalization.up.sql`
- Create: `migrations/000008_employee_personalization.down.sql`

```sql
-- up
CREATE TABLE favorite_item (
  user_id      UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  menu_item_id UUID NOT NULL REFERENCES menu_item(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, menu_item_id)
);
CREATE INDEX favorite_item_user_idx ON favorite_item(user_id, created_at DESC);
```

```sql
-- down
DROP TABLE IF EXISTS favorite_item;
```

**Acceptance**: `make migrate-up && make migrate-down` round-trip clean.

---

## Task 2：Favorites repo + service + handlers

**Files**:
- Create: `services/api/internal/menu/postgres/favorite_repo.go`
- Create: `services/api/internal/menu/postgres/favorite_repo_test.go`
- Modify: `services/api/internal/menu/service.go` (add `AddFavorite` / `RemoveFavorite` / `ListFavorites`)
- Modify: `services/api/internal/menu/http/handlers.go` (add 3 endpoints, see §API below)

**Repo interface**:
```go
type FavoriteRepo struct{ pool *pgxpool.Pool }

func (r *FavoriteRepo) Add(ctx context.Context, userID, menuItemID string) error
func (r *FavoriteRepo) Remove(ctx context.Context, userID, menuItemID string) error
func (r *FavoriteRepo) ListByUser(ctx context.Context, userID, targetDay, plant string, limit int, cursor *time.Time) ([]FavoriteChip, *time.Time, error)
```

`FavoriteChip` includes `MenuItemID`, `Name`, `UnitPrice`, `VendorID`, `AvailableToday bool` (computed via LEFT JOIN `meal_supply` on target_day + plant).

**Endpoints**:
| Method+Path | Body / Query | Status |
|---|---|---|
| `POST /api/employee/favorites` | `{menu_item_id}` | 201 (idempotent — ON CONFLICT DO NOTHING) |
| `DELETE /api/employee/favorites/{menu_item_id}` | — | 204 |
| `GET /api/employee/favorites?cursor=&limit=` | — | 200 + `{chips, next_cursor}` |

**Acceptance**:
- Repo test (testcontainers Postgres): add, list (with availability flag), remove, idempotency on duplicate add.
- Handler test: 401 when no bearer, 200 for valid.

---

## Task 3：Reorder service + endpoint (整單再點 partial 模式)

**Files**:
- Create: `services/api/internal/order/reorder_service.go`
- Create: `services/api/internal/order/reorder_service_test.go`
- Modify: `services/api/internal/order/http/handlers.go` (add `POST /reorder`)

**Service**:
```go
type ReorderService struct {
    orderRepo  OrderRepo
    quotaRepo  quota.SupplyRepo
    menuRepo   menu.ItemRepo
    clock      clock.Clock
}

type ReorderInput struct {
    UserID         string
    SourceOrderID  string
    SupplyDate     string // target day
    Plant          string
}

type ReorderResult struct {
    NewOrder         *Order
    UnavailableItems []UnavailableItem  // {menu_item_id, name, reason: "no_supply"|"cutoff_passed"|"out_of_quota"}
}

func (s *ReorderService) Reorder(ctx context.Context, in ReorderInput) (*ReorderResult, error)
```

Flow:
1. Load source order + items (verify `user_id == userID`).
2. For each source item, in a single tx:
   - Query `meal_supply` for `(menu_item_id, target_day, plant)`.
   - If no row → add to `UnavailableItems` reason="no_supply".
   - Else if `cutoff_at <= now()` → reason="cutoff_passed".
   - Else attempt conditional decrement (reuse existing `quota.SupplyRepo.Decrement`); if 0 rows affected → reason="out_of_quota".
3. Items that pass go into a new pending order via `OrderService.Create`.
4. If 0 items pass → return 409 + `unavailable_items`, no order created.
5. Else return 201 + `{new_order_id, unavailable_items}`.

**Endpoint**:
| Method+Path | Body |
|---|---|
| `POST /api/employee/orders/reorder` | `{source_order_id, supply_date}` |

Audit event written via existing audit middleware (`request_id` propagated).

**Acceptance**:
- Reorder all available → 201, new order has same items + qty.
- Reorder with 1/3 unavailable → 201 + 2 items in new order + `unavailable_items` has 1 entry with reason.
- Reorder with 3/3 unavailable → 409.
- Authorization: cannot reorder another user's order → 403.
- Idempotency: re-calling within same minute creates new order (no idempotency key in v1 — note for P10).

---

## Task 4：Home aggregate endpoint + service (目標日 + 3 chip queries)

**Files**:
- Create: `services/api/internal/menu/home_service.go`
- Create: `services/api/internal/menu/home_service_test.go`
- Create: `services/api/internal/menu/recommend.go` (Go-side score function)
- Create: `services/api/internal/menu/recommend_test.go`
- Modify: `services/api/internal/menu/postgres/item_repo.go` (add `PlantPopularity` + `UserVendorAffinity` query methods)
- Modify: `services/api/internal/order/postgres/order_repo.go` (add `RecentOrdersByUser` window-function query for reorder chips)
- Modify: `services/api/internal/menu/http/handlers.go` (add `/home`, `/reorders`, `/recommendations`)

**Target-day derivation** (`HomeService.Compute`):
```go
type HomeState struct {
    TargetDay    string
    HasOrdered   bool
    OrderSummary *order.Summary
}

// 1. today = clock.Now().In(serverTZ).Format("2006-01-02")
// 2. todayOrder, _ := orderRepo.GetByUserDate(userID, today, plant)
// 3. if todayOrder != nil:
//      if status in {picked_up, no_show}: return tomorrowView()
//      else: return today, true, todayOrder.Summary()
// 4. if menuRepo.AllCutoffsPassed(plant, today, now): return tomorrowView()
// 5. return today, false, nil
//
// Query param ?day=YYYY-MM-DD overrides derivation (member can preview tomorrow).
```

**Recommendation algorithm** (3 SQL + Go score):

```go
// recommend.go
type recommendInputs struct {
    Popularity      map[string]float64 // menu_item_id -> base count
    VendorAffinity  map[string]float64 // vendor_id -> normalized weight (sums to 1, or empty for cold-start)
    Items           []MenuItemMeta     // candidates from popularity keys
    Alpha           float64
}

func score(in recommendInputs) []RecommendChip {
    out := make([]RecommendChip, 0, len(in.Items))
    for _, item := range in.Items {
        aff := in.VendorAffinity[item.VendorID]
        s := in.Popularity[item.ID] * (1 + in.Alpha*aff)
        reason := "同事熱門"
        if aff > 0 {
            reason = "因為你常點此家"
        }
        out = append(out, RecommendChip{Item: item, Score: s, Reason: reason})
    }
    sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
    return out
}
```

Three thin queries (each ~5-7 lines):

```sql
-- (a) PlantPopularity(plant, target_day): menu_item_id, SUM(qty)
SELECT oi.menu_item_id, SUM(oi.qty)::float AS popularity
FROM "order" o JOIN order_item oi ON oi.order_id = o.id
WHERE o.supply_date = $1 AND o.plant_id = $2
  AND o.status IN ('cutoff','ready','picked_up')
GROUP BY oi.menu_item_id;

-- (b) UserVendorAffinity(user_id, since): vendor_id, COUNT(*)
SELECT mi.vendor_id, COUNT(*)::float AS cnt
FROM "order" o JOIN order_item oi ON oi.order_id = o.id
JOIN menu_item mi ON mi.id = oi.menu_item_id
WHERE o.user_id = $1 AND o.supply_date >= CURRENT_DATE - INTERVAL '30 days'
  AND o.status IN ('cutoff','ready','picked_up')
GROUP BY mi.vendor_id;

-- (c) MenuItemMeta(ids): id, name, unit_price, vendor_id
SELECT id, name, unit_price, vendor_id
FROM menu_item
WHERE id = ANY($1) AND archived_at IS NULL;
```

Go-side: normalize vendor_affinity to sum=1 (handle empty map → cold-start), score, exclude items in favorites, take Top N.

**Reorder chip query** (window function — already DB-side because window is hard in Go):

```sql
WITH ranked AS (
  SELECT o.id, o.vendor_id, o.supply_date, o.total_amount,
    COUNT(*) OVER (PARTITION BY o.vendor_id) AS freq,
    ROW_NUMBER() OVER (PARTITION BY o.vendor_id ORDER BY o.supply_date DESC) AS rn
  FROM "order" o
  WHERE o.user_id = $1
    AND o.status IN ('cutoff','ready','picked_up')
    AND o.supply_date >= CURRENT_DATE - INTERVAL '30 days'
)
SELECT id, vendor_id, supply_date, total_amount, freq
FROM ranked WHERE rn = 1
ORDER BY freq DESC, supply_date DESC
LIMIT $2;
```

Each reorder chip needs `available_today bool` — batch query `meal_supply` for all items at target_day in one `IN` clause.

**α env-tunable**:
```go
// config.go
type Config struct {
    // ...
    RecommendationAlpha float64 // default 1.0 via getenv
}
// FromEnv: parse RECOMMENDATION_ALPHA (default "1.0")
```

**Home payload shape**:
```jsonc
{
  "target_day": "2026-05-15",
  "has_ordered": false,
  "order_summary": null,
  "reorder_chips": [
    {"source_order_id": "...", "vendor_name": "...", "total_amount": 15000,
     "freq": 4, "items_preview": ["雞腿飯","紅茶"], "available_today": true}
  ],
  "favorite_chips": [
    {"menu_item_id": "...", "name": "...", "unit_price": 12000,
     "vendor_id": "...", "available_today": true}
  ],
  "recommend_chips": [
    {"menu_item_id": "...", "name": "...", "unit_price": 18000,
     "vendor_id": "...", "score": 4.2, "reason": "同事熱門"}
  ],
  "day_menu": [ /* existing GET /api/employee/menu shape */ ]
}
```

Each chip array capped at 5.

**Acceptance**:
- HomeService.Compute unit tests cover: no order / order placed but not picked up / picked_up triggers tomorrow view / all cutoffs passed triggers tomorrow view / `?day=` override.
- Recommend score() unit tests: cold-start user (empty affinity) sorts by popularity; user with vendor preference boosts that vendor's items; favorites excluded.
- Integration test: seed 5 employees same plant ordering 1-2 items each, target employee fetches /home, recommend chips reflect aggregate.

**Endpoints**:
| Method+Path | Query | Notes |
|---|---|---|
| `GET /api/employee/home` | `?day=YYYY-MM-DD` (optional override) | one-shot landing page |
| `GET /api/employee/reorders` | `?cursor=&limit=` | paginated past orders |
| `GET /api/employee/recommendations` | `?day=&cursor=&limit=` | paginated recommendations |

(`/favorites` already in Task 2.)

---

## Task 5：Frontend — 員工首頁三 chip carousel + MealCard ⭐ + 「看更多」

**Files**:
- Modify: `apps/employee/src/routes/+page.server.ts` (replace ad-hoc menu fetch with `/api/employee/home`)
- Modify: `apps/employee/src/routes/+page.svelte` (3 chip carousels + 已點/未點 conditional view)
- Modify: `packages/ui/src/MealCard.svelte` (add ⭐ favorite toggle button)
- Create: `apps/employee/src/lib/components/ChipCarousel.svelte` (3-row component)
- Create: `apps/employee/src/lib/components/ReorderChip.svelte`
- Create: `apps/employee/src/lib/components/FavoriteChip.svelte`
- Create: `apps/employee/src/lib/components/RecommendChip.svelte`
- Create: `apps/employee/src/routes/menu/reorders/+page.svelte` + `+page.server.ts`
- Create: `apps/employee/src/routes/menu/favorites/+page.svelte` + `+page.server.ts`
- Create: `apps/employee/src/routes/menu/recommendations/+page.svelte` + `+page.server.ts`

**Home page state machine**:
- `has_ordered=true` → top section: order_summary card with state badge + cancel/pickup CTA; chip carousels collapsed under a "今天還想加點？" disclosure
- `has_ordered=false` → top section: target_day banner with cutoff countdown; chip carousels expanded full width

**Chip interactions**:
- ReorderChip tap → submit form action to `POST /api/employee/orders/reorder`, on success navigate to `/orders/{new_id}`, on `unavailable_items[]` non-empty show toast: `"3 項中 2 項已加入購物車，雞腿飯今日無供應"`.
- FavoriteChip tap → submit form action to add item to cart (qty=1). Greyed-out chip (`available_today=false`) shows toast "今日無供應".
- RecommendChip tap → same as FavoriteChip; chip shows small reason badge.

**MealCard ⭐ button**:
- Optimistic UI: tap toggles state instantly, fire `POST` or `DELETE /api/employee/favorites/...` in background; on failure revert + toast.
- ⭐ icon uses `apps/employee` design tokens; aria-label `"加入最愛" / "取消最愛"`.

**「看更多」pages**:
- 三條 route 都用 cursor-based pagination, infinite scroll (`IntersectionObserver`) loading next page.
- Empty state messages: 「還沒有訂單紀錄 — 點完第一份午餐後就會出現」/「點 ⭐ 收藏喜歡的菜色」/「正在收集你的偏好，先看看同事都點什麼吧」.

**Acceptance**:
- Manual: 員工帳號登入 → 首頁看到 3 條 chip 列 → tap 任一 chip 加入購物車 → 結帳成功。
- Manual: 取餐後重新整理 → 首頁跳明天視角，3 條 chip 列重新計算。
- Playwright E2E: cold-start user → 三條 chip 都顯示 empty state；加 ⭐ → 重整後 favorite_chips 含該品項。

---

## Task 6：Contract sync + E2E + polish

**Files**:
- Modify: `contract/openapi/openapi.yaml` (regenerated via `make contract-sync`)
- Modify: `packages/api-client/src/schema.d.ts` (regenerated)
- Create: `apps/employee/playwright/tests/personalization.spec.ts`
- Create: `tests/e2e/personalization.spec.ts` (if cross-app needed)

**Contract sync**: run `make contract-sync` after Task 2-4 complete; commit only the generated diff. CI `contract-drift` job must stay green.

**E2E scenarios**:
1. Cold-start employee → home shows 3 empty chip rows with onboarding hint.
2. Employee favorites 1 item → home `favorite_chips` contains it.
3. Employee places + picks up today's order → home target_day flips to tomorrow.
4. Employee tries reorder of a past order with 1 archived item → 201 + `unavailable_items` toast.
5. Same-plant peer ordering boosts recommend ranking (seed scenario).

**Polish checks**:
- a11y: chip carousels keyboard-navigable, ⭐ button has aria-label, focus ring visible.
- Responsive: chip rows scroll horizontally on mobile (<768px), wrap on desktop.
- p99 of `/api/employee/home` under 200ms on local dev stack (manual smoke).

**Acceptance**:
- CI green: `ci-lint-test`, `ci-render-overlay`, `ci-e2e-smoke`, `contract-drift`.
- `make test-go && make test-web` clean locally.

---

## Open questions deferred to P10

1. KNN collaborative filtering swap-in for recommend backend (UI unchanged).
2. MCP tool exposure: `add_favorite` / `reorder_past_order`.
3. Reorder idempotency key (prevent double-submit on flaky network).
4. Recommend cache layer (Redis 5–10 min TTL) once `/home` p99 exceeds 200ms.

---

## Estimated effort

| Task | Effort |
|------|--------|
| 1: Migration | 0.5 d |
| 2: Favorites repo+API | 1 d |
| 3: Reorder service+API | 1 d |
| 4: Home aggregate + recommend | 1.5 d |
| 5: Frontend home + chip + "看更多" + MealCard ⭐ | 2 d |
| 6: Contract sync + E2E + polish | 0.5–1 d |
| **Total** | **6.5–7 person-days** |
