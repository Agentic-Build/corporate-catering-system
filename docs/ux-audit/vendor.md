# UX Audit — 商家端（15 routes）

> Audit agent 原始報告。基準：UberEats Manager / DoorDash Merchant Portal / foodpanda Partner / 美團商家版 / Toast POS / Square for Restaurants / Deliveroo Hub / 口碑後台 / Gojek Merchant。

**整體前提**：實際畫面寬度 1280px+，side-nav 佔 256px，右側剩約 1000px 內容區。Backend 當前 mock-failure，所有頁面頂部出現紅色「後端上游服務暫時不可用」（評估排除此干擾）。

---

## 1. `/vendor` 今日儀表板

**Components**：`PageHeader` + 3 個 action buttons、3 張 KPI `Card`（今日備餐 / 待上車 / 已送達）、兩張 `Card`（即將截單 / 今日配送狀態分布）、內嵌 `StateTag`。

**合理性**：⚠️。結構合理但空洞。3 張 KPI 卡寬度固定 `md:grid-cols-3`，但每張只有一個 3xl 數字 + 一行說明 — 三張卡佔 900px 寬只承載 3 個數字，**資訊密度極低**。

**第一直覺**：部分通過。「今日要備幾份」一眼可見，但「我現在該做什麼」不明確。3 顆 action button（進入看板 / 建立批次 / 更新菜單）排在 header 右上，和下方的截單卡並沒有視覺關聯。「目前政策：預購開放 N 天｜前日截單分鐘 N」以 `text-xs` 放在截單卡底部，幾乎看不見。

**資訊層級**：Action buttons 跟 KPI 是兩件事卻被 `PageHeader` 吞掉；真正的操作提示（剩多少份待備、哪個廠區有風險）被埋在第二列 `Card` 裡。分鐘數 900–1200 直接以 raw integer 顯示（例如「前日截單分鐘 1080」），商家必須自己換算成 18:00，違反 Toast / Square 的 human-readable time 慣例。

**Benchmark**：
- **UberEats Manager Home** 第一屏是 sales / orders / issues trend + top issues list；**DoorDash Merchant Portal** 左側大 chart 右側 task list（cancelled orders、menu down、reviews needing reply）。兩者首頁都暴露「有什麼事情需要我處理」，本頁只給 counts。
- **Deliveroo Hub / Toast Home** 以顏色和倒數時間強調「距離截單還有 2h 15m」；本頁只顯示 `modifyCancelCutoffMinuteOfDay = 1080`，無 countdown，雖然專案有 `countdown-badge.svelte` primitive 但未使用。
- **foodpanda partner portal** 首頁把待處理訂單直接以卡片 / list 呈現可點擊進入；本頁「即將截單」僅列出 plantId + 剩餘份數，無法點擊進入該廠區看板。

**建議**：
1. `upcomingByPlant` 每列轉成可點擊 link → `/vendor/today/[plantId]`，並加倒數 badge（用 `countdown-badge` primitive）。
2. 前日截單分鐘改成 `formatMinuteOfDay(1080) = "18:00"`，補 helper `minuteOfDayLabel`。
3. 3 張 KPI 卡加 delta / 對比（vs 昨天 / 本週平均），或併成單一 wide summary bar，把釋出的空間給「今日任務清單」（需審核取消、逾時未備、投訴等），對齊 UberEats Manager task queue。
4. 「今日配送狀態分布」目前只是 tag list；改成水平 stacked progress bar（PENDING → PREPARING → PACKED → OUT → DELIVERED）。
5. header 3 顆 action 其中 2 顆其實是 navigation，實質 CTA 只有「進入今日看板」。砍成 1 顆 primary + 「更多操作」dropdown。

---

## 2. `/vendor/today` 今日看板

**Components**：`PageHeader`、共用 `FulfillmentBoard`。Board 內：篩選 `Card`（日期 / 廠區 / 稽核 checkbox / 套用），3 張總覽 `Card`，「廠區彙總」`table`，「訂單詳情」`table`（含 inline `<select>` + submit button），選擇性「配送狀態稽核軌跡」`table`。

