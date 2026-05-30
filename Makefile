SHELL := /usr/bin/env bash
KUBECTL ?= kubectl
HELM    ?= helm

# Canonical Helm umbrella chart.
CHART_DIR ?= chart/tbite-platform
CHART_RELEASE ?= tbite
CHART_NAMESPACE ?= tbite
CHART_DEV_VALUES ?= $(CHART_DIR)/values-dev.yaml
CHART_LOCAL_HA_VALUES ?= $(CHART_DIR)/values-local-ha.yaml
LOCAL_HA_APP_POD_SELECTOR ?= app.kubernetes.io/component in (api,realtime,web-employee,web-merchant,web-admin,worker-outbox-relay,worker-payroll-settler,worker-on-time-evaluator,scheduler-cutoff,scheduler-no-show,scheduler-doc-expiry,scheduler-feedback)
# Default is the single-enterprise prod sizing (ADR-0008, fits
# 16 cores / 32 GiB). Override to values-dev.yaml for laptop kind
# clusters or stack values-prod-ha.yaml on top for multi-AZ HA.
CHART_VALUES ?= $(CHART_DIR)/values.yaml

.PHONY: help \
        dev dev-down dev-reset dev-logs \
        local-ha-cluster local-ha-metrics local-ha-bootstrap local-ha-image local-ha-deploy local-ha-wait local-ha-seed \
        local-ha-autoscale-api local-ha-autoscale-worker local-ha-zone-autoscale-api local-ha-drain-node local-ha-drain-apps local-ha-drain-stateful-blocker local-ha-drain-pdb-blocker local-ha-drain-zone local-ha-drain-zone-apps \
        local-ha-rebalance-apps local-ha-fail-metrics-server local-ha-fail-keda-metrics local-ha-fail-cnpg local-ha-fail-cnpg-pooler local-ha-cordon-cnpg-primary local-ha-fail-nats local-ha-fail-valkey local-ha-fail-minio local-ha-fail-app-object-storage-client local-ha-fail-vmagent local-ha-fail-vmalert local-ha-fail-kube-state-metrics local-ha-restart-kube-state-metrics local-ha-fail-otel-collector local-ha-fail-victoria-traces local-ha-fail-victoria-logs local-ha-evidence \
        migrate-up migrate-down migrate-new seed seed-tsmc \
        contract-sync test-go test-web test-e2e \
        coverage coverage-go coverage-web \
        lunch-flow lunch-flow-cluster \
        build clean \
        sops-encrypt sops-decrypt sops-edit \
        chart-deps chart-lint chart-render chart-install chart-upgrade chart-uninstall \
        image-build-local \
        demo-seed-tsmc demo-load-tsmc demo-crisis

help:
	@awk -F':.*##' '/^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-20s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

dev: ## Install/upgrade the local Kubernetes dev chart in the current kubectl context
	@$(HELM) dependency build $(CHART_DIR)
	@$(HELM) upgrade --install $(CHART_RELEASE) $(CHART_DIR) \
		-f $(CHART_DEV_VALUES) \
		--namespace $(CHART_NAMESPACE) \
		--create-namespace

dev-down: ## Uninstall the local Kubernetes dev release
	@$(HELM) uninstall $(CHART_RELEASE) --namespace $(CHART_NAMESPACE)

dev-reset: ## Delete the local Kubernetes dev namespace (destructive)
	@$(KUBECTL) delete namespace $(CHART_NAMESPACE)

dev-logs: ## Tail local Kubernetes logs (component=api|realtime|web-employee|...)
	@selector="app.kubernetes.io/instance=$(CHART_RELEASE)"; \
	if [ -n "$(component)" ]; then selector="$$selector,app.kubernetes.io/component=$(component)"; fi; \
	$(KUBECTL) -n $(CHART_NAMESPACE) logs -f -l "$$selector" --all-containers=true --max-log-requests=20

local-ha-cluster: ## Create/use a 3-zone kind cluster for local HA drills
	@scripts/local-ha/create-cluster.sh

local-ha-metrics: ## Install metrics-server for HPA CPU metrics on kind
	@scripts/local-ha/install-metrics-server.sh

local-ha-bootstrap: ## Create local-only Secrets used by the HA chart
	@NAMESPACE=$(CHART_NAMESPACE) scripts/local-ha/bootstrap-secrets.sh

local-ha-image: ## Build and load the platform image into the local HA kind cluster (TAG=local)
	@TAG=$${TAG:-local} scripts/local-ha/load-image.sh

