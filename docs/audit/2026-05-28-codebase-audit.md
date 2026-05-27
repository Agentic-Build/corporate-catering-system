# 全 Codebase 品質稽核報告

- 日期：2026-05-28
- 範圍：後端 Go（services/api，~61K LOC）、前端（apps + packages，~24K LOC）、文件（68 份 .md）、Ops/設定/Migrations
- 方法：5 個平行深掃描 agent ＋ 客觀工具（`go vet`、`go build`、`staticcheck`、`deadcode`、`govulncheck`、`gofmt`、`svelte-check`、自帶 SQL guard）
- 基準 commit：`55f0a66`（main）

標記說明：`[已驗證]` = 報告者已親自讀碼/grep 確認；其餘為 agent 回報、實作前需再確認。

---

## 0. 客觀工具基線（健康）

| 檢查 | 結果 |
|---|---|
| `go vet ./...` | ✅ clean |
| `go build ./...` | ✅ clean |
| `govulncheck` | ✅ 無相依套件漏洞 |
| 自帶 SQL injection guard | ✅ pass（SQL 全參數化、排序白名單化）|
| `_minor` 金額換算 grep | ✅ 後端 SQL/HTTP 無 /100 *100（但 MCP 層有，見 P0-1/P0-2）|
| TODO/FIXME/HACK 全庫 | ✅ 0 個 |
| `svelte-check`（全 app/package）| ✅ 0 errors（12 個 reactivity 警告，見 FE）|
| `staticcheck` | 7 項（見 P2）|
| `gofmt -l services/` | ⚠️ 19 檔未格式化 |
| `deadcode` | 12 個 unreachable 函式候選（見死碼節）|

整體工程品質高於業界平均：分層乾淨、交易邊界一致、quota 用原子條件 UPDATE 防超賣、狀態機用 `WHERE status=from` 條件更新、authz/IDOR 在 HTTP 與 MCP 雙路徑一致把關、OIDC 有 PKCE/nonce/state/safeReturnTo。問題集中在「**邊緣與接線**」：MCP/前端的金額單位、HTTP edge 複製貼上、composition root god-function、chart 與 code 的接線斷點、文件時效。

---

## P0 — 正確性 / 安全 bug（建議優先修）

### 後端
1. **[已驗證] MCP `search_menu` 價格篩選 `*100` 錯誤** — `services/api/internal/mcpserver/tools_menu.go:159,163`。把使用者輸入的元數乘 100 當 minor，但 `_minor` 存的是整數元；HTTP 同功能（`menu/http/handlers.go:471-475`）直接帶值不乘。後果：搜「50~200 元」變成比對 5000~20000，幾乎永遠空集合。修法：移除 `*100`。
2. **[已驗證] MCP ChatGPT 顯示價格 `/100` 錯誤** — `tools_chatgpt.go:272,278`（`it.PriceMinor/100`）。同檔 `:198` metadata 卻用原值，自相矛盾；NT$120 對外顯示成 NT$1。修法：去掉 `/100`。
3. **Outbox relay 交易順序 bug → 重複投遞** — `internal/order/postgres/outbox_repo.go:111-121` + `internal/order/relay/relay.go:65,74`。批次中途某事件發佈失敗時 `MarkFailed` 會 `Commit` 整個 tx；迴圈結束後對「已關閉的 tx」呼叫 `MarkPublished` 必然報錯，已送進 NATS 但 `published_at` 仍 NULL 的事件下個 cycle 會被重發。修法：成功/失敗標記放同一交易、迴圈後一次 commit。

