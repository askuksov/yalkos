MODULE  := github.com/askuksov/yalkos
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u '+%Y-%m-%d %H:%M')
LDFLAGS := -s -w -X '$(MODULE)/internal/buildinfo.Version=$(VERSION)' \
           -X '$(MODULE)/internal/buildinfo.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/buildinfo.Date=$(DATE)'

# Optional local overrides (ports, database password). Missing file is fine.
-include deploy/.env

YALKOS_DB_PORT     ?= 54320
YALKOS_PORT        ?= 8080
YALKOS_WEB_PORT    ?= 5173
# Local development password only; production sets its own (see README).
YALKOS_DB_PASSWORD ?= yalkos

# Pass the variables (including deploy/.env) to docker compose. The development
# password default keeps `make dev` working without a deploy/.env file.
export

COMPOSE := docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.dev.yml

# Go/Node commands run in containers by default (`make dev` needs Docker only).
# TOOLS=host uses locally installed toolchains instead (CI does this).
TOOLS ?= docker

ifeq ($(TOOLS),host)
RUN_GO      :=
RUN_WEB     := cd web &&
TEST_DATABASE_URL ?= postgres://yalkos:$(YALKOS_DB_PASSWORD)@localhost:$(YALKOS_DB_PORT)/postgres?sslmode=disable
export TEST_DATABASE_URL
BUILD_GO_CMD = CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yalkos ./cmd/yalkos
else
DOCKER_TEST_DATABASE_URL := postgres://yalkos:$(YALKOS_DB_PASSWORD)@db:5432/postgres?sslmode=disable
RUN_GO      := $(COMPOSE) run --rm --no-deps -e 'TEST_DATABASE_URL=$(DOCKER_TEST_DATABASE_URL)' app
RUN_WEB     := $(COMPOSE) run --rm --no-deps web
BUILD_GO_CMD = $(COMPOSE) run --rm --no-deps -e CGO_ENABLED=0 -e LDFLAGS="$(LDFLAGS)" app \
	sh -c 'go build -trimpath -buildvcs=false -ldflags "$$LDFLAGS" -o bin/yalkos ./cmd/yalkos'
endif

