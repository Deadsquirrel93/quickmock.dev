SHELL := /usr/bin/env bash

APP         := quickmock
PKG         := ./cmd/server
BIN         := bin/$(APP)
DEPLOY_HOST ?= deploy@your-server.example
APP_DIR     ?= /var/www/quickmock/current
COMPOSE     ?= docker compose

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-22s %s\n", $$1, $$2}'

## ---- Bootstrap (run once after clone) ----

.PHONY: bootstrap
bootstrap: tidy vendor-js ## One-shot setup: go mod tidy + download HTMX/Alpine

.PHONY: tidy
tidy: ## Run go mod tidy (generates go.sum)
	go mod tidy

.PHONY: vendor-js
vendor-js: ## Download HTMX + Alpine into web/static/js
	./scripts/vendor-js.sh

## ---- Local development (bare-metal) ----

.PHONY: run
run: ## Run the server locally with env from .env
	@set -a; [ -f .env ] && . ./.env; set +a; \
	go run $(PKG)

.PHONY: build
build: ## Build the production binary
	go build -trimpath -ldflags="-s -w" -o $(BIN) $(PKG)

.PHONY: migrate
migrate: build ## Apply database migrations
	$(BIN) migrate

.PHONY: test
test: ## Run unit tests
	go test ./... -race -count=1

.PHONY: lint
lint: ## Run go vet and i18n checks
	go vet ./...
	./scripts/check_i18n.sh

.PHONY: fmt
fmt: ## Format Go code
	gofmt -s -w .

.PHONY: i18n-check
i18n-check: ## Verify locale key parity against en.json
	./scripts/check_i18n.sh

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin/

## ---- Docker (local + prod) ----

.PHONY: docker-up
docker-up: ## Build and start the full stack via docker compose
	$(COMPOSE) up -d --build

.PHONY: docker-down
docker-down: ## Stop and remove containers (keeps volumes)
	$(COMPOSE) down

.PHONY: docker-logs
docker-logs: ## Tail app logs
	$(COMPOSE) logs -f app

.PHONY: docker-ps
docker-ps: ## Show container status
	$(COMPOSE) ps

.PHONY: docker-shell
docker-shell: ## Open a psql shell inside the postgres container
	$(COMPOSE) exec postgres psql -U quickmock -d quickmock

## ---- Database (bare-metal) ----

.PHONY: db-shell
db-shell: ## Open a psql shell against the local DB
	@set -a; [ -f .env ] && . ./.env; set +a; \
	psql "$$QUICKMOCK_PG_DSN"

.PHONY: db-reset
db-reset: ## DROP and recreate the local DB (destructive)
	@read -p "This will erase all local data. Continue? [y/N] " ans; [ "$$ans" = "y" ] || exit 1
	@set -a; [ -f .env ] && . ./.env; set +a; \
	psql "$$QUICKMOCK_PG_DSN" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
	$(MAKE) migrate

## ---- Deploy ----

.PHONY: deploy
deploy: ## Trigger a production deploy (git pull + rebuild on server)
	ssh $(DEPLOY_HOST) "$(APP_DIR)/deploy.sh"

.PHONY: logs
logs: ## Tail production logs (auto-detects docker vs systemd)
	ssh $(DEPLOY_HOST) "if [ -f $(APP_DIR)/docker-compose.yml ] && command -v docker >/dev/null; then \
		docker compose -f $(APP_DIR)/docker-compose.yml logs -f app; \
	else \
		sudo journalctl -u $(APP) -f; \
	fi"

.PHONY: status
status: ## Show production service status
	ssh $(DEPLOY_HOST) "if [ -f $(APP_DIR)/docker-compose.yml ] && command -v docker >/dev/null; then \
		docker compose -f $(APP_DIR)/docker-compose.yml ps; \
	else \
		sudo systemctl status $(APP) --no-pager; \
	fi"
