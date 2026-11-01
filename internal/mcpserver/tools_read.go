package mcpserver

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
)

const (
	defaultSearchLimit   = 25
	maxSearchLimit       = 100
	defaultCommentsLimit = 20
	maxCommentsLimit     = 50
	maxDescription       = 20_000
	issueComments        = 5
	maxIssueCommentBody  = 2_000
	maxCommentBody       = 10_000
	maxSubIssues         = 50
)

type searchIn struct {
	Project   string   `json:"project,omitempty" jsonschema:"Project key, e.g. YLK. Required when milestone is a name. Archived projects are searched only when named here."`
	Query     string   `json:"query,omitempty" jsonschema:"Words to find in title and description (prefix match), or an identifier such as YLK-12"`
	State     []string `json:"state,omitempty" jsonschema:"State names, e.g. [\"In Progress\"]"`
	StateType []string `json:"stateType,omitempty" jsonschema:"backlog, unstarted, started, completed, canceled"`
	Assignee  string   `json:"assignee,omitempty" jsonschema:"\"me\", \"none\" (unassigned), an email or a name"`
	Type      []string `json:"type,omitempty" jsonschema:"Type names, e.g. [\"Bug\"]; [\"none\"] = issues without a type"`
	Labels    []string `json:"labels,omitempty" jsonschema:"Issues with any of these labels"`
	Milestone string   `json:"milestone,omitempty" jsonschema:"Milestone name (needs project) or id; \"none\" = issues without a milestone"`
	Priority  []string `json:"priority,omitempty" jsonschema:"urgent, high, medium, low, none"`
	Parent    string   `json:"parent,omitempty" jsonschema:"Identifier of the parent issue (its sub-issues); \"none\" = only top-level issues"`
	OrderBy   string   `json:"orderBy,omitempty" jsonschema:"updated (default, newest first), created (newest first), priority (urgent first), dueDate (soonest first), manual (board order)"`
	Limit     int      `json:"limit,omitempty" jsonschema:"1–100, default 25"`
	Cursor    string   `json:"cursor,omitempty" jsonschema:"nextCursor from the previous call with the same filters"`
}

type issueIn struct {
	Issue string `json:"issue" jsonschema:"Identifier such as YLK-12"`
}

type listCommentsIn struct {
	Issue  string `json:"issue" jsonschema:"Identifier such as YLK-12"`
	Limit  int    `json:"limit,omitempty" jsonschema:"1–50, default 20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"olderCursor from the previous call"`
}

var orderBys = []struct {
	name  string
	order issue.Order
}{
	{"updated", issue.Order{Field: issue.UpdatedAt, Desc: true}},
	{"created", issue.Order{Field: issue.CreatedAt, Desc: true}},
	{"priority", issue.Order{Field: issue.Priority}},
	{"dueDate", issue.Order{Field: issue.DueDate}},
	{"manual", issue.Order{Field: issue.SortOrder}},
}

var stateTypes = []project.StateType{project.Backlog, project.Unstarted, project.Started, project.Completed, project.Canceled}

func parseOrder(s string) (issue.Order, error) {
	if strings.TrimSpace(s) == "" {
		return orderBys[0].order, nil
	}
	for _, o := range orderBys {
		if same(s, o.name) {
			return o.order, nil
		}
	}
	return issue.Order{}, errf("Unknown orderBy %q. Use one of: updated, created, priority, dueDate, manual.", strings.TrimSpace(s))
}

func parseStateType(s string) (project.StateType, error) {
	for _, t := range stateTypes {
		if strings.TrimSpace(s) == string(t) {
			return t, nil
		}
	}
	return "", errf("Unknown stateType %q. Use one of: backlog, unstarted, started, completed, canceled.", strings.TrimSpace(s))
}

func limitOf(n, def, maxN int) (int, error) {
	switch {
	case n == 0:
		return def, nil
	case n < 1 || n > maxN:
		return 0, errf("limit must be 1–%d.", maxN)
	}
	return n, nil
}

// cursorErr replaces a domain cursor error with advice for the agent.
func cursorErr(err error) error {
	var ie *kit.InputError
	if errors.As(err, &ie) && (ie.Field == "after" || ie.Field == "before") {
		return errf("cursor is invalid; repeat the search without it.")
	}
	return err
}

func yes() *bool { return boolPtr(true) }

