# 企業訂餐系統 — 第二階段補充規格

## Background

第一階段已完成後端 domain 邏輯、OpenAPI 契約（43 端點）、MCP Server（14 tools）、可觀測性基礎設施與 Kubernetes 部署配置。然而，系統尚無法實際執行端到端流程——缺少前端介面、資料庫持久化層、以及部分 API 流程缺口。本文件定義將現有後端骨架推進至可運行 MVP 所需的全部工作。

---

## 現況摘要

### 已完成

| 層級 | 內容 | 狀態 |
|------|------|------|
| Domain 邏輯 | 身份/權限、菜單/供應窗口、訂單狀態機、TOTP 領餐、商家合規生命週期、廠區配送映射、備餐看板、薪資代扣/月結、稽核軌跡、異常預警 | 89 tests 全過 |
| API 契約 | OpenAPI 3.1 規格（50 operation IDs）、TypeScript client 生成、CI 契約一致性驗證 | 完整 |
| MCP Server | 5 domain / 14 tools、OAuth + RBAC、bridge rotation | 完整 |
| 可觀測性 | OTEL collector、SLO 政策、k6 壓測腳本、Grafana dashboard | 完整 |
| 部署 | K8s manifests（HTTP / MCP / compliance worker）、HPA、PDB | 完整 |

### 缺少

| 層級 | 缺口 |
|------|------|
| **前端** | 完全未建立；INITIAL.md 明確要求 SvelteKit + Tailwind CSS |
| **資料庫** | 無持久化層；domain 邏輯為 in-memory struct，無 PostgreSQL schema、migration、connection pool |
| **認證整合** | 無實際 Corporate SSO / Vendor MFA 整合；僅有 identity model |
| **API 流程缺口** | 員工訂單列表查詢、員工整體扣款明細、商家自助文件上傳、菜單上架/下架狀態切換 |

---

## System Requirement

以下為第二階段需補齊的完整需求。需求編號接續 INITIAL.md，避免重複。

### 5. 資料庫持久化層

核心目標：將現有 in-memory domain 邏輯對接至 PostgreSQL，確保資料可持久、可查詢、可備份。

* 資料庫選型：PostgreSQL 16+，搭配 Rust 端 `sqlx` 或 `sea-orm` 作為查詢層；不使用 ORM 魔法，保持 SQL 可讀可稽核。
* Migration 管理：使用 `sqlx migrate` 或等效工具管理 schema 版本；每次 schema 變更皆需對應 migration 檔案，禁止手動修改正式環境 schema。
* Schema 設計原則：
  - 主鍵使用 UUID v7（時序排序友好）或 ULID，避免自增 ID 洩漏業務量。
  - 所有表格須包含 `created_at`、`updated_at` 時戳欄位（UTC）。
  - 金額欄位使用 `BIGINT`（以最小貨幣單位存儲，如分），禁止 `FLOAT` / `DOUBLE`。
  - 列舉值使用 PostgreSQL `TEXT` + CHECK constraint 或自訂 enum type，保持與 Rust enum 一致。
  - 軟刪除欄位 `deleted_at` 僅在有明確保留需求時使用（如訂單、稽核紀錄），否則直接物理刪除。
* 核心表格（對應現有 domain model）：
  - `employees`：員工基本資料、所屬廠區、在職狀態。
  - `vendors`：商家基本資料、合規狀態。
  - `vendor_documents`：商家上傳文件與審核紀錄。
  - `vendor_plant_delivery_mappings`：商家-廠區配送規則。
  - `menu_items`：菜單品項（含價格、健康標籤、每日供應量、狀態）。
  - `orders` + `order_line_items`：訂單主表與明細。
  - `order_timeline_events`：訂單生命週期事件（append-only）。
  - `pickup_verifications`：領餐 TOTP 核銷紀錄。
  - `payroll_ledger_entries`：薪資代扣分錄。
  - `payroll_disputes`：扣款爭議紀錄。
  - `payroll_settlement_cycles`：月結週期與鎖定狀態。
  - `anomaly_rules` + `anomaly_alerts`：異常規則與告警。
  - `audit_events`：稽核事件（append-only、加密 payload）。
* Connection Pool：使用 `sqlx::PgPool`，設定合理的 `max_connections`（建議 20-50）、`idle_timeout`、`max_lifetime`。
* 測試策略：整合測試需對接真實 PostgreSQL（可用 testcontainers-rs 啟動臨時容器），禁止 mock 資料庫。

### 6. 前端 — 員工端

核心目標：讓員工能透過瀏覽器完成「瀏覽菜單 → 預購 → 查看訂單 → 領餐」的完整流程。

技術棧：SvelteKit 2.x + Tailwind CSS 4.x + TypeScript；使用已生成的 TypeScript client（`contract/generated/ts-client/`）作為 API 呼叫層，禁止手寫 fetch。

* 6.1 登入與身份：
  - 整合 Corporate SSO（OAuth 2.0 / OIDC），登入後取得 JWT，夾帶 `actor_id`、`role`、`plant_scope`。
  - 未登入時導向 SSO 登入頁面；token 過期時靜默刷新或導回登入。
  - MVP 可先實作 mock auth middleware，以 cookie 模擬已驗證身份，但架構需保留替換為真實 SSO 的空間。

* 6.2 菜單瀏覽與預購（對應 `GET /api/v1/employee/menus`）：
  - 主介面為日曆/週檢視，員工可點選日期查看該日可訂餐廳與品項。
  - 自動依員工所屬廠區過濾可配送商家（呼叫 API 時帶入 plant_id）。
  - 支援搜尋（菜名、商家名）與篩選（價格區間、健康標籤 `MenuHealthTag`、是否有剩餘份數）。
  - 每個品項顯示：圖片、名稱、價格、健康標籤、精確剩餘數量、商家名稱。
  - 售完品項灰化顯示但不可選。