**合理性**：❌。這是最該做 Kanban 的頁面，卻是 4 張長型 table 疊疊樂。

**第一直覺**：不通過。看板日常是「把訂單從 PREPARING 往 PACKED 推」，但 UI 是：在表格第 6 欄下拉選單選下一個狀態 → 按「送出狀態更新」。每單 2 個 click + 1 次視覺掃描。

**資訊層級**：「配送狀態稽核軌跡」(audit trail) 預設 `includeAudit = true`，跟現場備餐人員無關，佔據畫面下半。廠區彙總和訂單詳情兩張表並列重複顯示 plantId / status counts。訂單詳情表格 6 欄，`餐點項目` 用 `<ul>` 換行擠壓列高。

**Benchmark**：
- **Toast KDS / Deliveroo Rider Hub / UberEats Merchant Order tab** 都是 column-based Kanban：每個配送狀態一欄，訂單以卡片呈現，單點或拖拉即可往右推進。本頁完全沒有這個結構。
- **美團商家版 / Gojek Merchant** 的訂單列表至少提供「一鍵接單 / 一鍵完成」單欄按鈕。本頁的 `nextDeliveryStatus` helper 已計算好下一狀態，卻仍暴露一個 6-option `<select>`，**明顯過度 configurable**。
- **DoorDash Order Manager** 會把「本單何時需送達、已延遲多久」紅黃綠化；此處完全沒有時間維度。
- 「顯示配送狀態稽核軌跡」是工程師視角，應該在 admin audit 頁面，不是備餐人員現場看板。

**建議**：
1. Primary 視圖 table → **kanban board**：5–6 欄 / 狀態，每張卡片顯示 `orderId 後四碼 + 廠區 + 份數 + 特殊需求 chips`，單顆「推進到下一狀態」按鈕直接套 `nextDeliveryStatus(status)`。
2. Table 作為 secondary tab（「列表模式」）保留給需排序 / export 的 ops user。
3. `includeAudit` 從 checkbox + 同頁顯示 → 改「查看稽核軌跡」link 進獨立 drawer / 子頁。
4. 廠區彙總卡改成頁首的 `plant selector` 水平 chip，對齊 Deliveroo Hub 多分店風格。
5. 特殊需求在訂單卡加紅邊或 emoji（對齊 UberEats `dietary warning pill`）。

---

## 3. `/vendor/today/[plantId]` 指定廠區看板

**Components**：完全 reuse `FulfillmentBoard`，差別在 `fixedPlantId` 鎖定、廠區 input readonly。

**合理性**：⚠️。Route 本身正確（可書籤），但內容層完全一樣、無法真正「Zoom in」。

**第一直覺**：跟 `/vendor/today` 幾乎看不出差異，除了 breadcrumb 多一節 + 廠區輸入框灰掉。

**資訊層級**：跟 #2 共享 pain points。廠區彙總 table 在 fixed-plant 視圖下只有一列，浪費空間。

**Benchmark**：多店商家後台（UberEats Manager Store switcher、foodpanda branch view）進入單店時通常會收掉 store-level 彙總、改顯示「這家店的時段分布 / top items / 員工」。此處只是把廠區欄位鎖死。

**建議**：
1. `fixedPlantId === true` 時**隱藏**廠區彙總 `Card` 與廠區 input，改放「回到總覽看板」breadcrumb + 該廠區專屬 summary（配送址、聯絡人、時段分布）。
2. 標題區 `${title} · ${plantId}` 改顯示 plant human name（目前只有 `fab-a` raw string）。
3. 考慮直接**廢掉 `/vendor/today`**，用 `/vendor/today?plant=fab-a` query param + plant chip selector 實現切換，避免雙 route 語意重疊（DoorDash 多店 portal 做法）。

---

## 4. `/vendor/menu` 菜單列表

**Components**：`PageHeader` + 「新增菜單」primary button、篩選 `Card`（起 / 迄日 / 狀態 / 套用）、`DataTable`（7 欄）。

