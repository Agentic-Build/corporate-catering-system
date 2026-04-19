# UX Audit — 福委會管理員端（24 routes）

> Audit agent 原始報告。基準：Stripe Dashboard（財務 / dispute / settlement 黃金標準）/ Salesforce Lightning Console / PagerDuty / Opsgenie / Grafana Alerting / Retool admin panel / 美團後台 / 口碑後台 / Chargebee。

**TL;DR**：後端語意完整（append-only trace、ISS-003/007 簽核、rule/template CRUD、稽核 facet、analytics breakdown），但 UI 層幾乎只是把 API schema 原樣翻成表單，距離 Stripe / Uber / Salesforce 的「inbox-first、工作流驅動」後台有三層差距：
1. `/admin` 雖叫 Inbox，實際是 KPI cards + 摘要卡；真正的 inbox（一條條可處理項）不存在。
2. 金額、日期、ID 大量以 `minor unit` / `epochDay` / `minuteOfDay` 裸露。
3. 缺 bulk action、缺 saved view / quick filter、大量依賴 localStorage（settlement cycles、disputes）。

---

## 1. `/admin` overview（統一 Inbox）

**Components**：PageHeader + 5 張 KPI metric cards（待審商家 / 開放告警 / SLA 超時 / 月結例外 / 爭議）+ 3 張 quick-action CTA cards（關帳 / lifecycle / 評估規則）+ 4 張摘要 Cards。

**合理性**：⚠️  
**3 秒直覺**：新進人員看到 5 顆數字但不知先處理哪個，點進去路徑長短也不知。標題「統一 Inbox」視覺卻是 dashboard；語意錯配。

**資訊層級**：KPI → 快速動作 → 摘要 → 下一步建議。層級合理，但 KPI 卡無 tone priority（待審是 amber、告警是 rose、SLA 超時也是 rose、月結是 cyan），顏色無法暗示「哪個最急」。

**Benchmark**：
- Stripe Dashboard 首頁：**Today 區 + Needs review 區**，每項是 actionable item，不是 KPI number。
- Uber Admin / DoorDash Ops：都是 **tri-pane inbox**（左 queue → 中 detail → 右 action）。
- Salesforce Lightning Console：用 **utility bar + pinned lists**；admin 打開就有未完成 tasks。
- 本頁更像 Grafana single-stat row。

**建議**：
1. 5 顆 KPI 降為 1 條 status ribbon；主視覺換 **actionable queue**（每列：item type icon / title / due / severity / 指派 / action button）。
2. KPI 依 SLA 破表優先排序，加 **sparkline 或 delta**。
3. 「SLA 超時告警」卡的 rose 色塊 + 卡內 item 卡再 rose，雙層 alert fatigue；改 neutral 容器 + 單列 severity pill。
4. 「最近關帳摘要」寫明「由本瀏覽器最近一次關帳匯總而得」= 這不是系統真相，是 localStorage；改為從 server 取最近 1 cycle 或隱藏到 secondary 面板。
5. 加入 **persona switcher / saved view**（My assignments / My vendors / All）。

---

## 2. `/admin/vendors` list

**Components**：PageHeader、篩選 Card（status / sortBy / sortOrder + 套用）、DataTable。

**合理性**：⚠️  
**3 秒直覺**：能看懂，但手動「套用 / 重新載入」才跑篩選，違反即時 filter 預期。

**資訊層級**：filter card 佔首屏 1/3 但只裝 3 欄位 + 1 按鈕。`vendorId` 第一欄不 scannable。

**Benchmark**：
- Stripe / Retool pattern：**無「套用」按鈕**，select 一改即重拉。
- 美團 / 口碑後台：狀態用 **tab bar**（全部 / 待審 / 啟用 / 停權）而非 dropdown，配 count badge。
- UberEats Ops：表格右上有 **column chooser + CSV export**。

**建議**：
1. status 改 **segmented tab with counts**（全部 120 / 待審 5 / 啟用 98 / 停權 12 / 拒絕 5），移除套用按鈕。
2. 欄位重排：**名稱 &gt; 狀態 &gt; 分類 &gt; vendorId &gt; 最後更新**。
3. `updatedAt` 補相對時間（"2 小時前"）並保留 absolute。
4. 加 **bulk action bar**；sort 直接點 column header。
5. 每列「詳情」按鈕多餘（整列已可點），移除。