// filter turns search input into a domain filter.
func (c *call) filter(ctx context.Context, in searchIn) (issue.Filter, error) {
	var f issue.Filter
	var p *project.Project
	if strings.TrimSpace(in.Project) != "" {
		pr, err := c.project(ctx, in.Project)
		if err != nil {
			return f, err
		}
		p = &pr
		f.Project = &issue.IDCond{Eq: &pr.ID}
	}
	if strings.TrimSpace(in.Query) != "" {
		q := in.Query
		f.Search = &q
	}
	if len(in.State) > 0 {
		var ids []uuid.UUID
		if p != nil {
			for _, name := range in.State {
				s, err := c.state(ctx, *p, name)
				if err != nil {
					return f, err
				}
				ids = append(ids, s.ID)
			}
		} else {
			var err error
			if ids, err = c.statesNamed(ctx, in.State); err != nil {
				return f, err
			}
		}
		f.State = &issue.IDCond{In: ids}
	}
	if len(in.StateType) > 0 {
		var ts []project.StateType
		for _, s := range in.StateType {
			t, err := parseStateType(s)
			if err != nil {
				return f, err
			}
			ts = append(ts, t)
		}
		f.StateType = &issue.StateTypeCond{In: ts}
	}
	if strings.TrimSpace(in.Assignee) != "" {
		id, err := c.person(ctx, in.Assignee)
		if err != nil {
			return f, err
		}
		if id == nil {
			f.Assignee = &issue.IDCond{Null: yes()}
		} else {
			f.Assignee = &issue.IDCond{Eq: id}
		}
	}
	if len(in.Type) > 0 {
		hasNone := false
		for _, s := range in.Type {
			hasNone = hasNone || isNone(s)
		}
		switch {
		case hasNone && len(in.Type) > 1:
			return f, errf(`type is either type names or ["none"], not both.`)
		case hasNone:
			f.Type = &issue.TypeCond{Null: yes()}
		default:
			var ids []uuid.UUID
			for _, s := range in.Type {
				id, err := c.typeLabel(ctx, s)
				if err != nil {
					return f, err
				}
				ids = append(ids, *id)
			}
			f.Type = &issue.TypeCond{In: ids}
		}
	}
	if len(in.Labels) > 0 {
		ids, err := c.labels(ctx, in.Labels)
		if err != nil {
			return f, err
		}
		f.Label = &issue.LabelCond{In: ids}
	}
	if ref := strings.TrimSpace(in.Milestone); ref != "" {
		switch {
		case isNone(ref):
			f.Milestone = &issue.IDCond{Null: yes()}
		case p != nil:
			m, err := c.milestone(ctx, *p, ref)
			if err != nil {
				return f, err
			}
			f.Milestone = &issue.IDCond{Eq: &m.ID}
		default:
			id, err := uuid.Parse(ref)
			if err != nil {
				return f, errf("milestone needs project when it is a name.")
			}
			if _, err := c.d.Milestones.Get(ctx, id); err != nil {
				return f, err
			}
			f.Milestone = &issue.IDCond{Eq: &id}
		}
	}
	if len(in.Priority) > 0 {
		var ns []int
		for _, s := range in.Priority {
			n, err := parsePriority(s)
			if err != nil {
				return f, err
			}
			ns = append(ns, n)
		}
		f.Priority = &issue.IntCond{In: ns}
	}
	if ref := strings.TrimSpace(in.Parent); ref != "" {
		if isNone(ref) {
			f.Parent = &issue.IDCond{Null: yes()}
		} else {
			i, err := c.issue(ctx, ref)
			if err != nil {
				return f, err
			}
			f.Parent = &issue.IDCond{Eq: &i.ID}
		}
	}
	return f, nil
}

type personOut struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type parentOut struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
}

type childOut struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	State      string `json:"state"`
}

type subIssuesOut struct {
	Completed int        `json:"completed"`
	Total     int        `json:"total"`
	Items     []childOut `json:"items"`
}

type attachmentOut struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type commentOut struct {
	ID        string `json:"id"`
	Author    string `json:"author"`
	CreatedAt string `json:"createdAt"`
	Edited    bool   `json:"edited"`
	Body      string `json:"body"`
	Truncated bool   `json:"truncated,omitempty"`
}