**合理性**：⚠️。結構標準、但以菜單管理來說「以 date 為 primary axis」奇特。

**第一直覺**：勉強通過。「菜單列表」清楚，但「為什麼預設看今天到 14 天後」不直覺。一般菜單 CMS 預設是「全部上架中 + 按類別分」。

**資訊層級**：
- 7 欄表格 OK，但**沒有圖片縮圖**。菜單表單有 `imageUrl` 欄位、`MENU_IMAGE` artifact class 也存在，列表卻沒顯示。這是商家後台最被高頻掃描的欄位。
- menuItemId (`menu-mo5i5z0mdnnmxdjz`) 在最左且 mono font 強調 — 工程 ID、不是業務資訊。
- 「剩餘」vs「上限」並列但沒有進度條；35/50 只能靠心算判斷「快賣完」。
- 「狀態」`StateTag` 出現在第 7 欄，應該更前面。

**Benchmark**：
- **UberEats Menu Manager / DoorDash Menu** 列表：左 thumbnail + 名稱 + 類別 + 售價，狀態 toggle 直接在列上一鍵切換。
- **美團商家版菜品管理** 提供「分類 tab」並可拖曳排序。本頁無分類，`menuType` 只是欄位不是導航軸。
- **Square for Restaurants** 預設展示 all active items；date range 是 secondary filter。本頁把 date 當主 filter 是把可供應日期模型直接暴露給 UI。

**建議**：
1. 列表加 **thumbnail 欄**（40x40，缺圖放 placeholder）。
2. 預設 filter 改成「狀態＝LISTED」而非「date range」；date 做次要 filter / tab（「全部 / 今天 / 本週」chip）。
3. 狀態欄改 inline `Switch`，直接 call `updateVendorMenuItemStatus` 不用進詳情頁。
4. menuItemId 藏在 row hover「複製 ID」icon button。
5. 「剩餘 / 上限」合併成 `23 / 50` + 細 progress bar，&lt;20% 變紅。

---

## 5. `/vendor/menu/new` 新增菜單（使用 `menu-form.svelte`）

**Components**：`PageHeader`、`MenuForm`（單一 `Card` 內 12 個 `FormField`，2-col grid）。

**合理性**：⚠️。表單流程運作、資料完整，但**把工程邏輯直接暴露給商家**。

**第一直覺**：不通過。第一個欄位是 `menuItemId` 預填 `menu-mo5i5z0mdnnmxdjz` — 商家根本不應該看到。接著「金額（minor 單位）hint：例如 TWD 120 元 = 12000」— 要求商家輸入 12000 代表 120 元違反所有商家 POS / menu builder 慣例。

**資訊層級**：
- `menuItemId` 是 required + 顯眼第一欄；應該 auto-generate 且不可見。
- `preorderOpenDaysAheadOverride (1–7)` / `modifyCancelCutoffMinuteOfDayOverride (900–1200)` 兩個高階 override 欄位和「名稱」「價格」平級放同張卡，對 95% 用例是 noise。
- 沒有圖片上傳 UI — 只有「圖片 URL」文字框。商家要先去 `/vendor/compliance/upload` 建立 upload plan、拿到 objectRef、貼回這裡。**跨頁切換、無引導**。
- 健康標籤是 raw enum（`LOW_CALORIE`、`VEGAN`），沒有中文翻譯。
- 「餐點類型」dropdown 顯示 raw enum 值（`BENTO` / `BOWL`）而非「便當 / 丼飯」。

**Benchmark**：
- **UberEats Menu Manager Item Editor**：image dropzone 佔頁首，名稱 / 描述 / 價格 groupings 分 section；進階設定（availability override、modifier groups）在 accordion 下。本頁 12 欄扁平平鋪無 section。
- **美團 / 口碑菜品新增**：分「基本信息 / 規格價格 / 售賣時間」三大塊，單位全用「元」而非 minor。
- **Square item editor** 金額用 `$12.00` input + auto-format。

