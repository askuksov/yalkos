# syntax=docker/dockerfile:1

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

# 3. Runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/yalkos /yalkos
USER nonroot
ENTRYPOINT ["/yalkos"]
CMD ["version"]
