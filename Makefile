SHELL := /usr/bin/env bash

.PHONY: help dev-up dev-down dev-reset dev-app dev-logs \
        migrate-up migrate-down migrate-new \
        contract-sync test-go test-web test-e2e \
        render-overlay build clean

help:
	@awk -F':.*##' '/^[a-zA-Z0-9_-]+:.*##/ {printf "  \033[36m%-20s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

dev-up: ## Start k3d cluster + apply single-node overlay
	@scripts/dev/dev-up.sh

dev-down: ## Tear down k3d cluster
	@scripts/dev/dev-down.sh

dev-reset: ## Tear down + delete volumes + reseed
	@scripts/dev/dev-reset.sh

dev-app: ## Run Go API + 3 SvelteKit dev servers locally
	@scripts/dev/dev-app.sh

dev-logs: ## Tail logs (service=name)
	@kubectl -n tbite logs -f -l app=$(service)

migrate-up: ## Apply pending migrations
	@scripts/db/migrate.sh up

migrate-down: ## Revert last migration
	@scripts/db/migrate.sh down 1

migrate-new: ## Create new migration (name=xxx)
	@scripts/db/migrate.sh create -ext sql -dir /migrations -seq $(name)

contract-sync: ## Generate OpenAPI from Go and regenerate TS client
	@echo "Placeholder for P1: contract-sync"

test-go: ## Go tests
	@go test ./...

test-web: ## Frontend checks
	@pnpm -r check && pnpm -r lint

test-e2e: ## E2E tests (placeholder for later phase)
	@echo "test-e2e: not implemented in P0"

render-overlay: ## Render kustomize overlay (env=single-node|gcp)
	@kubectl kustomize ops/kubernetes/overlays/$(env)

build: ## Build all
	@pnpm -r build && go build -o /tmp/tbite ./services/api/cmd/tbite

clean:
	@rm -rf node_modules apps/*/node_modules packages/*/node_modules apps/*/build target