---

## 3. `/admin/vendors/[vendorId]` detail

**Components**：PageHeader + 2 action buttons、商家 meta Card（8 項 dl）、Tabs（文件 / 審核歷程 / lifecycle 歷程）、下載文件 Card（objectRef 手輸）。

**合理性**：⚠️  
**3 秒直覺**：Meta 好懂，但「審核歷程 / lifecycle 歷程」切 tab 後 context 消失。

**資訊層級**：Meta 8 欄扁平；`保留期` 是 engineer 視角；reviewHistory count / documents count 是 meta，內容又在 tab 裡，重複。

**Benchmark**：
- Stripe Customer detail：**left overview column（固定 pinned）+ right tabs**。overview 不會消失。
- Salesforce Lightning Record Page：用 **tabs + related lists side rail**，action 在 highlight panel。
- 本頁 action「提交審核決策」塞到 PageHeader 右上，離 tab「審核歷程」很遠；應該在 review tab 內直接放 primary CTA。

**建議**：
1. 改 **two-column layout**：左 20% 固定 vendor summary（status pill、pending docs 數、SLA 倒數），右 80% tab 內容。
2. 「下載文件」input 去掉，直接在文件 table 的 objectRef 欄位做 icon button。
3. 「審核歷程」tab 底部直接鑲嵌 review 表單，不要 navigate 去另一頁。
4. 文件 table 的 `Status` / `Expires` 加顏色（過期 red、即將到期 amber、valid green）。
5. Meta 8 欄只挑真正 SLA-driving 的 4 個（分類 / 狀態 / 最後更新 / 待交文件數），其他折到「技術細節」。

---

## 4. `/admin/vendors/[vendorId]/review`

**Components**：PageHeader、Card 單一表單（decision select + 意見 textarea + 錯誤訊息 + 送出/取消）。

**合理性**：⚠️  
**3 秒直覺**：能用，但 decision 選項直接顯示 enum（`APPROVED / REQUEST_FIX / REJECTED`），無 label / 說明。

**資訊層級**：只有表單，無 context；做決策時看不到商家文件狀態、review 歷程、上一次 comment，等於 blind decision。

**Benchmark**：
- Stripe Dispute 審核：**左側是 evidence list（可勾選）、右側是 submit form + 前次回覆摘要**。
- 美團商家入駐審核：**stepper + 文件 thumbnail preview + decision**。
- Salesforce Approval Request：approver 總能看到 record 全貌 + 歷史 approval reasons。

**建議**：
1. 本頁應是 vendor detail 的 **side drawer / modal**，而非獨立 route。
2. 至少在 Card 內 embed：必填文件清單（含是否已交、是否過期）、最近 2 筆 review comments、上一次 REQUEST_FIX 尚未處理項。
3. decision select 改 **radio cards with 描述**（例：REQUEST_FIX 顯示「要求補件，商家收到通知但不扣信用分」）。
4. APPROVED 時做 inline pre-check（「偵測到 2 份必填文件尚未上傳，APPROVED 會被後端拒絕」）。
5. `comment ≥ 5 字` 應≥ 20 字並有 template quick-insert（「文件齊全」「需補 xxx」）。

---

## 5. `/admin/vendors/[vendorId]/mappings`

**Components**：PageHeader、映射 DataTable（mappingId / plantId / effect / precedence / 服務時段 / 動作）、新增/更新映射表單（6 欄 grid）。

**合理性**：⚠️  
**3 秒直覺**：effect（ALLOW / DENY）+ precedence 數字這組概念對新人太 technical，沒人知道 `65535` 是什麼。

**資訊層級**：表格 + 表單同頁正確，但「新增 / 更新映射」共用同組欄位，無 visual distinction。

**Benchmark**：
- Stripe Terminal location rules、AWS IAM policy UI：用 **"rule builder" with human sentence**（"Allow plant A during 10:00-14:00"）。
- 口碑後台 shop-location mapping：用 **checkbox matrix**（row = plant, col = day/time）。