**建議**：
1. 移除 `menuItemId` 欄位，完全 auto-gen。
2. 金額改 TWD 元顯示（`120` → submit 時乘 100）；helper 已有 `MoneyAmount` 元件可 reuse 邏輯。
3. 圖片改 `<input type="file">` dropzone，內部 flow：自動 call `createVendorObjectStorageUploadPlan` → PUT → 寫回 imageUrl。
4. `menuType` / `healthTags` 走 i18n label，對齊既有 `menuStatusLabel` 模式。
5. 「preorderOpenDaysAheadOverride / modifyCancelCutoffMinuteOfDayOverride」收進 `<details>`「進階訂購設定（可選）」；minute-of-day 改 `<input type="time">`。

---

## 6. `/vendor/menu/[menuItemId]` 編輯菜單

**Components**：`PageHeader` + 「返回列表」、狀態切換 `Card`（3 顆 button：LISTED / PAUSED / DELISTED）、`MenuForm`（`lockMenuItemId=true`）。

**合理性**：⚠️。Status action 拆到頂端、表單 reuse 是好做法；但背後的 `loadItem` 用 `listVendorMenuItems(today - 30 to today + 90)` 撈 500 筆再 `find()`，一旦該菜單的 deliveryDate 超過 90 天就會「找不到」— 隱藏的可用性陷阱。

**第一直覺**：通過。「這是編輯頁、左上三顆切換狀態」清楚。

**資訊層級**：
- 3 顆狀態 button 同時顯示 3 個選項，但目前狀態是 primary、其他是 secondary — 視覺上「哪個是目前狀態」僅靠顏色深淺區分，搭配同排 `StateTag` redundant。
- 沒有「已訂購人數 / 剩餘份數」readonly 資訊，商家看不到當前實際消費狀況。
- 沒有 delete / 歷史版本 / 複製新建。

**Benchmark**：
- **UberEats / DoorDash 編輯** 左側菜單 list sticky、右側編輯；本頁單欄、離開即失去位置。
- 多數 menu builder 提供「複製到其他日期」—本系統 deliveryDate 每筆菜單單日綁定，複製尤其重要（商家複製本週一便當到下週一）。

**建議**：
1. 用 `getVendorMenuItem(menuItemId)` 單點 API 取代 list + find。
2. 狀態切換改 `Segmented control` 或單顆「暫停上架 / 下架」primary action。
3. 頁首加「今日訂購 X 份 / 剩餘 Y 份 / 收入 Z」唯讀 summary card。
4. 加「以此為模板建立新菜單」button。
5. 錯誤訊息「找不到菜單 XXX，可能已被刪除或超出查詢範圍」會誤導使用者；修掉 root cause 或至少改文案。

---

## 7. `/vendor/schedule` 訂購政策

**Components**：`PageHeader`、兩張並列 `Card`：「目前有效政策」`<dl>` 顯示 2 key-value，「更新政策」兩個 number input + submit。

**合理性**：❌。全商家端 UX 最糟的一頁之一。

**第一直覺**：不通過。「預購開放天數 (1–7)」勉強可懂；「前日截單分鐘 (900–1200)」完全無法 3 秒理解。900 是 15:00、1200 是 20:00，使用者需要心算 ÷60。標題「訂購政策」抽象，不如「截單時間 / 預購規則」具體。

**資訊層級**：
- 左右兩張 card 對等寬度但資訊量不對稱（左 2 行、右 2 input + 按鈕），畫面極空。
- 沒有「例如：設為 1080 代表前一天 18:00 截單」live preview。
- 沒有時區提示。
- 沒有 history、沒有生效時間，「儲存即生效」對商家是高風險。

**Benchmark**：
- **Toast POS / Square 營業時段**：用 `<input type="time">` 選 18:00，背後存分鐘。
- **foodpanda partner portal 預購設定**：days ahead 用 0–7 radio group、cutoff 時間用時間挑選器 + 即時預覽「下一個可訂日為 04/21」。
- **UberEats Scheduled Orders**：直接用 per-day 時間軸 UI。

