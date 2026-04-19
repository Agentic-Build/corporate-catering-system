# UX Audit — 員工端（10 routes）

> Audit agent 原始報告。基準平台：UberEats / foodpanda / DoorDash / 美團 / 餓了麼 / LINE MAN / Deliveroo。
> 每頁結構：Components 盤點 → 合理性 → 第一直覺 → 資訊層級 → benchmark → 優化建議。

---

## `EMP-01` 今日 Home（`/employee`）

**目的**：讓員工一打開就知道「今天要不要領餐、有沒有快截單的菜單、最近訂單狀態」。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `PageHeader`（eyebrow + 問候 + 說明） | 標題區、歡迎訊息 | ✅ |
| `Card: 今日待領取` | 今天可領餐訂單 | ✅ |
| 待領取內部 `article`（orderId + 配送日 + 品項數 + `StateTag` + `MoneyAmount` + 「顯示領餐 QR」按鈕） | 單筆可行動卡 | ⚠️ 主資訊是 `orderId`（技術 ID），員工認不得 |
| `EmptyState`（今日沒有待領取） + 瀏覽菜單 CTA | 無資料引導 | ✅ |
| `Card: 即將截單` | 預購截單提醒 | ✅ |
| 截單內部 `article`（菜名 + 配送日 + 剩 N + `CountdownBadge` + `MoneyAmount`） | 單筆菜單預告 | ⚠️ 沒有「下單」按鈕，只能點「看全部菜單」再找一次 |
| `Card: 帳務摘要` | 最近 5 筆訂單 | ⚠️ 標題叫「帳務摘要」但內容是「訂單列表」，語意不匹配 |
| 「查看所有訂單」 / 「查看扣款」兩顆底部按鈕 | 導流 | ✅ |

### 第一直覺檢驗
**半是半否**。3 秒內能看到「今日待領取」很好，但首卡若空、第二卡是截單預告、第三卡叫「帳務摘要」卻塞訂單，新使用者會困惑「這裡是 dashboard 還是訂單列表？」。另外整頁訊息密度偏低，沒有「今天幾點吃、哪裡領」提示。

### 資訊層級
`PageHeader` 很強，但三張 `Card` 視覺份量一致（同 variant、同 padding），無法凸顯「今日待領取」是首要任務。`orderId` 用 `font-semibold text-slate-900` 是全頁最強字，實際員工最不需要 orderId。

### 外送平台 benchmark
- **UberEats**：首頁永遠是「Reorder / Your recent orders」橫向 carousel + 推薦餐廳，強調圖片與店名；我們沒有圖片、沒有店名。
- **foodpanda**：最上方「進行中訂單」橫幅，直接顯示預計送達時間 + 點擊追蹤；我們把「今日待領取」放在卡裡而非 hero 橫幅。
- **DoorDash**：Home 的 Reorder tile 用大圖、商家名、一鍵 reorder；我們只有文字 + 訂單 ID。
- **美團**：「我的訂單」浮動入口 + 倒數計時在首屏最上；我們的 `CountdownBadge` 在第二張卡內。
- **LINE MAN**：首頁把「推薦下一餐」做成個人化卡片。我們的「即將截單」類似，但缺「直接下單」按鈕。

### 具體優化建議
1. 把「今日待領取」從第一張 `Card` 升級為**全寬 hero 區塊**（類似 foodpanda 進行中訂單橫幅），移除 `orderId`，改顯示「今天 12:30 · 排骨便當 x1 · 在 B 棟茶水間取餐」。
2. 把 `Card title="帳務摘要"` 改名為「最近訂單」，或拆成真正的帳務摘要（本月扣款合計、待結算筆數）。
3. 「即將截單」的每個 `article` 內補「立即下單」主要按鈕，把 H → D → 下單 3 步縮成 1 步。
4. 首屏加 Reorder（過去常吃 top 3）橫向 tile，對齊 UberEats / DoorDash。
5. `article` 內的 `order.orderId` 改為「品項 + 數量」作為主標題，`orderId` 降級成 `text-xs text-slate-500`。

