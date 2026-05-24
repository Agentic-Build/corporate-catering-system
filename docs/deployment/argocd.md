# ArgoCD 持續部署（CD）

以 ArgoCD 為核心的 GitOps 部署流程：程式碼推上 `main` 後，GitHub Actions 自動 build 四個 container image 並推到 GitHub Container Registry（GHCR）；同一個 workflow 接著把該次 build 的不可變 `sha-<gitsha>` tag **git write-back** 進 single-node overlay 的 `images:`（commit 回 `main`）；ArgoCD 偵測到 overlay 的 git 變更，自動把新版同步到叢集——形成 push → 自動部署的閉環。

## 流程總覽

```
                push main / tag v*
                       │
                       ▼
        ┌───────────────────────────────┐
        │ GitHub Actions                 │
        │ cd-publish-images.yml          │
        │  build × 4 image               │
        │  push → ghcr.io/agentic-build/ │
        │         tbite-<name>           │
        │  tag: sha-<short> / latest /   │
        │       semver                   │
        ├───────────────────────────────┤
        │ job: bump-image-tags           │
        │  sed overlay images newTag     │
        │   → sha-<short>                │
        │  commit [skip ci] → main       │
        └───────────────┬───────────────┘
                        │ Git commit 觸發
                        ▼
        ┌───────────────────────────────┐
        │ ArgoCD Application             │
        │  tbite-single-node             │
        │  path = overlays/single-node   │
        │  (images 釘 GHCR sha，不覆寫)  │
        │  syncPolicy: automated         │
        │   (prune + selfHeal)           │
        └───────────────┬───────────────┘
                        │ kubectl apply（由 ArgoCD 執行）
                        ▼
                   叢集 namespace: tbite
```

設計重點：image tag 的**唯一來源**是 overlay 的 `images:`（釘不可變的 `ghcr.io/agentic-build/tbite-<name>:sha-<gitsha>`），由 `cd-publish-images` 的 `bump-image-tags` job 在每次 push `main` 時 write-back 更新。ArgoCD 直接同步該 overlay（**不**再用 `Application.spec.source.kustomize.images` 覆寫，也不依賴 argocd-image-updater）。`make dev` 是在 host 上跑 app（vite + go run），不經此 overlay，因此本地開發不受影響。

## image 命名與 tag 策略

CI 推送、ArgoCD 覆寫、本文件三處的 image 名稱**完全一致**：

| 元件        | image 名稱                                  |
| ----------- | ------------------------------------------- |
| API / scheduler / worker | `ghcr.io/agentic-build/tbite-api` |
| 員工 web    | `ghcr.io/agentic-build/tbite-web-employee`  |
| 商家 web    | `ghcr.io/agentic-build/tbite-web-merchant`  |
| 管理 web    | `ghcr.io/agentic-build/tbite-web-admin`     |

> 註：base 的 `tbite/api` 同時是 API、scheduler、worker 三個 Deployment 的 image，所以覆寫 `tbite/api` 一次即可涵蓋三者。

`docker/metadata-action` 產生的 tag：

| tag                | 觸發時機            | 用途                                        |
| ------------------ | ------------------- | ------------------------------------------- |
| `sha-<short-sha>`  | 每次 push / tag     | 不可變，overlay 釘的就是這個（部署用）      |
| `latest`           | push 到 `main`      | 可變，方便手動 / 首次 bootstrap 拉取        |
| `<semver>`         | push tag `v*`       | 例如 `v1.2.3` → `1.2.3` 與 `1.2`            |

overlay 釘的是不可變的 `sha-<short-sha>` tag；`bump-image-tags` 每次 push `main` 都會把它更新成該次 build 的 sha。`latest` 仍會推，但部署不依賴它（不可變 tag 才能精確對應 commit）。

## 前置作業（每個環境一次）

1. **可連線的 Kubernetes 叢集**且 `kubectl` 已指向它（單機 k3s/k3d/kind 皆可）。
2. **安裝 ArgoCD**（namespace `argocd`）：

   ```bash
   kubectl create namespace argocd
   kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
   ```