* 6.3 下單與訂單修改（對應 `POST /api/v1/employee/orders`、`PATCH .../orders/{orderId}`）：
  - 員工可選擇品項與數量，勾選特殊需求（`SpecialRequest` 固定選項：少飯、去蔥、醬料分開、不要餐具、加辣）。
  - 下單前顯示確認摘要（品項、數量、特殊需求、總金額）。
  - 截單前可修改或取消訂單；顯示截單倒數時間（依商家 `VendorOrderingPolicy`）。
  - 截單後訂單鎖定，介面明確提示不可修改。

* 6.4 訂單列表與狀態追蹤：
  - 「我的訂單」頁面列出所有訂單，顯示日期、商家、品項摘要、狀態（`OrderLifecycleState` 的中文對應）、金額。
  - 支援依狀態篩選（待處理 / 已完成 / 已取消 / 已退款）。
  - 訂單詳情頁顯示完整時間線（`OrderTimelineEvent`）。
  - 售完/缺貨時推播通知或顯示醒目提示。

* 6.5 領餐核銷（對應 `POST .../orders/{orderId}/pickup-verifications`）：
  - 訂單狀態為可領餐時，顯示 TOTP QR Code（每 30 秒刷新）。
  - QR Code 需足夠大且對比清晰，適合手機螢幕展示。
  - 核銷成功後即時更新訂單狀態為「已完成」。

* 6.6 薪資代扣明細（對應 `GET .../orders/{orderId}/payroll-ledger`）：
  - 「扣款明細」頁面列出每月扣款總額與逐筆明細。
  - 支援查看單筆訂單的扣款/退款狀態。
  - 異常扣款可點擊「提出爭議」（對應 `POST .../orders/{orderId}/disputes`），填寫原因後送出。
  - 爭議紀錄可追蹤處理狀態（`PayrollDisputeStatus` 的中文對應）。

* 6.7 通知偏好（對應 `PUT /api/v1/employee/rush-reminder-preferences`）：
  - 設定頁面可啟用/停用「開賣提醒」與「熱門搶購提醒」。

### 7. 前端 — 商家端

核心目標：讓合作商家能自主管理菜單、掌握備餐數量、追蹤配送狀態。

技術棧：同員工端（SvelteKit + Tailwind CSS + 生成的 TS client）。商家透過獨立的 Vendor MFA 登入。

* 7.1 菜單管理（對應 `PUT /api/v1/vendor/menu-items/{menuItemId}`）：
  - 商家可新增、編輯、複製菜單品項。
  - 每個品項設定：名稱、描述、價格、圖片上傳、每日可供應份數、健康標籤。
  - 支援「臨時缺貨」快速操作（將當日剩餘數量設為 0）。
  - 品項可設定狀態：上架 / 暫停供應 / 下架。

* 7.2 預購窗口與截單設定：
  - 商家可設定預購開放區間（預設最早一週前）與截單時間（預設前一天 17:00）。
  - 設定介面需清楚顯示時間軸與影響說明。

* 7.3 備餐看板與匯出（對應 `GET /api/v1/vendor/fulfillment-board`、`POST .../fulfillment-batches`）：
  - 每日備餐看板顯示：各廠區訂單數、品項明細、特殊需求彙總。
  - 一鍵產生備餐匯總表（PDF 或可列印 HTML）。
  - 一鍵產生配送籃清單與餐點標籤（依廠區分組）。
  - 匯出批次可追蹤（`FulfillmentBatchId`）。

* 7.4 配送狀態更新（對應 `POST .../orders/{orderId}/delivery-status`）：
  - 商家可逐筆或批次更新配送狀態（備餐中 → 已打包 → 配送中 → 已送達）。
  - 看板即時反映狀態變更。

* 7.5 訂單與營收檢視（對應 `GET /api/v1/vendor/orders`、`GET .../analytics/operations-dashboard`）：
  - 訂單列表支援日期、廠區、狀態篩選。
  - 月度營收摘要頁面，可對照平台結算資料。

### 8. 前端 — 福委會管理端

核心目標：讓福委會管理員透過 web 介面完成商家審核、廠區映射、月結對帳與異常治理，無需直接操作 API。

技術棧：同上。管理員透過 Corporate SSO 登入（role = CommitteeAdmin）。

* 8.1 商家審核與文件管理（對應 `POST .../vendors/{vendorId}/reviews`、合規相關端點）：
  - 商家申請列表，顯示合規狀態（`VendorComplianceStatus`）。
  - 審核介面：查看商家上傳文件、填寫審核意見、做出決定（核准 / 駁回 / 補件）。
  - 文件到期提醒看板：顯示即將到期與已到期文件清單。
  - 停權/復權操作需確認並記錄原因。

* 8.2 商家-廠區映射（對應 `PUT/DELETE .../vendors/{vendorId}/plant-delivery-mappings/{mappingId}`）：
  - 以矩陣檢視呈現商家 × 廠區的配送規則。
  - 支援快速啟用/停用特定商家對特定廠區的服務。
  - 修改即時生效，員工端立即反映。

* 8.3 月結扣款與例外處理（對應結算相關端點）：
  - 月結週期管理：查看/鎖定/解鎖結算週期（`PayrollSettlementLockState`）。
  - 扣款匯出：一鍵產生符合 HR 格式的薪資代扣資料（CSV/Excel）。
  - 例外清單：自動標記扣款異常（重複扣款、無對應訂單、金額不符、離職員工），支援逐筆處理。
  - 爭議管理：查看員工提出的爭議、指派處理人、更新處理結果。