**建議**：
1. `modifyCancelCutoffMinuteOfDay` 欄位改 `<input type="time" step="900">`（15 分鐘刻度）。
2. 加 live preview：「設定為 18:00 → 04/19 訂單於 04/18 18:00 後無法修改」。
3. `preorderOpenDaysAhead` 用 1-7 的 segmented button 取代 number input，附每選項業務解釋。
4. Submit 前加 `ConfirmDialog`「此變更將立即影響員工下訂」。
5. 合併兩張卡為單張或只保留 form（form value = current policy）。

---

## 8. `/vendor/batches` 備餐批次列表

**Components**：`PageHeader` + 「建立今日批次」、兩張 `Card`：批次查詢（input + button）、最近批次（localStorage 近 12 筆）。

**合理性**：❌。名為「列表」實際是「查詢框」，真正 list 是 client-side localStorage cache。

**第一直覺**：不通過。進來看到空的 `fbatch-...` 輸入框和「尚無最近批次」，完全不知道**系統裡有沒有**批次。換瀏覽器 / 清 cache，所有批次「消失」。

**資訊層級**：
- 左「批次查詢」和右「最近批次」同寬，左邊只是輸入框佔 500px。
- 沒有 server-side batches 列表 API 或頁面沒呼叫。

**Benchmark**：
- **UberEats Manager Reports / DoorDash Batch Orders** 皆為 server-side 分頁列表，按日期 / 狀態排序。
- 任何報表類後台（Toast Report、Stripe Dashboard）都不會只靠 localStorage recent list。

**建議**：
1. Backend 若有 `listVendorFulfillmentExportBatches`，前端改 server-side 表格；若沒有，這是 backend 缺口應優先補齊。
2. 「建立今日批次」放頁首 primary CTA，文案改「產生 YYYY-MM-DD 備餐快照」。
3. localStorage recent list 作 sidebar secondary 保留即可。
4. 空狀態應提示「建立一份批次以產生備餐彙總 / 分區表 / 標籤 / 籃清單」。

---

## 9. `/vendor/batches/new` 建立批次

**Components**：`PageHeader`、單張 `Card` 內單一 `FormField`（配送日）+ 「建立批次」button。

**合理性**：⚠️。簡單、但過度單薄。

**第一直覺**：通過操作層，但缺情境。

**資訊層級**：
- 整頁只有一個日期選擇 + 一個 button，畫面 80% 空白。
- 沒有「這個日期會產生哪些 artifact / 預估幾份 / 幾廠區」預覽。
- 建立後直接導至詳情、沒有 confirm。

**Benchmark**：
- **Toast KDS 「列印準備單」** 會先顯示「將產生 42 張標籤、3 張廠區分頁表」讓使用者二次確認。
- 報表類工具（Metabase export / Stripe report export）都有 preview。

**建議**：
1. 加「預覽」button：先 call fulfillment board API 取該日 order count + plant count，顯示「將產生 4 artifacts：每日彙總、3 張廠區分區表、42 張標籤、N 份籃清單」。
2. 加快捷日期 chip：「今天 / 明天 / 本週末」。
3. 考慮把此頁收進 `/vendor/batches` 變成 modal / inline form。

---

## 10. `/vendor/batches/[batchId]` 批次詳情

**Components**：`PageHeader` + 「列印 / 返回」、`Card` 批次基本資料（5 欄 `<dl>`）、`Card` artifacts（5 欄表：類型 / objectRef / MIME / bytes / SHA-256）、自訂 print CSS。

**合理性**：⚠️。資訊完整但完全是「工程視角」。

**第一直覺**：不通過。進頁面看到 `obj://batches/fbatch-xxx/daily-summary.csv`、`sha256 a3f2...`，商家要的是「下載 / 列印標籤」，此頁只給 metadata。