3. **GHCR 認證**，二選一：
   - **（建議）把四個 package 設為 public**：在 GitHub →  org `Agentic-Build` → Packages → 各 `tbite-*` package → Package settings → 改為 Public。叢集即可匿名拉取，不需要 imagePullSecret。
   - **若維持 private**：依 `ops/argocd/ghcr-pull-secret.example.yaml` 的說明建立一個 `docker-registry` 型別的 Secret（內含 read:packages 權限的 PAT），並掛到 `tbite` namespace 的 ServiceAccount 或 Deployment。**切勿把真實 token commit 進 repo。**

> 自動部署（image 更新）**不需要 argocd-image-updater，也不需要額外的 git 憑證**：由 `cd-publish-images.yml` 的 `bump-image-tags` job 用內建的 `GITHUB_TOKEN`（`contents: write`）把新 sha tag write-back 進 overlay。commit 帶 `[skip ci]`（且 `GITHUB_TOKEN` 推送本就不會觸發 workflow，雙重防遞迴）。

## 部署

ArgoCD 設定都收在 `ops/argocd/`：

```
ops/argocd/
  project.yaml                    # AppProject "tbite"
  application-single-node.yaml    # Application "tbite-single-node"
  kustomization.yaml              # 收攏上面兩個，方便 apply -k
  ghcr-pull-secret.example.yaml   # private GHCR 時的 imagePullSecret 範本（placeholder）
```

一次套用：

```bash
kubectl apply -k ops/argocd/
```

這會建立 `AppProject/tbite` 與 `Application/tbite-single-node`。由於 `syncPolicy.automated` 已開啟，ArgoCD 會自動把 single-node overlay 同步到 `tbite` namespace（含 `CreateNamespace=true` 自動建立 namespace）。

確認狀態：

```bash
argocd app get tbite-single-node          # 需先 argocd login
# 或純 kubectl：
kubectl -n argocd get applications
kubectl -n tbite get pods -w
```

## image 為什麼用不可變 tag

overlay 釘不可變的 `sha-<gitsha>`：每次部署的 image ref 都不同，kubelet 自然會拉新版，因此**不需要** `imagePullPolicy: Always`（用預設的 `IfNotPresent` 即可，同一 sha 內容固定）。早期為搭配可變 `:latest` 而加的 `Always` patch 已隨改用不可變 tag 一併移除。

## 手動 sync

```bash
argocd app sync tbite-single-node
```

或在 ArgoCD Web UI 點該 Application 的 **SYNC**。

若只想手動換 image，直接編輯 `ops/kubernetes/overlays/single-node/kustomization.yaml` 裡 `images:` 的 `newTag`（換成某個 `sha-<short>`），commit 後 ArgoCD 會自動同步。

## 回滾

GitOps 的回滾就是把 Git 退回上一個好的狀態：

- **用 ArgoCD 歷史**：

  ```bash
  argocd app history tbite-single-node          # 列出歷次 sync 的 revision
  argocd app rollback tbite-single-node <ID>    # 回滾到指定的歷史 revision
  ```

- **用 Git**：把 overlay `images:` 的 `newTag` 改回前一個 `sha-<short>`（或 `git revert` 那次 `bump-image-tags` 的 write-back commit），push 後 ArgoCD 自動同步回去。

> 提醒：`syncPolicy.automated.selfHeal` 會持續把叢集拉回 Git 宣告的狀態，所以直接 `kubectl edit` 改 live image 會被 ArgoCD 還原——回滾務必走 Git 或 `argocd app rollback`。

## 與本地開發的關係

- `make dev` 在 host 上跑 app（vite + go run），不經 single-node overlay，因此不受本流程影響。
- single-node overlay 的 `images:` 釘 GHCR `sha-<gitsha>`，由 CI write-back 維護；ArgoCD 同步它部署到叢集。