* 8.4 異常預警（對應 `GET/PUT/PATCH /api/v1/admin/anomaly/*`）：
  - 告警儀表板：依嚴重程度顯示未處理告警，標示 SLA 狀態（正常 / 風險 / 違約）。
  - 規則管理：新增/編輯異常規則（文件到期風險、準時率下降、滿意度偏低、客訴升高）。
  - 告警處理流程：指派 → 調查 → 修復 → 結案，每步留下紀錄。

* 8.5 稽核查詢（對應 `GET /api/v1/admin/audit/*`）：
  - 稽核日誌查詢：依操作者、操作類型、時間區間搜尋。
  - 責任歸屬查詢：追蹤特定事件的完整操作鏈。

* 8.6 營運分析（對應 `GET /api/v1/admin/analytics/operations-dashboard`）：
  - 儀表板顯示：訂單量趨勢、熱門品項排行、各廠區使用率、商家準時率。
  - 支援日期區間與廠區維度切換。

### 9. 前端共通需求

* 響應式設計：員工端需最佳化手機體驗（領餐場景以手機為主），商家端與管理端以桌面為主但不能在平板上崩潰。
* 國際化基礎：MVP 僅支援正體中文，但字串需集中管理（使用 SvelteKit i18n 方案或 JSON 字串檔），保留未來多語系空間。
* 錯誤處理：所有 API 錯誤需轉譯為使用者可理解的中文提示；網路斷線時顯示離線提示而非白屏。
* Loading 狀態：所有非同步操作需有 loading indicator，避免使用者重複點擊。
* URL routing：使用 SvelteKit file-based routing；員工端 `/employee/*`、商家端 `/vendor/*`、管理端 `/admin/*`，三端共用同一 SvelteKit app 但依角色導向。
* 圖片上傳：菜單圖片上傳需支援壓縮與預覽；MVP 可使用 S3 相容 object storage，或直接以 base64 存入 DB（限制大小 ≤ 500KB）。

### 10. 認證與授權整合

核心目標：將現有 `identity.rs` 的 model 對接實際認證流程。

* Corporate SSO：員工與管理員透過公司 OIDC provider 登入；後端驗證 JWT、提取 claims（employee_id、role、plant_ids）。
* Vendor MFA：商家透過帳號密碼 + TOTP 二次驗證登入；後端核發 session token。
* Session 管理：JWT access token（短效 15 min）+ refresh token（長效 7 天）；前端處理 token 刷新。
* 權限中介層：SvelteKit server hooks 驗證 token 並注入 `AuthenticatedActorContext`；頁面層級的 route guard 依 role 阻擋未授權存取。
* MVP 簡化：第一版可用 mock auth（寫死幾組測試帳號），但 middleware 架構需完整，確保替換為真實 SSO 時僅需改 auth provider 實作。

### 11. API 流程缺口補齊

以下為對照前端需求後，發現現有 OpenAPI 契約需補充的端點：

* `GET /api/v1/employee/orders`：員工查詢自己的訂單列表，支援狀態篩選、日期區間、分頁。目前僅有建立與修改端點，缺少列表查詢。
* `GET /api/v1/employee/payroll-summary`：員工查詢月度扣款彙總（非單一訂單），顯示當月扣款總額、退款總額、淨扣款。
* `POST /api/v1/vendor/documents`：商家自助上傳合規文件（目前合規流程僅有管理端審核，缺少商家端上傳入口）。
* `GET /api/v1/vendor/documents`：商家查看已上傳文件與審核狀態。
* `PATCH /api/v1/vendor/menu-items/{menuItemId}/status`：菜單品項狀態切換（上架/暫停/下架），與既有 PUT upsert 端點區分。
* 所有新增端點需同步更新 OpenAPI 規格、TypeScript client 與 CI 契約驗證。

### 12. 雲端基礎設施與橫向擴展

核心目標：支撐數千萬使用者的併發訂餐場景（尖峰時段數十萬 RPS），所有有狀態元件皆可橫向擴展或由託管服務承擔，應用層完全 stateless。

#### 12.1 現況缺口

現有 K8s manifests 僅部署 stateless API pods（HTTP 6 replicas / MCP 4 replicas / compliance worker），缺少以下關鍵基礎設施：

| 缺口 | 影響 |
|------|------|
| 無 PostgreSQL 部署 | 無持久化，domain 邏輯無法落地 |
| 無 connection pooler | 數百 pods 直連 DB 將耗盡 `max_connections` |
| 無快取層 | 菜單、配送映射等高頻讀取直擊 DB，延遲與成本不可控 |
| 無訊息佇列 | 訂單事件、通知、結算批次皆同步處理，尖峰時阻塞 API |
| 無 object storage | 菜單圖片無處存放 |
| 無 Ingress / Gateway | 無 TLS 終端、無邊緣限流、無路由分流 |
| 無 NetworkPolicy | 命名空間內所有 pod 可互訪，違反最小權限 |
| 無 Secret 管理 | 僅一個 secretKeyRef，無 rotation、無 Vault 整合 |
| 無 multi-AZ 拓撲 | 單一可用區故障即全站停擺 |
| HPA maxReplicas: 30 | 不足以支撐數千萬使用者的尖峰流量 |
| 無前端部署 | SvelteKit app 無 Deployment / Service / Ingress |
| 無 Kustomize overlay | 只有 base，無 staging / production 分層 |

#### 12.2 PostgreSQL 叢集

* 部署方式：使用 CloudNativePG operator 管理 PostgreSQL 叢集，或對接雲端託管服務（如 Cloud SQL / RDS / Neon）。自建叢集需定義 `Cluster` CRD。
* 拓撲：
  - 1 Primary + N Read Replicas（初始 2 replicas，依讀取負載擴展）。
  - Primary 負責所有寫入；read replicas 透過 streaming replication 同步。
  - 讀寫分離：後端 connection string 區分 `DATABASE_URL`（RW）與 `DATABASE_URL_RO`（RO）；菜單瀏覽、訂單列表等純讀查詢走 RO。