local-ha-deploy: ## Deploy values.yaml + values-local-ha.yaml into the current context
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) CHART_DIR=$(CHART_DIR) LOCAL_HA_VALUES=$(CHART_LOCAL_HA_VALUES) scripts/local-ha/deploy.sh

local-ha-wait: ## Wait for local HA rollouts and show autoscaling state
	@NAMESPACE=$(CHART_NAMESPACE) scripts/local-ha/wait-ready.sh

local-ha-seed: ## Seed local HA workload data for behavior drills
	@NAMESPACE=$(CHART_NAMESPACE) scripts/local-ha/seed-workload.sh

local-ha-autoscale-api: ## Run API HPA load scenario (DURATION/TOTAL_RPS/CONCURRENCY override)
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-autoscale-api.sh

local-ha-autoscale-worker: ## Run deterministic worker KEDA backlog scenario
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-autoscale-worker-keda.sh

local-ha-zone-autoscale-api: ## Cordon one modeled zone, evacuate API pods, and verify API HPA load still scales
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-zone-drain-api-autoscale.sh

local-ha-fail-metrics-server: ## Scale metrics-server to zero and verify CPU HPAs report resource metric failure
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-metrics-server.sh

local-ha-fail-keda-metrics: ## Scale KEDA metrics apiserver to zero and verify KEDA HPAs report external metric failure
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-keda-metrics-apiserver.sh

local-ha-drain-node: ## Drain one worker node (NODE=... optional)
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-drain-node.sh

local-ha-drain-apps: ## Cordon a worker and evict only tbite app pods; avoids kind local-path PVC pinning
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) DRAIN_POD_SELECTOR='$(LOCAL_HA_APP_POD_SELECTOR)' scripts/local-ha/scenario-drain-node.sh

local-ha-drain-stateful-blocker: ## Drain a node with a local StatefulSet PVC, verify blocker signals, then restore
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-drain-stateful-pvc-blocker.sh

local-ha-drain-pdb-blocker: ## Cordon an app worker and verify voluntary eviction is blocked by PDB, then restore
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-drain-pdb-blocker.sh

local-ha-drain-zone: ## Drain one modeled zone (ZONE=local-a|local-b|local-c)
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-drain-zone.sh

local-ha-drain-zone-apps: ## Cordon a modeled zone and evict only tbite app pods
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) DRAIN_POD_SELECTOR='$(LOCAL_HA_APP_POD_SELECTOR)' scripts/local-ha/scenario-drain-zone.sh

local-ha-rebalance-apps: ## Restart platform app deployments to restore topology spread after drain drills
	@NAMESPACE=$(CHART_NAMESPACE) DEPLOYMENT_SELECTOR='app.kubernetes.io/instance=$(CHART_RELEASE),app.kubernetes.io/name=tbite-platform' scripts/local-ha/rebalance-apps.sh

local-ha-fail-cnpg: ## Delete the current CNPG primary and wait for failover
	@NAMESPACE=$(CHART_NAMESPACE) scripts/local-ha/scenario-fail-cnpg-primary.sh

local-ha-fail-cnpg-pooler: ## Scale the CNPG RW pooler to zero and verify access-layer failure/recovery
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-cnpg-pooler.sh

local-ha-cordon-cnpg-primary: ## Cordon the CNPG primary node and verify operator-driven switchover
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-cordon-cnpg-primary-node.sh

local-ha-fail-nats: ## Delete one NATS pod and wait for the StatefulSet
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-nats-pod.sh

local-ha-fail-valkey: ## Delete the current Valkey master and wait for the StatefulSet
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-valkey-primary.sh

local-ha-fail-minio: ## Delete one MinIO tenant pod and wait for the StatefulSet
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-minio-pod.sh

local-ha-fail-app-object-storage-client: ## Patch API object-storage endpoint and verify client-side dependency readiness signals
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-app-object-storage-client.sh

local-ha-fail-vmagent: ## Suspend vmagent briefly and wait for metrics collection to recover
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-vmagent-pod.sh

local-ha-fail-vmalert: ## Scale vmalert to zero and verify alerting-plane outage is surfaced without data loss
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-vmalert.sh

local-ha-fail-kube-state-metrics: ## Scale kube-state-metrics to zero and verify Kubernetes inventory metric staleness/recovery
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-kube-state-metrics.sh

local-ha-restart-kube-state-metrics: ## Restart kube-state-metrics and verify dashboard survives duplicate KSM series
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-restart-kube-state-metrics.sh

local-ha-fail-otel-collector: ## Scale OTel Collector to zero and verify app telemetry staleness/recovery
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-otel-collector.sh