**建議**：
1. mappingId 後端自動生成，UI 不暴露。
2. effect 欄位改 toggle（綠 ALLOW / 紅 DENY）+ tooltip。
3. precedence 改 **drag-to-reorder**（row 可拖），背後自動算。
4. 編輯模式用 inline row edit 或 detail drawer，而非下方大表單。
5. 服務時段加 **weekly repeating pattern**（週一到週五 10:00-14:00）而非單次 datetime。

---

## 6. `/admin/compliance/templates` list

**Components**：PageHeader + 2 actions、篩選 Card（分類）、DataTable（templateId / 分類 / 顯示名稱 / 必填 / 有效天數 / 提醒 / 動作）。

**合理性**：✅（結構 OK）  
**3 秒直覺**：能懂，「提醒」欄位顯示 `30, 14, 7` 缺單位。

**Benchmark**：
- Salesforce / Stripe template gallery：有 **preview thumbnail + usage count**。
- 本頁缺：每個 template 影響多少 vendors 的 callout。

**建議**：
1. 「提醒」欄改 tag 串（`30d` `14d` `7d`），加單位。
2. 每 row 加「影響 n 家商家」callout。
3. 篩選 Card 只有 1 個下拉卻佔整 row，收成 header chip。
4. 「必填」欄用 icon（lock / optional）更 scannable。
5. templateId mono font 很 engineer-facing，業務人員看「顯示名稱」就夠，ID 可折到 hover。

---

## 7. `/admin/compliance/templates/new`

**Components**：PageHeader + TemplateForm（7 欄）。

**合理性**：⚠️  
**3 秒直覺**：`reminderDaysBeforeExpiryCsv` 用逗號分隔字串是 dev 捷徑；`suspensionGraceDays` 意思不明。

**Benchmark**：
- Stripe tax rule / webhook form：**分 section（Basic / Scheduling / Notifications）**。
- Chargebee subscription plan：提醒規則用 **chip input + "+ add"** 而非 CSV。

**建議**：
1. Form 分 3 區：`身份`、`有效期`、`提醒`。
2. reminder 改 chip input。
3. suspensionGraceDays 改 **"文件到期後 __ 天自動停權"** inline sentence。
4. templateId 加自動生成按鈕（基於 displayName slugify）。
5. 預設 initial（`business-license`、`30,14,7`、`3d`）應命名為「範本：商業登記」而非塞進欄位。

---

## 8. `/admin/compliance/templates/[id]` edit

**Components**：同 `new` 但 lockKey。

**合理性**：⚠️  
**3 秒直覺**：URL 用 `compositeId = {category}-{templateId}` composite key，手動 parse，脆弱。

**Benchmark**：
- Stripe edit page：右上有 **delete / disable** 動作 + 「本模板已用於 n 筆 vendor」callout。

**建議**：
1. 補 delete / disable 按鈕。
2. 補「本模板影響 n 家商家、n 份文件」status ribbon。
3. 顯示版本歷程（此模板有人改過 maxValidityDays 從 180 → 365）。
4. 解 composite key 失敗時錯誤文字改「模板網址有誤，請回清單重新進入」。
5. lockKey 視覺 disable（灰底 + lock icon）。

---

## 9. `/admin/compliance/lifecycle`

**Components**：PageHeader、執行設定 Card（runDate / dry run checkbox / 執行按鈕）、最近一次執行結果 Card。

**合理性**：✅  
**3 秒直覺**：能懂，有 dry run 是好設計。

**Benchmark**：
- Chargebee dunning run / Stripe subscription webhook replay：**preview mode + 「本次影響 n 筆」estimate**。
- DoorDash cron-triggered jobs：顯示 **上次執行時間 + 結果 + 下次排程**。

**建議**：
1. 執行前顯示 **估計影響數**（"今日將發送 12 封提醒、停權 3 家、復權 1 家"）。
2. 補 **最近 N 次執行歷程**，目前只有最近一次。
3. dry run 成功後顯示 breakdown（哪幾家 vendor 會被影響）。
4. 加 **自動排程 UI**（cron setup）。
5. 把「結果」Card 改 timeline style。