* 儲存：
  - PVC 使用高 IOPS 的 StorageClass（如 `gp3` / `pd-ssd`），初始容量 100Gi，啟用 volume expansion。
  - WAL 獨立 PVC（20Gi），避免 WAL 與資料爭搶 I/O。
  - 備份：啟用 CloudNativePG 的 `backup` 排程（每日 full + 持續 WAL archiving 至 object storage），保留 30 天。
* 連線數規劃：
  - PostgreSQL `max_connections` 設為 200（預留給 pooler 與管理連線）。
  - 應用層不直連 DB，一律透過 PgBouncer 連線池。
* 高可用：
  - 自動 failover（CloudNativePG 內建），failover 時間 < 30 秒。
  - PDB 保護：至少 1 個 replica 存活。

#### 12.3 連線池（PgBouncer）

* 部署為獨立 Deployment（不嵌入 sidecar），2+ replicas，前方掛 Service。
* 模式：`transaction` mode（每次交易結束歸還連線），適合高併發短查詢場景。
* 容量規劃：
  - 每個 PgBouncer instance 的 `default_pool_size` = 25，`max_client_conn` = 5000。
  - API pods 連線至 PgBouncer Service，不直連 PostgreSQL。
  - 讀寫分離：部署兩組 PgBouncer Service —— `pgbouncer-rw`（指向 Primary）與 `pgbouncer-ro`（指向 Read Replicas）。
* 監控：PgBouncer 暴露 `SHOW STATS` / `SHOW POOLS` 指標，透過 exporter 接入 VictoriaMetrics。

#### 12.4 快取層（Valkey / Redis）

* 部署方式：Valkey（Redis 開源替代）Cluster mode，使用 Kubernetes operator（如 Spotahome Redis Operator）或託管服務。
* 叢集拓撲：3 primary + 3 replica shards（初始），啟用 hash slot 自動分片。
* 用途與 TTL 策略：

| 用途 | Key pattern | TTL | 說明 |
|------|-------------|-----|------|
| 菜單快取 | `menu:{plant_id}:{date}` | 5 min | 員工瀏覽菜單時先查快取，miss 再查 DB |
| 配送映射快取 | `delivery:{vendor_id}:{plant_id}` | 10 min | 高頻查詢但低頻變更 |
| 庫存計數器 | `stock:{menu_item_id}:{date}` | 至截單時間 | 使用 `DECR` 原子扣減，避免超賣 |
| Session / Token | `session:{token_hash}` | 15 min | JWT refresh token 黑名單、rate limit 計數 |
| 分散式鎖 | `lock:{resource}` | 30 sec | 月結鎖定、批次匯出等互斥操作 |

* 快取失效策略：菜單/配送映射變更時主動 `DEL` 對應 key（write-through invalidation），不依賴 TTL 過期。
* 儲存：快取層為純記憶體，不啟用持久化（RDB/AOF off）；資料遺失時由 DB 回填，不影響正確性。

#### 12.5 訊息佇列（NATS JetStream）

* 選型：NATS JetStream——輕量、雲原生、支援 at-least-once 語意與持久化。
* 部署：3 節點 NATS cluster，啟用 JetStream 並掛載 PVC（50Gi，用於 stream 持久化）。
* Streams 與 Consumers：

| Stream | Subject pattern | Consumer group | 說明 |
|--------|----------------|----------------|------|
| `ORDERS` | `orders.created`, `orders.modified`, `orders.cancelled`, `orders.fulfilled` | `order-fulfillment-worker` | 訂單事件驅動商家備餐看板更新、庫存回補 |
| `NOTIFICATIONS` | `notify.rush-reminder`, `notify.sold-out`, `notify.pickup-ready` | `notification-dispatcher` | 非同步推播/email/in-app 通知 |
| `SETTLEMENTS` | `settlement.cycle-locked`, `settlement.export-requested`, `settlement.hr-sync` | `settlement-worker` | 月結批次處理、HR API 同步 |
| `COMPLIANCE` | `compliance.doc-expiring`, `compliance.review-completed` | `compliance-event-handler` | 文件到期提醒、合規狀態更新 |
| `AUDIT` | `audit.event` | `audit-writer` | 稽核事件非同步寫入（解耦 API 延遲） |

* 背壓控制：每個 consumer 設定 `max_ack_pending`，超過時暫停拉取，避免 worker 過載。
* 死信：連續 5 次處理失敗的訊息移至 `*.dlq` subject，搭配告警規則通知 on-call。

#### 12.6 Object Storage（MinIO / S3）

* 部署方式：MinIO Tenant（K8s operator）或直接對接雲端 S3 / GCS / R2。
* Buckets：

| Bucket | 用途 | 存取模式 |
|--------|------|----------|
| `menu-images` | 菜單品項圖片 | 商家上傳（presigned PUT）、員工讀取（public read 或 presigned GET） |
| `compliance-docs` | 商家合規文件 | 商家上傳、管理端讀取（private） |
| `fulfillment-exports` | 備餐匯總表/標籤 PDF | 商家端讀取（presigned GET，1hr TTL） |
| `settlement-exports` | HR 扣款檔案 | 管理端讀取（private，audit logged） |
| `db-backups` | PostgreSQL WAL archive + base backup | 系統內部（private） |

* 圖片處理：上傳時後端驗證 MIME type（僅允許 image/jpeg, image/png, image/webp）、限制大小 ≤ 2MB、自動生成縮圖（300x300）。
* 前端上傳流程：前端向後端請求 presigned upload URL → 前端直傳 MinIO → 後端收到 callback 或前端確認後更新 menu_item record。

