# Plant Registry + Image Object Storage — 設計文件

日期：2026-05-25
範圍：兩條互相獨立的工作流，各開一個 worktree 平行實作。

---

## 背景與現況（探勘結論）

- **plant（廠區/廠/領餐點）目前是一個 `TEXT` 字串**，沒有獨立 table。格式 `<site>-<building>-<floor>`（如 `hc-12a-1f`）。
  同一個字串同時扮演「廠區/廠/領餐點」，階層只存在於命名慣例。
- 商家 ↔ plant 是多對多，存在 `vendor_plant_mapping`（`vendor_id`, `plant`, `active`, `service_window`）。
- 目前商家**不能自選** plant——是福委會（`welfare_admin`）在 approve 時用 `plants[]` 指派。
- 員工 app（`apps/employee/src/lib/plants.ts`）與 admin app 把 `tn-a..tn-d` **硬編碼**，與 tsmc seed 的 `hc-12a-1f` 新格式對不上。
- 圖片目前 seed 成 SvelteKit **static 路徑**（`/brand/items/i001.jpg`），`blob_uri` 原樣當 `<img src>`。
- S3/MinIO 基礎設施已齊全：`services/api/internal/platform/storage/s3.go`（`PutObject`/`PresignedGet`/`PresignedPut`），docker-compose + k3s chart 都有 MinIO，bucket `tbite-prod`（內部 service `minio.tbite.svc.cluster.local`）。
- **商家 live 上傳目前是壞的**：`ImageUploader.svelte` → `/api/uploads` proxy → `POST /api/merchant/uploads`（multipart），但 Go 端只有 `/api/merchant/uploads/presigned`，沒有那個 multipart 端點。

## 已確認的關鍵決策

1. **圖片服務模式**：對外公開 MinIO + 瀏覽器直連 public-read bucket。`blob_uri` 存完整公開 URL，API 不碰 bytes。
2. **範圍**：連 live 上傳一起修，讓 seed 灌圖與商家上傳走同一條 pipeline。
3. **URI 可攜性**：committed seed SQL 用佔位符 `__ASSET_BASE__/...`，seed 時以 `S3_PUBLIC_BASE_URL` + bucket 替換成完整 URL；live 上傳由 API 組完整 URL 後寫入。DTO 與前端 `<img src>` 維持 verbatim，不改動。

---

## Stream A：Plant Registry（吞掉原 Task 1 + Task 2）

### 資料模型
新 migration `000018_plant_registry.up/down.sql`：
```sql
CREATE TABLE plant (
  code       TEXT PRIMARY KEY,            -- e.g. "hc-12a-1f"
  label      TEXT NOT NULL,               -- 顯示名稱，如 "新竹 Fab 12A 1F"
  address    TEXT NOT NULL DEFAULT '',    -- 福委會輸入的地址，給商家看「送到哪」
  active     BOOLEAN NOT NULL DEFAULT true,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```
- 同一 migration backfill：從現有 `vendor_plant_mapping` 的 distinct `plant` 插入 registry（`label=code`、`address=''` 佔位）。
- 之後對 `vendor_plant_mapping.plant` 加 FK → `plant(code)`（backfill 後才加，避免破壞既有資料）。

### 後端（Go / Huma）
- 新 endpoints（福委會）：`GET /api/admin/plants`、`POST /api/admin/plants`、`PUT /api/admin/plants/{code}`（改 label/address/active）。
- 共用：`GET /api/plants`（authed，回傳 active plants 含 label/address）— 員工、商家共用。
- 商家自選：`GET /api/merchant/plants`（我目前綁的）、`PUT /api/merchant/plants`（body `plants[]`，驗證都在 registry 且 active，再呼叫既有 `Plants.Set(myVendorID, plants)`）。
- 權限沿用 `requireAdmin` / `requireVendor`（`vendors/http/handlers.go`）。
- registry 可放新 package `services/api/internal/plants/` 或併入 `vendors`；商家 set 沿用 `vendor_plant_mapping` repo。

### 前端
- **admin**：新增 plant registry 管理頁（列表 + 新增 + 編輯 label/address/active）。`vendors/[id]` approve 頁與 dashboard 的 `KNOWN_PLANTS` 改吃 `GET /api/admin/plants`。
- **merchant**：新增「服務廠區」頁（多選 active plants，顯示 label + address，可存、可改）。
- **employee**：`lib/plants.ts` 硬編碼改成 fetch `GET /api/plants`；連動 `+layout.svelte`、`+page.server.ts`。

---

## Stream B：圖片 Object Storage（原 Task 3）

### Config
- `services/api/internal/config/config.go` 新增 `S3PublicBaseURL`（env `S3_PUBLIC_BASE_URL`）。
  - 本地 dev：`http://localhost:9000`；prod：對外 MinIO tunnel host。
  - 完整公開 URL = `{S3_PUBLIC_BASE_URL}/{bucket}/{key}`（MinIO path-style）。
- `.env.example`、chart configmap/values 同步新增。

### 修好 live 上傳
- 實作缺的 `POST /api/merchant/uploads`（multipart，vendor-scoped）：沿用 `validateImageUpload` → `Storage.PutObject(key, ...)`（key = `menu-images/{vendorID}/{uuid}.{ext}`）→ 回傳 `{url: 完整公開URL}`。`ImageUploader.svelte` 即可正常運作。

### Seed 灌圖
- 新增 seed 資產上傳步驟（建議 `scripts/db/seed-assets.sh`，用 `mc` 或 aws-cli，**冪等**）：把 `apps/employee/static/brand/**`（與 merchant 重複）上傳到 bucket 的 `brand/` 前綴（`brand/items/i001.jpg`、`brand/logos/r001.png`、`brand/stores/r001-cover.jpg`、`brand/categories/taiwanese.jpg`）。
- `scripts/dev/gen-seed.py`：圖片路徑改輸出佔位符 `__ASSET_BASE__/brand/...`；`scripts/db/seed.sh` 在套用前 `sed` 把 `__ASSET_BASE__` 換成 `${S3_PUBLIC_BASE_URL}/${S3_BUCKET}`。涵蓋 `menu_item_image.blob_uri`、`vendor.cover_image_uri`、`vendor.logo_uri`、`cuisine_category.banner_uri`。
- 重新產生 `scripts/dev/seed-p2.sql`。

### 基礎設施（chart）
- 對外公開 MinIO：在 cloudflared/ingress 加一條 route（如 `files-*` host → minio svc:9000）。
- bucket 設 public-read（`mc anonymous set download`，可放在既有 minio setup job 或 seed-assets）。
- 標註：套用到 live CD box 是獨立步驟。

---

## 平行化與整合
- 兩 stream 互不重疊：A 動 `vendors`/新 `plants`/三個前端的 plant 選單/migration 000018；B 動 `menu/http`/`config`/seed/chart/`ImageUploader`。
- 已知整合衝突：`contract/openapi/*` 與 `packages/api-client/src/schema.d.ts`（兩邊都會重生）→ 合併後在整合端**重生一次**即可。
- 各 worktree 自行：`go build ./...`、相關 `go test`、前端 `check`/build、commit 到各自 branch。