---

## `EMP-02` 菜單瀏覽 + 下單（`/employee/discover`）

**目的**：員工依週 / 日曆檢視菜單，並直接下單。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `Card: 檢視與條件`（週/日曆切換按鈕 + 起訖日 + 備註輸入） | 篩選與全局備註 | ⚠️「訂單備註」放全局篩選區會誤會是篩選條件 |
| 「週檢視」/「日曆檢視」雙按鈕 | 切換模式 | ⚠️ 該用 tab / segmented control 而非兩顆 `Button` |
| 起始 / 結束日 `type="date"` | 自訂日期 | ⚠️ 沒有快捷「今天 / 明天 / 本週」chip |
| 「套用條件」按鈕 | 送出篩選 | ❌ 外送平台幾乎都是改就刷新，多一個按鈕是摩擦 |
| 菜單卡 `article`（圖片、菜名、描述、配送日、價格） | 單品呈現 | ✅ |
| `StateTag: 剩 N` + `可下單/已關閉` + `CountdownBadge` | 狀態與急迫感 | ✅ |
| 數量 `<input type="number">`（w-24，1–20） | 選份量 | ⚠️ 應該用 +/- stepper |
| 「立即下單」按鈕 | 直接下單 | ⚠️ 一鍵下單沒有 review / 確認，誤按代價高（會扣薪） |
| `Card: 日期分組預覽` | 以日期分組統計 | ❌ 只顯示「N 筆可見項目」沒有任何可行動價值 |

### 第一直覺檢驗
**否**。打開後第一屏是「檢視與條件」表單而非菜，資訊反轉 — 應該先看到菜。此外「訂單備註」放 header 區會困惑「這是全局或單筆備註」。

### 資訊層級
菜單卡設計不差（圖 → 名 → 說明 → 價格 → 狀態 → 數量 → 按鈕），但整頁被篩選 `Card` 壓頂，視覺重量失衡。

### 外送平台 benchmark
- **UberEats**：菜品卡標準是「大圖 + 菜名 + 星級 + 價格 + 右下 `+` 按鈕」，點 `+` 後進 bottom sheet 選份量 / 客製化，**不是一鍵下單**；我們少了 bottom sheet。
- **foodpanda**：有「加入購物車」而非「立即下單」，可多品一起結帳；我們缺購物車概念。
- **DoorDash**：價格下方小字「截單時間」以紅字提醒；我們用 `CountdownBadge` ✅。
- **美團 / 餓了麼**：菜品卡右下永遠有數量 stepper（`- 數字 +`），number input 在行動端幾乎沒人用；我們反慣例。
- **Deliveroo**：所有篩選變動即時刷新，沒有「套用」按鈕；我們是反慣例摩擦。

### 具體優化建議
1. 把「週 / 日曆 + 日期 + 備註」的 `Card` 收折進 `PageHeader` 的 filter bar，或做成 sticky top；首屏優先顯示菜。
2. 數量 `<input type="number">` 換成 stepper 元件（`- [1] +`）。
3. 移除「套用條件」按鈕，改 `$effect` 自動 refresh。
4. 「立即下單」改「加入當日訂單」+ 底部浮動 CTA「下 N 筆訂單（合計 $X）」；至少在 `立即下單` 前插 confirm dialog。
5. 刪除「日期分組預覽」卡，或改成日期 pill 導航（點跳到當日菜單 anchor）。
6. 把「訂單備註」從全局篩選區移到下單 confirm dialog 裡（per-order）。

---

## `EMP-03` 訂單列表（`/employee/orders`）