### 前端
4. **時區 off-by-one（UTC vs 本地）** — `apps/employee/src/routes/+layout.svelte:19`、`menu/favorites/+page.server.ts`、`prep-sheet/+page.server.ts`、`merchant/orders|labels/+page.server.ts` 用 `new Date().toISOString().slice(0,10)`（UTC 日期）。台灣 00:00–08:00 會算成昨天，且與 merchant 首頁 `dayId()`（本地）不一致。修法：抽共用 `todayLocalISO()`，全前端統一。
5. **merchant `cutoff_at` 時區＋語意錯** — `apps/merchant/src/routes/+page.svelte:180` 與 `+page.server.ts:97` 送 `${date}T17:00:00Z`：(a) `Z`→台灣變隔天 01:00；(b) UI 各處說截單是「取餐日**前一日** 17:00」但這裡送當天。與後端/顯示語意不符。（與後端 P1「reorder cutoff_at 寫死」同一主題，建議一起釐清 cutoff 語意。）
6. **merchant 加菜 race** — `apps/merchant/src/routes/+page.svelte:85-91` `addFromLibrary` 同時送 publish 與 setSupply 兩個隱藏表單，setSupply 可能在 publish 完成前到後端（品項仍 archived）→ 偶發失敗。修法：publish 成功後再觸發 setSupply。

### Ops / 安全
7. **[已驗證] OIDC client_secret secret 接線不一致 → login 漂移/502** — `chart/tbite-platform/templates/_helpers.tpl:349-353` 從 Secret `tbite-oidc-clients` key `apiClientSecret` 取值，但唯一範本 `ops/secrets/example.sops.yaml` 把它放在 `tbite-app-secrets/AUTH_PROVIDER_AUTHENTIK_CLIENT_SECRET`，且**沒有定義 `tbite-oidc-clients`**。正是反覆出現的 login 502 根因。修法：對齊 secret 名/key。
8. **[已驗證] CD 直推無保護的 main、無 concurrency → 弄丟 merged PR** — `.github/workflows/cd-publish-images.yml:88-113` `bump-image-tags` 直接 `git push origin HEAD:main`，全 repo 無 `concurrency:` block。多 PR 同時 merge 時競態（已發生：#85 消失、#92/#93/#94 reject）。修法：加 `concurrency`、push 前 `git pull --rebase`，或改走 PR。

---

## P1 — 潛在 bug / 高風險

### 後端正確性
- **NATS 發佈無 msg-id 去重** — `internal/platform/messaging/nats.go:81` 用 `JS.Publish` 但不設 `Nats-Msg-Id`，JetStream 內建去重失效；配合 relay 重發與 DLQ replay，消費端會重複計數。修法：以 outbox 穩定 id 當 `jetstream.WithMsgID`。
- **reorder `cutoff_at` 寫死 17:00 UTC** — `internal/order/reorder_service.go:240`，與 `Service.Place`（用 vendor.CutoffHour + 時區）不一致；此欄位會 gate Modify，導致 reorder 的訂單可修改截止偏離 vendor 設定。
- **reorder 缺 vendor-plant 驗證** — `reorder_service.go:144-251` 只驗 UserID，未驗 vendor 是否仍服務該 plant（`Place` 有 `Plants.ListByVendor`）→ 換廠區員工可對不服務其新廠區的 vendor 下單。
- **payroll 當期爭議未檢查訂單狀態** — `internal/payroll/service.go:233-265` `OpenDisputeByOrder` 的 entry-less 分支未擋不可爭議狀態（如 cancelled）；resolve 退款只在 picked_up/no_show 生效，對 cancelled 開的爭議會記錄 refund 卻不退款。
- **DLQ replay TOCTOU 雙重發佈** — `internal/dlq/http/handlers.go:155-167` 先 Publish 後 MarkReplayed 且防重非原子；並發 replay 會雙發。修法：條件式 `UPDATE ... WHERE replayed_at IS NULL` 取勝後再 Publish。
- **menu `ListForEmployee` N+1** — `internal/menu/service.go:206` rows 迴圈逐筆 `Images.ListByItem`；他處讀路徑都用 `ANY($1)` 批次。改批次載入。
- **compliance on-time evaluator 無 idempotency** — `internal/compliance/evaluator/ontime_rate.go:122-170` at-least-once 但重投會重複加進 rolling window，污染 on-time rate。

