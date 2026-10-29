MODULE  := github.com/askuksov/yalkos
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u '+%Y-%m-%d %H:%M')
LDFLAGS := -s -w -X '$(MODULE)/internal/buildinfo.Version=$(VERSION)' \
           -X '$(MODULE)/internal/buildinfo.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/buildinfo.Date=$(DATE)'

YALKOS_DB_PORT ?= 54320
export YALKOS_DB_PORT
TEST_DATABASE_URL ?= postgres://yalkos:yalkos@localhost:$(YALKOS_DB_PORT)/postgres?sslmode=disable
export TEST_DATABASE_URL

COMPOSE := docker compose -f deploy/docker-compose.yml

# pnpm runs from web/ so corepack picks the version pinned in web/package.json.
PNPM := cd web && pnpm

.DEFAULT_GOAL := help
.PHONY: help db-up db-down generate check-generated build web-deps build-web build-go dev test lint fmt docker clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: build-web build-go ## Build frontend and the yalkos binary into bin/

web-deps: ## Install frontend dependencies (from the lockfile)
	$(PNPM) install --frozen-lockfile

build-web: web-deps ## Build web/dist
	$(PNPM) build

build-go: ## Build bin/yalkos (embeds whatever is in web/dist)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/yalkos ./cmd/yalkos

dev: web-deps ## Run the Vite dev server (Go server joins in M1)
	$(PNPM) dev

test: web-deps db-up ## Run Go and frontend tests
	go test -race ./...
	$(PNPM) test

lint: web-deps ## Run Go and frontend linters
	golangci-lint run
	$(PNPM) lint

db-up: ## Start local PostgreSQL (Docker Compose)
	$(COMPOSE) up -d --wait db

db-down: ## Stop local Docker Compose services
	$(COMPOSE) down

generate: ## Regenerate GraphQL code (gqlgen)
	go tool gqlgen generate --config gqlgen.native.yml
	go tool gqlgen generate --config gqlgen.linear.yml

check-generated: generate ## Fail if generated code is out of date
	git diff --exit-code -- internal/graphql
	@test -z "$$(git status --porcelain -- internal/graphql)" || (git status --porcelain -- internal/graphql; exit 1)

fmt: ## Format Go code
	golangci-lint fmt

docker: ## Build the yalkos:dev Docker image
	docker build -f deploy/Dockerfile \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg DATE="$(DATE)" \
		-t yalkos:dev .

clean: ## Remove build outputs (keeps web/dist/.gitkeep)
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
