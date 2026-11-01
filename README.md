# Yalkos

Lightweight, self-hosted issue tracker with a Linear-compatible API.

> Status: early development. Accounts, invites, API keys, projects, issues, labels, milestones, comments and file attachments work in the browser and through the API; AI agents such as Claude Code and OpenCode work with issues over MCP.

## Run with Docker

```sh
cd deploy
echo "YALKOS_DB_PASSWORD=$(openssl rand -hex 24)" > .env   # once; keep this file private
docker compose up -d
```

Open http://localhost:8080 (set `YALKOS_PORT` to use another port). Two containers start: `app` and `db` (PostgreSQL 18). Migrations run automatically on start. Only the app port is published; the database is reachable from the `app` container only. Uploaded files live in the `yalkos-data` volume (`/data` in the app container).

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

### Backup

Back up the database and the files together:

```sh
docker compose exec -T db pg_dump -U yalkos yalkos > yalkos.sql
docker run --rm -v yalkos_yalkos-data:/data -v "$PWD":/backup alpine tar czf /backup/yalkos-data.tgz -C /data .
```

Files are stored under `/data/blobs/` by content hash; restore both the dump and the archive.

### API keys

Create a key under **API keys** (it is shown once). Keys look like `yal_api_…` and work with these endpoints:

| Endpoint | For |
| :--- | :--- |
| `POST /graphql` | Linear-compatible API for `@linear/sdk` scripts and tools ([below](#linear-compatible-api)) |
| `POST /api/graphql` | Yalkos API |
| `POST /mcp` | AI agents (MCP): Claude Code, OpenCode |

Send the key as `Authorization: <key>` or `Authorization: Bearer <key>`. Keys act as their user but cannot manage credentials: creating or revoking keys, changing the password and inviting people need a signed-in browser session.

### AI agents (MCP)

`https://<host>/mcp` is an MCP server (Streamable HTTP) for AI coding agents. Connect it with an API key from **Settings → API keys**: the agent acts as the key's user, and every change it makes shows in the issue's history under that user. The endpoint accepts API keys only, not browser sessions. The **API keys** page shows both setups below with your address filled in.

The examples read the key from `YALKOS_API_KEY`; set it in the shell first:

```sh
export YALKOS_API_KEY=yal_api_...   # the key from Settings → API keys
```

**Claude Code:**

```sh
claude mcp add --transport http yalkos https://app.example.com/mcp --header "Authorization: Bearer $YALKOS_API_KEY"
```

This saves the server for you in this folder (scope `local`, the default). With `--scope project` Claude Code writes `.mcp.json` into the repository instead — then keep the key out of it with a variable:

```json
{
  "mcpServers": {
    "yalkos": {
      "type": "http",
      "url": "https://app.example.com/mcp",
      "headers": { "Authorization": "Bearer ${YALKOS_API_KEY}" }
    }
  }
}
```

**OpenCode** (`opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "yalkos": {
      "type": "remote",
      "url": "https://app.example.com/mcp",
      "headers": { "Authorization": "Bearer {env:YALKOS_API_KEY}" },
      "oauth": false
    }
  }
}
```

Start OpenCode from a shell where `YALKOS_API_KEY` is set. `"oauth": false` tells OpenCode the server uses API keys, so a wrong key shows an error instead of an OAuth login.

Tools:

| Tool | Does |
| :--- | :--- |
| `list_projects` | Projects with their keys; archived ones on request |
| `list_states` | Workflow states of a project in board order, with their type |
| `list_milestones` | Milestones of a project with status, progress, target date and lead |
| `list_labels` | Issue types and labels, with label groups |
| `list_users` | Active people with name, email and role; `"me": true` marks the key's owner |
| `search_issues` | Find issues by project, words, state, assignee, type, labels, milestone, priority or parent; paged |
| `get_issue` | One issue with description, sub-issues, files and the latest comments |
| `list_comments` | Comments of an issue, newest page first |
| `create_issue` | Create an issue or a sub-issue |
| `update_issue` | Change fields of an issue; only the fields passed change |
| `add_comment` | Add a Markdown comment to an issue |
| `create_milestone` | Create a milestone in a project |
| `update_milestone` | Change a milestone, or pin or release its status |

Conventions: issues are named by identifier (`YLK-12`); states, labels, types, milestones and people by name (people also by email); `me` is the key's owner; `none` clears a value in updates and matches an empty one in searches. Times are UTC, written `YYYY-MM-DD HH:mm`.

### Linear-compatible API

`POST https://<host>/graphql` speaks a Linear-compatible GraphQL API, so scripts written for `@linear/sdk` work with Yalkos once `apiUrl` and the key change. Use an API key from **Settings → API keys**, sent as `Authorization: <key>` or `Authorization: Bearer <key>`.

```js
import { LinearClient } from "@linear/sdk";

const client = new LinearClient({
  apiKey: process.env.YALKOS_API_KEY,
  apiUrl: "https://tracker.example.com/graphql",
});

const me = await client.viewer;
const mine = await client.issues({ filter: { assignee: { isMe: { eq: true } } } });
```

- Tested with `@linear/sdk` 100.x. The SDK requires an `https://` URL except on `localhost`.
- Names: a Team is a Yalkos project, a Project is a milestone, a WorkflowState is a state, an IssueLabel is a label (issue types are the labels of the Type group), the Organization is the workspace.
- Supported: the viewer, users, teams with their states, members and labels, workflow states, issues (read, filter, sort, search, create, update, move to another team with `teamId`, delete), comments, labels and label groups, projects with their statuses, and the files uploaded to an issue (read only). `issue(id:)` and `team(id:)` also take `YLK-12` and `YLK`.
- Not available: estimates, cycles, issue relations, archiving issues, link attachments and webhooks. An input or filter field that is not supported is rejected with an error naming it, never ignored.
- After `deleteIssue` or `deleteProject` the deleted item can no longer be read: use `payload.success` and `payload.entityId`, not `await payload.entity`.
- Query cost: each query may cost up to 10 000 points (0.1 per field, 1 per object, times the page size for lists); the `X-Complexity` response header shows a query's cost. With the SDK's full selections keep `first` at 200 or below.
- Other tools need a configurable endpoint: `LINEAR_GRAPHQL_ENDPOINT=https://<host>/graphql` for linear-cli; SDK-based tools need `apiUrl`.
- API keys are limited to 5 000 requests per hour per user on every endpoint (`API_RATE_LIMIT`, `0` turns it off). Responses carry `X-RateLimit-Requests-Limit`, `-Remaining` and `-Reset`; over the limit the answer is 429 with `Retry-After`.

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

### Labels, milestones, comments and files

- **Labels** belong to the workspace: `labels`, `createLabel`, `updateLabel`, `deleteLabel`. A label with `isGroup: true` holds labels one level deep; in an `exclusive` group an issue carries at most one of its labels. The group **Type** (Bug, Feature, Task, Improvement) gives every issue its type: a new issue without one gets the default type (`labelSettings { defaultType }`, `setDefaultType`). On issues: `labels`, `type`; inputs `labelIds` (replace), `addLabelIds`, `removeLabelIds`; filters `label: {in, nin, null}` and `type: {in, null}`.
- **Milestones** belong to one project: `project { milestones }`, `milestone(id:)`, `createMilestone`, `updateMilestone`, `reorderMilestones`, `deleteMilestone`. The status follows the issues (planned → in progress → done) until set by hand; `updateMilestone(input: {statusAutomatic: true})` returns to automatic. `progress { completed total }` counts live issues, canceled ones aside. Issues have `milestone` and `milestoneId`; a moved issue leaves its milestone.
- **Comments** are flat Markdown: `issue { comments commentCount }`, `createComment`, `updateComment` (author only), `deleteComment` (author, Owner or Admin; the text is gone for good). `issue { timeline }` lists comments and changes by time.
- **Files** go over plain HTTP with a session or an API key; nothing is public:

```sh
# Upload (multipart; send issueId before the file). Without issueId the file waits for
# the issue or comment that links it, and is removed after 24 hours if nothing does.
curl -s http://localhost:8080/api/attachments -H "Authorization: $KEY" -F issueId=<issue id> -F file=@shot.png
# {"id":"…","url":"/api/attachments/…/shot.png","name":"shot.png","size":48213,"contentType":"image/png","inline":true}

curl -s -o shot.png -H "Authorization: $KEY" http://localhost:8080/api/attachments/<id>/shot.png
```

  The server detects the file type from its content. PNG, JPEG, GIF, WebP and PDF open in the browser; every other file (SVG and HTML included) downloads. Links in Markdown are stored relative (`/api/attachments/…`). `issue { attachments }` lists an issue's files, `deleteAttachment` removes one (uploader, Owner or Admin); `limits { maxUploadMb }` is the size limit.

## Using Yalkos

- **My issues** lists everything assigned to you, grouped by state type. Each project has a **list** (grouped by state, sub-issues nested under their parent) and a **board** (one column per state; drag cards between columns or within one).
- Filters and display options live in the URL, so a filtered view can be bookmarked or shared.
- The issue page edits the title and the Markdown description in place; properties (state, type, labels, milestone, priority, assignee, due date, parent, project) open a picker. Moving an issue to another project gives it a new identifier and removes its milestone; old identifiers and old project keys keep working as links.
- Paste or drop a screenshot into a description or comment to upload it; drop files anywhere on an issue page or use **Attach file** to add them to its **Attachments**. `YLK-12` in any text links to that issue; `@` in an editor mentions a member.
- Comments and changes share one timeline under the issue. `Cmd/Ctrl+Enter` posts a comment; an unsent comment is kept as a draft in the browser.
- Each project has **Milestones** (drag to reorder) with progress, target date and lead; a milestone's page lists its issues. Labels, groups and the default type are under **Settings → Labels**.
- `Cmd+K` (`Ctrl+K` on Windows and Linux) opens the command palette: search issues by text, type an identifier such as `YLK-12` to open it, jump to a project, or change the open issue.
- **Browse projects** lists every project; join or leave them there. Owners and Admins create projects and manage states, members and archiving under the project's **Settings**.
- Theme: System, Light or Dark from the user menu at the bottom of the sidebar.

Keyboard shortcuts (shortcuts follow the physical key, so they work with any keyboard layout; `?` shows them in the app):

| Key | Where | Action |
| :--- | :--- | :--- |
| `Cmd/Ctrl+K` | anywhere | Command palette |
| `/` | anywhere | Search issues |
| `C` | anywhere | Create issue (in the current project, or as a sub-issue on an issue page) |
| `J` / `K` | list, board | Next / previous issue |
| `H` / `L` | board | Previous / next column |
| `Enter` | list, board | Open the selected issue |
| `X` | list, board | Select for bulk changes (state, assignee, priority, delete) |
| `S` / `A` / `P` | selected or open issue | Change state / assignee / priority |
| `T` / `M` | selected or open issue | Set type / milestone |
| `L` | list, issue page | Add a label |
| `E` | issue page | Edit the description |
| `F` | list, board | Focus the text filter |
| `[` | anywhere | Collapse the sidebar |
| `?` | anywhere | Shortcuts help |
| `Esc` | anywhere | Close the dialog, clear the selection, go back from an issue |

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
| `DATA_DIR` | `./data` (`/data` in Docker) | Uploaded files (created with mode 0700) |
| `MAX_UPLOAD_MB` | `25` | Largest file to upload, 1–1024 |
| `API_RATE_LIMIT` | `5000` | API-key requests per hour and user on every endpoint; `0` turns the limit off |

Local ports used by `make dev` / `make test`, override if they are taken:

| Variable | Default | Used for |
| :--- | :--- | :--- |
| `YALKOS_PORT` | `8080` | Go server |
| `YALKOS_WEB_PORT` | `5173` | Vite dev server |
| `YALKOS_DB_PORT` | `54320` | PostgreSQL container |

## License

[MIT](LICENSE)