---

## 10. `/admin/settlement` hub

**Components**：PageHeader、3 張 action cards（關帳 / 週期 / 爭議）、最近月結摘要 Card（8 dl fields）。

**合理性**：⚠️  
**3 秒直覺**：三個 quick-action 卡清楚，但「最近月結摘要」再次依賴本瀏覽器 localStorage。

**Benchmark**：
- Stripe Billing Invoices hub：**timeline + upcoming invoice + failed payments**。
- Chargebee Revenue Hub：首頁直接 **MRR / churn / dunning queue**。

**建議**：
1. 把 3 張 action 卡改 1 條 step tracker（`1. 確認例外 → 2. 關帳 → 3. 鎖定 → 4. 解決爭議`）。
2. 「最近月結摘要」從 server 拉最近一筆 batch。
3. 加 **pending close countdown**（下次關帳建議日、距離 HR cut-off 幾天）。
4. 摘要的金額欄位 `totalAmountMinor` 在 close page 有露但此頁無；應整齊 TWD 金額。
5. Disputed / Deduction failed / Refunded 3 個數字加點擊穿透到 filtered list。

---

## 11. `/admin/settlement/close`

**Components**：PageHeader、關帳表單 Card（cycleKey / issueChecklist / page / pageSize / sortBy / sortOrder / 錯誤 / 送出）、結果 Card、ConfirmDialog。

**合理性**：❌  
**3 秒直覺**：**整個 admin portal 最 misdesigned 的頁**。「頁大小」、「排序欄位」、「issueChecklist CSV」全露給使用者，讓業務人員覺得自己在寫 API；關帳是一年一度的財務動作，UI 卻像個 CLI 包裝。

**資訊層級**：關鍵不可逆動作（建 HR SFTP 批次）和瑣碎 pagination 參數混在一起、權重相同。

**Benchmark**：
- Stripe Billing 月結 / Chargebee close period：**全畫面 wizard**，Step 1 preview → Step 2 檢查例外 → Step 3 簽核 → Step 4 關帳（紅色大按鈕 + 二次確認）。
- Pachira / Workday payroll period close：**不會讓 user 填 pageSize 或 sortBy**。
- 目前此頁其實是 API POST body editor。

**建議（最關鍵）**：
1. 徹底重做成 **4-step wizard**：
   - (1) 選 cycle（下拉「2026-03 (建議)」/「2026-02」）
   - (2) 例外預檢（列出各 disputed / deduction_failed row，全部標記「已處理」後才能下一步）
   - (3) ISS-003 簽核（checkbox「我已在 governance issue 上取得簽核」+ 顯示本次簽核人姓名/時間）
   - (4) 執行（紅色 destructive button，需打字輸入 cycleKey 才能 enable）。
2. 移除 `page / pageSize / sortBy / sortOrder` UI 欄位——那是結果頁參數。
3. `issueChecklistRaw` 當前是手輸 CSV；改 checkbox「已取得 ISS-003 結算發佈簽核」。
4. 結果 Card 的 `reconciliation.totalAmountMinor` raw 數字改 MoneyAmount。`exceptions` 截前 50 筆存 localStorage 是 debug hack，正式產品該讀 server。
5. ConfirmDialog 描述「此動作會建立不可回復的 HR SFTP 批次並鎖定資料源」很好，但缺 **本次影響金額 + 筆數 re-summary**。

---

## 12. `/admin/settlement/cycles` list

**Components**：PageHeader + 執行關帳按鈕、cycles DataTable、empty state。

**合理性**：❌  
**3 秒直覺**：description 誠實寫「後端無專用 list API，此清單為本地紀錄」——feature gap，不該暴露給使用者。

**Benchmark**：
- Stripe Invoices / Chargebee settlement list：**永遠從 server 拉**，有 search、filter、export CSV、download PDF per row。

