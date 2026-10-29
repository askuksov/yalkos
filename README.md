# Yalkos

Lightweight, self-hosted issue tracker with a Linear-compatible API.

> Status: early development. Nothing to run yet.

## Development

Requirements: Go 1.26+, Node.js 24+, pnpm (via `corepack enable`), Docker, golangci-lint v2.

| Command | What it does |
| :--- | :--- |
| `make build` | Build the frontend and `bin/yalkos` with the UI embedded |
| `make dev` | Run the Vite dev server |
| `make test` | Go tests (with `-race`) and frontend tests |
| `make lint` | golangci-lint and oxlint |
| `make fmt` | Format Go code |
| `make docker` | Build the `yalkos:dev` image |
| `make clean` | Remove build outputs |

Run `make` with no target to list everything. The `.githooks/` directory holds the maintainer's opt-in local hooks.

## License

[MIT](LICENSE)
