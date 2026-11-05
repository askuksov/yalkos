# syntax=docker/dockerfile:1

# 0. Development images (source is bind-mounted; see docker-compose.dev.yml).
#    Everything a developer needs runs here: Go, air, golangci-lint, Node and pnpm.
FROM golang:1.26-alpine AS dev
# git: Go version stamping and tooling; build-base: gcc/musl for `go test -race`.
RUN apk add --no-cache git make ca-certificates tzdata build-base
COPY --from=golangci/golangci-lint:v2.12.2-alpine /usr/bin/golangci-lint /usr/local/bin/golangci-lint
RUN go install github.com/air-verse/air@v1.67.1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM node:24.11.1-alpine AS web-dev
RUN corepack enable
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

# 1. Frontend build
FROM node:24-alpine AS web
RUN corepack enable
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# 2. Go build with the frontend embedded
FROM golang:1.26-alpine AS go
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X 'github.com/askuksov/yalkos/internal/buildinfo.Version=${VERSION}' \
      -X 'github.com/askuksov/yalkos/internal/buildinfo.Commit=${COMMIT}' \
      -X 'github.com/askuksov/yalkos/internal/buildinfo.Date=${DATE}'" \
    -o /out/yalkos ./cmd/yalkos
# Uploaded files live in /data; a named volume mounted there starts with this owner.
RUN mkdir -p /out/data

# 3. Runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/yalkos /yalkos
COPY --from=go --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/yalkos"]
CMD ["serve"]