#### 12.7 Ingress / Gateway

* 使用 Gateway API（`gateway.networking.k8s.io/v1`）取代傳統 Ingress，搭配 Envoy Gateway 或 Cilium Gateway。
* 路由規則：

| Host / Path | 目標 Service | 說明 |
|-------------|-------------|------|
| `catering.corp.example/api/*` | `corporate-catering-api:80` | HTTP REST API |
| `catering.corp.example/mcp/*` | `corporate-catering-mcp:80` | MCP gateway |
| `catering.corp.example/*` | `corporate-catering-frontend:80` | SvelteKit SSR |
| `catering.corp.example/health/*` | `corporate-catering-api:80` | 健康探針（bypass auth） |

* TLS：由 cert-manager 自動簽發與續約（Let's Encrypt 或公司內部 CA）。
* 邊緣限流（Rate Limiting）：
  - 全域：10,000 RPS（保護後端免於 DDoS）。
  - Per-user：employee 60 req/min、vendor 120 req/min、admin 300 req/min。
  - 下單端點（`POST /api/v1/employee/orders`）額外限制 10 req/min/user，防止搶購腳本。
* CORS：僅允許 `catering.corp.example` origin。
* Request size limit：一般端點 1MB、圖片上傳端點 5MB。

#### 12.8 前端部署

* SvelteKit app 以 `@sveltejs/adapter-node` 打包為 Docker image，部署為獨立 Deployment。
* Replicas：min 3 / max 20，HPA 以 CPU 70% 與 RPS 為指標。
* 靜態資源（`_app/immutable/*`）由 Gateway 層設定 `Cache-Control: public, max-age=31536000, immutable`。
* 如有 CDN（CloudFront / Cloudflare）：靜態資源走 CDN，SSR 請求回源至 SvelteKit pods。
* 環境變數注入：`PUBLIC_API_BASE_URL`、`PUBLIC_SENTRY_DSN` 等透過 ConfigMap 掛載。

#### 12.9 NetworkPolicy

* 預設拒絕所有 ingress/egress（`default-deny-all`），僅白名單放行必要流量：

| From | To | Port | 說明 |
|------|----|------|------|
| Gateway | frontend pods | 3000 | SSR 請求 |
| Gateway | api pods | 8080 | REST API |
| Gateway | mcp pods | 8081 | MCP gateway |
| api / mcp / compliance-worker pods | pgbouncer-rw | 6432 | 資料庫寫入 |
| api / mcp pods | pgbouncer-ro | 6432 | 資料庫讀取 |
| api / mcp / worker pods | valkey | 6379 | 快取 |
| api / mcp / worker pods | nats | 4222 | 訊息佇列 |
| api / worker pods | minio | 9000 | Object storage |
| frontend pods | api pods | 8080 | SSR 端 server-side fetch |
| otel-collector | victoria-metrics | 8428 | 指標匯出 |

#### 12.10 Secret 管理

* 敏感資料一律存放於 Kubernetes Secret，透過 `ExternalSecret`（External Secrets Operator）同步自外部 secret store（HashiCorp Vault / AWS Secrets Manager / GCP Secret Manager）。
* 需管理的 secrets：

| Secret name | 內容 | Rotation |
|-------------|------|----------|
| `db-credentials` | PostgreSQL superuser + app user 密碼 | 90 天 |
| `pgbouncer-auth` | PgBouncer userlist.txt | 隨 db-credentials 連動 |
| `valkey-auth` | Valkey `requirepass` | 90 天 |
| `nats-auth` | NATS NKey / credential file | 90 天 |
| `minio-credentials` | MinIO access key / secret key | 90 天 |
| `oidc-client-secret` | Corporate SSO client secret | 依 IdP 政策 |
| `pickup-totp-secret` | TOTP 簽發密鑰 | 180 天 |
| `jwt-signing-key` | JWT RS256 private key | 180 天 |

* Secret rotation 時不中斷服務：支援 dual-key window（新舊 key 並存驗證），rotation 完成後移除舊 key。

#### 12.11 Multi-AZ 與拓撲約束

* 所有 Deployment 加入 `topologySpreadConstraints`，確保 pods 均勻分散至不同可用區：
  ```yaml
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          app.kubernetes.io/name: <component>
  ```
* PostgreSQL primary 與 replica 強制分散至不同 AZ（CloudNativePG `affinity` 配置）。
* Valkey primary/replica 分散至不同 AZ。
* NATS 節點分散至 3 個 AZ。

#### 12.12 HPA 與擴展策略

現有 HPA maxReplicas: 30 不足以支撐數千萬使用者。重新規劃：

| Component | minReplicas | maxReplicas | Scale metric | Target |
|-----------|-------------|-------------|--------------|--------|
| API server | 10 | 200 | CPU 65% / RPS 150/pod / in_flight 50/pod | 尖峰 30,000 RPS |
| MCP gateway | 4 | 50 | CPU 60% / tool_requests 80/pod | AI agent 整合 |
| Frontend SSR | 3 | 60 | CPU 70% / RPS 200/pod | SSR 渲染 |
| Compliance worker | 2 | 10 | NATS pending messages | 事件驅動 |
| Order event worker | 3 | 40 | NATS pending `orders.*` | 訂單事件處理 |
| Notification dispatcher | 2 | 30 | NATS pending `notify.*` | 通知推播 |
| Settlement worker | 1 | 8 | NATS pending `settlement.*` | 月結僅月底尖峰 |