**資訊層級**：
- 「類型」中文化了（每日彙總 / 廠區分區表 / 餐盒標籤 / 配送籃清單）—亮點。
- 但沒有「下載」按鈕 — 每個 artifact 需手動複製 objectRef 跳到 `/vendor/compliance/access-links` 貼上、產生 URL、複製、貼到瀏覽器。**四跳才能下載一個 CSV**，違反所有商家後台慣例。
- SHA-256 和 bytes 對前台商家沒意義。
- `window.print()` 會印整頁 metadata，不是印真正的 artifact（標籤 / 分區表）。「列印」按鈕誤導。

**Benchmark**：
- **UberEats / DoorDash batch report**：每行一顆「下載 CSV / PDF」button。
- **Toast End-of-Day Report** 直接 inline PDF viewer + 列印。
- 此頁 print style 只隱藏 `header, aside, nav` 但實際要印的是 `LABELS` artifact（PDF / PNG），flow 完全斷裂。

**建議**：
1. 每個 artifact 行加「下載」button，後端透過 `createVendorObjectStorageAccessLink(objectRef)` 一次帶回、直接開新分頁。
2. SHA-256 / bytes 收進 row 的「詳細資料」toggle。
3. 標籤類型（LABELS）若 PDF，提供 inline preview + 直接「列印」（打該 PDF），不是 `window.print()`。
4. 批次基本資料卡用 2-col grid + 重要欄位（批次編號 / 配送日）放大字。
5. 加「重新產生 / 作廢」操作（若 backend 支援）。

---

## 11. `/vendor/orders` 營運訂單查詢

**Components**：`PageHeader`、篩選 `Card`（廠區 required / 起日 / 迄日 / 狀態 / 套用）、`DataTable`（4 欄：訂單 / 廠區 / 配送日 / 狀態）。

**合理性**：⚠️。當查詢工具算堪用，但作為訂單中樞遠遠不足。

**第一直覺**：通過基本操作。「輸入廠區 → 套用 → 看清單」可完成。但「這頁跟 `/vendor/today` 的訂單詳情差在哪」不清楚。

**資訊層級**：
- 只有 4 欄：沒有金額、餐點項目、特殊需求、訂購人。
- `plantId` 是 required text input，沒有下拉 — 商家若有 3 個廠區要手動打 `fab-a`。
- 沒有 summary（總單數 / 總金額 / 完成率）只顯示「共 N 筆」。

**Benchmark**：
- **foodpanda / DoorDash Orders tab** 預設顯示當日 + 前 7 天，欄位：# / 時間 / 顧客 / 項目 / 金額 / 狀態 / action。本頁缺顧客、項目、金額。
- **美團商家版訂單** 頂部有 summary bar（今日接單數、金額、退款數）。

**建議**：
1. `plantId` 改從 `data.actor.scope.plantIds` 產生 dropdown；多廠區可選 All。
2. 增加欄位：訂購人 / 項目摘要 / 金額（`MoneyAmount`）/ 特殊需求 chips。
3. 頁首加 summary bar：當前 filter 下總單數 / 總金額 / 已取消 / 已退款數。
4. Row click 開 drawer 顯示 full order（line items、audit log），不跳頁。
5. 加 export CSV button。

---

## 12. `/vendor/compliance` 合規狀態（誠實 UX）

**Components**：`PageHeader`、4 張 `Card`：`info` variant 的 4 步驟說明、兩張平行 action card（建立上傳計畫 / 建立下載連結）、常見狀態說明 2-col dictionary。

**合理性**：✅（相對其他頁）。誠實告知「商家端尚未開放查看自己狀態的 API」、給出 workaround，文案面做得不錯。**這是整個商家端最好的一頁**。

**第一直覺**：通過。「我進來就知道：上傳 / 下載、並被提醒 status 要找福委會」。

**資訊層級**：
- info card 的 4 步驟清單字密度偏高，但內容都必要。
- 上傳 / 下載兩個 action card 同寬 OK，但「建立上傳計畫」button 放在 snippet `actions` 裡（右上），與卡片描述「支援 MAX 20MB...」位置分離；直覺上 button 應在說明下方。
- 常見狀態說明 4 種狀態，但沒和「建立上傳計畫」連動。