**建議**：
1. 呼叫後端 list API（若沒有，是 backend gap，應補）。
2. 表格加「鎖定狀態」欄（LOCKED / UNLOCKED）。
3. 加「HR 對帳狀態」欄（SUCCEEDED / FAILED / PENDING）。
4. 加 download batch CSV 快捷動作。
5. empty state 不該建議「執行關帳」——那是破壞性動作；改建議「檢視歷史」或「了解月結流程」。

---

## 13. `/admin/settlement/cycles/[cycleKey]`

**Components**：PageHeader、週期資訊 Card、最近鎖定狀態 Card、兩欄並列 lock / unlock Cards。

**合理性**：⚠️  
**3 秒直覺**：lock / unlock 並列同層是 dangerous pattern——unlock 應該是罕見 escalation 操作，不該和 lock 同等視覺權重。

**Benchmark**：
- Stripe financial report lock：unlock 在 **「... more actions」隱藏菜單** 下，且會跳 challenge dialog。
- 口碑月結：unlock 需要「輸入簽核人姓名 + 輸入 cycle 編號 confirm」。

**建議**：
1. Unlock 隱藏到 secondary menu 或 require elevated confirm。
2. lock / unlock 同區顯示當前狀態 pill（LOCKED → 顯示 Unlock、UNLOCKED → 顯示 Lock），不要兩個都給。
3. 週期資訊 Card server-side 拉真實狀態。
4. reason textarea 加 suggested templates。
5. lock history timeline 顯示。

---

## 14. `/admin/settlement/disputes` list

**Components**：PageHeader、查找特定爭議 Card、最近紀錄爭議 Card。

**合理性**：❌  
**3 秒直覺**：爭議列表靠 localStorage 組合；不 acceptable 的合規 UX。

**Benchmark**：
- **Stripe Dispute Dashboard（行業黃金標準）**：tabs by status（Needs response / Under review / Won / Lost）、每列顯示金額 / due date / evidence 完成度 / customer、可 bulk respond、filter by date/amount/reason code。
- 本頁幾乎所有能力都沒有。

**建議**：
1. 改 server-side dispute list，必有：disputeId、cycleKey、員工（encrypted, 可遮罩）、爭議金額、狀態、owner、opened at、SLA due、reason。
2. Tab by status（OPEN / IN_REVIEW / RESOLVED_REFUND / RESOLVED_REJECTED）+ count badges。
3. Bulk action：批次指派 owner、批次 reject。
4. 「查找 disputeId」改 type-ahead search。
5. Sort by SLA due 是 default。

---

## 15. `/admin/settlement/disputes/[disputeId]`

**Components**：PageHeader、Tabs（ASSIGN_OWNER / RESOLVE_REFUND / RESOLVE_REJECTED）、condition 表單欄位、錯誤、送出、結果 Card。

**合理性**：⚠️  
**3 秒直覺**：**頁面進來看不到 dispute 本身**（員工、金額、cycle、證據、為何被 dispute），只看到 3 個 action tab + 表單；使用者要盲目決策。

**Benchmark**：
- **Stripe Dispute detail**：**左 60% 是 evidence / message history / customer context**、右 40% 才是 submit evidence form。這頁只做到右側。
- PagerDuty incident detail：左 timeline、右 action，一樣 context-first。

**建議**：
1. 頂部加 **dispute 全景 card**：員工 masked ID、金額（TWD）、cycle、opened at、SLA due countdown、原 charge rationale、目前 owner、歷程 timeline。
2. Operation tab 改單一表單（radio button operation + 動態欄位）。
3. `refundAmountMinor` 不裸露 minor unit；用 MoneyAmount + step。
4. RESOLVE_REFUND 後顯示「會產生 xxx 退款、扣回員工 yyy」預覽。
5. 結果 Card 只有 3 欄太薄；應 replace 上方 dispute 全景 card 的資料。

---

## 16. `/admin/anomalies` list

**Components**：PageHeader + 2 actions、篩選 Card（7 欄）、DataTable。

**合理性**：❌  
**3 秒直覺**：`asOfEpochDay` / `asOfMinuteOfDay` 是天文單位 filter；業務人員根本算不出今天是第幾個 epochDay。

