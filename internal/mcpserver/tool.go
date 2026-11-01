package mcpserver

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/label"
)

// call is one tool call: the services, the key's user and lists loaded once
// per call.
type call struct {
	d     *Deps
	actor account.User

	users  []account.User
	labelX *labelIndex
}

// labelIndex is every label with the type group split out.
type labelIndex struct {
	all    []label.Label
	byID   map[uuid.UUID]label.Label
	groups map[uuid.UUID]label.Label
	types  label.TypeInfo
	isType map[uuid.UUID]bool
}

// members returns every user of the workspace, disabled ones included.
func (c *call) members(ctx context.Context) ([]account.User, error) {
	if c.users == nil {
		us, err := c.d.Accounts.ListMembers(ctx, c.actor)
		if err != nil {
			return nil, err
		}
		c.users = us
	}
	return c.users, nil
}

func (c *call) userByID(ctx context.Context, id uuid.UUID) (account.User, bool, error) {
	us, err := c.members(ctx)
	if err != nil {
		return account.User{}, false, err
	}
	for _, u := range us {
		if u.ID == id {
			return u, true, nil
		}
	}
	return account.User{}, false, nil
}

func (c *call) labelIdx(ctx context.Context) (*labelIndex, error) {
	if c.labelX != nil {
		return c.labelX, nil
	}
	all, err := c.d.Labels.List(ctx)
	if err != nil {
		return nil, err
	}
	ti, err := c.d.Labels.TypeInfo(ctx)
	if err != nil {
		return nil, err
	}
	x := &labelIndex{all: all, byID: map[uuid.UUID]label.Label{}, groups: map[uuid.UUID]label.Label{}, types: ti, isType: map[uuid.UUID]bool{}}
	for _, l := range all {
		x.byID[l.ID] = l
		if l.IsGroup {
			x.groups[l.ID] = l
		}
	}
	for _, id := range ti.LabelIDs {
		x.isType[id] = true
	}
	c.labelX = x
	return x, nil
}

func boolPtr(b bool) *bool { return &b }

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// addTool registers a tool whose handler gets the call context; its result
// becomes one text block of compact JSON, its error an isError text.
func addTool[In any](srv *mcp.Server, d *Deps, t *mcp.Tool, h func(ctx context.Context, c *call, in In) (any, error)) {
	mcp.AddTool(srv, t, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		a, ok := authn.ActorFrom(ctx)
		if !ok {
			// Guard admits only API-key callers; this is a wiring error.
			return nil, nil, errors.New("no API key actor")
		}
		c := &call{d: d, actor: a.User}
		out, err := h(ctx, c, in)
		var text string
		if err == nil {
			text, err = encode(out)
		}
		isErr := err != nil
		if isErr {
			text = userText(err, d.Logger)
		}
		d.Logger.Info("mcp tool", "tool", t.Name, "user_id", a.User.ID, "duration_ms", time.Since(start).Milliseconds(), "error", isErr)
		return &mcp.CallToolResult{IsError: isErr, Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	})
}

func writes(title string, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(false), IdempotentHint: idempotent, OpenWorldHint: boolPtr(false)}
}