# Terminal colors; NO_COLOR=1 turns them off.
ifeq ($(NO_COLOR),)
BLUE   := \033[1;34m
GREEN  := \033[1;32m
YELLOW := \033[1;33m
RED    := \033[1;31m
CYAN   := \033[1;36m
DIM    := \033[2m
NC     := \033[0m
endif

# Progress lines: $(call step,Doing something) and $(call done,Something done).
# Messages must not contain commas or single quotes.
step = @printf '$(BLUE)→ %s$(NC)\n' '$(1)'
done = @printf '$(GREEN)✓ %s$(NC)\n' '$(1)'
warn = @printf '$(YELLOW)! %s$(NC)\n' '$(1)'

.DEFAULT_GOAL := help
.PHONY: help \
	dev up stop down restart rebuild ps logs logs-app logs-web logs-db \
	shell web-shell db-up db-down db-shell db-logs migrate-up migrate-down migrate-status \
	build web-deps build-web build-go docker \
	generate check-generated fmt lint check \
	test test-go test-web compat compat-latest \
	clean clean-all

# =============================================================================
##@ Getting started
# =============================================================================

dev: up ## Start db, Go server and Vite in Docker (hot reload on save)

up: ## Build and start the whole development stack in Docker
	$(call step,Building and starting the development stack)
	$(COMPOSE) up -d --build --wait --wait-timeout 300 --remove-orphans
	@printf '\n$(BLUE)Endpoints:$(NC)\n'
	@printf '  $(GREEN)App (Vite):$(NC)  http://localhost:$(YALKOS_WEB_PORT)\n'
	@printf '  $(GREEN)Go server:$(NC)   http://localhost:$(YALKOS_PORT)\n'
	@printf '  $(GREEN)PostgreSQL:$(NC)  127.0.0.1:$(YALKOS_DB_PORT)\n'
	@printf '\n$(DIM)Everything runs in Docker — no local Go or Node needed.$(NC)\n'
	@printf '$(DIM)Logs: make logs$(NC)\n\n'
	$(call done,Development stack is running)

stop: ## Stop the development stack (containers and data are kept)
	$(call step,Stopping the development stack)
	$(COMPOSE) stop
	$(call done,Development stack stopped)

down: ## Stop and remove the development containers (data is kept)
	$(call step,Removing the development containers)
	$(COMPOSE) down --remove-orphans
	$(call done,Development containers removed)

restart: ## Restart the development containers
	$(call step,Restarting the development stack)
	$(COMPOSE) restart
	$(call done,Development stack restarted)

rebuild: ## Rebuild the dev images and recreate the containers
	$(call step,Rebuilding the development images)
	$(COMPOSE) build app web
	$(COMPOSE) up -d --force-recreate --wait --wait-timeout 300 --remove-orphans
	$(call done,Development stack rebuilt)

ps: ## Show the state of the development containers
	$(COMPOSE) ps

logs: ## Follow logs of all development containers
	$(COMPOSE) logs -f --tail=100

logs-app: ## Follow the Go server logs
	$(COMPOSE) logs -f --tail=100 app

logs-web: ## Follow the Vite logs
	$(COMPOSE) logs -f --tail=100 web

logs-db: ## Follow the PostgreSQL logs
	$(COMPOSE) logs -f --tail=100 db

# =============================================================================
##@ Shells
# =============================================================================

shell: ## Open a shell in the Go container (make dev must be running)
	$(COMPOSE) exec app sh

web-shell: ## Open a shell in the Node container (make dev must be running)
	$(COMPOSE) exec web sh

db-shell: db-up ## Open psql in the development database
	$(COMPOSE) exec db psql -U yalkos -d yalkos

# =============================================================================
##@ Database
# =============================================================================

db-up: ## Start only PostgreSQL (YALKOS_DB_PORT, default 54320)
	$(call step,Starting PostgreSQL)
	$(COMPOSE) up -d --wait db
	$(call done,PostgreSQL is ready on 127.0.0.1:$(YALKOS_DB_PORT))

db-down: ## Stop the PostgreSQL container (data is kept)
	$(call step,Stopping PostgreSQL)
	$(COMPOSE) stop db
	$(call done,PostgreSQL stopped)

db-logs: ## Follow PostgreSQL logs
	$(COMPOSE) logs -f --tail=100 db

migrate-up: db-up ## Apply pending migrations to the development database
	$(call step,Applying migrations)
	$(RUN_GO) go run ./cmd/yalkos migrate up
	$(call done,Migrations applied)

migrate-down: db-up ## Roll back the most recent migration
	$(call step,Rolling back the most recent migration)
	$(RUN_GO) go run ./cmd/yalkos migrate down
	$(call done,Migration rolled back)

migrate-status: db-up ## Show migration status of the development database
	$(RUN_GO) go run ./cmd/yalkos migrate status

# =============================================================================
##@ Build
# =============================================================================

# Sequential even under -j: bin/yalkos embeds web/dist.
build: build-web ## Build the frontend and bin/yalkos with the UI embedded
	@$(MAKE) --no-print-directory build-go

web-deps: ## Install frontend dependencies in the Node container
	$(call step,Installing frontend dependencies)
	$(RUN_WEB) pnpm install --frozen-lockfile
	$(call done,Frontend dependencies installed)

build-web: web-deps ## Build web/dist
	$(call step,Building web/dist)
	$(RUN_WEB) pnpm build
	$(call done,Built web/dist)

build-go: ## Build bin/yalkos (embeds whatever is in web/dist)
	$(call step,Building bin/yalkos)
	$(BUILD_GO_CMD)
	$(call done,Built bin/yalkos)

docker: ## Build the production yalkos:dev Docker image
	$(call step,Building the yalkos:dev image)
	docker build -f deploy/Dockerfile \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg DATE="$(DATE)" \
		-t yalkos:dev .
	$(call done,Image yalkos:dev built)

# =============================================================================
##@ Code
# =============================================================================

generate: ## Regenerate code (sqlc, gqlgen, web GraphQL types)
	$(call step,Generating Go code)
	$(RUN_GO) sh -c 'go tool sqlc generate && \
		go tool gqlgen generate --config gqlgen.native.yml && \
		go tool gqlgen generate --config gqlgen.linear.yml'
	$(call step,Generating web types)
	$(RUN_WEB) pnpm codegen
	$(call done,Code generated)

check-generated: generate ## Fail if generated code is out of date
	git diff --exit-code -- internal/graphql internal/store/q web/src/gql
	@test -z "$$(git status --porcelain -- internal/graphql internal/store/q web/src/gql)" || (git status --porcelain -- internal/graphql internal/store/q web/src/gql; exit 1)
	$(call done,Generated code is up to date)

fmt: ## Format Go code
	$(call step,Formatting Go code)
	$(RUN_GO) golangci-lint fmt
	$(call done,Go code formatted)

lint: ## Run Go and frontend linters
	$(call step,Running Go linters)
	$(RUN_GO) golangci-lint run
	$(call step,Running web linters)
	$(RUN_WEB) pnpm lint
	$(call done,Linters passed)

# Sequential even under -j: generation rewrites files the linters read.
check: check-generated ## Run linters and check generated code (what CI checks besides tests)
	@$(MAKE) --no-print-directory lint
	$(call done,All checks passed)

# =============================================================================
##@ Tests
# =============================================================================

test: test-go test-web ## Run Go and frontend tests
	$(call done,All tests passed)

test-go: db-up ## Run Go tests with -race against PostgreSQL in Docker
	$(call step,Running Go tests)
	$(RUN_GO) go test -race ./...

test-web: ## Run frontend tests in the Node container
	$(call step,Running frontend tests)
	$(RUN_WEB) pnpm test

# The compatibility check builds a host binary (make build makes a Linux one in
# Docker) and needs psql, Node and pnpm; the database comes from db-up unless
# TOOLS=host (CI has its own service).
COMPAT_DATABASE_URL ?= postgres://yalkos:$(YALKOS_DB_PASSWORD)@localhost:$(YALKOS_DB_PORT)/postgres?sslmode=disable
COMPAT_PNPM         ?= pnpm
export COMPAT_DATABASE_URL

compat: $(if $(filter host,$(TOOLS)),,db-up) ## Run the SDK and webhook compatibility checks against a local build
	$(call step,Building bin/yalkos-host)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yalkos-host ./cmd/yalkos
	$(call step,Installing compatibility check dependencies)
	cd compat && $(COMPAT_PNPM) install --frozen-lockfile
	$(call step,Running compatibility checks)
	cd compat && COMPAT_BIN=../bin/yalkos-host node run.mjs
	$(call done,Compatibility checks passed)

compat-latest: $(if $(filter host,$(TOOLS)),,db-up) ## Run the compatibility checks with the newest SDK version (never touches the lockfile)
	$(call step,Building bin/yalkos-host)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yalkos-host ./cmd/yalkos
	$(call step,Installing compatibility check dependencies)
	cd compat && $(COMPAT_PNPM) install --frozen-lockfile
	$(call step,Running compatibility checks with the newest SDK)
	cd compat && COMPAT_BIN=../bin/yalkos-host node run.mjs --mode latest
	$(call done,Compatibility checks finished)

# =============================================================================
##@ Utilities
# =============================================================================

clean: ## Remove build outputs (keeps web/dist/.gitkeep)
	$(call step,Removing build outputs)
	@rm -rf bin tmp
	@find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	$(call done,Build outputs removed)

clean-all: clean ## Remove build outputs, node_modules and dev caches
	@rm -rf web/node_modules
	@$(COMPOSE) down --remove-orphans 2>/dev/null || true
	@docker volume rm -f yalkos_yalkos-dev-gomod yalkos_yalkos-dev-gocache yalkos_yalkos-dev-node-modules yalkos_yalkos-dev-pnpm-store 2>/dev/null || true
	$(call done,Everything cleaned)

help: ## Show this help
	@printf '\n$(BLUE)yalkos$(NC) $(DIM)— development environment in Docker$(NC)\n'
	@printf '$(DIM)Usage:$(NC) make $(CYAN)<target>$(NC)\n'
	@awk 'BEGIN {FS = ":.*## "} \
		/^##@ / {printf "\n$(GREEN)%s:$(NC)\n", substr($$0, 5); next} \
		/^[a-z-]+:.*## / {printf "  $(CYAN)%-16s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\n$(YELLOW)Ports:$(NC) app $(CYAN)%s$(NC), web $(CYAN)%s$(NC), db $(CYAN)%s$(NC)\n' \
		'$(YALKOS_PORT)' '$(YALKOS_WEB_PORT)' '$(YALKOS_DB_PORT)'
	@printf '$(YELLOW)Tools:$(NC) run in Docker by default; $(CYAN)TOOLS=host$(NC) uses local Go/Node\n'
	@printf '\n$(YELLOW)Example:$(NC) make dev\n\n'