type issueOut struct {
	Identifier           string          `json:"identifier"`
	URL                  string          `json:"url"`
	Title                string          `json:"title"`
	Description          string          `json:"description,omitempty"`
	DescriptionTruncated bool            `json:"descriptionTruncated,omitempty"`
	Project              projectRef      `json:"project"`
	State                string          `json:"state"`
	StateType            string          `json:"stateType"`
	Priority             string          `json:"priority"`
	Type                 string          `json:"type,omitempty"`
	Labels               []string        `json:"labels,omitempty"`
	Assignee             *personOut      `json:"assignee,omitempty"`
	Creator              *personOut      `json:"creator,omitempty"`
	Milestone            string          `json:"milestone,omitempty"`
	Parent               *parentOut      `json:"parent,omitempty"`
	SubIssues            *subIssuesOut   `json:"subIssues,omitempty"`
	DueDate              string          `json:"dueDate,omitempty"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
	CompletedAt          string          `json:"completedAt,omitempty"`
	CanceledAt           string          `json:"canceledAt,omitempty"`
	Attachments          []attachmentOut `json:"attachments,omitempty"`
	CommentCount         int             `json:"commentCount"`
	Comments             []commentOut    `json:"comments,omitempty"`
}

type projectRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (c *call) personOf(id *uuid.UUID) *personOut {
	if id == nil {
		return nil
	}
	for _, u := range c.users {
		if u.ID == *id {
			return &personOut{Name: u.Name, Email: u.Email}
		}
	}
	return nil
}

// comments shapes comments for output: absolute links, bodies cut at limit.
func (c *call) comments(cs []comment.Comment, limit int) []commentOut {
	out := make([]commentOut, len(cs))
	for n, cm := range cs {
		body, cutDone := cut(absLinks(cm.Body, c.base()), limit)
		out[n] = commentOut{
			ID: cm.ID.String(), Author: c.userName(&cm.AuthorID), CreatedAt: stamp(cm.CreatedAt),
			Edited: cm.EditedAt != nil, Body: body, Truncated: cutDone,
		}
	}
	return out
}

func (c *call) getIssue(ctx context.Context, ref string) (issueOut, error) {
	i, err := c.issue(ctx, ref)
	if err != nil {
		return issueOut{}, err
	}
	sums, err := c.summaries(ctx, []issue.Issue{i})
	if err != nil {
		return issueOut{}, err
	}
	s := sums[0]
	p, err := c.d.Projects.Get(ctx, i.ProjectID.String())
	if err != nil {
		return issueOut{}, err
	}
	desc, descCut := cut(absLinks(i.Description, c.base()), maxDescription)
	out := issueOut{
		Identifier: i.Identifier, URL: s.URL, Title: i.Title, Description: desc, DescriptionTruncated: descCut,
		Project: projectRef{Key: p.Key, Name: p.Name}, State: s.State, StateType: s.StateType, Priority: s.Priority,
		Type: s.Type, Labels: s.Labels, Assignee: c.personOf(i.AssigneeID), Creator: c.personOf(&i.CreatorID),
		Milestone: s.Milestone, DueDate: s.DueDate, CreatedAt: stamp(i.CreatedAt), UpdatedAt: s.UpdatedAt,
		CompletedAt: stampPtr(i.CompletedAt), CanceledAt: stampPtr(i.CanceledAt),
	}
	if i.ParentID != nil {
		parents, err := c.d.Issues.ByIDs(ctx, []uuid.UUID{*i.ParentID})
		if err != nil {
			return issueOut{}, err
		}
		if len(parents) == 1 {
			out.Parent = &parentOut{Identifier: parents[0].Identifier, Title: parents[0].Title}
		}
	}

	first := maxSubIssues
	children, err := c.d.Issues.List(ctx, issue.Filter{Parent: &issue.IDCond{Eq: &i.ID}, IncludeArchived: true}, issue.Order{Field: issue.SortOrder}, page.Args{First: &first})
	if err != nil {
		return issueOut{}, err
	}
	if len(children.Items) > 0 {
		prog, err := c.d.Issues.SubIssueProgress(ctx, []uuid.UUID{i.ID})
		if err != nil {
			return issueOut{}, err
		}
		stateIDs := make([]uuid.UUID, len(children.Items))
		for n, ch := range children.Items {
			stateIDs[n] = ch.StateID
		}
		states, err := c.d.Projects.StatesByIDs(ctx, stateIDs)
		if err != nil {
			return issueOut{}, err
		}
		names := map[uuid.UUID]string{}
		for _, st := range states {
			names[st.ID] = st.Name
		}
		sub := &subIssuesOut{Completed: prog[i.ID].Completed, Total: prog[i.ID].Total}
		for _, ch := range children.Items {
			sub.Items = append(sub.Items, childOut{Identifier: ch.Identifier, Title: ch.Title, State: names[ch.StateID]})
		}
		out.SubIssues = sub
	}

	files, err := c.d.Attachments.List(ctx, i.ID)
	if err != nil {
		return issueOut{}, err
	}
	for _, a := range files {
		out.Attachments = append(out.Attachments, attachmentOut{Name: a.Name, Size: a.Size, URL: c.link(attachment.URL(a))})
	}

	counts, err := c.d.Comments.Counts(ctx, []uuid.UUID{i.ID})
	if err != nil {
		return issueOut{}, err
	}
	out.CommentCount = counts[i.ID]
	last := issueComments
	cs, err := c.d.Comments.List(ctx, i.ID, page.Args{Last: &last})
	if err != nil {
		return issueOut{}, err
	}
	out.Comments = c.comments(cs.Items, maxIssueCommentBody)
	return out, nil
}

func registerReadTools(srv *mcp.Server, d *Deps) {
	addTool(srv, d, &mcp.Tool{
		Name: "search_issues", Title: "Search issues", Annotations: readOnly("Search issues"),
		Description: "Find issues. Every filter is optional; filters combine with AND, and a list inside one filter means any of. Without filters it returns the most recently updated issues of all active projects. Returns at most limit issues plus nextCursor when there are more — pass it back as cursor for the next page. Descriptions are not included; use get_issue.",
	}, func(ctx context.Context, c *call, in searchIn) (any, error) {
		limit, err := limitOf(in.Limit, defaultSearchLimit, maxSearchLimit)
		if err != nil {
			return nil, err
		}
		order, err := parseOrder(in.OrderBy)
		if err != nil {
			return nil, err
		}
		f, err := c.filter(ctx, in)
		if err != nil {
			return nil, err
		}
		args := page.Args{First: &limit}
		if in.Cursor != "" {
			args.After = &in.Cursor
		}
		res, err := c.d.Issues.List(ctx, f, order, args)
		if err != nil {
			return nil, cursorErr(err)
		}
		total, err := c.d.Issues.Count(ctx, f)
		if err != nil {
			return nil, err
		}
		sums, err := c.summaries(ctx, res.Items)
		if err != nil {
			return nil, err
		}
		out := struct {
			Issues     []summary `json:"issues"`
			TotalCount int       `json:"totalCount"`
			NextCursor string    `json:"nextCursor,omitempty"`
		}{Issues: sums, TotalCount: total}
		if res.Info.HasNext && res.Info.End != nil {
			out.NextCursor = *res.Info.End
		}
		return out, nil
	})

	addTool(srv, d, &mcp.Tool{
		Name: "get_issue", Title: "Get issue", Annotations: readOnly("Get issue"),
		Description: "Get one issue by identifier (YLK-12): all fields, the description, sub-issues, files and the latest comments. Use list_comments for older comments.",
	}, func(ctx context.Context, c *call, in issueIn) (any, error) {
		return c.getIssue(ctx, in.Issue)
	})

	addTool(srv, d, &mcp.Tool{
		Name: "list_comments", Title: "List comments", Annotations: readOnly("List comments"),
		Description: "List the comments of an issue, newest page first, each page in time order. Pass olderCursor back as cursor to read earlier comments.",
	}, func(ctx context.Context, c *call, in listCommentsIn) (any, error) {
		limit, err := limitOf(in.Limit, defaultCommentsLimit, maxCommentsLimit)
		if err != nil {
			return nil, err
		}
		i, err := c.issue(ctx, in.Issue)
		if err != nil {
			return nil, err
		}
		args := page.Args{Last: &limit}
		if in.Cursor != "" {
			args.Before = &in.Cursor
		}
		res, err := c.d.Comments.List(ctx, i.ID, args)
		if err != nil {
			return nil, cursorErr(err)
		}
		if _, err := c.members(ctx); err != nil {
			return nil, err
		}
		out := struct {
			Issue       string       `json:"issue"`
			Comments    []commentOut `json:"comments"`
			OlderCursor string       `json:"olderCursor,omitempty"`
		}{Issue: i.Identifier, Comments: c.comments(res.Items, maxCommentBody)}
		if res.Info.HasPrev && res.Info.Start != nil {
			out.OlderCursor = *res.Info.Start
		}
		return out, nil
	})
}
