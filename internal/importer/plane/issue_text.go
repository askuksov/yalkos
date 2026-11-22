package plane

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/store/q"
)

// relationKinds are the relation kinds Plane reports, in the order their
// lines are written, with the words of a line.
var relationKinds = []struct{ plane, text string }{
	{"blocking", "blocks"},
	{"blocked_by", "blocked by"},
	{"duplicate", "duplicate of"},
	{"relates_to", "relates to"},
	{"start_after", "starts after"},
	{"start_before", "starts before"},
	{"finish_after", "finishes after"},
	{"finish_before", "finishes before"},
}

const emptyComment = "_(empty comment)_"

// issueNamer names Plane work items as KEY-N: an imported one by its Yalkos
// key (it may have been renumbered), any other by the Plane identifier and
// number, also in projects that are not imported.
type issueNamer struct {
	c        *Client
	db       *q.Queries
	ex       *Existing
	projects map[string]Project
	// yalkos caches the current key of imported issues; "" when deleted.
	yalkos map[uuid.UUID]string
	// known maps the Plane id of every surveyed work item to its KEY-N.
	known map[string]string
	// fetched caches work items read one by one; "" when Plane does not show it.
	fetched map[string]string
}

func newIssueNamer(c *Client, db *q.Queries, ex *Existing, p Plan) *issueNamer {
	n := &issueNamer{
		c: c, db: db, ex: ex, projects: p.PlaneProjects,
		yalkos: map[uuid.UUID]string{}, known: map[string]string{}, fetched: map[string]string{},
	}
	for _, pp := range p.Projects {
		for _, it := range pp.Data.Items {
			n.known[it.ID] = issueKey(pp.Data.Identifier, it.SequenceID)
		}
	}
	return n
}

// imported is the current Yalkos key of an imported work item; false when
// it is not imported or was deleted in Yalkos.
func (n *issueNamer) imported(ctx context.Context, planeID string) (string, bool, error) {
	id, ok := n.ex.mapped(kindIssue, planeID)
	if !ok {
		return "", false, nil
	}
	k, ok := n.yalkos[id]
	if !ok {
		row, err := n.db.GetIssue(ctx, id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return "", false, fmt.Errorf("imported issue: %w", err)
		default:
			k = issueKey(row.ProjectKey, int(row.Issue.Number))
		}
		n.yalkos[id] = k
	}
	return k, k != "", nil
}

// local names a work item without asking Plane: imported or surveyed.
func (n *issueNamer) local(ctx context.Context, id string) (string, bool, error) {
	if k, ok, err := n.imported(ctx, id); ok || err != nil {
		return k, ok, err
	}
	k, ok := n.known[id]
	return k, ok, nil
}

// name names the target of a relation, reading it from Plane when it was not
// surveyed. false: Plane does not show it (deleted, archived or out of reach).
func (n *issueNamer) name(ctx context.Context, ref RelationRef) (string, bool, error) {
	if k, ok, err := n.local(ctx, ref.IssueID); ok || err != nil {
		return k, ok, err
	}
	if k, ok := n.fetched[ref.IssueID]; ok {
		return k, k != "", nil
	}
	p, ok := n.projects[ref.ProjectID]
	if !ok || ref.IssueID == "" {
		// A bare id names no project: a later reference with its project
		// may still find the work item, so nothing is remembered for it.
		if ref.ProjectID != "" {
			n.fetched[ref.IssueID] = ""
		}
		return "", false, nil
	}
	it, err := n.c.WorkItem(ctx, ref.ProjectID, ref.IssueID)
	var se *StatusError
	if errors.As(err, &se) && se.Code >= http.StatusBadRequest && se.Code < http.StatusInternalServerError {
		n.fetched[ref.IssueID] = ""
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("related work item: %w", err)
	}
	k := ""
	if it.SequenceID > 0 {
		k = issueKey(p.Identifier, it.SequenceID)
	}
	n.fetched[ref.IssueID] = k
	return k, k != "", nil
}

// relationLines lists the relations of a work item as "- blocked by KEY-N"
// lines; unresolved is set when a target could not be named.
func (n *issueNamer) relationLines(ctx context.Context, rels Relations) (lines []string, unresolved bool, err error) {
	for _, kind := range relationKinds {
		var seen []string
		for _, ref := range rels[kind.plane] {
			k, ok, err := n.name(ctx, ref)
			if err != nil {
				return nil, false, err
			}
			if !ok {
				unresolved = true
				continue
			}
			if slices.Contains(seen, k) {
				continue
			}
			seen = append(seen, k)
			lines = append(lines, "- "+kind.text+" "+k)
		}
	}
	return lines, unresolved, nil
}

// linkLines lists the web links as "- [title](url)" lines, or "- url" when a
// link has no title. A URL that is not safe to link is kept as text.
func linkLines(links []Link) []string {
	var lines []string
	for _, l := range links {
		u := strings.TrimSpace(l.URL)
		if u == "" {
			continue
		}
		title := oneLine(l.Title)
		switch {
		case !safeURL(u):
			text := u
			if title != "" {
				text = title + ": " + u
			}
			lines = append(lines, "- "+escapeFallback(text))
		case title == "":
			lines = append(lines, "- "+destEscaper.Replace(u))
		default:
			lines = append(lines, "- ["+escapeFallback(title)+"]("+destEscaper.Replace(u)+")")
		}
	}
	return lines
}

// describe appends the Links and Relations sections to a converted
// description. A body over the limit is cut to leave room for the sections,
// so they survive; cut reports that.
func describe(body string, links, relations []string) (desc string, cut bool) {
	var sections string
	if len(links) > 0 {
		sections += "\n\n### Links\n\n" + strings.Join(links, "\n")
	}
	if len(relations) > 0 {
		sections += "\n\n### Relations\n\n" + strings.Join(relations, "\n")
	}
	if body, cut = cutRunes(body, max(maxText-utf8.RuneCountInString(sections), 0)); cut {
		body += cutNote
	}
	return strings.TrimLeft(body+sections, "\n"), cut
}

// imagesOf lists the Plane image ids embedded in HTML.
func imagesOf(htmlText string) []string {
	if strings.TrimSpace(htmlText) == "" {
		return nil
	}
	res, _ := ToMarkdown(htmlText, Refs{})
	return res.Images
}

// convert turns Plane HTML into Markdown with mentions, work item embeds and
// the copied images resolved; unknown tags are counted into out.
func (w *Writer) convert(ctx context.Context, htmlText string, n *issueNamer, files *issueFiles, out *issueOutcome) (string, error) {
	if strings.TrimSpace(htmlText) == "" {
		return "", nil
	}
	var lookupErr error
	// A conversion error still leaves the plain text, which is kept.
	res, _ := ToMarkdown(htmlText, Refs{
		User: func(id string) (string, bool) {
			name, ok := w.names[id]
			return name, ok
		},
		Issue: func(id string) (string, bool) {
			k, ok, err := n.local(ctx, id)
			if err != nil && lookupErr == nil {
				lookupErr = err
			}
			return k, ok
		},
		Image: files.url,
	})
	if lookupErr != nil {
		return "", lookupErr
	}
	for tag, c := range res.Unknown {
		out.unknown[tag] += c
	}
	return res.Markdown, nil
}