**Benchmark**：
- **Square for Restaurants / Toast 合規 tab** 直接顯示「Business License — Valid until 2025-12-31」具體文件清單。此處承認無法做到、走 email 通路，屬合理取捨。
- **foodpanda partner portal document center** 會顯示「Pending review (2 days)」 — 本系統走 email 沒問題但應在 UI 提示「上傳後請發信給 compliance@...」含預填信件連結。

**建議**：
1. info card 末尾加「寄信通知福委會」`mailto:compliance@...` link。
2. 兩卡合併成流程圖：步驟 1 建立上傳計畫 → 步驟 2 瀏覽器 PUT（此處其實沒有 UI）→ 步驟 3 下載連結驗證。目前 Step 2 完全缺。
3. 「我該做什麼」第 3 條「被停權後復權」文案語氣可改柔和。
4. 狀態 dictionary 改 timeline 圖。
5. 加「最後上傳時間」讓使用者判斷「我該再上傳嗎」。

---

## 13. `/vendor/compliance/upload` 建立上傳計畫

**Components**：`PageHeader` + 左 `Card`「計畫資料」6 `FormField`（artifactClass / fileName / mimeType / sizeBytes / thumbnailSizeBytes / locale）+ 建立 button；右 `Card`「結果」顯示 objectRef / uploadUrl / expiresAt。

**合理性**：❌。把 S3 presigned URL API 原封不動 wrap 成表單。

**第一直覺**：完全不通過。要求商家手動填：
- fileName（要記得打副檔名）
- mimeType（要記得是 `application/pdf`）
- sizeBytes（要自己量檔案大小）
- thumbnailSizeBytes（MENU_IMAGE 才填，但沒有條件化顯示）

沒有檔案選擇器、沒有實際上傳。拿到 uploadUrl 後商家要自己開 terminal curl PUT，非開發者完全做不到。

**資訊層級**：
- 左右卡同寬 OK。
- 結果卡顯示 `uploadExpiresAtEpochSeconds: 1713456789` raw epoch — 需心算成可讀時間。

**Benchmark**：
- **UberEats / DoorDash 合規文件上傳** 一律 dropzone，選檔後自動 PUT + 進度條。
- **沒有任何合格商家後台會要求人工填 mimeType**。

**建議（最關鍵）**：
1. 改**檔案 dropzone**（`<input type="file">`）：拖檔進來 → 前端自動讀取 name / type / size → 呼叫 `createVendorObjectStorageUploadPlan` → 立刻 `fetch(uploadUrl, {method: 'PUT', body: file})` → 顯示進度與完成狀態。
2. `artifactClass` 用 tab 切換（COMPLIANCE_DOCUMENT / MENU_IMAGE），根據選擇自動檢驗 mimeType / size limit。
3. `thumbnailSizeBytes` 只在選 MENU_IMAGE 時顯示；或由前端自動產 thumbnail 並算 bytes。
4. `uploadExpiresAtEpochSeconds` 用 `formatTaipeiDateTime` 顯示。
5. 上傳成功後自動 redirect / 把 objectRef 複製到 clipboard + toast「已複製 objectRef，可至菜單建立頁貼上」。

---

## 14. `/vendor/compliance/access-links` 建立下載連結

**Components**：`PageHeader` + 左 `Card` objectRef + locale 輸入、右 `Card` 結果顯示 downloadUrl / expiresAt。

**合理性**：❌。同樣是 API 反向對映 UI。

**第一直覺**：不通過。「objectRef」商家不懂；`obj://...` 這種 URI 沒地方取得。

**資訊層級**：
- 整頁只有一個文字框，孤立。
- `downloadExpiresAtEpochSeconds` 是 epoch，無法閱讀。

**Benchmark**：
- 應整合進「批次詳情」和「合規文件列表」，不是獨立頁。
- 沒有哪個商家後台會要求使用者自己輸入 object ref。

