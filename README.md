# T-Bite Corporate Catering System

企業合作訂餐平台 · monorepo skeleton (P0)。
A monorepo skeleton for a corporate catering platform: SvelteKit frontends, a Go modular monolith API, and a dual Kubernetes overlay (single-node / GCP).

## Overview

本 repo 採 monorepo 架構，分為三個面向使用者的 SvelteKit 2 + Svelte 5 前端（員工 / 商家 / 福委會），一個 Go 1.23 modular monolith API service，與兩套 kustomize overlay（single-node k3d 用於本機與 self-host；gcp 綁定 Cloud SQL / Memorystore / GCS）。
P0 階段交付骨架：dev server 起得來、`/healthz` 回 200、overlay 能 render。業務邏輯由 P1+ 逐 phase 補上。

## Repo layout

```
.
├── apps/                 # SvelteKit 前端
│   ├── employee/         # 員工點餐 (port 5173)
│   ├── merchant/         # 商家後台 (port 5174)
│   └── admin/            # 福委會治理 (port 5175)
├── packages/             # 共用 workspace package
│   ├── tokens/           # 設計 token + Tailwind preset
│   ├── ui/               # 共用 Svelte 元件
│   └── api-client/       # OpenAPI 生成 client (P1 填入)
├── services/
│   └── api/              # Go modular monolith (role=api|worker|scheduler)
├── ops/
│   ├── kubernetes/
│   │   ├── base/
│   │   └── overlays/{single-node,gcp}
│   └── observability/
├── migrations/           # golang-migrate SQL 檔
├── docs/
│   └── plans/            # 設計文件與 phase 計畫
├── contract/             # OpenAPI artifacts (P1 生成)
└── scripts/
    ├── dev/              # dev-up / dev-app / dev-down / dev-reset
    └── db/               # migrate 包裝
```

## Tech stack

- **Workspace**：`pnpm` 9 workspace
- **Frontend**：Svelte 5 + SvelteKit 2 (adapter-node) + Tailwind 3
- **Backend**：Go 1.23 + `chi` router + `golang-migrate`
- **Infra**：`kustomize` + `k3d`（local single-node）/ GKE（gcp）
- **CI**：GitHub Actions（lint/test, overlay render, image build）

## Quick start (local dev)

需要先安裝：Node 20.11+、`pnpm` 9、Go 1.23、Docker。
完整 K8s 模式另需 `k3d`、`kubectl`、`kustomize`。

### Option A：`make dev-app` — 本機 process（最快）

啟動 Go API + 三個 SvelteKit dev server，不需 K8s：

```bash
pnpm install
( cd services/api && go mod download )
make dev-app
```

之後可開啟：

- `http://localhost:8080/healthz` — Go API
- `http://localhost:5173` — 員工 app
- `http://localhost:5174` — 商家 app
- `http://localhost:5175` — 福委會 app

### Option B：`make dev-up` — k3d cluster（接近 prod）

```bash
make dev-up        # 建立 k3d cluster、apply single-node overlay
make dev-down      # 收掉
make dev-reset     # dev-down + 清 volume + 重起
```

P0 階段 overlay 內含 Postgres / Redis / NATS / MinIO 的 single-pod 版（HA 留 P8）。

### Option C：手動跑單一 app

```bash
pnpm --filter @tbite/employee dev
( cd services/api && go run ./cmd/tbite --role=api )
```

## Common make targets

| target | 說明 |
| --- | --- |
| `make dev-up` | 建立 k3d cluster 並 apply single-node overlay |
| `make dev-down` | 收掉 k3d cluster |
| `make dev-app` | 平行起 Go API + 3 個 SvelteKit dev server |
| `make migrate-up` | 套用待執行的 migration |
| `make contract-sync` | 由 Go 重新生成 OpenAPI 與 TS client（P1 填入） |
| `make test-go` | `go test ./...` |
| `make test-web` | `pnpm -r check && pnpm -r lint` |
| `make render-overlay env=single-node` | `kustomize build` 該 overlay |
| `make render-overlay env=gcp` | 同上，gcp overlay |

完整列表跑 `make help`。

## Architecture

整體設計文件：[`docs/plans/2026-05-13-tbite-refactor-design.md`](docs/plans/2026-05-13-tbite-refactor-design.md)（模組劃分、資料流、SLO、安全模型）。

## Phases

本 repo 採分 phase 推進。P0 為骨架，P1+ 逐步補業務功能：

- **P0**：monorepo skeleton（本 phase）—— [`docs/plans/2026-05-13-p0-skeleton.md`](docs/plans/2026-05-13-p0-skeleton.md)
- **P1**：identity + OIDC + 員工登入流
- **P2**：Postgres schema 第一波 + menu / quota / vendor
- **P3–P8**：見設計文件 §15

## Contributing

- 從 `main` 切 branch：`feat/<scope>-<topic>`（例：`feat/identity-oidc`）
- PR 前本地跑 `make test-go && make test-web`
- 一個 PR 對應一個 phase task；commit message 採 conventional commits

## License

Internal / TBD.
