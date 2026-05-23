# ArgoCD 持續部署（CD）

以 ArgoCD 為核心的 GitOps 部署流程：程式碼推上 `main` 後，GitHub Actions 自動 build 四個 container image 並推到 GitHub Container Registry（GHCR），ArgoCD（搭配 Image Updater）偵測到新 image 後，把 single-node overlay 同步到叢集。

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
        └───────────────┬───────────────┘
                        │ 新 image 出現在 GHCR
                        ▼
        ┌───────────────────────────────┐
        │ argocd-image-updater           │
        │  偵測新的 sha-* tag             │
        │  write-back 到 Git             │
        │  （改寫 Application 的          │
        │    kustomize.images tag）       │
        └───────────────┬───────────────┘
                        │ Git commit 觸發
                        ▼
        ┌───────────────────────────────┐
        │ ArgoCD Application             │
        │  tbite-single-node             │
        │  path = overlays/single-node   │
        │  kustomize.images 覆寫成 GHCR  │
        │  syncPolicy: automated         │
        │   (prune + selfHeal)           │
        └───────────────┬───────────────┘
                        │ kubectl apply（由 ArgoCD 執行）
                        ▼
                   叢集 namespace: tbite
```

設計重點：ArgoCD 的 `Application.spec.source.path` 直接指向 `ops/kubernetes/overlays/single-node`，但**不修改該 overlay 本身**。overlay 仍把四個 app 釘在本地 `tbite/<name>:dev`（保留 `make dev` 的本地開發語意）；ArgoCD 透過 `Application.spec.source.kustomize.images` 在部署時把這四個 image **覆寫**成 GHCR 上的版本。本地開發與叢集部署因此各走各的 image，互不干擾。

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
| `sha-<short-sha>`  | 每次 push / tag     | 不可變，Image Updater 追蹤的就是這個        |
| `latest`           | push 到 `main`      | 可變，方便手動 / 首次 bootstrap 拉取        |
| `<semver>`         | push tag `v*`       | 例如 `v1.2.3` → `1.2.3` 與 `1.2`            |

Image Updater 採 `newest-build` 策略，只接受符合 `^sha-[0-9a-f]+$` 的 tag——亦即追蹤不可變的 commit image，而非可變的 `latest`。

## 前置作業（每個環境一次）

1. **可連線的 Kubernetes 叢集**且 `kubectl` 已指向它（單機 k3s/k3d/kind 皆可）。
2. **安裝 ArgoCD**（namespace `argocd`）：

   ```bash
   kubectl create namespace argocd
   kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
   ```

3. **安裝 ArgoCD Image Updater**（image 自動化所需）：

   ```bash
   kubectl apply -n argocd \
     -f https://raw.githubusercontent.com/argoproj-labs/argocd-image-updater/stable/manifests/install.yaml
   ```

4. **GHCR 認證**，二選一：
   - **（建議）把四個 package 設為 public**：在 GitHub →  org `Agentic-Build` → Packages → 各 `tbite-*` package → Package settings → 改為 Public。叢集即可匿名拉取，不需要 imagePullSecret。
   - **若維持 private**：依 `ops/argocd/ghcr-pull-secret.example.yaml` 的說明建立一個 `docker-registry` 型別的 Secret（內含 read:packages 權限的 PAT），並掛到 `tbite` namespace 的 ServiceAccount 或 Deployment。**切勿把真實 token commit 進 repo。**

     Image Updater 若要讀取 private registry，另需在 `argocd-image-updater-config` ConfigMap 設定對應的 registry 認證（見官方文件 *Private registries*）。

5. **Git write-back 認證**：Image Updater 要把新 tag 寫回 `main`，需要一把對本 repo 有寫入權限的憑證。最常見的做法是建立一個 secret 並讓 Image Updater 引用：

   ```bash
   kubectl -n argocd create secret generic git-creds \
     --from-literal=username=<github-username> \
     --from-literal=password=<github-PAT-with-repo-write>
   ```

   再於 Application 上補一個 annotation 指向它（若採此 secret 名稱）：

   ```
   argocd-image-updater.argoproj.io/write-back-target: kustomization
   argocd-image-updater.argoproj.io/git-credentials: argocd/git-creds
   ```

   > 若暫時不想啟用 write-back，可改用 `write-back-method: argocd`（直接改 live state），但會與 `selfHeal` 互相拉扯，不建議。本 repo 預設採 `git` write-back。

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

## imagePullPolicy 的考量

Application 內含一個 kustomize patch，把所有 Deployment 第一個 container 的 `imagePullPolicy` 設為 `Always`。原因：`kustomize.images` 預設覆寫成 `:latest`（可變 tag），若用預設的 `IfNotPresent`，節點上一旦快取過 `latest` 就不會再拉新版。設成 `Always` 可保證首次 bootstrap 一定抓到最新。

待 Image Updater 開始運作後，它會把 tag 改寫成不可變的 `sha-<short>`，屆時 `Always` 實質上是 no-op（同一 tag 內容固定），但保留它不會有副作用。

## 手動 sync

```bash
argocd app sync tbite-single-node
```

或在 ArgoCD Web UI 點該 Application 的 **SYNC**。

若只想手動換 image 而不等 Image Updater，可直接編輯 `ops/argocd/application-single-node.yaml` 裡 `kustomize.images` 的 tag（例如把 `:latest` 換成某個 `sha-<short>`），commit 後 ArgoCD 會自動同步。

## 回滾

GitOps 的回滾就是把 Git 退回上一個好的狀態：

- **用 ArgoCD 歷史**：

  ```bash
  argocd app history tbite-single-node          # 列出歷次 sync 的 revision
  argocd app rollback tbite-single-node <ID>    # 回滾到指定的歷史 revision
  ```

- **用 Git**：把 `kustomize.images` 的 tag 改回前一個 `sha-<short>`（或 `git revert` Image Updater 的 write-back commit），push 後 ArgoCD 自動同步回去。

> 提醒：`syncPolicy.automated.selfHeal` 會持續把叢集拉回 Git 宣告的狀態，所以直接 `kubectl edit` 改 live image 會被 ArgoCD 還原——回滾務必走 Git 或 `argocd app rollback`。

## 與本地開發的關係

- `make dev` / single-node overlay 仍使用本地 `tbite/<name>:dev`，不受本流程影響。
- 本 CD 流程只在「真實叢集 + ArgoCD」情境下，透過 Application 的 image 覆寫改用 GHCR image。
- 兩者共用同一份 overlay，差別只在 ArgoCD 額外疊上的 `kustomize.images` 與 patch。
