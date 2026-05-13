# T-Bite 平台重構設計文件

**日期**：2026-05-13
**作者**：takala + Claude（brainstorming session）
**狀態**：Design accepted, ready for planning
**範圍**：完全重構為「3 個 SvelteKit 前端 + Go 模組化單體後端 + 雙 overlay 部署」

---

## 1. 目標與非目標

### 目標

- 把現有 Rust 單體後端 + 單 SvelteKit web 前端，重構成：
  - **3 個獨立 SvelteKit 前端**：員工 (Employee) / 商家 (Merchant) / 福委會 (Admin)
  - **1 個 Go 模組化單體後端**（同 binary，三種 entrypoint：api / worker / scheduler）
- 前端視覺 1:1 還原 `~/Downloads/T-Bite Design System/`，移除身分切換器，三 app 各自獨立網域
- 雙部署 overlay：
  - **Single-Node K8s**：k3s/kind，全套 self-hosted image
  - **GCP**：優先綁定 GCP managed services（Cloud SQL / Memorystore / GCS / GCLB / Cloud CDN / Secret Manager / Cloud DNS / Artifact Registry）
- 同一份 Go code、同一套 K8s base manifest，差異只在 overlay
- best-practice 平台：HA、可觀測、可水平擴展、契約一致、安全預設

### 非目標

- 不保留現有 Rust schema 或 API 形狀，允許完全 breaking change
- 不支援 Pub/Sub / Kafka（兩個 overlay 都用 NATS JetStream）
- 不導入 service mesh（NetworkPolicy + cert-manager 已足夠 MVP）
- 不導入推薦引擎、AI 分析儀表板（INITIAL.md 列為第二階段）

---

## 2. 高階架構

```
                          ┌──────────────────────────────────────────┐
                          │     CDN (Cloud CDN | NGINX cache)        │
                          │  靜態 asset · 商家文件 · 食物圖           │
                          │  邊緣 WAF (Cloud Armor | ModSecurity)    │
                          │  邊緣 rate limit / DDoS                  │
                          └─────────────────────┬────────────────────┘
                                                │
                                       ┌────────┴─────────┐
                                       │  Ingress (TLS)   │
                                       │  GCLB | NGINX    │
                                       └────────┬─────────┘
                                                │
   ┌────────────────────┬─────────────────┬─────┴───────┬──────────────────┐
   │ app.tbite.com      │ merchant.       │ admin.      │ api.tbite.com    │
   │                    │ tbite.com       │ tbite.com   │                  │
┌──▼──────────┐   ┌─────▼─────────┐  ┌────▼────────┐ ┌──▼──────────────┐
│ employee    │   │ merchant      │  │ admin       │ │ Go API          │
│ SvelteKit   │   │ SvelteKit     │  │ SvelteKit   │ │ (role=api)      │
│ adapter-node│   │ adapter-node  │  │ adapter-node│ │ HTTP + MCP      │
│ SSR         │   │ SSR           │  │ SSR         │ │ HPA: RPS + p95  │
└──┬──────────┘   └─────┬─────────┘  └────┬────────┘ └──┬──────────────┘
   │                    │                 │             │
   └────────────────────┴─────────────────┴─────────────┤
                       server→server (cluster-internal, no CORS)
                                                         │
        ┌────────────────────┬──────────────┬────────────┼────────────┬──────────────┐
        │                    │              │            │            │              │
   ┌────▼──────┐       ┌─────▼─────┐  ┌─────▼─────┐ ┌────▼──────┐ ┌──▼──────────┐
   │ Postgres  │◀──────│ PgBouncer │  │   Redis   │ │   NATS    │ │  Object     │
   │ Patroni / │  RW   │  txn mode │  │  Cluster  │ │ JetStream │ │  Storage    │
   │ Cloud SQL │       └───────────┘  │ Sentinel /│ │ 3-node    │ │ MinIO / GCS │
   │ multi-AZ  │                      │ Memorystore│ │ RAFT      │ │ S3 API      │
   └─────┬─────┘                      └───────────┘ └─────▲─────┘ └─────────────┘
         │ RO                                              │
         │ (replicas → admin 報表)                          │
         │                                                  │
         │              ┌────────────────────┐              │
         │              │ Worker Pool        │              │
         │              │ (role=worker)      ├──────────────┤
         │              │ KEDA HPA on        │              │
         │              │ NATS pending msgs  │              │
         │              └────────────────────┘              │
         │                                                  │
         │              ┌────────────────────┐              │
         └──────────────│ Outbox Relay       ├──────────────┘
                        │ FOR UPDATE SKIP    │
                        │ LOCKED → publish   │
                        └────────────────────┘

                        ┌────────────────────┐
                        │ Scheduler          │  leader election (K8s Lease)
                        │ (role=scheduler)   │  17:00 cutoff / 月結鎖帳 / 文件到期掃描
                        └────────────────────┘
```

---

## 3. 前端架構

### 3.1 三個 SvelteKit 應用

| App | 網域 | 對應 reference 檔案 | 主要 routes |
|---|---|---|---|
| Employee | `app.tbite.com` | `reference_src/employee.jsx` + `meal-detail-modal.jsx` | `/` (today picker)、`/menu/[day]`、`/cart`、`/orders`、`/orders/[id]`、`/orders/[id]/pickup` (TOTP QR)、`/profile` |
| Merchant | `merchant.tbite.com` | `reference_src/merchant.jsx` + `add-meal-modal.jsx` | `/` (today board)、`/menus`、`/menus/new`、`/menus/[id]`、`/supply` (份數設定)、`/cutoff-rules`、`/orders` (aggregated)、`/labels` (列印標籤)、`/settle` (月結對帳) |
| Admin | `admin.tbite.com` | `reference_src/admin.jsx` | `/` (governance dashboard)、`/vendors`、`/vendors/[id]` (文件 / 審核)、`/mapping` (商家×廠區)、`/payroll` (月結批次)、`/payroll/[batchId]/disputes`、`/anomalies`、`/audit` |

