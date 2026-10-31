# Yalkos

Lightweight, self-hosted issue tracker with a Linear-compatible API.

> Status: early development. Accounts, invites, API keys, projects and issues work; projects and issues are API-only until the issue UI lands.

## Run with Docker

```sh
cd deploy
echo "YALKOS_DB_PASSWORD=$(openssl rand -hex 24)" > .env   # once; keep this file private
docker compose up -d
```

Open http://localhost:8080 (set `YALKOS_PORT` to use another port). Two containers start: `app` and `db` (PostgreSQL 18). Migrations run automatically on start. Only the app port is published; the database is reachable from the `app` container only.

PostgreSQL stores the password when its data volume is first created; changing `YALKOS_DB_PASSWORD` later requires changing it inside the database as well.

Behind a domain, set the public URL in `deploy/.env` (invite links and secure cookies depend on it). Open the app at exactly this address — requests from another address (for example the server IP) are rejected with “request origin does not match BASE_URL”:

```sh
YALKOS_BASE_URL=https://app.example.com
```

### First run

A fresh instance prints a one-time setup link in its log. Whoever opens it first becomes the Owner:

```sh
docker compose logs app | grep "setup required"
# {"level":"WARN","msg":"setup required","url":"http://localhost:8080/setup?code=7EV2RUJH"}
```

The code changes on every restart until setup is done. After that, invite people from **Members** (links are single-use and expire after 7 days).

Forgot the password? There is no email in this version; reset it from the server:

```sh
docker compose exec -T app /yalkos user set-password --email you@example.com <<< 'new-password'
```

### API keys

Create a key under **API keys** (it is shown once). Keys look like `yal_api_…` and work with both endpoints:

| Endpoint | For |
| :--- | :--- |
| `POST /graphql` | Linear-compatible API (`@linear/sdk`, Linear tools) |
| `POST /api/graphql` | Yalkos API |

```ts
import { LinearClient } from '@linear/sdk'

const linear = new LinearClient({ apiKey: process.env.YALKOS_API_KEY, apiUrl: 'https://app.example.com/graphql' })
const me = await linear.viewer
```

Send the key as `Authorization: <key>` or `Authorization: Bearer <key>`. Keys act as their user but cannot manage credentials: creating or revoking keys, changing the password and inviting people need a signed-in browser session.

### Projects and issues

Projects, workflow states and issues (with sub-issues) are available through the Yalkos API at `POST /api/graphql`; the schema is in `api/graphql/native/` and introspection is on. Owners and Admins manage projects and states; every user works with issues and can join or leave a project (`addProjectMember` / `removeProjectMember` with their own id). API keys can do both.

```sh
API=http://localhost:8080/api/graphql
KEY=yal_api_...

# Create a project: it gets the states Backlog, Todo (default), In Progress, Done, Canceled.
curl -s $API -H "Authorization: $KEY" -H 'Content-Type: application/json' \
  -d '{"query":"mutation { createProject(input: {key: \"YLK\", name: \"Yalkos\"}) { id key } }"}'

# Create an issue: it becomes YLK-1.
curl -s $API -H "Authorization: $KEY" -H 'Content-Type: application/json' \
  -d '{"query":"mutation($in: CreateIssueInput!) { createIssue(input: $in) { identifier state { name } } }",
       "variables":{"in":{"projectId":"<project id>","title":"First issue","priority":2,"dueDate":"2026-12-01"}}}'

# My open issues, highest priority first.
curl -s $API -H "Authorization: $KEY" -H 'Content-Type: application/json' \
  -d '{"query":"{ issues(filter: {assignee: {isMe: true}, stateType: {in: [UNSTARTED, STARTED]}}, orderBy: {field: PRIORITY}, first: 50) { nodes { identifier title priority } pageInfo { hasNextPage endCursor } } }"}'
```

- `issue(id:)` accepts a UUID or an identifier such as `YLK-1`. Identifiers keep working after the issue moves to another project or the project key changes.
- Lists are Relay connections: `first`/`after` or `last`/`before` (default 50, max 250). `issues { totalCount }` counts all matches.
- `filter: {search: "fix log"}` finds issues whose title or description has words starting with each term; an identifier such as `YLK-12` also finds that issue.
- Every change to a project, state or issue is recorded; read it with `issue { history { nodes { action actor { name } changes { field from to } } } }`.
- Validation errors carry `extensions.code: "INVALID_INPUT"`, `extensions.field`, `extensions.reason` (for example `required`, `too_long`, `taken`, `archived`) and optional `extensions.params`.

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
| `BASE_URL` | `http://localhost:8080` | Public URL (invite links, `Secure` cookies for https) |

Local ports used by `make dev` / `make test`, override if they are taken:

| Variable | Default | Used for |
| :--- | :--- | :--- |
| `YALKOS_PORT` | `8080` | Go server |
| `YALKOS_WEB_PORT` | `5173` | Vite dev server |
| `YALKOS_DB_PORT` | `54320` | PostgreSQL container |

## License

[MIT](LICENSE)