local-ha-fail-victoria-traces: ## Scale VictoriaTraces to zero and verify trace export failure/recovery
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-victoria-traces.sh

local-ha-fail-victoria-logs: ## Scale VictoriaLogs to zero and verify log ingest staleness/recovery
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/scenario-fail-victoria-logs.sh

local-ha-evidence: ## Collect Kubernetes state for a local HA drill
	@NAMESPACE=$(CHART_NAMESPACE) RELEASE=$(CHART_RELEASE) scripts/local-ha/collect-evidence.sh

migrate-up: ## Apply pending migrations
	@scripts/db/migrate.sh up

migrate-down: ## Revert last migration
	@scripts/db/migrate.sh down 1

migrate-new: ## Create new migration (name=xxx)
	@scripts/db/migrate.sh create -ext sql -dir /migrations -seq $(name)

seed: ## Seed/refresh DATABASE_URL with demo data (requires psql + S3 env)
	@scripts/db/seed.sh

seed-tsmc: ## Seed/refresh DATABASE_URL with the 50k-person TSMC demo scenario
	@scripts/db/seed-tsmc.sh

contract-sync: ## Generate OpenAPI from Go and regenerate TS client
	@go run ./services/api/cmd/contract-export
	@pnpm --filter @tbite/api-client generate

test-go: ## Go tests
	@go test ./...

test-web: ## Frontend checks
	@pnpm -r check && pnpm -r lint

