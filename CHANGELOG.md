# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-27

First release.

### Added

- Web app: My issues, project list and board with drag and drop, issue page with Markdown, filters and display options in the URL, `Cmd/Ctrl+K` command palette, keyboard shortcuts, light and dark themes.
- Projects with their own states, members and archiving; issues with sub-issues, priority, assignee, due date, type and labels; issue history; moving issues between projects.
- Milestones with target dates, progress and ordering; workspace-wide labels with groups; comments with mentions; file attachments and pasted screenshots.
- First-run setup, email and password sign-in, invite links, Owner/Admin/Member roles, personal API keys.
- Yalkos GraphQL API at `/api/graphql`.
- Linear-compatible GraphQL API at `/graphql` for existing scripts and tools; checked on every change with `@linear/sdk` 103.0.0.
- Webhooks in the Linear-compatible format with signatures, retries, a delivery log, test deliveries and secret rotation.
- MCP server at `/mcp` for AI agents such as Claude Code and OpenCode.
- `yalkos import plane`: import of a Plane workspace (projects, states, labels, modules as milestones, issues, comments, files).
- Docker image `ghcr.io/askuksov/yalkos` for amd64 and arm64, a compose file, `yalkos healthcheck`, `TRUSTED_PROXIES` for reverse proxies, and install documentation.

### Known limitations

- One workspace per instance; no email (invites and password resets are links and server commands); English interface only.
- No cycles, estimates, custom fields, saved views, notifications or private projects.
- Import from Plane: follow-ups are tracked in #55.
