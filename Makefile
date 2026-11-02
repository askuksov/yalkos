MODULE  := github.com/askuksov/yalkos
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u '+%Y-%m-%d %H:%M')
LDFLAGS := -s -w -X '$(MODULE)/internal/buildinfo.Version=$(VERSION)' \
           -X '$(MODULE)/internal/buildinfo.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/buildinfo.Date=$(DATE)'

YALKOS_DB_PORT ?= 54320
export YALKOS_DB_PORT
# Local development password only; production sets its own (see README).
YALKOS_DB_PASSWORD ?= yalkos
export YALKOS_DB_PASSWORD
TEST_DATABASE_URL ?= postgres://yalkos:$(YALKOS_DB_PASSWORD)@localhost:$(YALKOS_DB_PORT)/postgres?sslmode=disable
export TEST_DATABASE_URL
DEV_DATABASE_URL ?= postgres://yalkos:$(YALKOS_DB_PASSWORD)@localhost:$(YALKOS_DB_PORT)/yalkos?sslmode=disable

YALKOS_PORT ?= 8080
export YALKOS_PORT
YALKOS_WEB_PORT ?= 5173

COMPOSE := docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.dev.yml

# pnpm runs from web/ so corepack picks the version pinned in web/package.json.
PNPM := cd web && pnpm

# Terminal colors; NO_COLOR=1 turns them off.
ifeq ($(NO_COLOR),)
BLUE   := \033[0;34m
GREEN  := \033[0;32m
YELLOW := \033[0;33m
CYAN   := \033[0;36m
NC     := \033[0m
endif

# Progress lines: $(call step,Doing something) and $(call done,Something done).
# Messages must not contain commas or single quotes.
step = @printf '$(BLUE)→ %s$(NC)\n' '$(1)'
done = @printf '$(GREEN)✓ %s$(NC)\n' '$(1)'

.DEFAULT_GOAL := help
.PHONY: help \
	dev dev-go dev-web db-up db-down db-shell db-logs migrate-up migrate-status \
	build web-deps build-web build-go docker \
	generate check-generated fmt lint check \
	test test-go test-web \
	clean clean-all

# =============================================================================
##@ Development
# =============================================================================

dev: web-deps db-up ## Run the Go server and Vite with live reload of the UI
	@printf '\n$(BLUE)Endpoints:$(NC)\n'
	@printf '  $(GREEN)App (Vite):$(NC)  http://localhost:$(YALKOS_WEB_PORT)\n'
	@printf '  $(GREEN)Go server:$(NC)   http://localhost:$(YALKOS_PORT)\n'
	@printf '  $(GREEN)PostgreSQL:$(NC)  localhost:$(YALKOS_DB_PORT)\n\n'
	$(MAKE) -j2 dev-go dev-web

dev-go: ## Run only the Go server (YALKOS_PORT, default 8080)
	# BASE_URL is the Vite origin: the browser talks to Vite, which proxies to Go.
	DATABASE_URL="$(DEV_DATABASE_URL)" LISTEN_ADDR=:$(YALKOS_PORT) BASE_URL=http://localhost:$(YALKOS_WEB_PORT) go run ./cmd/yalkos serve

dev-web: ## Run only the Vite dev server (YALKOS_WEB_PORT, default 5173)
	$(PNPM) dev --port $(YALKOS_WEB_PORT) --strictPort

db-up: ## Start local PostgreSQL (Docker Compose, YALKOS_DB_PORT, default 54320)
	$(call step,Starting PostgreSQL)
	$(COMPOSE) up -d --wait db
	$(call done,PostgreSQL is up on localhost:$(YALKOS_DB_PORT))

db-down: ## Stop local Docker Compose services (data is kept)
	$(call step,Stopping Docker Compose services)
	$(COMPOSE) down
	$(call done,Services stopped)

db-shell: db-up ## Open psql in the development database
	$(COMPOSE) exec db psql -U yalkos -d yalkos

db-logs: ## Follow PostgreSQL logs
	$(COMPOSE) logs -f db

migrate-up: db-up ## Apply pending migrations to the development database
	$(call step,Applying migrations)
	DATABASE_URL="$(DEV_DATABASE_URL)" go run ./cmd/yalkos migrate up
	$(call done,Migrations applied)

migrate-status: db-up ## Show migration status of the development database
	DATABASE_URL="$(DEV_DATABASE_URL)" go run ./cmd/yalkos migrate status

# =============================================================================
##@ Build
# =============================================================================

build: build-web build-go ## Build the frontend and bin/yalkos with the UI embedded
	$(call done,Built bin/yalkos)

web-deps: ## Install frontend dependencies (from the lockfile)
	$(PNPM) install --frozen-lockfile

build-web: web-deps ## Build web/dist
	$(call step,Building web/dist)
	$(PNPM) build

build-go: ## Build bin/yalkos (embeds whatever is in web/dist)
	$(call step,Building bin/yalkos)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yalkos ./cmd/yalkos

docker: ## Build the yalkos:dev Docker image
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

generate: web-deps ## Regenerate code (sqlc, gqlgen, web GraphQL types)
	$(call step,Generating code)
	go tool sqlc generate
	go tool gqlgen generate --config gqlgen.native.yml
	go tool gqlgen generate --config gqlgen.linear.yml
	$(PNPM) codegen
	$(call done,Code generated)

check-generated: generate ## Fail if generated code is out of date
	git diff --exit-code -- internal/graphql internal/store/q web/src/gql
	@test -z "$$(git status --porcelain -- internal/graphql internal/store/q web/src/gql)" || (git status --porcelain -- internal/graphql internal/store/q web/src/gql; exit 1)
	$(call done,Generated code is up to date)

fmt: ## Format Go code
	golangci-lint fmt

lint: web-deps ## Run Go and frontend linters
	$(call step,Running linters)
	golangci-lint run
	$(PNPM) lint
	$(call done,Linters passed)

check: lint check-generated ## Run linters and check generated code (what CI checks besides tests)
	$(call done,All checks passed)

# =============================================================================
##@ Tests
# =============================================================================

test: test-go test-web ## Run Go and frontend tests
	$(call done,All tests passed)

test-go: db-up ## Run Go tests with -race against PostgreSQL in Docker
	$(call step,Running Go tests)
	go test -race ./...

test-web: web-deps ## Run frontend tests
	$(call step,Running frontend tests)
	$(PNPM) test

# =============================================================================
##@ Utilities
# =============================================================================

clean: ## Remove build outputs (keeps web/dist/.gitkeep)
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	$(call done,Build outputs removed)

clean-all: clean ## Remove build outputs and web/node_modules
	rm -rf web/node_modules
	$(call done,Dependencies removed)

help: ## Show this help
	@printf '$(BLUE)Usage:$(NC) make $(CYAN)<target>$(NC)\n'
	@awk 'BEGIN {FS = ":.*## "} \
		/^##@ / {printf "\n$(GREEN)%s:$(NC)\n", substr($$0, 5); next} \
		/^[a-z-]+:.*## / {printf "  $(CYAN)%-16s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@printf '\n$(YELLOW)Example:$(NC) make dev\n'