**目的**：查看所有訂單，可依狀態與日期篩選，並從列表直接領餐。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `PageHeader` | 標題 | ✅ |
| `Card: 篩選條件`（狀態 `<select>` + 起訖日 + 套用） | 篩選 | ⚠️ 同 EMP-02，「套用」是多餘摩擦 |
| 狀態 `<select>` 有 8 個選項（含 SOLD_OUT/REFUND_PENDING/REFUNDED） | 狀態篩選 | ⚠️ 員工不熟這些內部狀態名，應該分群（進行中/已完成/有爭議） |
| `DataTable`（5 欄：訂單編號/配送日/狀態/金額/操作） | 列表 | ⚠️ 第一欄是 `orderId`，員工認不得 |
| 每列操作：「詳情」+「領餐」（條件） | 列內動作 | ✅ |
| `EmptyState`（尚無訂單 → 瀏覽菜單） | 空狀態 | ✅ |

### 第一直覺檢驗
**是，有條件地**。知道自己在「我的訂單」頁，但「訂單編號 EMP-xxx」作為第一欄沒有記憶錨點；應該是日期/菜名為主、編號次要。

### 資訊層級
`DataTable` 風格平整，所有欄位視覺權重相同，無法一眼找到「今天的那筆」。沒有群組（本週/上週/上月），也沒有排序切換 UI。

### 外送平台 benchmark
- **UberEats / DoorDash**：訂單歷史永遠是「縮圖 + 商家名 + 日期 + 狀態 tag + reorder 按鈕」，從不顯示訂單 ID；我們顯示 ID 是 B2B 思維污染了 C 端 UI。
- **foodpanda**：以「進行中 / 過去訂單」tab 分群，配縮圖 + 總金額 + 「再點一次」；我們完全用 `<select>` 控制。
- **美團**：列表有「再來一單」「開發票」「評價」「申訴」多個 inline action；我們只有「詳情」「領餐」。
- **Deliveroo**：預設顯示過去 3 個月，再往前需點「載入更多」；我們一次 fetch 200 筆、沒有分頁 UI。

### 具體優化建議
1. 用 tab 或 segmented（`進行中 / 已完成 / 有爭議`）取代 `<select>`，內部狀態 mapping 到這三群；保留 `<select>` 作進階。
2. `DataTable` 在行動端會橫向擠壓，考慮改用卡片列表（foodpanda 風格）。
3. 移除「套用」按鈕，讓 select / date onchange 立即 refresh。
4. 加一個「再點一次」action（呼叫 EMP-02 的 create API 預填），外送 app 標配。
5. `orderId` 降級為 `text-xs text-slate-500`，主欄改為「配送日 · 品項摘要」。

---

## `EMP-04` 訂單詳情（`/employee/orders/[orderId]`）

**目的**：呈現單筆訂單所有細節與可用動作。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `PageHeader`（title = orderId） | 頁面識別 | ⚠️ 技術 ID 當標題 |
| `Card: 訂單概要`（4 欄：狀態/配送日/建立時間/金額） | 關鍵摘要 | ✅ |
| `Card: 品項` + `DataTable`（品項/數量/單價/小計） | 明細 | ⚠️「品項」欄顯示 `menuItemId` 而非菜名 |
| `Card: 時間軸` + `ol` | 狀態變化歷史 | ⚠️ 顯示 `eventType` 英文原文（如 `ORDER_CREATED`） |
| `Card: 可用動作`（修改/取消/領餐 QR/申訴/扣款明細） | 動作 hub | ✅ |

### 第一直覺檢驗
**否**。標題是一串 orderId，「品項」表裡是 menuItemId 而不是菜名，員工會懷疑「我點的什麼菜？」— 這是最嚴重的可用性問題。

### 資訊層級
4 格概要排得整齊，「狀態」用 `StateTag` + 「配送日」/「金額」並列是對的。問題在下方「品項」表，欄位 id 全是技術字串。

### 外送平台 benchmark
- **UberEats**：訂單詳情永遠有「餐點縮圖 + 菜名 + 數量 + 客製化選項 + 小計」，絕不顯示 ID。
- **foodpanda**：「訂單時間軸」用中文人話 + icon（已建立 → 餐廳接單 → 製作中 → 外送中 → 送達）；我們用 `ORDER_CREATED` 原文。
- **DoorDash**：詳情頁底部永遠有一個大「Help」按鈕；我們有「提交申訴」，但跟其他 4 顆按鈕並排沒有優先級。
- **美團**：詳情頁「聯絡商家」「聯絡騎手」inline，我們沒有聯絡廠商的能力。