**Benchmark**：
- **PagerDuty Incidents**：filter 分群 by status (Triggered/Acknowledged/Resolved) + severity，日期範圍用 date picker。
- **Grafana Alerting**：有 saved search + label matcher。
- **Opsgenie**：inbox-style，每 alert card 顯示 **severity + assigned to me + SLA due 倒數**。
- 本頁缺 saved view、缺「my open alerts」預設、缺 date picker。

**建議**：
1. `asOfEpochDay / asOfMinuteOfDay` 換 **Date picker + time picker**（UI 側轉 epoch）。
2. status 改 tab bar（OPEN / ACKNOWLEDGED / CLOSED）+ count badge。
3. 加 **「我負責的」/「SLA 即將超時」/「所有」** 3 個預設 view。
4. DataTable 列顯示 **SLA countdown chip**（-2h、30m left），不是 slaStatus enum。
5. escalatedOnly ALL/TRUE/FALSE 換 tri-state toggle。

---

## 17. `/admin/anomalies/[alertId]`

**Components**：PageHeader、告警詳情 Card（12 dl fields）、轉換歷程 Card、推進狀態 Card（operation tabs + 動態欄位 + issueChecklist / closureNote / closureEvidenceRefs CSV / ticketReference / note）+ ConfirmDialog。

**合理性**：✅（流程齊全）但 ⚠️ 細節  
**3 秒直覺**：詳情 Card 12 欄資訊 dump，沒主次；「推進狀態」tab bar 的 operation 名稱是 enum。

**Benchmark**：
- **PagerDuty Incident detail**：大 header 顯示 `P1 · SLA 剩 2h3m · Assigned to @you`，然後 timeline + actions；action buttons 是 Acknowledge / Escalate / Resolve 大按鈕。
- **Opsgenie**：有 **runbook link、teammate tag、slack thread embed**；close incident 一律 require resolution note。
- 本頁 CLOSE 有 evidence refs + ISS-007 簽核是好的，但 CSV 輸入太 dev-facing。

**建議**：
1. 頂部 hero：`severity pill · SLA 倒數大字 · 目前 owner avatar · 觀測值 vs 門檻`；其他欄位折到「技術細節」可展開。
2. transition trace 改 timeline（icon + 時間戳 + actor + note），不要 table。
3. `closureEvidenceRefs` CSV 換 **chip input**。
4. Operation tab bar 改 Stripe-style **primary action buttons**：`Acknowledge（綠）/ Escalate（amber）/ Close（red）/ Assign（secondary）`；CLOSE 自動彈 modal。
5. 把 ISS-007 簽核 checkbox 放進 close modal 而非 page 裡。

---

## 18. `/admin/anomalies/evaluate`

**Components**：PageHeader、評估參數表單 Card（8 欄）、觸發告警結果 Card。

**合理性**：⚠️  
**3 秒直覺**：這個頁是 QA / 測試工具，不是日常 ops。

**Benchmark**：
- Stripe **Webhook test event** / Retool **function test**：獨立「Test」或「Developer」區。
- 本頁更適合藏在 rule detail 下「測試這條規則」。

**建議**：
1. 把本頁移到 anomalies/rules/[ruleId] 下的 tab「Test」。
2. epoch 輸入換 datetime picker。
3. 欄位 label 換業務語（「近 7 天到期風險」等）。
4. 結果顯示 observedValue / threshold 加人話（「準時率 92% 低於門檻 95%，觸發 P2」）。
5. 加「載入真實值」按鈕，從最近分析資料拉 vendor 目前指標。

---

## 19. `/admin/anomalies/rules` list

**Components**：PageHeader + 新增規則 action、DataTable（ruleId / 名稱 / kind / 嚴重度 / 門檻 / 啟用 / 編輯）。

**合理性**：✅  
**3 秒直覺**：能懂，門檻欄位 `LTE 7（7d / 240m）` 需腦內 parse。

**Benchmark**：
- Grafana Alert rules：有 **labels chip、silence toggle、last fired time、affected targets count**。
- 本頁缺：每條規則「最近觸發幾次」「目前有幾個 open alert」。