### Ops 接線斷點（程式做了、chart 沒接 → 悄悄沒生效）
- **[已驗證] `NATS_STREAM_REPLICAS` chart 從不設** — code 讀（`config.go:155`，#92 已實作），但 chart（含 values-prod-ha）從未設定 → JetStream 串流永遠 replica=1，HA NATS 形同虛設。
- **prod-ha `tbite.affinity` 是死 key** — `values-prod-ha.yaml:25-36`，無 template 讀 `.Values.tbite.*`（排程只看 `.Values.global.affinity`）→ 宣稱的 anti-affinity 沒生效。
- **[已驗證] DB pool 飽和告警查不存在的 metric** — `templates/vmalert-rules.yaml` 查 `tbite_db_pool_*`，但 code 從不 emit → 最核心的容量告警永不觸發。
- **6 個 dashboard 查未註冊的 business metric** — `catering_order_place_duration_*` / `_placed_count_total` / `_pickup_verified_*` / `_ready_*` 僅在 stress 工具註解出現，API/worker 沒註冊 → 訂單/取餐/備餐面板永久空白（已知 instrumentation gap）。
- **NetworkPolicy 對 MinIO egress label/port 不符** — `networkpolicy-allow-egress.yaml:64-71` 只放行 operator tenant label + port 9000，但預設用 `minioStandalone`（label 不同、Service 80→9000）→ 啟用 NetworkPolicy 時圖片/簽章上傳可能被擋。

### 前端
- **SSE proxy 上游連線洩漏** — `apps/employee/src/routes/menu/events/+server.ts:10`、`merchant/src/routes/orders/events/+server.ts:13` 上游 `fetch` 沒帶 `signal: event.request.signal` → 斷線重連留下未關閉的上游連線。
- **47 處把 problem-details JSON 原文噴給使用者** — 各 app `+page.server.ts` action `fail(…, { error: JSON.stringify(r.error) })`；`employee/+page.server.ts:196-203` 才是正確示範（取 detail）。抽 `problemMessage()`。
- **三個 app 都沒有 `+error.svelte`** — load throw/502/403 顯示 SvelteKit 預設白頁。
- **型別安全大規模放棄** — admin/merchant `+page.server.ts` 50+ 處 `as any` + 12 處 `body … as never`，但 `schema.d.ts`（6046 行）有完整契約型別。後端契約漂移無法在編譯期攔截（金額/欄位 bug 溫床）。
- **app tsconfig 未繼承 base** — 缺 `noUncheckedIndexedAccess`（base 有開）→ 索引存取潛在 undefined 不被攔。
- **`setTimeout` 無 cleanup** — `employee/+page.svelte:205-208` showToast、`+layout.svelte:53-60` bump 連點堆疊計時器。
- **購物車無單一商家限制**；**員工端 SSE 無 onerror/fallback**（merchant 有 60s fallback poll）。
- **Drawer 永遠掛載** — `packages/ui/src/Drawer.svelte:50-78` 只用 `pointer-events-none` 隱藏，關閉時 `role="dialog" aria-modal` 仍在 DOM 與 tab order（Modal 用 `{#if open}` 是對的）。

### 品質 / 架構
- **`cmd/tbite/main.go` 915 行 god-function** — `RoleAPI`(:183) 與 `RoleMCPStdio`(:690) 各自 inline 重建整個 service graph（payroll/order/repos 重複 2-3 次），且已開始漂移（mcp-stdio 漏 Exceptions、Storage nil）。抽 `buildServices()`。
- **`mapErr` 在 11 個 context 各自重寫** — order/payroll/menu/vendors/feedback/settlement/plants/compliance/quota/dlq/identity，形狀相同但規則不一致（order 記 log 其他不記、feedback 用 422、vendors 手刻 502）。抽 rule-table helper。
- **`require*` 跨 context ~22 個複製** — `requireEmployee`/`requireVendor`/`requireAdmin` body 逐位元組相同，只依賴 `idhttp.UserFromContext`。移進 `idhttp`。

---

## P2 — 品質 / 整潔 / 死碼

