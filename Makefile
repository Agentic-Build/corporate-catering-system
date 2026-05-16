SHELL := /usr/bin/env bash
DEV_COMPOSE := docker compose -f ops/local/docker-compose.dev.yml
KUBECTL ?= kubectl

.PHONY: help \
        dev dev-down dev-reset dev-logs \
        migrate-up migrate-down migrate-new \
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
	@$(KUBECTL) kustomize ops/kubernetes/overlays/$(env)

build: ## Build everything (web bundles + Go binary)
	@pnpm -r build && go build -o /tmp/tbite ./services/api/cmd/tbite

clean: ## Remove node_modules + build artifacts
	@rm -rf node_modules apps/*/node_modules packages/*/node_modules apps/*/build target

prod-up: ## Apply overlay to current kubectl context (env=single-node|gcp)
	@test -n "$(env)" || (echo "usage: make prod-up env=single-node|gcp" >&2 && exit 2)
	@echo "==> context: $$($(KUBECTL) config current-context)"
	@echo "==> overlay: ops/kubernetes/overlays/$(env)"
	@read -r -p "Apply? [y/N] " yn; [ "$$yn" = "y" ] || [ "$$yn" = "Y" ] || (echo "aborted." && exit 1)
	@$(KUBECTL) kustomize ops/kubernetes/overlays/$(env) | $(KUBECTL) apply -f -

prod-status: ## Show rollout status (env=single-node|gcp)
	@$(KUBECTL) -n tbite get deploy,svc,ingress

prod-down: ## Remove overlay from current kubectl context (env=single-node|gcp)
	@test -n "$(env)" || (echo "usage: make prod-down env=single-node|gcp" >&2 && exit 2)
	@echo "==> context: $$($(KUBECTL) config current-context)"
	@read -r -p "Delete? [y/N] " yn; [ "$$yn" = "y" ] || [ "$$yn" = "Y" ] || (echo "aborted." && exit 1)
	@$(KUBECTL) kustomize ops/kubernetes/overlays/$(env) | $(KUBECTL) delete --ignore-not-found -f -