三個 app 都以 **SvelteKit adapter-node** 部署（SSR），不走純 static。理由：

- 員工端首屏需即時剩餘份數與廠區個人化，工廠 wifi 不穩 → SSR 首屏快
- 三個 app 都需 server-side OIDC session 處理（cookie 不外洩 access token 給 browser）
- form action + progressive enhancement，無 JS 也能下單 / 改單

### 3.2 Monorepo 結構

```
corporate-catering-system/
├── apps/
│   ├── employee/             # SvelteKit
│   ├── merchant/             # SvelteKit
│   └── admin/                # SvelteKit
├── packages/
│   ├── ui/                   # 共用 Svelte 元件
│   │   ├── src/
│   │   │   ├── Button.svelte
│   │   │   ├── Card.svelte
│   │   │   ├── MealCard.svelte
│   │   │   ├── StateTag.svelte
│   │   │   ├── StatCard.svelte
│   │   │   ├── Sidebar.svelte
│   │   │   ├── LocationBar.svelte
│   │   │   ├── PlantAggregationCard.svelte
│   │   │   ├── TBiteLogo.svelte
│   │   │   └── icons/        # I.* 一對一移植
│   │   └── assets/           # logos / stores / items / categories
│   ├── tokens/               # colors_and_type.css + Tailwind preset
│   ├── api-client/           # openapi-typescript + openapi-fetch
│   └── eslint-config/
├── services/
│   └── api/                  # Go module
│       ├── cmd/
│       │   └── tbite/        # main.go --role=api|worker|scheduler
│       ├── internal/
│       │   ├── identity/
│       │   ├── menu/
│       │   ├── quota/
│       │   ├── order/
│       │   ├── pickup/
│       │   ├── vendor/
│       │   ├── payroll/
│       │   ├── fulfillment/
│       │   ├── notification/
│       │   ├── audit/
│       │   ├── outbox/
│       │   ├── platform/     # db / cache / queue / storage adapter
│       │   ├── httpserver/   # echo/chi router, middleware
│       │   ├── mcpserver/
│       │   ├── observability/
│       │   └── config/
│       └── pkg/              # 對外可 import 的 SDK（如有）
├── contract/
│   └── openapi/
│       ├── openapi.yaml      # canonical, committed, generated from Go
│       ├── openapi.json
│       └── index.html        # Redoc
├── migrations/               # golang-migrate (*.up.sql / *.down.sql)
├── ops/
│   └── kubernetes/
│       ├── base/
│       ├── components/
│       └── overlays/
│           ├── single-node/
│           └── gcp/
├── docs/
├── scripts/
├── pnpm-workspace.yaml
├── package.json
├── go.mod
└── Makefile
```

### 3.3 設計 token 共用

`packages/tokens` 提供：

- `tokens.css`：`colors_and_type.css` 的 `--tb-*` CSS vars
- `tailwind-preset.js`：把 `--tb-*` 映射成 Tailwind theme（`tb-red-600`、`tb-amber-300`、`tb-rounded-2xl`、`font-noto-tc`…）
- `fonts.css`：Noto Sans TC (400/500/600/700/800/900) + JetBrains Mono (500/600) 自託管 woff2

三個 app 的 `tailwind.config.js`：

```js
import tbitePreset from "@tbite/tokens/tailwind";
export default {
  presets: [tbitePreset],
  content: ["./src/**/*.{html,svelte,ts}", "../../packages/ui/src/**/*.svelte"],
};
```

### 3.4 API client 生成