test-e2e: ## Playwright e2e against $$E2E_BASE_URL (default app.tbite.local)
	@E2E_BASE_URL=$${E2E_BASE_URL:-http://app.tbite.local} \
	 pnpm exec playwright test --config=tests/e2e/playwright.config.ts

coverage: coverage-go coverage-web ## Full coverage report (Go + frontend)

coverage-go: ## Go coverage of internal/ (excludes cmd wiring + load tool); serialized (-p 1) so testcontainers runs one DB at a time (no thrash). Writes coverage.out + coverage.html
	@TESTCONTAINERS_RYUK_DISABLED=true go test ./services/api/internal/... \
		-coverprofile=coverage.out -covermode=atomic -p 1 -timeout 25m
	@go tool cover -html=coverage.out -o coverage.html
	@echo "==> total (raw):"; go tool cover -func=coverage.out | tail -1
	@# Effective coverage excludes genuinely untestable infra that requires a live
	@# external runtime (documented in docs/plans/2026-05-31-test-coverage-99.md):
	@#   platform/leader — K8s Lease leader election (needs an in-cluster API server)
	@grep -vE 'internal/platform/leader/' coverage.out > coverage.eff.out || true
	@echo "==> total (effective, excl. K8s leader-election infra):"; go tool cover -func=coverage.eff.out | tail -1
	@echo "==> wrote coverage.out + coverage.html"

coverage-web: ## Frontend coverage (vitest v8). Requires: pnpm install (adds @vitest/coverage-v8)
	@pnpm -r --filter "./packages/*" --filter "./apps/*" test -- --coverage

build: ## Build everything (web bundles + Go binary)
	@pnpm -r build && go build -o /tmp/tbite ./services/api/cmd/tbite

clean: ## Remove node_modules + build artifacts
	@rm -rf node_modules apps/*/node_modules packages/*/node_modules apps/*/build target

stress: ## Drive traffic against a running API (scenario=mixed|place-only|cancel-storm|lunch-crunch|modify-storm|browse|adjust-supply, duration=5m)
	@go run ./services/api/cmd/stress \
		--scenario=$${scenario:-mixed} \
		--duration=$${duration:-5m} \
		--rps=$${rps:-5} \
		--concurrency=$${concurrency:-8} \
		--employees=$${employees:-30}

stress-lunch-crunch: ## Lunch peak — focused load on one plant×vendor, blow up quota_exhausted + alerts
	@go run ./services/api/cmd/stress --scenario=lunch-crunch --duration=$${duration:-3m} --rps=10 --concurrency=12 --employees=60

stress-modify-storm: ## High contention on a small pool of orders — 5xx race conditions + 409 tx conflicts
	@go run ./services/api/cmd/stress --scenario=modify-storm --duration=$${duration:-2m} --rps=10 --concurrency=10 --employees=20

stress-auth-flood: ## 1500 garbage-token requests against /api/employee/orders — exercises Auth dashboard + AuthFailureSurge alert
	@for i in $$(seq 1 1500); do curl -fsS -o /dev/null -H "Authorization: Bearer bogus-$$RANDOM" "http://localhost:8080/api/employee/orders" 2>/dev/null; done; echo "auth-flood done"

lunch-flow: ## Provision and run the 50k Gaussian lunch prepare/pickup flow (mode=all|setup|prepare|pickup|cleanup)
	@go run ./services/api/cmd/lunch-flow \
		--mode=$${mode:-all} \
		--run-id=$${run_id:-} \
		--cleanup=$${cleanup:-keep} \
		--replace=$${replace:-false} \
		--employees=$${employees:-50000} \
		--merchants=$${merchants:-100} \
		--pickup-points=$${pickup_points:-100} \
		--stage2-rps=$${stage2_rps:-100}

lunch-flow-cluster: ## Build and run lunch-flow as an in-cluster Kubernetes Job
	@scripts/perf/lunch-flow-cluster.sh

chart-deps: ## Resolve and download Helm subchart dependencies (offline-friendly after first run)
	@$(HELM) dependency update $(CHART_DIR)

chart-lint: ## Lint the umbrella chart against dev, local HA, and prod HA values
	@$(HELM) lint $(CHART_DIR) -f $(CHART_DIR)/values-dev.yaml
	@$(HELM) lint $(CHART_DIR) -f $(CHART_DIR)/values.yaml -f $(CHART_LOCAL_HA_VALUES)
	@$(HELM) lint $(CHART_DIR) -f $(CHART_DIR)/values-prod-ha.yaml

chart-render: ## Render the chart to stdout. Usage: make chart-render CHART_VALUES=chart/tbite-platform/values-prod-ha.yaml
	@$(HELM) template $(CHART_RELEASE) $(CHART_DIR) -f $(CHART_VALUES) --namespace $(CHART_NAMESPACE)

chart-install: ## Install the umbrella chart into the current kubectl context
	@echo "==> context: $$($(KUBECTL) config current-context)"
	@echo "==> chart:   $(CHART_DIR)"
	@echo "==> values:  $(CHART_VALUES)"
	@read -r -p "Install? [y/N] " yn; [ "$$yn" = "y" ] || [ "$$yn" = "Y" ] || (echo "aborted." && exit 1)
	@$(HELM) install $(CHART_RELEASE) $(CHART_DIR) -f $(CHART_VALUES) --namespace $(CHART_NAMESPACE) --create-namespace

chart-upgrade: ## Upgrade the umbrella chart release
	@echo "==> context: $$($(KUBECTL) config current-context)"
	@$(HELM) upgrade $(CHART_RELEASE) $(CHART_DIR) -f $(CHART_VALUES) --namespace $(CHART_NAMESPACE) --install

chart-uninstall: ## Uninstall the umbrella chart release
	@echo "==> context: $$($(KUBECTL) config current-context)"
	@read -r -p "Uninstall release $(CHART_RELEASE) from namespace $(CHART_NAMESPACE)? [y/N] " yn; [ "$$yn" = "y" ] || [ "$$yn" = "Y" ] || (echo "aborted." && exit 1)
	@$(HELM) uninstall $(CHART_RELEASE) --namespace $(CHART_NAMESPACE)

image-build-local: ## Build the platform image for the local Docker daemon's native architecture (TAG=local override)
	@TAG=$${TAG:-local}; \
	docker build -f services/api/Dockerfile -t ghcr.io/agentic-build/tbite-api:$$TAG . && \
	echo "built ghcr.io/agentic-build/tbite-api:$$TAG"

demo-seed-tsmc: ## Seed current Kubernetes context with the 50k-person TSMC demo scenario
	@ops/demo/seed-tsmc-enterprise.sh

demo-load-tsmc: ## Run TSMC lunch-crunch traffic against current Kubernetes context
	@ops/demo/run-tsmc-load.sh

demo-crisis: ## Delete one demo component pod (component=api|realtime|worker-outbox-relay|cloudflared|minio)
	@ops/demo/crisis-drill.sh $(or $(component),api)

sops-encrypt:  ## Encrypt a YAML file in place. Usage: make sops-encrypt FILE=ops/secrets/example.sops.yaml
	@test -n "$(FILE)" || (echo "FILE=... required"; exit 1)
	sops -e -i $(FILE)

sops-decrypt:  ## Decrypt a SOPS-encrypted file to stdout. Usage: make sops-decrypt FILE=ops/secrets/prod.sops.yaml
	@test -n "$(FILE)" || (echo "FILE=... required"; exit 1)
	sops -d $(FILE)

sops-edit:     ## Edit a SOPS-encrypted file with $$EDITOR. Usage: make sops-edit FILE=ops/secrets/prod.sops.yaml
	@test -n "$(FILE)" || (echo "FILE=... required"; exit 1)
	sops $(FILE)
