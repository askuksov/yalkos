# Yalkos

Lightweight, self-hosted issue tracker with a Linear-compatible API.

> Status: early development. Nothing to run yet.

## Development

Requirements: Go 1.26+, Node.js 24+, pnpm (via `corepack enable`), Docker, golangci-lint v2.

| Command | What it does |
| :--- | :--- |
| `make build` | Build the frontend and `bin/yalkos` with the UI embedded |
| `make dev` | Start PostgreSQL (Docker), the Go server and the Vite dev server |
| `make test` | Go tests (with `-race`, against PostgreSQL in Docker) and frontend tests |
| `make db-up` / `make db-down` | Start / stop the local PostgreSQL container |
| `make generate` | Regenerate GraphQL code after editing `api/graphql/**` |
| `make lint` | golangci-lint and oxlint |
| `make fmt` | Format Go code |
| `make docker` | Build the `yalkos:dev` image |
| `make clean` | Remove build outputs |

Run `make` with no target to list everything. The `.githooks/` directory holds the maintainer's opt-in local hooks.

Server configuration (environment variables):

| Variable | Default | Meaning |
| :--- | :--- | :--- |
| `DATABASE_URL` | — (required) | PostgreSQL connection string |
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

Local ports used by `make dev` / `make test`, override if they are taken:

| Variable | Default | Used for |
| :--- | :--- | :--- |
| `YALKOS_PORT` | `8080` | Go server |
| `YALKOS_WEB_PORT` | `5173` | Vite dev server |
| `YALKOS_DB_PORT` | `54320` | PostgreSQL container |

## License

[MIT](LICENSE)