- Go API 用 [`huma`](https://github.com/danielgtaylor/huma) 或 [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) **從 handler 反推** OpenAPI 3.1
- 產出 `contract/openapi/openapi.yaml`，commit
- `pnpm contract:sync` 用 `openapi-typescript` + `openapi-fetch` 產 `packages/api-client/src/`
- CI gate 阻擋 spec drift（沿用現有 `openapi-contract.yml` workflow 的思路）
- SvelteKit server-side 使用：
  ```ts
  import { client } from "@tbite/api-client";
  // +page.server.ts
  export async function load({ cookies }) {
    const session = await loadSession(cookies);
    return await client.GET("/employee/menu", {
      params: { query: { day: "2026-05-13", plant: "F12B-3F" } },
      headers: { authorization: `Bearer ${session.accessToken}` },
    });
  }
  ```

### 3.5 設計系統移植清單

按 `reference_src/ui.jsx` 的 `I.*` namespace 與 `ui_kits/tbite/` 元件，逐項翻譯成 Svelte，**保留同名 prop 與 className 結構**：

| Reference | Svelte 元件 | 備註 |
|---|---|---|
| `I.Cart, I.QR, I.Plus, I.Minus, I.Chevron, I.Filter, I.Search, I.Close, I.Download, I.Check, I.Alert, I.Doc, I.Toggle` | `packages/ui/src/icons/` 個別檔 | stroke-only, 24×24 viewBox, 1.8px 標準 / 2.2px stepper |
| `SideIcon` (home/doc/qr/heart/card/tag/wallet/bell/cog) | `Sidebar` 內部 | 同左 |
| `Button` | `Button.svelte` | primary/secondary/ghost/danger × sm/md |
| `Card` | `Card.svelte` | default + tone (info/warning/success/danger) |
| `StateTag` | `StateTag.svelte` | pill, 4 tones |
| `MealCard` | `MealCard.svelte` | stepper、low-stock 脈動、sold-out mask |
| `StatCard` | `StatCard.svelte` | merchant dashboard 大數字 |
| `Sidebar` | `Sidebar.svelte` | employee 左側 nav, sticky top-[100px] |
| `LocationBar` | `LocationBar.svelte` | 廠區 + 日選 |
| `PlantAggregationCard` | `PlantAggregationCard.svelte` | merchant 廠區匯總 |
| `TBiteLogo` | `TBiteLogo.svelte` | red-500→rose-700 gradient + amber dot |

**動畫**：`fadeUp` (220ms) / `cartBump` (320ms) / `animate-pulse` 直接在 `packages/ui` 用 CSS keyframes 實作。

---

## 4. 後端架構（Go 模組化單體）

### 4.1 服務拆分原則

- 單一 Go module，三種 entrypoint 同 binary：
  - `--role=api`：HTTP + MCP server
  - `--role=worker`：NATS consumer 池
  - `--role=scheduler`：cron-like jobs，K8s Lease leader election
- 內部 domain 嚴格分模組：`internal/{identity, menu, quota, order, pickup, vendor, payroll, fulfillment, notification, audit, outbox}`
- **跨模組僅透過 port interface 溝通**，不直接 import 別模組的 internal struct
  - e.g. `order` 模組依賴 `quota.Service interface { Decrement(ctx, mealId, day, count) error }`
  - 真實實作 wire 在 `cmd/tbite/main.go`
  - 這個 boundary 同時是未來抽離成 microservice 的切線
- `platform/` 抽象 infra：`platform.DB`、`platform.Cache`、`platform.Queue`、`platform.Storage`、`platform.Clock`、`platform.IDGen` —— 業務模組對它們依賴 interface 而非具體實作

### 4.2 對外 API 路徑表

| Path prefix | 對應前端 | RBAC scope |
|---|---|---|
| `POST /auth/oidc/{provider}/start` | 三端共用 | public |
| `GET /auth/oidc/{provider}/callback` | 三端共用 | public |
| `POST /auth/logout` | 三端共用 | authenticated |
| `POST /auth/refresh` | 三端共用 | authenticated |
| `/api/employee/*` | Employee | `role:employee` |
| `/api/merchant/*` | Merchant | `role:vendor_operator` |
| `/api/admin/*` | Admin | `role:welfare_admin` |
| `/api/internal/*` | （未對外） | service-to-service token |
| `/healthz` `/readyz` `/metrics` | infra | public（cluster 內） |
| `/mcp` (stdio / SSE) | AI agent | scoped token |

### 4.3 模組職責

| 模組 | 對外能力 | 內部主表 |
|---|---|---|
| `identity` | OIDC start/callback、session refresh、user lookup、白名單匹配 | `user`, `user_identity`, `session`, `employee_directory` |
| `menu` | 商家菜單 CRUD、複製、圖片上傳預簽 URL、上架/下架 | `vendor`, `menu_item`, `menu_item_image`, `menu_category` |
| `quota` | 每日份數設定、剩餘查詢、條件式扣減/退還 | `meal_supply`, `daily_supply_snapshot` |
| `order` | 下單、改單、取消、查詢、狀態機 | `order`, `order_item`, `order_state_event` |
| `pickup` | TOTP 產生、核銷 verify | `pickup_token` (Redis-only, ephemeral) |
| `vendor` | 商家入駐、文件生命週期、廠區映射 | `vendor`, `vendor_document`, `vendor_plant_mapping` |
| `payroll` | 月結批次、HR 匯出、退款、爭議處理 | `payroll_batch`, `payroll_entry`, `payroll_dispute` |
| `fulfillment` | 備餐匯總、分區表、配送籃、標籤生成 | `fulfillment_aggregate` (Redis projection) |
| `notification` | 推播 / email / SMS dispatcher | `notification_outbound` |
| `audit` | 寫入 append-only audit、查詢 | `audit_event` |
| `outbox` | DB→NATS relay | `outbox_event` |

### 4.4 訂單狀態機

```
       ┌────────┐
       │ DRAFT  │ (cart, not committed)
       └───┬────┘
           │ submit
           ▼
   ┌───────────────┐  modify   ┌───────────────┐
   │   PLACED      │──────────▶│   PLACED      │ (idempotent re-place)
   │ (餘額凍結?)    │           └───────┬───────┘
   └───┬────────┬──┘                   │
       │        │                       │
  cancel│       │auto-cutoff           │auto-cutoff
       ▼        ▼                       ▼
  ┌────────┐ ┌──────────┐         ┌──────────┐
  │CANCEL'D│ │ CUTOFF   │◀────────│  CUTOFF  │
  └────────┘ │ (鎖單)    │         │ (鎖單)    │
             └────┬─────┘         └──────────┘
                  │ vendor mark ready
                  ▼
             ┌──────────┐  no-show TTL
             │  READY   │────────────────┐
             └────┬─────┘                ▼
                  │ TOTP verify     ┌──────────┐
                  ▼                 │ NO_SHOW  │
             ┌──────────┐           └──────────┘
             │PICKED_UP │
             └────┬─────┘
                  │ admin refund
                  ▼
             ┌──────────┐
             │ REFUNDED │
             └──────────┘
```

合法轉換在 Go 程式碼 enforce（state machine table）。每次轉換寫 `audit_event(order_id, from, to, actor_id, actor_role, reason, payload, at)`，append-only trigger 保護。

### 4.5 Quota 扣減（核心正確性）

```sql
-- 下單：條件式 UPDATE，single-row atomic
UPDATE meal_supply
   SET remain = remain - $1,
       updated_at = now()
 WHERE meal_item_id = $2
   AND supply_date = $3
   AND remain >= $1
RETURNING remain;
```

- 若 0 rows affected → 回 `409 Conflict { code: "OUT_OF_STOCK" }`
- 取消 / 改少：對稱地 `+ delta`
- Redis 只 cache `quota:display:{meal}:{date}` (TTL 1s) 給瀏覽用，**從不**作為扣減來源
- 17:00 截單後，scheduler 將當日所有 `meal_supply` row 投影到 `daily_supply_snapshot`（immutable），所有後續備餐 / 月結查詢走 snapshot，不再壓活躍 row

### 4.6 TOTP 核銷

- Secret：員工 session 建立時隨機產生並存於 `session` 表，僅在 server 與 SvelteKit 之間流動
- Algorithm：HMAC-SHA256(secret, floor(unixtime / 30))，取 6 位 digit
- QR Code 內容：`tbite://pickup?order={order_id}&token={totp}`（顯示用，實際 verify 由前端 form POST）
- Verify endpoint 條件式 UPDATE：
  ```sql
  UPDATE "order"
     SET status = 'PICKED_UP', picked_up_at = now()
   WHERE id = $1 AND status = 'READY';
  ```
  → 同 transaction 寫 audit + outbox
- TOTP 視窗容忍：接受當前 + 前一個 30s window
- 速率限制：同一員工 5 次/min；同一訂單 3 次/min

### 4.7 Outbox Pattern

```sql
CREATE TABLE outbox_event (
  id              BIGSERIAL PRIMARY KEY,
  aggregate_type  TEXT NOT NULL,        -- 'order' | 'payroll' | 'vendor' | ...
  aggregate_id    UUID NOT NULL,
  subject         TEXT NOT NULL,        -- 'order.placed.v1'
  payload         JSONB NOT NULL,
  headers         JSONB NOT NULL,       -- {trace_id, span_id, actor}
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at    TIMESTAMPTZ,
  attempts        INT NOT NULL DEFAULT 0,
  last_error      TEXT
);
CREATE INDEX outbox_unpublished_idx
  ON outbox_event (id) WHERE published_at IS NULL;
```

Relay worker（同 binary `--role=worker --consumer=outbox-relay`）：

```sql
SELECT id, subject, payload, headers
  FROM outbox_event
 WHERE published_at IS NULL
 ORDER BY id
 LIMIT 100
 FOR UPDATE SKIP LOCKED;
```

→ 批次推 NATS → `UPDATE outbox_event SET published_at = now()`。失敗時 `attempts++` + `last_error`，超過 N 次進 admin 視野。

Consumer 端 dedup：用 `event_id` 作 Redis `SETNX idem:{event_id} 1 EX 86400`。

---

## 5. 資料模型（Postgres Schema 摘要）

完整 schema 在 `migrations/`，這裡只列關鍵表與設計約定：

### 5.1 全域約定

- PK：UUID v7（時間排序，索引友善）
- 金額：`BIGINT` 最小貨幣單位（台幣 → 元，不 × 100，因為 $ 整數定價；保留 minor unit domain 便於未來換幣別）
- 列舉：Postgres `ENUM`
- 時間：`TIMESTAMPTZ`，server 一律 `now()`
- Append-only：`audit_event`、`payroll_entry` 用 trigger 阻擋 UPDATE/DELETE/TRUNCATE
- 多租戶：MVP 單租戶（TSMC），但所有表預留 `tenant_id` 欄位（default = `'default'`）

### 5.2 核心表

```sql
-- identity
user (id, primary_email, employee_id?, plant?, department?, role, status, created_at)
user_identity (id, user_id, provider, external_subject, raw_claims, linked_at)
                                          -- UNIQUE (provider, external_subject)
session (id, user_id, totp_secret, refresh_token_hash, expires_at, revoked_at, ...)
employee_directory (employee_id PK, primary_email, plant, department, status)

-- vendor
vendor (id, display_name, legal_name, contact_email, status, ...)
                                          -- status: PENDING | APPROVED | SUSPENDED | TERMINATED
vendor_operator (id, vendor_id, user_id, role)
vendor_document (id, vendor_id, kind, blob_uri, expires_at, status, ...)
vendor_plant_mapping (id, vendor_id, plant_id, time_windows, status)

-- menu / quota
menu_category (id, vendor_id, name, sort_order)
menu_item (id, vendor_id, category_id, name, description, price_minor,
           tags[], badges[], status, archived_at)
menu_item_image (id, menu_item_id, blob_uri, alt, sort_order)
meal_supply (id, menu_item_id, supply_date, capacity, remain, pickup_window,
             eta_label, cutoff_at)
                                          -- UNIQUE (menu_item_id, supply_date)
daily_supply_snapshot (supply_date, menu_item_id, vendor_id, plant_ids,
                       capacity, sold, frozen_at)
                                          -- 截單後 immutable

-- order
order (id, user_id, vendor_id, plant_id, supply_date, status,
       total_price_minor, payment_method, placed_at, cutoff_at,
       picked_up_at, cancelled_at, refunded_at)
                                          -- status ENUM 對應狀態機
order_item (id, order_id, menu_item_id, qty, unit_price_minor)
order_state_event (id, order_id, from_state, to_state, actor_id, actor_role,
                   reason, payload, at)
                                          -- append-only

-- payroll
payroll_batch (id, period_start, period_end, status, locked_at, exported_at)
payroll_entry (id, batch_id, user_id, order_ids[], amount_minor, status, ...)
                                          -- append-only
payroll_dispute (id, entry_id, opened_by, status, resolution, evidence_uri[])

-- governance
audit_event (id, actor_id, actor_role, action, target_kind, target_id,
             payload, at, request_id)
                                          -- append-only
anomaly_alert (id, kind, target_kind, target_id, severity, payload,
               triaged_at, closed_at, evidence_uri[])

-- infra
outbox_event (id, aggregate_type, aggregate_id, subject, payload, headers,
              created_at, published_at, attempts, last_error)
```

### 5.3 索引策略

- `order(user_id, supply_date DESC)` — 員工歷史訂單
- `order(vendor_id, supply_date)` — 商家當日匯總
- `order(status) WHERE status IN ('PLACED','CUTOFF','READY')` — partial index 給 dashboard
- `meal_supply(supply_date)` — 截單掃描
- `audit_event(target_kind, target_id, at DESC)` — 稽核查詢
- `outbox_event(id) WHERE published_at IS NULL` — relay 熱路徑

---

## 6. Auth Flow

### 6.1 OIDC（Google / GitHub）

```
Browser ───────────GET /login────────────▶ SvelteKit (employee app)
                                                │
                                                ▼
                              SvelteKit redirect to Go API
                              /auth/oidc/google/start
                                                │
                                                ▼
                                  Go API 產 state+PKCE,
                                  存 Redis oidc:{state}, 5min TTL
                                                │
                                                ▼
                                  302 → Google authorize
                                                │
                                                ▼
                                   user 登入 + 同意
                                                │
                                                ▼
                              Google → /auth/oidc/google/callback
                                                │
                                                ▼
                              Go API 驗 state, exchange code,
                              拿 id_token, 驗 signature/aud/exp,
                              upsert user_identity,
                              match employee_directory by email,
                              產 access_token (JWT 15min) + refresh_token,
                              refresh_token_hash 寫 session 表
                                                │
                                                ▼
                              302 → SvelteKit /oauth/callback
                              with short-lived exchange code
                                                │
                                                ▼
                              SvelteKit 換 token, 寫 server-side
                              session 到 Redis sess:{sid},
                              Set-Cookie sid=...; HttpOnly; Secure;
                              SameSite=Lax; Domain=app.tbite.com
                                                │
                                                ▼
                              302 → / (logged in)
```

### 6.2 RBAC

- JWT claim：`sub`, `role`, `employee_id?`, `plant?`, `vendor_id?`, `scopes`
- Go API middleware：
  - 解 JWT → context
  - 比對 path prefix 與 role：`/api/employee/*` 要 `role=employee`
  - 細粒度授權在 handler 內呼叫 `access.Allow(ctx, action, resource)`

### 6.3 敏感操作 step-up

福委會做 `vendor.suspend / payroll.lock / payroll.refund` 等動作時：

- 檢查 JWT 的 `auth_time` 是否在 5min 內
- 否則回 `401 { code: "REAUTH_REQUIRED", auth_url: "..." }`
- 前端跳一次 `max_age=0` 的 OIDC re-auth

---

## 7. 事件骨幹（NATS JetStream）

### 7.1 Stream 設計

| Stream | Subjects | Storage | Retention | Replicas |
|---|---|---|---|---|
| `orders.v1` | `order.placed.v1` `order.modified.v1` `order.cancelled.v1` `order.cutoff.v1` `order.ready.v1` `order.picked_up.v1` `order.refunded.v1` `order.no_show.v1` | File | 30d | 3 |
| `quota.v1` | `quota.low_warning.v1` `quota.sold_out.v1` `quota.replenished.v1` | File | 7d | 3 |
| `payroll.v1` | `payroll.batch_locked.v1` `payroll.export_ready.v1` `payroll.dispute_opened.v1` `payroll.dispute_resolved.v1` | File | 90d | 3 |
| `vendor.v1` | `vendor.applied.v1` `vendor.approved.v1` `vendor.document_expiring.v1` `vendor.suspended.v1` `vendor.reinstated.v1` | File | 90d | 3 |
| `notify.v1` | `notify.email.v1` `notify.push.v1` `notify.sms.v1` | File | 24h | 3 |
| `<stream>.dlq` | mirror of failed deliveries | File | 30d | 3 |

### 7.2 Durable Consumers

| Consumer | Stream | 處理 | 部署 |
|---|---|---|---|
| `outbox-relay` | (poll Postgres → publish) | DB → NATS bridge | worker (KEDA on outbox lag) |
| `prep-aggregator` | `orders.v1` | 維護 Redis 中 `fulfillment:{vendor}:{date}:{plant}` 聚合視圖 | worker (KEDA on NATS pending) |
| `quota-watcher` | `orders.v1` | 監測 → publish `quota.low_warning` / `sold_out` | worker |
| `payroll-settler` | `payroll.batch_locked.v1` | 生成 HR CSV、上 MinIO/GCS、發 `payroll.export_ready` | worker |
| `compliance-monitor` | `vendor.v1` + 定時掃描 | 文件即將到期通知 | worker |
| `anomaly-evaluator` | `orders.v1` | rolling window 計算準時率、發 `anomaly_alert` | worker |
| `notification-dispatcher` | `notify.v1` | fan-out 到 email/push 通道 | worker |
| `audit-projector` | all `*.v1` | 寫 analytics 表（次要，可 lag） | worker (lower priority) |

### 7.3 重試與 DLQ

- consumer ack wait：3s 起跳，exponential backoff
- max deliver：5
- 第 5 次失敗 → publish 到 `<stream>.dlq`
- DLQ 由 admin 端 `/admin/dlq` 顯示，可手動 replay 或標記丟棄
- 所有 consumer 必須 **idempotent**（用 `event_id` 在 Redis `idem:{id}` 24h TTL 做 dedup）

---

## 8. 部署拓樸（雙 overlay）

### 8.1 Mapping

| 元件 | `single-node` overlay | `gcp` overlay |
|---|---|---|
| K8s 發行版 | k3s / kind / Docker Desktop K8s | GKE Autopilot (預設) 或 Standard |
| Postgres | Bitnami `postgresql-ha` Helm（3-node Patroni） | Cloud SQL Postgres Enterprise Plus, HA + 1 read replica |
| Connection Pool | PgBouncer StatefulSet, transaction mode | Cloud SQL Auth Proxy sidecar + PgBouncer |
| Redis | Bitnami `redis-cluster` 6 pod (3M+3R) | Memorystore Redis Standard tier, HA |
| NATS | 官方 NATS Helm 3-replica JetStream | 同左跑 GKE（不換 Pub/Sub，理由見 §10） |
| Object Storage | MinIO Bitnami Helm | Cloud Storage (GCS), S3-interop HMAC |
| Ingress | NGINX Ingress Controller | GKE Ingress + BackendConfig + Cloud Armor |
| CDN | NGINX cache (optional, 預設關) | Cloud CDN 綁 GCLB |
| TLS | cert-manager + Let's Encrypt HTTP-01 | Google-managed certificate |
| Secrets | K8s Secret + SOPS (age 加密 in repo) | Secret Manager + External Secrets Operator |
| DNS | dev 走 `/etc/hosts`；prod 外部 DNS | Cloud DNS |
| Image Registry | k3s embedded / GHCR | Artifact Registry |
| Workload Identity | K8s SA + ProjectedToken | GKE Workload Identity（no static SA key） |
| Backup | pg_dump CronJob → MinIO bucket | Cloud SQL automated backup + GCS lifecycle |
| Observability | VictoriaMetrics/Logs/Traces + Grafana | 同左跑 GKE；OTel Collector 可選並行送 Cloud Operations |

### 8.2 Kustomize 結構

```
ops/kubernetes/
├── base/
│   ├── deployment-api.yaml
│   ├── deployment-worker.yaml
│   ├── deployment-scheduler.yaml
│   ├── deployment-web-employee.yaml
│   ├── deployment-web-merchant.yaml
│   ├── deployment-web-admin.yaml
│   ├── service-*.yaml
│   ├── ingress.yaml
│   ├── configmap.yaml
│   ├── hpa-api.yaml
│   ├── networkpolicy-default-deny.yaml
│   ├── networkpolicy-allow-*.yaml
│   ├── poddisruptionbudget-*.yaml
│   ├── serviceaccount-*.yaml
│   └── kustomization.yaml
├── components/
│   ├── keda-worker-autoscaling/
│   ├── multi-az-topology/
│   └── leader-election/
└── overlays/
    ├── single-node/
    │   ├── kustomization.yaml
    │   ├── postgres-ha-statefulset.yaml
    │   ├── redis-cluster.yaml
    │   ├── nats-cluster.yaml
    │   ├── minio.yaml
    │   ├── nginx-ingress.yaml
    │   ├── cert-manager.yaml
    │   ├── observability-victoria.yaml
    │   └── secret-bootstrap.yaml         # SOPS-encrypted
    └── gcp/
        ├── kustomization.yaml
        ├── cloudsql-binding.yaml         # 指 DATABASE_RW_URL 到 cloud-sql-proxy
        ├── memorystore-binding.yaml
        ├── gcs-binding.yaml
        ├── gke-ingress-managed-cert.yaml
        ├── cloud-armor-policy.yaml
        ├── workload-identity.yaml
        ├── external-secrets.yaml         # 拉 Secret Manager
        └── nats-cluster.yaml             # 仍 self-host
```

### 8.3 K8s Deployment 矩陣

| Deployment | Replicas（prod 預設） | HPA | PDB |
|---|---|---|---|
| `web-employee` | 3 | RPS or CPU | minAvailable=2 |
| `web-merchant` | 2 | RPS or CPU | minAvailable=1 |
| `web-admin` | 2 | RPS or CPU | minAvailable=1 |
| `api` | 4 | RPS + p95 latency | minAvailable=3 |
| `worker` | 2（按 consumer 群拆多個 Deployment） | KEDA on NATS pending | minAvailable=1 |
| `scheduler` | 2（active/standby via Lease） | 不縮放 | minAvailable=1 |

### 8.4 Workload Identity（GCP）

- Go API 的 K8s SA `tbite-api` 綁定 GCP SA `tbite-api@<proj>.iam.gserviceaccount.com`
- GCP SA 持有：Cloud SQL Client、Memorystore User、Storage Object Admin（限 bucket）、Secret Manager Secret Accessor
- 完全沒有 static key 進入 image 或 K8s Secret

### 8.5 Single-Node 啟動體驗

```bash
# 一鍵起本機環境
make dev-up               # k3d up + apply overlays/single-node + 種子資料
make dev-app              # 跑 Go API + 三個 SvelteKit dev server
open http://app.tbite.test/      # /etc/hosts: 127.0.0.1 *.tbite.test
open http://merchant.tbite.test/
open http://admin.tbite.test/
```

`/etc/hosts` entry：

```
127.0.0.1 app.tbite.test merchant.tbite.test admin.tbite.test api.tbite.test
```

---

## 9. 可觀測性 & SLO

### 9.1 Pipeline

```
Go API / Worker / Scheduler / SvelteKit
       │ OTLP gRPC
       ▼
OTel Collector (DaemonSet) ───┬──▶ VictoriaMetrics (metrics)
                              ├──▶ VictoriaLogs (logs)
                              └──▶ VictoriaTraces (traces)
                                          │
                                          ▼
                                       Grafana
```

GCP overlay 可加第二 exporter 同時送 Cloud Operations Suite，提供「主 Victoria 故障時的 fallback」。

### 9.2 Hard SLO

| SLI | Target | 視窗 | 量測點 |
|---|---|---|---|
| API 可用率 | 99.9% | rolling 30d | ingress → API 5xx ratio |
| API p99 latency | < 300ms | rolling 7d | API HTTP server histogram（排除 upload） |
| 訂單下單成功率 | > 99.9%（在截單窗口內） | per-day | `/api/employee/orders` 2xx ratio |
| TOTP verify p95 | < 100ms | rolling 1h | `/api/employee/pickup/verify` |
| Outbox lag p95 | < 5s | rolling 5min | `published_at - created_at` |
| Quota race lost rate | < 1% | rolling 1h | `OUT_OF_STOCK` / 總嘗試 |
| Worker DLQ rate | < 0.1% | rolling 1d | 進 dlq / 總 delivery |

CI hard-SLO gate（沿用現有 `load-gate` workflow 概念）跑 k6 against 預發環境，違反任一就 block deploy。

### 9.3 Tracing 規矩

- 一個 request 一個 root span（ingress 注入 traceparent）
- DB query 自動 span（`pgx` otel hook）
- Redis / NATS publish & consume 自動 span
- Outbox publish 把 trace context 寫進 NATS header，consumer 還原成繼續的 span

---

## 10. CI / CD

### 10.1 Gate 矩陣

| Workflow | 觸發 | 內容 |
|---|---|---|
| `lint-and-test` | PR / main | go test、go vet、staticcheck、pnpm lint、pnpm test |
| `contract-check` | PR / main | 生成 OpenAPI → diff committed → 阻擋 drift |
| `migration-check` | PR / main | 真 Postgres up/down 跑全部 migration + invariant verifier |
| `image-build` | merge to main | 多 arch build（linux/amd64 + linux/arm64）+ sign + push registry |
| `e2e-smoke` | post image-build | spin up kind cluster + overlays/single-node → playwright 跑三 app golden path |
| `load-gate` | nightly + pre-release | k6 hard-SLO baseline 在預發環境 |
| `deploy-staging` | post image-build (auto) | apply gcp overlay 到 staging 專案 |
| `deploy-prod` | manual approval | apply gcp overlay 到 prod，跑 canary（10% → 50% → 100%）|
| `gcp-overlay-render-check` | PR | `kustomize build overlays/gcp` 必須成功 + schema 驗證 |
| `single-node-overlay-render-check` | PR | 同上對 single-node |

### 10.2 Migration 部署順序

- 在 deploy 前先跑 K8s Job：`golang-migrate up`
- Migration 必須 **expand-then-contract**：先加欄位、後刪欄位，中間 release 兩端都能跑
- 大量資料 backfill 由 worker 在背景跑（chunked, idempotent），不阻擋 deploy

### 10.3 Release Evidence

每次 prod deploy 產出 artifact bundle（沿用 ISS-005 思路）：

- 對應 git SHA
- 對應 image digest（all services）
- migration 版本
- 對應 OpenAPI spec hash
- 對應 hard-SLO load report JSON
- 對應 staged ramp policy 評估結果

存 GCS bucket，retention 90d。

---

## 11. 安全

- TLS everywhere（含 cluster 內 east-west；single-node 用 cert-manager 自簽 CA，GCP 用 mTLS via NEG）
- K8s NetworkPolicy：default-deny + 白名單允許 frontend→api、api→postgres/redis/nats/storage、worker→postgres/redis/nats
- Container：non-root、`readOnlyRootFilesystem: true`、`drop: [ALL]`、`seccompProfile: RuntimeDefault`、`allowPrivilegeEscalation: false`
- OWASP top10：
  - SQL Injection：全走 prepared statements（pgx）
  - XSS：SvelteKit 預設 escape；任何 raw HTML 過白名單 sanitizer
  - CSRF：SvelteKit form action + `SameSite=Lax` cookie + Origin check
  - SSRF：商家文件上傳走預簽 PUT URL，server 從不主動 fetch user-supplied URL
- Secrets：絕不進 image、絕不進 git plaintext；single-node 用 SOPS + age；GCP 用 Secret Manager
- 速率限制兩層（邊緣 IP + API user_id）
- 稽核：所有寫入動作走 `audit_event`，append-only trigger 保護

---

## 12. MCP Server

- 同 binary `--role=api` 順帶啟一個 MCP server（HTTP/SSE + stdio）
- Tools / Resources 全部走 internal domain service interface（與 HTTP handler 共用），不重寫一套商業規則
- 高風險工具（`vendor.suspend`、`payroll.refund`）需要 scoped token 標記 `mcp:write:high_risk`
- MCP 操作同樣寫 `audit_event` 並標記 `actor_role: agent`
- 沿用現有 `MCP contract parity` 概念，CI 阻擋 spec 與實作漂移

---

## 13. 開發者體驗

### 13.1 Makefile（重設計）

```
make dev-up         # k3d/kind + overlays/single-node + 種子
make dev-down       # 拆
make dev-reset      # 拆 + 砍 volume + 重種
make dev-app        # 本機跑 Go API + 三 SvelteKit dev
make dev-logs svc=  # tail
make migrate-new name=xxx
make migrate-up
make migrate-down
make contract-sync  # Go → OpenAPI → TS client
make test-go
make test-web
make test-e2e
make load-baseline
make render-overlay env=single-node|gcp
```

### 13.2 種子資料

`scripts/seed.go` 種：

- 3 個廠區（F12B-3F / F15-2F / F18-RF）
- 5 個商家（其中一個 PENDING、一個 SUSPENDED 做治理 demo）
- 每商家 5-10 個 menu_item，附 reference 食物圖
- 員工 100 人，分配廠區與部門
- 福委會 2 人
- 今日 + 未來 7 日 meal_supply
- 一筆 payroll_batch（locked + exported）
- 一筆 vendor_document_expiring_alert
- 一筆 anomaly_alert（已 triaged）

讓三個 app 一打開就有完整故事可看。

---

## 14. 從現有 Rust codebase 遷移

由於允許完全 breaking change，採「**重寫但保留設計資產**」策略：

### 保留

- `migrations/` 的設計思路（domain ENUM、append-only trigger、global PK 約定）→ 重寫成 Go-friendly 版本
- `ops/observability/` 的 OTel collector / SLO policy / k6 thresholds → 直接搬，改 service label
- `ops/kubernetes/base/` 的 networkpolicy / pgbouncer / topology → 大部分可重用
- `INITIAL.md` 的需求清單 → 唯一不變的需求源
- `contract/openapi/` 的 CI gate workflow → 改吃 Go 生成的 spec

### 重寫

- `src/*.rs` → `services/api/internal/`（每個 Rust mod 對到一個 Go package）
- `apps/web/` (現有 SvelteKit) → `apps/employee` + `apps/merchant` + `apps/admin`
- `Dockerfile.*` → 改 Go base image
- `Cargo.toml` → `go.mod`
- `package.json` → pnpm workspace root + per-app package.json

### 砍掉

- 現有 `apps/web/` 單一 SPA（被三 app 取代）
- Rust 特定的 tooling（sqlx CLI、cargo workspace）
- 舊 e2e tests（重新針對新 routes 寫）

---

## 15. 階段化執行計畫

| Phase | 範圍 | 退出條件 | 狀態 |
|---|---|---|---|
| **P0 - Skeleton** | Monorepo 結構、tokens/ui package、Go skeleton、golang-migrate、雙 overlay 渲染通 CI | `make dev-up` 起本機；三 app dev server 渲染 hello world；Go API `/healthz` 通 | ✅ Done |
| **P1 - Identity** | OIDC (Google+GitHub) × 3 端、employee_directory、vendor 邀請碼、session、refresh | 三 app 都能登入登出；福委會白名單擋住非授權 | ✅ Done |
| **P2 - Menu & Quota** | 商家 CRUD 菜單、員工瀏覽、Postgres-anchored quota 扣減、Redis cache | 員工能瀏覽今日菜單；併發測試證明不會超賣 | ✅ Done |
| **P3 - Order Lifecycle** | 下單、改單、取消、狀態機、audit_event、outbox、NATS streams | 訂單可走到 PLACED，cutoff scheduler 能鎖單 | ✅ Done |
| **P4 - Pickup & Fulfillment** | TOTP 核銷、商家備餐匯總、廠區分區表、標籤列印 | 模擬尖峰 1000 並發 verify p95<100ms | ✅ Done |
| **P5 - Payroll** | 月結批次、HR CSV、爭議流程、退款 | 一個完整月結 cycle 跑通並產出 evidence | ✅ Done |
| **P6 - Governance** | 商家文件生命週期、anomaly_alert、Admin DLQ、稽核查詢 | 文件到期觸發推播 + admin 可重送 DLQ | ✅ Done |
| **P7 - MCP** | MCP server + tools + parity CI gate | MCP 能查單、下單、發起退款、查稽核 | ✅ Done |
| **P8 - Hardening** | hard-SLO load gate、安全測試、災難演練、文件 | 通過 load-gate 並通過 chaos drill | ✅ Done |

**Refactor complete: P0-P8 all delivered.** See `CHANGELOG.md` for a phase-by-phase
summary and `README.md` for the consolidated architecture / operations guide.

每個 phase 完成都要：跑 e2e smoke、跑 contract drift gate、commit 對應 design doc 更新、產 release evidence。

---

## 16. 未決事項

- **CDN cache key**：員工剩餘份數頁如何避免 cache poisoning？傾向：員工頁全程 `Cache-Control: no-store`，CDN 只 cache 靜態 asset 與商家公開菜單摘要。
- **多廠區同步準時率計算窗口**：rolling 7d 是設計值，待 P6 跑出來確認是否要彈性化。
- **MCP write tool 的 RBAC 對應**：是否需要 fine-grained scope vs. role-based 即可？傾向 role-based 起步。
- **GKE Autopilot vs Standard**：Autopilot 預設能跑大部分需求，但 NATS / Memorystore peering 與 Workload Identity 細節需在 staging 驗證；若有問題退到 Standard。

---

## 17. 變更記錄

| 日期 | 變更 | 作者 |
|---|---|---|
| 2026-05-13 | 初版（brainstorming 全程紀錄） | takala + Claude |
