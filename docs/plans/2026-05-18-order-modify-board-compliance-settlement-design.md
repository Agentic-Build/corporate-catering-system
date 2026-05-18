# 設計:修改訂單 / 看板特殊需求 / 文件補件 / 看板即時性 / 月結例外清單

日期:2026-05-18
分支:`feat/order-modify-board-compliance-settlement`

## 背景

`docs/` 的功能稽核發現 INITIAL.md 需求清單仍有缺口。本設計處理其中五項:

| 代號 | 功能 | 對應需求 |
| --- | --- | --- |
| A | 員工修改訂單 | 員工「截單前修改或取消訂單」 |
| B | 訂單看板特殊需求 | 商家訂單看板「特殊需求」 |
| C | 文件補件流程 | 福委會「商家入駐文件生命週期 — 補件」 |
| D | 訂單看板即時性 | 商家「即時看板」 |
| E | 月結例外清單 | 福委會「月結扣款例外處理」 |

所有新 HTTP 端點以 huma 註冊,自動進 OpenAPI;完成後執行 `make contract-sync`,確保 CI `contract-drift` 不被擋。A~E 各自獨立 commit。

---

## A. 員工修改訂單

範圍:截單前可增刪餐點與調整數量;維持同一張訂單 ID 與 TOTP;不可換商家、日期、廠區。

- **資料**:無新表。`order_item` 以「刪舊列、插新列」更新(已有 `ON DELETE CASCADE`,僅 `qty>0` 檢查,無 append-only 觸發器)。
- **後端**:`order.Service.Modify(ctx, ModifyOrderInput{OrderID, UserID, Items, Notes})` — 全量替換語意。
  - 檢查:擁有者、`status=placed`、`now<cutoff_at`、新餐點全部屬於該訂單既有 `vendor_id`。
  - quota 依差額處理:同一 menu_item 新數量 > 舊數量 → `DecrementTx(差額)`;新 < 舊或移除 → `RestoreTx(差額)`;新增品項 → `DecrementTx(新量)`。全程包在單一 `pgx.BeginFunc`,任一步失敗(含 `ErrOutOfStock`)整筆 rollback。
  - 重算 `total_price_minor`、更新 `updated_at`。
  - 寫 `order.modify` audit + `order.modified.v1` outbox。狀態不變,故不寫 `order_state_event`。
  - 新增 repo 方法 `OrderTx.ReplaceItemsTx(tx, orderID, items, totalMinor, notes)`。
- **契約**:`PUT /api/employee/orders/{id}`,body `{items:[{menu_item_id, qty}], notes}`。MCP 新增 `order.modify` tool(與 `order.place`/`order.cancel` 一致)。
- **前端**:員工 `/orders/[id]` 加「編輯」模式 — 既有餐點數量增減 + 從該商家當日菜單加點;送出呼叫 `PUT`。
- **測試**:`order/service_test.go` 補 `Modify` 案例 — 增量扣 quota、減量還 quota、超量 `ErrOutOfStock` rollback、過截單 `ErrCutoffPassed`、非擁有者 `ErrForbidden`、跨商家拒絕。

## B. 訂單看板特殊需求

> 決策:依使用者明確指示,只做**單一自由文字備註欄位**(per-order),不做 INITIAL.md 建議的固定選項。此為刻意偏離。

- **資料**:migration `order` 表加 `notes TEXT NOT NULL DEFAULT ''`。
- **後端**:`PlaceOrderInput` / `ModifyOrderInput` 帶 `Notes string`;`CreateTx` / `ReplaceItemsTx` 寫入。員工訂單 DTO 與商家訂單 DTO 都帶 `notes`。長度上限(如 500 字)於 handler 驗證。
- **前端**:員工結帳/編輯頁加備註輸入框;商家看板與訂單明細顯示 `notes`。

## C. 文件補件流程(商家自助)

- **資料**:migration `vendor_document` 加 `supersedes UUID REFERENCES vendor_document(id)`。
  不新增 enum 值(避免 golang-migrate 對 `ALTER TYPE ... ADD VALUE` 的交易限制);「已被取代」由「是否被他列以 `supersedes` 指向」推導。
