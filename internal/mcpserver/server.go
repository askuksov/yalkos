// Package mcpserver serves the MCP endpoint: tools for AI agents over the
// domain services, acting as the owner of the API key.
package mcpserver

import (
	"log/slog"
	"net/http"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/buildinfo"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// maxBody caps request bodies (1 MiB), the same as the GraphQL endpoints.
const maxBody = 1 << 20

const instructions = `Yalkos is an issue tracker. Projects have keys such as YLK; issues have identifiers KEY-N such as YLK-12 — use them in tool calls, commits and branch names. Refer to states, labels, types, milestones and people by name; "me" is the owner of the API key and "none" clears a value. Every issue has at most one type (Bug, Feature, Task, Improvement unless renamed). Times are UTC, written YYYY-MM-DD HH:mm.`

// Deps are the services the tools use.
type Deps struct {
	Accounts    *account.Service
	Projects    *project.Service
	Issues      *issue.Service
	Milestones  *milestone.Service
	Labels      *label.Service
	Comments    *comment.Service
	Attachments *attachment.Service
	// BaseURL makes links in tool output absolute.
	BaseURL *url.URL
	Logger  *slog.Logger
}

// NewHandler returns the MCP Streamable HTTP handler: stateless, JSON
// responses only. Put it behind Guard.
func NewHandler(d Deps) http.Handler {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "yalkos", Title: "Yalkos", Version: buildinfo.Version},
		&mcp.ServerOptions{Instructions: instructions, Logger: d.Logger},
	)
	registerTools(srv, &d)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// A reverse proxy on the same host forwards its public Host to a
		// loopback address; Guard checks Origin and requires an API key instead.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        maxBody,
		Logger:                     d.Logger,
	})
}

func registerTools(srv *mcp.Server, d *Deps) {
	registerLookupTools(srv, d)
}
