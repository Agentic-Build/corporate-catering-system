# 設計:稽核剩餘 11 項缺失

日期:2026-05-18
分支:`feat/remaining-audit-gaps`
前置:A~E(PR #21)已合併。本文件處理稽核報告其餘 11 項。

所有新 HTTP 端點以 huma 註冊 → 自動進 OpenAPI;完成後 `make contract-sync`。每項獨立 commit。

| 代號 | 功能 | 優先 |
| --- | --- | --- |
| G | 菜單複製 | P0 |
| H | 臨時缺貨處理 | P2 |
| I | 商家級截單/開放區間設定 | P0 |
| J | 備餐與配送輸出 | P0 |
| K | 員工扣款明細查詢 | P1 |
| L | 員工日曆/週檢視 | P2 |
| M | 售完即時反應 | P2 |
| N | 廠區時段映射 | P2 |
| O | 異常治理動作 | P2 |
| P | 人可讀 API 文件 | P1 |
| Q | MCP 文件審核 + 異常治理 tools | P1 |

---

## G. 菜單複製

- **後端**:`menu.Service.CopyItem(ctx, vendorID, itemID)` — 載入來源品項,驗證屬該 vendor,建立新 `menu_item`(`status=draft`,名稱加「（複製）」,複製 description/price/tags/badges/category)。
- **契約**:`POST /api/merchant/menu-items/{id}/copy`。
- **前端**:商家菜單頁每列加「複製」按鈕。
- **測試**:複製產生獨立 draft、跨 vendor 拒絕。

## H. 臨時缺貨處理

- **資料**:migration `meal_supply` 加 `sold_out BOOLEAN NOT NULL DEFAULT false`。
- **後端**:`quota.Service.SetSoldOut(itemID, date, soldOut)`;員工菜單的 `sold_out` 改為 `remain<=0 OR meal_supply.sold_out`。
- **契約**:`POST /api/merchant/supply/{itemID}/{date}/sold-out`,body `{sold_out}`。
- **前端**:商家供應量編輯加「缺貨」開關。
- **測試**:標記缺貨後員工菜單顯示售完;DecrementTx 不受影響。

## I. 商家級截單/開放區間設定

- **資料**:migration `vendor` 加 `cutoff_hour INT NOT NULL DEFAULT 17`(當地時)、`preorder_window_days INT NOT NULL DEFAULT 7`。
- **後端**:`order.Service.Place` 截單改查該 vendor 的 `cutoff_hour`,以 `time.Local` 計算前一天該時(同時修正既有 17:00 UTC 寫死的問題);員工菜單端點以 `preorder_window_days` 過濾超出商家開放天數的日期。`vendor.Service` 加 `GetSettings`/`UpdateSettings`。
- **契約**:`GET /api/merchant/settings`、`PUT /api/merchant/settings`。
- **前端**:商家新增「營運設定」頁。
- **測試**:自訂 cutoff_hour 後 Place 的截單時間正確;超出 window 的日期被過濾。

## J. 備餐與配送輸出

- **後端**:`order.Service` 加備餐彙總 — 給定 vendor + date,回傳:廠區分區(plant → 品項彙總)、每張訂單的餐點標籤資料、配送籃清單(plant → 訂單)。品項名稱 join `menu_item`。
- **契約**:`GET /api/merchant/prep-sheet?date=` 回傳結構化 JSON。
- **前端**:商家「備餐輸出」頁 — 列印友善版面(分區表 / 標籤 / 配送籃三段)+ CSV 匯出(配送清單)。
- **測試**:彙總正確分區、份數加總、含訂單備註。

## K. 員工扣款明細查詢

- **後端**:`payroll.EntryRepository.ListByUser(userID)`(新方法,join batch 取期間與狀態);`payroll.Service.ListMyEntries(userID)`。
- **契約**:`GET /api/employee/payroll` — 回傳該員工各批次的代扣明細(期間、金額、退款、淨額、批次狀態)。
- **前端**:員工新增 `/payroll` 頁。
- **測試**:只回傳該員工的 entry;含退款後淨額正確。

## L. 員工日曆/週檢視

- 純前端。員工首頁日期選擇由水平帶強化為**週檢視格狀版面**(7 日卡片,標示日期、星期、是否有既有訂單)。沿用既有 `day` 參數導航。

## M. 售完即時反應(輕量)

- **後端**:`order` 模組加 `MenuHub`(廣播式,無 key);`RunBoardConsumer` 收到任一訂單事件時,除 `BoardHub.Publish` 外再 `MenuHub.Broadcast()`。
- **契約**:`GET /api/employee/menu/events` huma SSE — 訂單活動時送輕量 ping。
- **前端**:員工菜單頁開 `EventSource`,收到 ping 即 `invalidate` 重抓菜單 → 售完即時反映。不做持久化通知。
- **測試**:`MenuHub` 廣播 / 訂閱單元測試。

## N. 廠區時段映射

- **資料**:migration `vendor_plant_mapping` 加 `service_window TEXT NOT NULL DEFAULT ''`(如「11:30-13:00」)。
- **後端**:`vendor.Service` 新增 `SetPlantWindow(vendorID, plant, window)`;`PlantMappingRepository` 對應方法。
- **契約**:`PUT /api/admin/vendors/{id}/plants/{plant}/window`,body `{service_window}`。
- **前端**:福委會商家詳情頁可編輯各廠區時段。
- **測試**:設定時段後讀回正確。

## O. 異常治理動作

- **後端**:`compliance.Service.TriageAnomaly` 擴充 — 選用 `action`(`""`/`warn`/`suspend`)。`suspend` 對 anomaly 的目標 vendor 呼叫 vendor 停權;`warn` 寫 `vendor.warning` audit。Service 需注入 vendor 停權能力(`VendorSuspender` 介面)。
- **契約**:`POST /api/admin/anomalies/{id}/triage` body 加 `action`、`action_notes`。
- **前端**:福委會異常頁 triage 時可選治理動作。
- **測試**:triage+suspend 使 vendor 停權並留痕;triage+warn 寫 audit。

## P. 人可讀 API 文件

- 確認 huma `DefaultConfig` 內建的 `/docs`(Stoplight Elements)與 `/openapi.json` 是否已可用;若被停用則掛回。README 補上文件入口連結。
- 多為驗證 + 文件;若需掛載則以 chi handler 提供。

## Q. MCP 文件審核 + 異常治理 tools

- **後端**:`mcpserver` 新增 5 個 tool — `document.list`、`document.review`、`anomaly.list`、`anomaly.triage`、`anomaly.close`,委派既有 `compliance.Service`,權限同 HTTP(welfare_admin),`auditAfter` 留痕。`anomaly.triage` 支援 O 的治理動作。
- **測試**:`tools_test.go` 預期清單更新為 21 個 tool。

---

## 實作順序

商家面(G、H、I、J)→ 員工面(K、L、M)→ 福委會面(N、O)→ 平台(P、Q)→ `make contract-sync` + 全測試 + CHANGELOG。每項先 migration → 後端(TDD)→ 契約 → 前端,獨立 commit。

## 風險與假設

- I 的 `cutoff_hour` 以 `time.Local` 計算 — 與既有 `HomeService.ServerTZ: time.Local` 一致。
- M 採廣播式 ping;訂單量不大,全域重抓成本可接受。
- P 取決於 huma 預設是否已掛 `/docs` — 實作時確認。
- O 的 `VendorSuspender` 介面避免 compliance → vendors 的循環相依。