### 死碼（deadcode 工具 + agent 雙重確認，可安全移除）
- `internal/platform/idgen/`（整個 package：`Generator`/`DefaultGen`/`NewUUID`/`NewToken`）— 零 production importer。
- `internal/platform/messaging/dlq.go:24 dlqMessages` + `:47 WriteDLQ` — **整個 DLQ 寫入端是孤兒**（無 worker 呼叫），admin read surface 管理一張沒人寫的表；註解卻說「workers write via messaging.WriteDLQ」。需決策：接上 NATS max-deliver/terminate 路徑，或移除寫入端＋修正註解。
- `internal/payroll/postgres/current_lines_repo.go`（`NewCurrentLinesRepo`/`ListCurrentLines` + `CurrentLinesRepository` 介面）— `Service.CurrentLines` 欄位從未被 wiring 設值，永遠 fallthrough；純為測試間接層（過度抽象）。
- `internal/compliance/service.go:233 OpenAnomaly`（unreachable，且本應是 anomaly 建立的唯一路徑卻被 worker 繞過 → metric 不一致）、`:310 GetAnomaly`（僅 test 用）。
- `internal/feedback/service.go:317 GetComplaint`（僅 test 用）。
- `internal/quota/service.go:113 GetForItem`（僅 test 用）。
- 前端 `packages/ui/src/RemainBar.svelte` — 從 `@tbite/ui` export 但全庫零 import。
- **保留**（deadcode 誤判的 test seam）：`httpserver/server.go:224 Server.Handler`、`platform/clock/clock.go:15 FixedClock.Now`。

### staticcheck（7 項）
- `cmd/tbite/roles.go:474`、`internal/httpserver/server.go:76` — `middleware.RealIP` 已棄用（IP spoofing 風險，SA1019）。
- `internal/identity/hydra/discovery.go:31-32` — `rp.Director` 自 Go 1.26 棄用，改用 `Rewrite`。
- 3 項 test 命名/struct 慣例（ST1012、S1016）。

### 格式 / CI 把關落差
- **gofmt 19 檔未格式化**（CI 沒跑 gofmt 檢查）。
- CI `ci-lint-test.yml:28` `pnpm -r lint || true`（prettier 非阻斷）；`ci-e2e-smoke.yml` 整支 `if: false`（e2e 停用）。

### 可抽共用 / 重複（前端）
- auth 路由樹三倍複製：`apps/{admin,employee,merchant}/src/routes/auth/{start,landing,logout}/+server.ts`（差 1-2 行）；`login/+page.server.ts` 三檔逐位元組相同 → 抽進 `@tbite/web-auth`。
- `lib/server/env.ts` ×3 相同；`apiFor(token)` admin/merchant 相同、employee 沒有。
- 金額格式化各自定義（admin/vendor-settlements/payroll inline、merchant 有 `lib/money.ts`、employee inline）→ 升 `formatMinor` 到共用 package。
- order/complaint 狀態 label/tone 對照表散在 ~10 檔 → 集中或做 `<StatusTag>`。
- buildDays/7 日視窗邏輯三份。

### 可抽共用 / 重複（後端）
- payroll `http/handlers.go`(716) → 拆 batch/dispute/exception/employee 四檔（menu/http 已有多檔先例）。
- order `http/handlers.go:588 & :623` 兩個近乎相同的 SSE loop → 泛型 `streamSSE[T]`。
- `*time.Time→*string` RFC3339 idiom ×28（8 個 handler 檔）→ 共用 `formatTimePtr`。
- menu/http 同概念三種命名（`requireEmployee`/`requireEmployeeUser`/`requireEmployeePlant`）。
- order.Service 直接依賴 menu/vendor 具體 repo（其餘 dep 都是 local port）→ 定義 order-owned `ItemReader`/`PlantReader`。
- `httpserver/server.go:79-95` 自述「Temporary」的存取 log middleware（記錄 Authorization 是否存在）→ 驗證已完成，移除或加 debug flag。

