# Test coverage → 90% (進度 / 交接)

Branch: `test/coverage-90`　Worktree: `.worktrees/test-coverage-90`　Date: 2026-05-27

目標：後端 + 前端整體測試覆蓋率拉到 **90%（方向性目標**，`cmd`/glue 可低）。

## 已完成（本批，未 commit）

### Phase 0 — 可信量測工具
- `Makefile`：新增 `coverage` / `coverage-go` / `coverage-web`。
  - `coverage-go` 用 **`-p 1` 序列化**跑 `go test ./services/...`，testcontainers 一次只起一個 DB → 不再 Docker thrash / 假性 15m timeout。輸出 `coverage.out` + `coverage.html` + total。
  - `coverage-web` 跑 vitest `--coverage`。
- 前端 coverage 工具：`packages/pickup`、`apps/merchant` 加 `@vitest/coverage-v8` devDep（需 `pnpm install` 才生效）。

### Phase 1（已完成）— 後端 HTTP handler 測試（先前全 0%）
純 `httptest` + 假 repo，**完全不需 DB / Docker**，全部 `go test ./<pkg>/http/` 通過（已實測覆蓋率）：

| package | 測試函式 | 覆蓋率 |
|---|---|---|
| quota/http | 13 | 92.3% |
| compliance/http | 50 | 92.5% |
| feedback/http | 73 | 93.2% |
| payroll/http | 79 | 97.5% |
| settlement/http | 37 | 99.1% |
| dlq/http | 26 | 92.4% |
| **合計** | **278** | **92–99%** |

模式（見 `quota/http/handlers_test.go` 範本）：chi + `humachi.New` + `api.Register` + `httptest.NewServer`；用 chi middleware 呼叫 `idhttp.ContextWithUser` 注入認證 user；service 用假 repo 建構。

### Phase 2（已完成）— order / menu / vendors http
同模式再擴一批，全 Docker-free、實跑通過：

| package | 覆蓋率 | 備註 |
|---|---|---|
| vendors/http | 97.1% | 無需重構（service 無 Pool） |
| menu/http | 90.3% | 無需重構；presign/upload 卡 `*storage.S3Client` 具體型別 |
| order/http | 77.7% | 套 txBeginner 重構；SSE 串流端點只能測 auth、reorder 2xx 卡具體 pool |

9 個 http 包總覽：settlement 99.1 / vendors 97.1 / payroll 97.5 / feedback 93.2 / compliance 92.5 / dlq 92.4 / quota 92.3 / menu 90.3 / order 77.7。

### Phase 1b（已完成）— Pool→interface 重構，解鎖寫入路徑
原本寫入路徑用具體 `*pgxpool.Pool`（`pgx.BeginFunc`）無法假造。已把 `Service.Pool *pgxpool.Pool` 改成 service 內 local `txBeginner` interface（`Begin(ctx)(pgx.Tx,error)`），`*pgxpool.Pool` 自動滿足 → **cmd 接線與既有 testcontainers `service_test.go` 不用改**。測試注入 `fakeBeginner`（回傳 no-op `fakeTx{ pgx.Tx }`，Commit/Rollback→nil），假 repo 忽略 tx → 寫入 2xx 可純測。
- 改動檔：settlement/compliance/feedback/payroll 的 `service.go`（+ payroll `current_lines.go`：`QueryCurrentLines` 參數同步改 interface；+ compliance `Storage *S3Client` → `objectStore` interface 以測 upload）。
- 已驗證：`go build ./services/api/...` 通過、6 包 http 測試全綠。

## ⚠️ 順手發現（未修，建議跟進）

多個 huma input DTO 的非指標欄位未加 `omitempty`，被 huma 當**必填**（缺了就 422），但 service 視為選填 —
- quota `setCapacity`: `pickup_window` / `eta_label`
- feedback `rateOrder`: `comment`（service 允許不填）
- payroll `resolveDispute`: `refund_minor`

修法在各 `*/http/handlers.go`（改指標或加 `,omitempty`），屬 API 合約調整，本批不動。

## 量測方式（晚點一次跑）

```bash
cd .worktrees/test-coverage-90
make coverage-go          # 序列化，約數分鐘；產出 coverage.out + coverage.html
go tool cover -func=coverage.out | tail -1   # 整體 %
# 前端：
pnpm install && make coverage-web
```

## 待辦（後續批次）

- [ ] 跑一次 `make coverage-go` 取得**可信整體基準數字**（本機之前被 thrash，未取得大型 package 真實值）。
- [ ] 修 `compliance` 疑似真壞的 SERVICE 測試 `TestTriageAnomaly_SuspendCallsVendorGov`（governance_test.go，testcontainers，187s FAIL 非 timeout）— 需能跑測試時確認；注意新增的 http 層 `TestTriageAnomaly_Suspend_OK_204` 是通過的。
- [x] Phase 2：`order/http`→77.7%、`menu/http`→90.3%、`vendors/http`→97.1%。
- [x] Phase 2b（部分）：`mcpserver` 16%→94.7%（+server.go txBeginner 重構）、`plants/http` 35%→100%、`identity/http` 49%→96.7%、`menu/readmodel` 41%→82.8%（RedisCache 卡具體 redis client）。
- [x] Phase 3（postgres 整合測試，testcontainers）：plants/postgres 0→95.1%、vendors/postgres 46→88.5%、payroll/postgres 58→86.9%、order/postgres 57→83.0%。（commit b627e81）
- [ ] Phase 2b 續：`compliance/scanner` 56%、`identity/oidc` 23%、`platform/observability` 11%、`identity/hydra` 13%（外部 OAuth，低價值）、`httpserver` 49%、各 service 包內部分支（compliance 40%、order 73%…）、其餘 postgres（quota 61.5% 等）。
- 目標分母：`make coverage-go` 已改為只測 `services/api/internal/...`（排除 cmd）。
- [ ] 殘留 storage/pool-bound：menu presign/upload、order SSE + reorder 2xx — 需把 `*storage.S3Client`、reorder 的 `pool` 抽成 interface（同 txBeginner 手法）才能純測。
- [ ] Phase 3：前端 3 個 app 元件/邏輯測試（目前 ≈0%）。
- [ ] (選) 修 accidental-required DTO 欄位（見上）。
```
