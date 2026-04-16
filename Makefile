.PHONY: dev dev-up dev-app dev-down dev-reset dev-logs dev-ps

dev:
	./scripts/setup-dev.sh dev

dev-up:
	./scripts/setup-dev.sh up

dev-app:
	./scripts/setup-dev.sh app

dev-down:
	./scripts/setup-dev.sh down

dev-reset:
	./scripts/setup-dev.sh reset

dev-logs:
	./scripts/setup-dev.sh logs $(service)

dev-ps:
	./scripts/setup-dev.sh ps