### 具體優化建議
1. `PageHeader` title 改為「{配送日} {品項摘要}」，orderId 降級為 eyebrow 或 meta 資訊。
2. 「品項」表第一欄改用菜名（若 API 未帶，應在 `EmployeeOrderView` 加欄位或於 BFF join）。
3. 「時間軸」event type 透過 `friendlyEventType()` mapping 成中文。
4. 「可用動作」5 顆按鈕權重調整 — 當前狀態下最可能的動作用 `primary`，其他降 `ghost`。
5. 品項表若只有 1 筆，可改為內嵌卡片顯示。

---

## `EMP-05` 修改訂單（`/employee/orders/[orderId]/edit`）

**目的**：調整訂單內各品項的數量（1–20）並送出修改。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `PageHeader`（`截單前可改`） | 引導 | ✅ |
| `Card: 目前狀態不可修改`（PENDING/MODIFIED 以外） | 狀態守門 | ✅ |
| 品項列 grid（菜名 + 單價 + 數量輸入 + 小計） | 逐項編輯 | ⚠️「菜名」欄仍是 `menuItemId` |
| `FormField label="數量（1–20）"` + `number input` | 改數量 | ⚠️ 該用 stepper |
| 「送出修改」/「取消並返回」按鈕 | 送出 | ⚠️ 沒有 diff preview |
| 沒有「新增品項」/「刪除品項」 | 編輯範圍 | ❌ 只能改數量 |

### 第一直覺檢驗
**是**。顯示「修改訂單 · EMP-xxx」+「調整數量 1–20」很清楚，但同樣面對 menuItemId 問題。

### 資訊層級
表格三欄 grid 合理。但送出前沒有「原金額 → 新金額」對比，員工不知道多花多少。

### 外送平台 benchmark
- 一般外送平台送單後**不能修改**，只能取消重下；我們的「修改」是 B2B 預購場景獨有。
- 若對標購物車：UberEats/foodpanda 都有「數量 stepper + 刪除 + 新增」，我們只有改數量。

### 具體優化建議
1. 加「新增品項」按鈕（連到 discover 當日菜單），和「刪除此品項」。
2. 數量改 stepper。
3. 底部加「變動預覽」：`原金額 $200 → 新金額 $300（+$100）`。
4. 菜名欄改顯示真名，不是 menuItemId。
5. 「送出修改」按鈕用 `sticky bottom` 在手機上常駐。

---

## `EMP-06` 取消訂單（`/employee/orders/[orderId]/cancel`）

**目的**：截單前輸入原因並取消訂單。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `PageHeader` | 標題 | ✅ |
| `Card: 目前狀態不可取消` 警示 | 狀態守門 | ✅ |
| `Card: 取消原因` + `FormField` | 輸入原因 | ✅ |
| `textarea`（maxlength=200，min-h-32） | 文字輸入 | ✅ |
| 「確認取消」按鈕（disabled until 5 字） | 送出 | ✅ |
| `ConfirmDialog`（二次確認） | 避免誤操作 | ✅ |
| 沒有「退款金額預告」區塊 | 用戶疑問點 | ⚠️ 取消後到底退多少、何時入帳？沒說 |

### 第一直覺檢驗
**是**。Header + 輸入框 + 紅色「確認取消」一眼看懂。是本套路由裡最對味的頁面之一。

### 資訊層級
清晰：標題 → 原因輸入 → 紅色 primary action → ghost 返回；ConfirmDialog 再一層保險。

### 外送平台 benchmark
- **UberEats**：取消前會顯示「取消費用 $X / 可退金額 $Y」；我們 0 資訊。
- **foodpanda**：取消原因是單選 radio（送錯 / 等太久 / 其他），不是自由文字。
- **DoorDash**：取消流程 2 步（原因 → 確認），還會顯示「訂單已退款」結果頁。