**建議**：
1. 加欄位「最近觸發時間」「目前 open alerts」。
2. 「啟用」改 inline toggle（點即切，搭配 confirm）。
3. 門檻欄位用三行：`比較器 + 閾值`、`評估窗口 N 天`、`SLA N 分鐘`。
4. 加 bulk enable/disable + 匯出規則 JSON。
5. kind 加 icon（EXPIRY_RISK = 時鐘、ON_TIME = 卡車、SATISFACTION = 星星）。

---

## 20. `/admin/anomalies/rules/new`

**Components**：PageHeader + RuleForm（11 欄）。

**合理性**：⚠️  
**3 秒直覺**：governanceIssueId 要人工輸入 ISS-007 字串。

**Benchmark**：
- Grafana / Datadog monitor create：**wizard 三步**（1. define metric、2. threshold、3. notification）。
- Datadog 每步都有 **live preview**。

**建議**：
1. 分組 section：身份、閾值、嚴重度 & SLA。
2. governanceIssueId 改 combobox with ISS- suggestions 或 required pattern validator。
3. threshold 組合顯示 live sentence "若 `kind` `comparator` `value` 持續 `window` 天"。
4. severity 旁顯示 SLA 預設（例：P1=30m、P2=4h）。
5. ruleId 自動生成選項。

---

## 21. `/admin/anomalies/rules/[ruleId]` edit

**Components**：PageHeader + RuleForm (lockRuleId)。

**合理性**：⚠️  
**3 秒直覺**：同 new；但 edit 少 disable / duplicate。

**建議**：
1. 補 disable / duplicate / delete 按鈕。
2. 顯示「過去 30 天此規則觸發次數 / 誤報率」。
3. 顯示最近觸發的 top 5 alerts。
4. 修改 threshold 時提示「會影響 n 個現有 open alerts」。
5. 規則 threshold 歷史變更要看得到。

---

## 22. `/admin/audit`

**Components**：PageHeader + 責任歸屬 action、filter Card（7 欄）、DataTable。

**合理性**：⚠️  
**3 秒直覺**：稽核頁寫「稽核留痕」是對的，但 filter 又是 epoch day，稽核人員要查「上週某 vendor 的改動」要自己算。

**Benchmark**：
- **Salesforce Setup Audit Trail / Stripe Logs**：有 **faceted search（actor / action / entity → count）**、date range picker、CSV export、shareable saved filter URL。
- **美團後台稽核**：有 **時間軸 timeline view** + 表格 view 切換。
- 本頁 filter 組合有，但無 facet count、無 saved search、無 export。

**建議**：
1. `occurredFromEpochDay / occurredToEpochDay` 必須換 date range picker。
2. 加 **facet sidebar**：action value 分佈、entityType 分佈、actor top 10，點即 filter。
3. 加 CSV / JSONL export。
4. correlationId 支援 click-to-filter。
5. 表格 entity 欄位加 link（entityType=VENDOR → link 去 vendor detail）。

---

## 23. `/admin/audit/responsibilities`

**Components**：PageHeader、同樣 filter、DataTable（actor / role / eventCount / actions 集合 / entities 集合）。

**合理性**：✅ 概念對、⚠️ 呈現生  
**3 秒直覺**：「責任歸屬」名字好，但 actions 集合 / entities 集合以 comma-join 字串顯示，可讀性差。

**Benchmark**：
- Salesforce Audit Trail `Who did what to what` view：每 actor 可 drilldown 到 action histogram、entity list pagination。
- 本頁 entities 集合無上限展示，大 actor 會把列撐超高。

**建議**：
1. actions 集合改 chip list（每個 action 一個 chip + count）；entities 集合用 "顯示前 5 + 更多" expander。
2. 排序預設 by eventCount desc；內建 sort。
3. actor 欄加 role color tag + avatar。
4. 加「檢視此 actor 的全部事件 →」按鈕。
5. 此 view 和 audit events view 是互補，建議做 **toggle tab（Events ↔ Responsibilities）** 而非獨立頁。

---

## 24. `/admin/analytics`

**Components**：PageHeader、filter Card（fromEpochDay / toEpochDay）、指標定義 Card、Tabs（商家 / 廠區 / 時間分解 + table per tab）。