* KEDA（Kubernetes Event-Driven Autoscaling）：NATS consumer 類型的 worker 使用 KEDA `ScaledObject`，依佇列深度自動擴縮，idle 時可縮至 0（settlement-worker 非月底時）。
* Scale-up 策略：`stabilizationWindowSeconds: 0`，最大 burst +100% / 60s。
* Scale-down 策略：`stabilizationWindowSeconds: 300`，每 60 秒最多縮 10%，避免振盪。

#### 12.13 Kustomize Overlays

從現有 `base/` 建立分層 overlay：

```
ops/kubernetes/
├── base/                    # 現有共用 manifests
├── components/              # 可選元件（KEDA triggers、NetworkPolicy）
├── overlays/
│   ├── dev/                 # 本地開發（minikube / kind）
│   │   └── kustomization.yaml   # replicas: 1、resource limits 最低、mock auth
│   ├── staging/             # 預發布環境
│   │   └── kustomization.yaml   # replicas 縮減、連接 staging DB/cache
│   └── production/          # 正式環境
│       └── kustomization.yaml   # 完整 HA 配置、real secrets、full HPA
```

* `dev` overlay：PostgreSQL 單節點（無 replica）、Valkey 單節點、NATS 單節點、MinIO standalone；適合本地 `kind` cluster 快速啟動。
* `staging` overlay：PostgreSQL 1+1、Valkey 3 節點、NATS 3 節點；HPA maxReplicas 降為生產的 1/10。
* `production` overlay：完整 HA 配置如上述各節所定義。

#### 12.14 CI/CD Pipeline 擴充

在既有 `openapi-contract.yml` 與 `observability-slo-gate.yml` 之上，追加：

* **image-build.yml**：多階段 Docker build（Rust release binary + SvelteKit adapter-node），推送至 GHCR，tag 為 `git sha` + `latest`。
* **db-migration-check.yml**：PR 時自動起 PostgreSQL testcontainer，執行所有 migration up/down，確保可逆。
* **e2e-smoke.yml**：部署至 staging（或 ephemeral namespace），跑 Playwright e2e smoke test，驗證核心流程。
* **deploy-production.yml**：merge to main 後觸發，執行 kustomize build production overlay → kubectl apply，搭配 Argo Rollouts 或 K8s rolling update。
* **load-test-gate.yml**：production deploy 前以 k6 對 staging 執行壓測，未達 SLO 閾值則阻擋。

#### 12.15 容量規劃參考

以 1,000 萬活躍使用者、午餐尖峰 30 分鐘內 70% 使用者同時操作為假設：

| 指標 | 估算 |
|------|------|
| 尖峰併發使用者 | 7,000,000 |
| 平均每使用者操作 | 3 req（瀏覽 + 下單 + 確認） |
| 尖峰總請求量 | 21,000,000 / 1,800s ≈ **11,700 RPS** |
| 加上 headroom 2x | **~25,000 RPS** |
| API pod 需求（@150 RPS/pod） | ~167 pods |
| DB read QPS（@80% read） | ~20,000 QPS → PgBouncer + read replicas |
| Cache hit rate 目標 | ≥ 90%（菜單/映射查詢） → DB read 降至 ~2,000 QPS |
| 訊息佇列 throughput | ~12,000 msg/s（orders + notifications） |

此規劃為保守估算；系統需在 k6 壓測中驗證實際 pod 數與延遲，並依結果調整 HPA 參數。

### 13. 本地開發環境

核心目標：任何開發者在 clone repo 後，一條指令即可啟動完整的本地開發環境（含所有有狀態依賴），不需手動安裝 PostgreSQL、Valkey、NATS 等服務，也不需存取正式雲端資源。

#### 13.1 容器運行環境

* 推薦 OrbStack（macOS）作為主要容器與 K8s 運行環境；輕量、啟動快、原生支援 Docker API 與內建 K8s cluster。
* 替代方案：Docker Desktop（跨平台）、Podman（Linux）。CI 環境使用 Docker-in-Docker 或 GitHub Actions runner 原生 Docker。
* 不強制要求開發者安裝 minikube 或 kind——日常開發以 Docker Compose 為主，K8s 相關開發（manifest 驗證、overlay 測試）才需本地 K8s cluster。

#### 13.2 Docker Compose 開發堆疊

專案根目錄提供 `docker-compose.yml`（日常開發）與 `docker-compose.full.yml`（完整堆疊含可觀測性）：

**`docker-compose.yml`（精簡版，啟動 < 30 秒）：**

| Service | Image | Port | 說明 |
|---------|-------|------|------|
| `postgres` | `postgres:16-alpine` | 5432 | 單節點，自動執行 migration（mount `migrations/` 目錄） |
| `valkey` | `valkey/valkey:8-alpine` | 6379 | 單節點 standalone mode，無密碼 |
| `nats` | `nats:2-alpine` | 4222, 8222 | 單節點 JetStream enabled，8222 為 monitoring |
| `minio` | `minio/minio:latest` | 9000, 9001 | standalone mode，預設 bucket 自動建立（9001 為 console UI） |
| `api` | build from Cargo | 8080 | Rust API server，hot-reload 使用 `cargo-watch` |
| `frontend` | — | 5173 | SvelteKit `vite dev`，直接在 host 執行或 container 化 |

* `postgres` 啟動時自動建立開發用資料庫 `catering_dev`，用戶 `catering` / 密碼 `catering`（僅限本地）。
* `minio` 啟動時透過 `mc` client init script 自動建立所有 buckets（menu-images、compliance-docs 等）。
* NATS 啟動時透過 config 自動建立所有 JetStream streams。

**`docker-compose.full.yml`（完整版，含可觀測性）：**

在精簡版基礎上額外啟動：

