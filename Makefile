SHELL := /usr/bin/env bash
DEV_COMPOSE := docker compose -f ops/local/docker-compose.dev.yml
KUBECTL ?= kubectl
# Monitoring base reads SoT files from ops/observability/ via configMapGenerator;
# kustomize blocks that by default for security. We opt in here because the
# referenced files live in-repo and are reviewed alongside the manifests.
KUSTOMIZE_FLAGS ?= --load-restrictor=LoadRestrictionsNone

.PHONY: help \
        dev dev-down dev-reset dev-logs \
        migrate-up migrate-down migrate-new seed \
        contract-sync test-go test-web test-e2e \
        render-overlay build clean \
        prod-up prod-down prod-status

help:
	@awk -F':.*##' '/^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-20s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

dev: ## Start deps + migrate + seed + run Go API + 3 SvelteKit dev servers
	@scripts/dev/dev-app.sh

dev-down: ## Stop deps (volumes persisted)
	@$(DEV_COMPOSE) down

dev-reset: ## Stop deps and wipe volumes (destructive)
	@$(DEV_COMPOSE) down -v

dev-logs: ## Tail deps logs (svc=postgres|redis|nats|minio for one)
	@$(DEV_COMPOSE) logs -f $(svc)

migrate-up: ## Apply pending migrations
	@scripts/db/migrate.sh up

migrate-down: ## Revert last migration
	@scripts/db/migrate.sh down 1

migrate-new: ## Create new migration (name=xxx)
	@scripts/db/migrate.sh create -ext sql -dir /migrations -seq $(name)

seed: ## Seed/refresh the dev DB with demo data (idempotent)
	@scripts/db/seed.sh

contract-sync: ## Generate OpenAPI from Go and regenerate TS client
	@go run ./services/api/cmd/contract-export
	@pnpm --filter @tbite/api-client generate

test-go: ## Go tests
	@go test ./...

test-web: ## Frontend checks
	@pnpm -r check && pnpm -r lint

test-e2e: ## Playwright e2e against $$E2E_BASE_URL (default localhost:5173)
	@E2E_BASE_URL=$${E2E_BASE_URL:-http://localhost:5173} \
	 pnpm exec playwright test --config=tests/e2e/playwright.config.ts

render-overlay: ## Render kustomize overlay (env=single-node|gcp)
	@$(KUBECTL) kustomize $(KUSTOMIZE_FLAGS) ops/kubernetes/overlays/$(env)

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

prod-up: ## Apply overlay to current kubectl context (env=single-node|gcp)
	@test -n "$(env)" || (echo "usage: make prod-up env=single-node|gcp" >&2 && exit 2)
	@echo "==> context: $$($(KUBECTL) config current-context)"
	@echo "==> overlay: ops/kubernetes/overlays/$(env)"
	@read -r -p "Apply? [y/N] " yn; [ "$$yn" = "y" ] || [ "$$yn" = "Y" ] || (echo "aborted." && exit 1)
	@$(KUBECTL) kustomize $(KUSTOMIZE_FLAGS) ops/kubernetes/overlays/$(env) | $(KUBECTL) apply -f -

prod-status: ## Show rollout status (env=single-node|gcp)
	@$(KUBECTL) -n tbite get deploy,svc,ingress

prod-down: ## Remove overlay from current kubectl context (env=single-node|gcp)
	@test -n "$(env)" || (echo "usage: make prod-down env=single-node|gcp" >&2 && exit 2)
	@echo "==> context: $$($(KUBECTL) config current-context)"
	@read -r -p "Delete? [y/N] " yn; [ "$$yn" = "y" ] || [ "$$yn" = "Y" ] || (echo "aborted." && exit 1)
	@$(KUBECTL) kustomize $(KUSTOMIZE_FLAGS) ops/kubernetes/overlays/$(env) | $(KUBECTL) delete --ignore-not-found -f -