### 具體優化建議
1. 輸入框上方加「取消後將退款 $200 到薪資扣款（下次結算抵扣）」預告卡。
2. 原因改成 radio + 其他文字，預設：「當天被派外出」「臨時改約」「訂錯品項」「其他」。
3. 取消成功後在詳情頁 toast 外，加結果 banner「已取消，預計 $X 將於 YYYY-MM-DD 退回」。
4. 「至少 5 字」在 radio 模式下可放寬。

---

## `EMP-07` 領餐 QR（`/employee/orders/[orderId]/pickup`）

**目的**：在領餐點出示動態 QR 供現場人員掃描核銷。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| 窄寬 `max-w-md` 容器 | 手機優先版型 | ✅ |
| `PageHeader` 說明 `每 30 秒刷新` | 預期管理 | ✅ |
| 狀態守門（找不到 / 不可領餐） | 防呆 | ✅ |
| QR `<img>`（320px，white bg border） | 主要物件 | ✅ |
| `verificationCode` 文字 | 後備識別 | ✅ |
| 倒數提示「QR 更新倒數 N 秒」 | 狀態回饋 | ✅ |
| 「立即刷新 QR」按鈕 | 手動刷新 | ⚠️ `primary` 權重過強 |
| 「完成領餐核銷」按鈕 | 員工自核銷 | ⚠️ 按了代表員工自己核銷（繞過現場掃碼？）語義模糊 |
| 亮度提升、防鎖屏、震動等 | 領餐實體場景 | ❌ 缺失 |

### 第一直覺檢驗
**是**。一眼 QR + 倒數，目的極清楚。是本套路由裡體驗最接近成熟外送平台的頁面。

### 資訊層級
QR 居中、倒數置中 amber 色，層級 OK。兩顆按鈕同寬 `fullWidth`，但主色 / 次色分配錯 — 刷新不該是 `primary`。

### 外送平台 benchmark
- **UberEats / DoorDash 取餐碼**：永遠是四位數字大字（50–80px），不用 QR；我們 QR + 驗證碼並陳是對的，但驗證碼字體太小（`text-xs`）。
- **foodpanda pickup**：進入時自動把螢幕亮度拉滿、加入 wake lock；我們沒做。
- **LINE Pay / 7-11 取貨碼**：點 QR 會放大全螢幕；我們不能放大。
- **美團取餐碼**：顯示「店員將輸入此 4 位碼」而非要求員工自核銷；我們的「完成領餐核銷」按鈕是反慣例。

### 具體優化建議
1. `verificationCode` 字體從 `text-xs` 放大到 `text-3xl tracking-widest font-mono`。
2. 進入此頁時呼叫 `navigator.wakeLock.request('screen')` 與最大化亮度，離開時釋放。
3. 「立即刷新 QR」降級 `secondary`；「完成領餐核銷」改成僅在「demo / 自助場景」顯示。
4. QR 點擊可全螢幕放大。
5. 倒數剩 5 秒時加 `StateTag tone="warning"` 視覺強化。

---

## `EMP-08` 提交申訴（`/employee/orders/[orderId]/dispute`）

**目的**：針對有扣款爭議的訂單提交申訴並追蹤進度。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `PageHeader` | 標題 | ✅ |
| `Card: 申訴原因` + `FormField required` + `textarea` | 輸入 | ⚠️ 沒有 maxlength、placeholder 只是範例 |
| 「送出申訴」按鈕（disabled 直到輸入內容） | 送出 | ⚠️ 只要 1 字就能送，無最小長度 |
| `Card: 申訴追蹤`（既有申訴列表） | 歷史進度 | ✅ |
| 單筆 dispute `article`（id + status tag + 負責人 + 時間 + `trace`） | 進度細節 | ⚠️ `disputeId`、`ownerActorId` 技術 ID 為主 |
| 沒有「訂單摘要」/「扣款金額」區塊 | 上下文 | ❌ 看不到「我在申訴哪一筆、金額多少」 |
| 沒有附件 / 截圖上傳 | 證據 | ❌ 爭議申訴無圖真相難判 |