| Service | Image | Port | 說明 |
|---------|-------|------|------|
| `otel-collector` | `otel/opentelemetry-collector-contrib` | 4317, 4318 | OTLP gRPC + HTTP receiver |
| `victoria-metrics` | `victoriametrics/victoria-metrics` | 8428 | 指標儲存 |
| `victoria-logs` | `victoriametrics/victoria-logs` | 9428 | 日誌儲存 |
| `grafana` | `grafana/grafana-oss` | 3000 | 預載 SLO dashboard + data source |
| `pgbouncer` | `bitnami/pgbouncer` | 6432 | 測試 pooler 行為（API 可選連接） |

#### 13.3 一鍵啟動指令

```bash
# 日常開發（最常用）
make dev              # = docker compose up -d && cargo watch -x run

# 完整堆疊（含可觀測性）
make dev-full         # = docker compose -f docker-compose.full.yml up -d

# 僅啟動依賴（不啟動 API，自己用 cargo run）
make dev-deps         # = docker compose up -d postgres valkey nats minio

# 重置資料庫（drop + re-migrate）
make dev-reset-db     # = sqlx database drop && sqlx database create && sqlx migrate run

# 灌入種子資料
make dev-seed         # = cargo run --bin seed

# 停止所有服務
make dev-down         # = docker compose down -v
```

* `Makefile` 統一所有開發指令，避免開發者記憶長指令。
* 所有 `make dev*` 指令預設使用 `.env.development` 環境變數檔案。

#### 13.4 環境變數與設定檔

專案根目錄提供 `.env.development`（已入版控、僅含本地開發值，無真實密鑰）：

```env
DATABASE_URL=postgres://catering:catering@localhost:5432/catering_dev
DATABASE_URL_RO=postgres://catering:catering@localhost:5432/catering_dev
VALKEY_URL=redis://localhost:6379
NATS_URL=nats://localhost:4222
MINIO_ENDPOINT=http://localhost:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin
JWT_SECRET=dev-only-not-for-production
PICKUP_TOTP_SECRET=dev-only-totp-secret
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317
RUST_LOG=corporate_catering=debug,tower_http=debug
AUTH_MODE=mock
MOCK_EMPLOYEE_ID=EMP001
MOCK_EMPLOYEE_PLANT=PLANT-A
```

* `AUTH_MODE=mock` 啟用 mock auth middleware，自動以 `MOCK_EMPLOYEE_ID` 身份登入，免去 SSO 設定。
* `.env.development` 入版控；`.env.local`（開發者個人覆蓋）加入 `.gitignore`。
* `.env.production` 與 `.env.staging` **不入版控**，由 CI/CD 注入。

#### 13.5 種子資料（Seed Data）

提供 `src/bin/seed.rs` 二進位檔，執行後灌入可完整操作的開發資料：

* 3 個廠區（PLANT-A, PLANT-B, PLANT-C）
* 5 家商家（含不同合規狀態：核准、審核中、停權）
* 每家商家 5-10 個菜單品項（含各種健康標籤）
* 商家-廠區配送映射（涵蓋完整/部分/不可配送組合）
* 10 名員工（分屬不同廠區，含 1 名已離職）
* 預建 20 筆訂單（涵蓋所有 `OrderLifecycleState`）
* 當月結算週期（含鎖定與未鎖定狀態）
* 3 條異常規則 + 5 筆告警（涵蓋所有嚴重程度與 SLA 狀態）
* 稽核事件範例

種子資料設計原則：覆蓋每個 UI 頁面的正常與邊界情境，開發者無需手動造資料即可驗證完整流程。

#### 13.6 本地 Kubernetes 開發（選用）

需要測試 K8s manifests、HPA 行為或 Kustomize overlay 時：

* 使用 OrbStack 內建 K8s 或 `kind`（Kubernetes IN Docker）建立本地 cluster。
* 套用 `dev` overlay：`kubectl apply -k ops/kubernetes/overlays/dev/`
* `dev` overlay 特性：
  - 所有元件 replicas: 1。
  - 資源 requests/limits 降至最低（CPU 100m / Memory 128Mi）。
  - 使用本機 image（`imagePullPolicy: Never`），無需推送至 registry。
  - ConfigMap 注入 `.env.development` 等效設定。
  - 不部署 NetworkPolicy（簡化偵錯）。
* Tilt 或 Skaffold（選用）：如需 K8s 環境下的即時重載，可搭配 Tilt 定義 `Tiltfile`，偵測 Rust/SvelteKit 原始碼變更後自動重建 image 並 rolling update。

#### 13.7 開發工具鏈

以下為建議的本地開發工具安裝（透過 `scripts/setup-dev.sh` 自動檢查與安裝提示）：

| 工具 | 用途 | 安裝方式 |
|------|------|----------|
| Rust toolchain | 後端編譯 | `rustup` |
| `cargo-watch` | Rust hot-reload | `cargo install cargo-watch` |
| `sqlx-cli` | DB migration 管理 | `cargo install sqlx-cli` |
| Node.js 22+ | 前端開發 | `fnm` / `nvm` |
| PNPM 10+ | 前端套件管理 | `corepack enable` |
| Docker / OrbStack | 容器運行 | OrbStack.app 或 docker.com |
| `mc` (MinIO Client) | Object storage 偵錯 | `brew install minio/stable/mc` |
| `nats` CLI | NATS 偵錯 | `brew install nats-io/nats-tools/nats` |
| `jq` | JSON 處理 | `brew install jq` |

`scripts/setup-dev.sh` 行為：
1. 檢查上述工具是否已安裝，未安裝則印出安裝指令（不自動安裝，尊重開發者偏好）。
2. 檢查 Docker daemon 是否運行。
3. 複製 `.env.development` → `.env.local`（如不存在）。
4. 執行 `pnpm install` + `cargo check` 確認工具鏈完整。