### Ops 整潔 / 死設定
- `ops/kubernetes/monitoring/`（整棵：13 舊 dashboard + alertmanager.yml + vmalert-rules + grafana-provisioning）— 與 chart 提供的 19 dashboard 分岔，無任何引用 → 刪除或歸檔。
- `ops/observability/{slo,load/reports,load/prelaunch-thresholds.yaml,load/staged-capacity-policy.json}`、`ops/state/audit-trail.json` — 無引用的一次性產物。
- `_helpers.tpl` 的 `DATABASE_MAX_CONNS`/`DATABASE_MAX_CONNS_RO` — code 不讀（讀的是 `DB_MAX_CONNS*`）→ 死 env，每個 deployment 多塞。
- `.sops.yaml:36,47` placeholder age key（忘了換會產出無法解密的檔）；建議 pre-commit 擋 placeholder。
- `ops/secrets/README.md:79-81`、`ops/argocd/application-helm.yaml:13-15`、`tbite-nycu.yaml:11-13` 註解/路徑過時（指 kustomize/不存在路徑）。
- `values.schema.json` 無 `additionalProperties: false` → `tbite.affinity` 這種死 key 不被擋。
- 根目錄 `tbite`（92MB 二進位）已 gitignore、未追蹤，屬本機殘留（Makefile build 其實輸出 `/tmp/tbite`），可刪。
- **設定三方落差**：`HYDRA_PUBLIC_URL`/`HYDRA_ADMIN_URL`、`MCP_BEARER_TOKEN`、`RECOMMENDATION_ALPHA`、各排程間隔、`DB_MIN_CONNS*` — code 讀但 chart/.env.example 未列。

---

## 文件

### 需更新（與 code 不符）
- **`docs/mcp.md`** — 文件化一個**不存在**的工具 `order.get_pickup_code`；宣稱「27 tools」實際 26；`mcpserver/server.go:61` 註解「21 tools」也舊。會誤導 AI agent/接入者。
- **`README.md:138`** — 「12 roles」實際 11（`config/config.go` 只 11 個 role 常數）；倉庫布局漏列 `packages/pickup`。
- **`CHANGELOG.md`** — 停在 2026-05-18，漏 #66~#95（含 #72 plant/object storage、#85 商家改版、#87 覆蓋率、#88 移除 docker-compose、#92-95），已失參考價值。
- **`packages/api-client/README.md`** — 仍是「Placeholder … in P1」占位文，實際早已生成。
- **`docs/plans/2026-05-27-test-coverage-90-progress.md`** — 「未 commit 交接」狀態已失效（#87 已 merge）。

### 可歸檔
- `docs/plans/`（25 份已完成計畫，部分引用已移除的 `make dev-up/dev-app/dev-down`、kustomize）→ 移 `docs/plans/archive/` 或標註「歷史」。
- `docs/ux-redesign.md` — 前提是重構不存在的 `*-portal-mvp.svelte`（早完成）→ 標註「重構已完成」。

### 缺漏
- 輕量本地開發 onboarding（docker-compose 已於 #88 移除，README 只剩起整個 chart 一條路）。
- `packages/{pickup,ui}` 無 README。

---

## 建議分批實作計畫（Waves）

| Wave | 內容 | 風險 | 價值 |
|---|---|---|---|
| **W1 P0 程式 bug** | 兩個金額 bug、relay 交易、前端時區 util/cutoff_at/SSE signal、加回歸測試 | 低 | 高（功能正確性）|
| **W2 死碼＋格式** | 移除 idgen/DLQ寫入端/current_lines_repo/4 個 test-only 方法/RemainBar；gofmt 19 檔；staticcheck（RealIP/Director 棄用）| 低 | 中高（可讀性）|
| **W3 文件** | mcp.md 幽靈工具、README 11 roles/pickup、CHANGELOG 補錄、歸檔 plans、加 onboarding | 低 | 中 |
| **W4 Ops 接線** | OIDC secret 對齊、CD concurrency、NATS_STREAM_REPLICAS、prod-ha affinity、db pool/business metric、networkpolicy、清孤兒 ops | 中（線上）| 高（HA/事故）|
| **W5 前端品質** | 拔 as any 改 schema 型別＋tsconfig base、problemMessage＋各 app +error.svelte、抽 auth/money/狀態表共用、Drawer a11y | 中 | 中高 |
| **W6 後端結構重構** | buildServices、errmap rule-table、require* 移 idhttp、拆 payroll/order handlers、SSE 泛型、formatTimePtr | 中 | 中高（可維護性）|
| **W7 其餘 P1 bug** | NATS msg-id 去重、reorder vendor-plant/ cutoff、dispute 狀態檢查、DLQ TOCTOU、menu N+1、ontime idempotency | 中 | 中高 |
| **W8 CI 加固** | gofmt 檢查、prettier 阻斷、恢復 e2e、SQL guard 已有 | 低 | 中 |