### 第一直覺檢驗
**是**。「提交申訴」四個字 + 原因 textarea 一眼懂，但缺少上下文讓人沒安全感（「我在申訴的到底是哪筆訂單？什麼內容？」）。

### 資訊層級
兩卡並陳 OK，但「申訴追蹤」裡 `dispute.disputeId` 作主標、`ownerActorId` 顯示原始 id（沒 mapping 成人名），都是技術穿透。

### 外送平台 benchmark
- **UberEats help center**：申訴前先問分類（品項少了 / 拿錯 / 冷掉 / 其他），逐步引導；我們直接讓員工寫自由文字。
- **foodpanda**：可上傳照片佐證；我們沒有。
- **DoorDash**：退款進度條「提交 → 審核中 → 已解決 → 退款到帳」；我們的 `trace` 用文字列。
- **美團**：會顯示「預計 3 個工作日回覆」期望值；我們沒有 SLA 指示。

### 具體優化建議
1. 頁面最上加「你正在申訴的訂單」摘要 card（品項 + 金額 + 配送日 + 目前狀態）。
2. 申訴原因改成分類（radio）+ 自由補充。
3. 加附件上傳（若 API 尚未支援，先 UI 佔位）。
4. 申訴進度 `trace` 改成 stepper / timeline 視覺元件。
5. 顯示 SLA 期望：「福委會通常於 3 個工作日內回覆」。
6. `ownerActorId` 透過 actor lookup mapping 成人名。

---

## `EMP-09` 薪資扣款總覽（`/employee/wallet`）

**目的**：彙整所有訂單的扣款總額、申訴狀態，並選擇某筆看明細。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `Card: 帳務摘要`（4 格：已載入訂單/淨扣款/進行中申訴/已結案申訴） | 關鍵指標 | ⚠️「已載入訂單」不是業務指標，是前端 lazy-load 副作用 |
| 左右分欄 `lg:grid-cols-[1.1fr,2fr]` | 列表 + 明細 | ⚠️ 右欄永遠顯示 EmptyState，浪費 60% 螢幕 |
| 左欄訂單 `<a>` 連結列表 | 選單 | ✅ |
| 右欄 placeholder `Card` | 預留 | ❌ master-detail 在同一頁但 detail 是另一個路由，點下去整頁跳走 |
| 沒有月份 / 時段篩選 | 薪資場景 | ❌ 薪資扣款天生按月結算 |
| 沒有 export CSV / 下載薪資單 | B2B 需求 | ⚠️ 員工可能想匯出報稅 |

### 第一直覺檢驗
**否**。「已載入訂單」讓人困惑；右半邊空卡「點了還沒反應？」；使用者看到「淨扣款」是「已載入前 3 筆」的總和，而不是真實總額，誤導。

### 資訊層級
4 個 stat card 排得整齊，但半數是假指標。版面 1.1 : 2 但右邊 0 內容。

### 外送平台 benchmark
- **UberEats wallet**：顯示「本月已花」+「交易記錄列表」，每筆可點進詳情。
- **美團錢包**：月份切換 + 類別分類 + 匯出。
- **LINE Pay 交易紀錄**：tab（全部/收入/支出）+ 日期範圍。
- **銀行 app 對帳單**：永遠有「當月總支出」「上月」「近 6 月趨勢圖」。

### 具體優化建議
1. 移除「已載入訂單」stat，改為「本月扣款合計」+「上月扣款」+「進行中申訴」+「已結案」。
2. 加月份 picker（預設本月）。
3. 右欄 placeholder 砍掉，改單欄 + 列表。
4. 加「匯出 CSV」按鈕。
5. 「淨扣款總額」旁邊註明「基於當前篩選條件」。

---

## `EMP-10` 扣款明細（`/employee/wallet/[orderId]`）

**目的**：顯示單一訂單的薪資流水、退款事件與申訴追蹤。