**建議**：
1. 把這頁從 navigation 拿掉；access link 由每個需要下載的入口自動產生（批次詳情 download button、合規文件列表 download button）。
2. 若要保留作為 debug tool，至少改「最近 objectRef」下拉（從 localStorage 近期上傳紀錄）。
3. 下載連結產生後直接 auto-open `window.open(downloadUrl)`。

---

## 15. `/vendor/insights` 營運分析

**Components**：`PageHeader`、日期範圍 `Card`（起始 epochDay / 結束 epochDay / 套用）、指標 schema `Card`（版本 / 生成時間 / metric chips）、各廠區指標 table（動態欄）、時間序列 table（動態欄）。

**合理性**：❌。「商家分析頁」最低限度要有圖表，此處零圖表。

**第一直覺**：不通過。
- 「起始 epochDay」與「結束 epochDay」完全是 Unix epoch day (19831 = 2024-04-19)，商家要自己算。專案其他頁明明用 ISO date。
- 沒有任何圖：bar / line / pie 都沒有。全是 raw number table。
- 指標名稱依賴 backend dynamic schema，UI 無法預測有什麼、無法分 grouping。

**資訊層級**：
- 指標 schema 卡顯示 `metricSchemaVersion` 給誰看？工程版本號。
- 兩張 table 的 X/Y 軸差別：一張是 plantId、一張是 date。沒有交叉透視。

**Benchmark**：
- **UberEats Merchant Analytics**：首頁是 trend line（sales over time）+ top items bar + heat map（時段 × 星期）。
- **DoorDash Business Manager Insights**：可切 time / store / item / category 維度的 dashboard，所有 metric 都有 spark line。
- **Toast Reports**：每 metric 一張 card + mini chart，點進去才是 table。
- 本頁是 v0 資料框，完全沒進入分析場景。

**建議**：
1. 日期改 `<input type="date">`，前端轉 epochDay 才 call API。加快捷：本週 / 本月 / 上月。
2. 指標 schema card 拔掉（移到「關於指標」footer link）。把 `metricDefinitions` 轉成 metric card grid：每指標一張 card + 當期數值 + WoW delta + mini sparkline。
3. 廠區指標 table 加 bar 視覺化；時間序列 table 至少加 line chart。
4. 補「Top menu item」「Top special request」排行。
5. Export CSV button。

---

## 跨頁總結

**視覺密度**：全商家端**沒有真正善用桌機 canvas**。side-nav 固定 256px、content 1000px，但多數頁面用 2-col grid 擺兩張 card 就結束，下半螢幕空白。看板 / 菜單列表 / 分析這 3 個應該是資訊高密度的頁面全都是垂直堆疊 + 稀疏 table，**符合「桌機上塞手機版」批評**。

**Kanban 缺席**：今日看板核心備餐動作完全 table-based，沒有 Toast KDS / Deliveroo Hub 風格 column board。配送狀態推進是下拉 + submit 兩段式操作。

**工程 ID 洩漏**：menuItemId、objectRef、epochDay、minuteOfDay、sizeBytes、sha256 — 這些純工程字段全部裸露在商家 UI。多半是把 API contract 直接 wrap 成表單。上傳 / 下載 / 菜單建立 3 處最嚴重。

**Action 斷裂**：菜單建圖片需跨到 compliance upload；批次詳情下載需跨到 access-links；合規上傳拿到 URL 還要自己 curl。每個跨頁都是未收尾 flow。

**相對好的頁面**：`/vendor/compliance` 誠實 UX 文案好、`/vendor/schedule` 結構簡單但內容明確（雖然分鐘 vs 時間格式問題）、`/vendor` 首頁資訊選擇 OK 只是密度太低。

**最該先修 3 頁**：
1. `/vendor/today` fulfillment-board — 改 Kanban + 單鍵推進狀態（影響每日主要工作流）。
2. `/vendor/compliance/upload` — 改檔案 dropzone（目前商家實際上無法上傳）。
3. `/vendor/menu/new` — 隱藏 menuItemId、金額用元、圖片上傳整合、override 收進 accordion。