**合理性**：⚠️  
**3 秒直覺**：寫「營運分析儀表板」但全是表格，無一張圖；又是 epoch day。

**Benchmark**：
- **Stripe Sigma / Analytics**、**UberEats Merchant Analytics**：大 KPI 行 + 時間序列 chart + 表格 drilldown；定義通常在 `(i)` tooltip。
- DoorDash Dashers Ops：有 **heatmap by hour-of-day × day-of-week**。

**建議**：
1. 加 1–2 張關鍵 chart（時間序 line、vendor top-N bar），表格保留做 drilldown。
2. 指標定義收成 header `(i)` tooltip 或右側可展開側欄。
3. date filter 換 range picker + preset。
4. vendor/plant breakdown 表格要能 **排序 + click row drilldown**（跳 vendor detail 並帶區間）。
5. 加 CSV export 和「分享這個 view」可複製 URL。

---

## 跨頁 / 系統層 findings（管理員端整體）

**A. 「Inbox」是名義不是實作**：`/admin` 是 KPI hub。真正「今天該處理什麼」的 inbox 應該是：一條條可 inline Acknowledge / Assign / Snooze 的 queue（merged from 待審 vendors、open alerts、open disputes、overdue lifecycle tasks），Stripe / Uber / Opsgenie 都長這樣。

**B. 簽核（ISS-003、ISS-007）UX 比 Stripe dispute 差兩代**：目前是 text input 打 "ISS-003" 字串做 checklist。Stripe dispute 審核是 side panel with evidence + big "Submit" button + re-echo amount confirm modal。建議：checkbox「我已確認取得 ISS-003/007 簽核」+ 顯示簽核單位/人/時間。

**C. 月結流程 vs Stripe / Chargebee**：本系統 close page 把關帳和 pageSize/sortBy 混在一起，複雜度 3 倍於需要。Stripe close period 是 4 步 wizard，Chargebee close invoice run 是單按鈕 + preview。建議做 wizard（見 §11）。

**D. Audit 查詢無 pivot/facet/saved view**：目前是 filter form + table。Salesforce、Honeycomb、Grafana Loki 都有 facet sidebar + saved search + export + correlation drilldown。

**E. 缺 bulk actions / quick filter / saved views 系統性問題**：24 頁中無一頁有 multi-select、無一頁有「我的 / 待處理 / 全部」saved view、無一頁有 column chooser / CSV export。**後台最基本三件套全缺**。

**F. 金額 / 日期 / ID 排版 scannable 度低**：
- 金額：`totalAmountMinor` raw 數字直接顯示於 close/dispute 頁；有 MoneyAmount primitive 卻沒用。
- 日期：全系統混 `formatTaipeiDateTime`（可讀）和 `epochDay` / `minuteOfDay`（不可讀）兩套，filter 多半是後者。
- ID：vendorId / alertId / ruleId 常 mono font 同字級混在 name 旁，搶走焦點；Stripe pattern 是 ID 小一級 + 灰字 + hover tooltip copy。

**G. 大量「進去才發現缺 context」反 pattern**：vendor review、dispute detail、anomaly evaluate 進入時都不帶 record 全景；使用者要做決策卻看不到上下文。原因是後端把每個 operation 做獨立 POST endpoint，前端也跟著 1-page-1-action，忽略 Stripe / Salesforce 的「record-first, action-second」規律。

**H. localStorage 當資料源**：settlement/cycles、settlement/disputes、analytics 「最近關帳摘要」都從 `readRecentSettlements()` 讀 browser 儲存。合規後台不應如此 — 換瀏覽器就失憶，且有留痕矛盾。應補 server-side list。

**I. 正面值得保留**：
- `ConfirmDialog` 在 close / CLOSE anomaly 有用上，符合 destructive action 標配。
- `state-tag` / `money-amount` / `page-header` 等 primitive 設計正確，只是使用率不均。
- append-only trace（review history、lifecycle history、anomaly trace）後端語意完整，前端只需補 timeline viz。
- dry run 模式（compliance/lifecycle）是少見的 preview 設計，應推廣到 close settlement。