### Components 盤點
| 區塊 | 目的 | 評級 |
|---|---|---|
| `PageHeader` + 「提交新申訴」action | 快速跳轉 | ✅ |
| `Card: 訂單概要`（訂單/配送日/淨扣款） | 基本資訊 | ⚠️ 無品項摘要，看不到「原訂單金額 vs 淨扣款」差異原因 |
| `Card: 薪資流水` + 每筆 `entry`（kind + 金額 + 時間 + 來源事件） | 流水列表 | ⚠️ `kind`、`sourceEventKind` 全是 enum 原文 |
| 單筆流水 `article`（flex 排版） | 單筆呈現 | ⚠️ `kind` 用 `font-semibold` 主標 → 但 kind 是 `DEBIT` 等英文 |
| `Card: 申訴追蹤` | 申訴進度 | ⚠️ 與 EMP-08 重複結構 |
| 沒有「扣款 vs 退款」正負視覺 | 財務可讀性 | ❌ 所有金額同色 |

### 第一直覺檢驗
**半是半否**。標題「扣款明細 · orderId」可懂，但進去看到一排 `DEBIT_PAYROLL` / `sourceEventKind` 的英文 enum 會失去方向感。

### 資訊層級
三個 `Card` 縱向排列清楚，但「薪資流水」卡裡每筆 entry 的 `kind` 太弱，金額也沒區分正負顏色。

### 外送平台 benchmark
- **銀行/第三方支付**：支出紅色/退款綠色是標配；我們所有金額同色。
- **UberEats receipt**：每筆 line item 下有清楚「Subtotal / Service fee / Tip / Total」分層；我們 `sourceEventKind` 外洩技術資訊。
- **美團帳單**：每筆交易有 icon + 中文 label；我們純文字 + 英文 enum。
- **LINE Pay 交易詳情**：顯示「原始金額 → 折扣 → 實付」推導；我們看不到「原單 - 退款 = 淨扣」推導。

### 具體優化建議
1. `entry.kind` 用 mapping 轉中文（`DEBIT_PAYROLL` → 「薪資扣款」等），依正負給 `text-rose-700` / `text-emerald-700`。
2. 「訂單概要」加兩列：「原訂單金額」→「扣除退款」→「淨扣款」推導流程。
3. 「申訴追蹤」抽成共用元件 `<DisputeTrackingCard>`，EMP-08 / EMP-10 共用。
4. `sourceEventKind` / `sourceEventReference` 降級放小註腳；主標改中文事件名 + 時間。
5. 加「下載此訂單的收據 PDF」action。

---

## 跨 10 頁共通病徵（按影響排序）

1. **技術 ID 穿透**：`orderId`、`menuItemId`、`disputeId`、`ownerActorId`、`eventType`、`kind`、`sourceEventKind` 遍佈各頁主標。需建立統一的 mapping layer。
2. **篩選需按「套用」**：EMP-02、EMP-03 都是，不符外送平台即時 refresh 慣例。
3. **缺購物車模型**：EMP-02 單品單筆下單，不能多品合併；EMP-05 修改只能改數量、不能增刪。
4. **缺正負金額語意色**：EMP-09、EMP-10 扣款與退款同色。
5. **數量輸入用 `<input type=number>`**：應用 stepper。
6. **EMP-07 QR 頁缺手機基本體驗**：亮度 / wake lock / 大字驗證碼。
7. **EMP-09 右欄永遠空白**：master-detail 結構未實作。
8. **EMP-08 申訴缺上下文與附件**。

優先級處理順序：
- `/employee/wallet/+page.svelte`（右欄空白、假指標）
- `/employee/discover/+page.svelte`（購物車、stepper、即時篩選）
- `/employee/orders/[orderId]/+page.svelte`（menuItemId 最嚴重）
- `/employee/orders/[orderId]/pickup/+page.svelte`（手機體驗 + 驗證碼放大）
- `/employee/api.ts`（擴充 friendly mapping）