- **後端**:新增商家自助上傳 — `compliance.Service.ResupplyDocument(ctx, vendorID, kind, blob, filename, expiresAt, supersedesID)`。
  tx 內:插入新 `vendor_document`(`status=pending`);若 `supersedesID` 非空,驗證該 doc 屬同一 vendor 且狀態為 `rejected`/`expired`,設定 `supersedes`。寫 audit。
  `GET /api/merchant/compliance` 回應每份文件加計算欄位 `needs_resupply`(`rejected` 或 `expired`)。
- **契約**:`POST /api/merchant/documents`(商家端,vendor_id 取自 auth context)。沿用 admin 上傳的檔案處理方式(見實作時確認 `uploadVendorDocument`)。
- **前端**:商家 `/compliance` 頁加上傳/補件表單,被拒/到期文件顯示「補件」入口。
- **測試**:`ResupplyDocument` — 新文件 pending、`supersedes` 連結正確、補非自己 vendor 的文件被拒、補 `approved` 文件被拒。

## D. 訂單看板即時性(SSE)

- **後端**:
  - API 程序新增 NATS 連線(讀 config 既有 NATS URL)。
  - `order` 模組新增 `BoardHub`:啟動時以 ephemeral consumer 訂閱 `ORDERS_V1`(`order.*.v1`),維護 `vendor_id → 訂閱者 channel` 映射,收到事件依 `vendor_id` 路由。
  - 補 `order.cancelled.v1`、`order.no_show.v1` outbox payload 的 `vendor_id`(目前缺,Hub 無法路由);`order.modified.v1` 一開始就帶。
- **契約**:`GET /api/merchant/orders/events` — huma SSE 端點(若 huma 版本不支援 SSE,比照 `/mcp` 掛 raw chi handler,文件化於 docs)。事件 payload 僅 `{kind, order_id}`。
- **前端**:商家看板開 `EventSource`,收到事件即呼叫 `invalidate` 重抓看板。refetch 為真實來源,避免增量同步 bug。
- **測試**:`BoardHub` 單元測試 — 事件依 vendor 路由、訂閱者離線清理。

## E. 月結例外清單(清單 + 解決工作流)

針對薪資代扣 `payroll_batch` / `payroll_entry`(非 `vendor_settlement`)。

- **資料**:新表 `payroll_exception`:
  `id, batch_id(FK→payroll_batch ON DELETE CASCADE), entry_id(FK→payroll_entry), user_id(FK→user), kind TEXT, status TEXT DEFAULT 'open', detail TEXT, resolution TEXT, resolved_by(FK→user), resolved_at, created_at`。
  `kind`:`employee_departed`(自動)、`deduction_failed`(手動)。
  `status`:`open` / `resolved` / `excluded`。
  唯一索引 `(batch_id, entry_id, kind)` 供 idempotent upsert。
- **後端**:
  - 自動偵測:Build 批次後掃描 entry,`user.status ≠ 'active'` → upsert `employee_departed` 例外。
  - 手動:福委會對某 entry 標記 `deduction_failed`。
  - 解決:標記 `resolved`(系統外處理完成)或 `excluded`(排除出 CSV)。
  - `settler.renderCSV` 載入該批次例外:CSV 加 `exception` 欄(填 kind);`excluded` 的 entry 不輸出扣款列。
- **契約**:
  - `GET /api/admin/payroll/batches/{id}/exceptions` — 讀時重掃(idempotent upsert 後回傳全部)。
  - `POST /api/admin/payroll/batches/{id}/exceptions` — 手動新增 `deduction_failed`。
  - `POST /api/admin/payroll/exceptions/{id}/resolve` — body `{status, resolution}`。
- **前端**:福委會 payroll 頁顯示各批次例外清單 + 解決操作。
- **測試**:偵測離職員工、idempotent 重掃不重複、resolve 狀態流轉、`excluded` entry 不進 CSV。

---

## 實作順序

每項先 migration → 後端(TDD)→ 契約 → 前端,各自獨立 commit:

1. A 修改訂單
2. B 訂單備註(與 A 共用 `order` 寫入路徑,緊接其後)
3. C 文件補件
4. D 看板即時性
5. E 月結例外清單
6. `make contract-sync` + 全測試 + 文件更新(CHANGELOG)

## 風險與假設

- huma 版本是否內建 SSE 註冊 — 實作時確認;不支援則 raw handler fallback。
- API 程序目前可能未連 NATS — D 需新增連線,屬合理擴充。
- `user_status` enum 實際值 — 實作時確認,`employee_departed` 偵測以「≠ active」為準。
- B 偏離 INITIAL.md「固定選項」建議,為使用者明確決定。