#### 13.8 前端開發伺服器

* SvelteKit 開發伺服器（`pnpm -C frontend dev`）以 Vite proxy 將 `/api/*` 與 `/mcp/*` 轉發至本地 Rust API（`localhost:8080`），免去 CORS 問題。
* Vite config 範例：
  ```js
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/mcp': 'http://localhost:8080'
    }
  }
  ```
* 前端 hot-reload（HMR）由 Vite 處理，後端 hot-reload 由 `cargo-watch` 處理；兩者獨立運行，變更即時反映。

#### 13.9 測試執行

```bash
# 後端單元測試（不需任何外部依賴）
make test-unit        # = cargo test --lib

# 後端整合測試（需 Docker 啟動 PostgreSQL testcontainer）
make test-integration # = cargo test --test '*'

# 前端單元測試
make test-frontend    # = pnpm -C frontend test

# E2E 測試（需完整 dev 堆疊運行）
make test-e2e         # = pnpm -C frontend test:e2e

# 契約驗證（現有 CI gate 的本地等效）
make test-contract    # = pnpm contract:verify

# 全部跑（CI 等效）
make test-all         # = test-unit + test-integration + test-frontend + test-contract
```

* 整合測試使用 `testcontainers-rs` 自動起臨時 PostgreSQL container，與 `docker-compose` 啟動的開發 DB 互不干擾。
* E2E 測試使用 Playwright，對本地完整堆疊（API + frontend + DB + cache）跑核心流程。

---

## Evaluation Criteria（第二階段追加）

在 INITIAL.md 既有評估標準之上，追加以下項目：

* **端到端流程可運行性**：系統需能從「員工登入 → 瀏覽菜單 → 下單 → 商家備餐 → 員工領餐 → 月結扣款」跑通完整 MVP 流程。
* **前端品質**：介面是否易用、響應式是否合理、錯誤處理是否完善、Loading 狀態是否適切。
* **資料持久化正確性**：DB schema 是否與 domain model 一致、migration 是否可重現、query 是否正確且有適當 index。
* **前後端契約一致性**：前端是否使用生成的 TypeScript client 而非手寫 fetch、API 呼叫是否與 OpenAPI 規格一致。
* **基礎設施即程式碼**：所有基礎設施元件（DB、快取、佇列、storage、gateway、secret）是否皆有對應 K8s manifest 或 Helm/operator 配置，可一鍵部署。
* **橫向擴展能力**：是否能透過調整 HPA/KEDA 參數線性擴展至目標流量，無需修改程式碼或 schema。
* **高可用與容錯**：單一 AZ 故障時系統是否持續運行、DB failover 時間是否 < 30 秒、佇列消費者是否具備 at-least-once 保證。
* **安全性**：NetworkPolicy 是否正確隔離流量、Secret 是否由外部 store 同步、TLS 是否全鏈路啟用。

---

## Technical Constraints（補充）

* 前端框架：SvelteKit 2.x（`@sveltejs/adapter-node` 部署）
* CSS 框架：Tailwind CSS 4.x
* 套件管理：PNPM（monorepo workspace，frontend 為獨立 workspace）
* API Client：使用已生成的 `contract/generated/ts-client/` TypeScript client
* 資料庫：PostgreSQL 16+（CloudNativePG operator 或託管服務）
* 連線池：PgBouncer（transaction mode）
* 快取：Valkey cluster（Redis 相容）
* 訊息佇列：NATS JetStream
* Rust DB 層：`sqlx`（compile-time query checking）
* Migration：`sqlx migrate`
* 圖片儲存：S3 相容 object storage（MinIO operator 或雲端 S3/GCS/R2）
* Secret 管理：External Secrets Operator + 外部 secret store（Vault / AWS SM / GCP SM）
* Gateway：Gateway API + Envoy Gateway 或 Cilium Gateway
* 憑證管理：cert-manager
* 事件驅動擴縮：KEDA
* 前端測試：Vitest（unit）+ Playwright（e2e）
* 專案結構：Cargo workspace root + `frontend/` SvelteKit workspace，共用 `contract/` 產物
* Container runtime：containerd（K8s 1.28+）
* 映像檔構建：multi-stage Dockerfile（Rust + Node.js）

---

## Implementation Priority

以下為建議實作順序，依相依關係排列：

1. **本地開發環境**（§13）— Docker Compose、Makefile、`.env.development`、seed data；所有後續開發的基礎
2. **資料庫層與基礎設施**（§5, §12.2-12.3）— PostgreSQL + PgBouncer 部署與 migration
3. **快取與佇列**（§12.4-12.5）— Valkey + NATS 部署，API 層接入
4. **Object Storage**（§12.6）— MinIO 部署，圖片上傳流程
5. **Gateway 與網路安全**（§12.7, §12.9-12.10）— Ingress 路由、NetworkPolicy、Secret 管理
6. **認證整合**（§10）— SSO / Vendor MFA 對接
7. **API 缺口補齊**（§11）— 確保前端所需端點皆存在
8. **前端 — 員工端**（§6）— 最大使用族群、MVP 核心
9. **前端 — 商家端**（§7）— 供應端流程
10. **前端 — 管理端**（§8）— 營運管理
11. **前端部署與 CDN**（§12.8）— SvelteKit Deployment + static asset caching
12. **前端共通需求收尾**（§9）— 響應式、錯誤處理、i18n 基礎
13. **Multi-AZ 與擴展調校**（§12.11-12.12）— 拓撲約束、HPA/KEDA 參數驗證
14. **Kustomize Overlays 與 CI/CD**（§12.13-12.14）— dev/staging/prod 分層、pipeline 擴充
15. **壓力測試與容量驗證**（§12.15）— k6 全鏈路壓測，調校至目標 RPS
