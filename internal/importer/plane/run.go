package plane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/store/q"
)

// sourceKind is the import_sources kind of a Plane workspace.
const sourceKind = "plane"

// RunOptions configure one import run.
type RunOptions struct {
	Client *Client
	Pool   *pgxpool.Pool
	// Store receives the copied files, each at most MaxUpload bytes.
	Store     blob.Store
	MaxUpload int64
	// Actor is recorded for what Plane's seed bot or an unknown user created.
	Actor account.User
	// Keys are the Plane project identifiers to import; none means all.
	Keys []string
	// DryRun only reads the Plane lists and reports what a run would do.
	DryRun bool
	// URL and Workspace name the Plane instance and workspace in the report.
	URL, Workspace string
	// Progress (may be nil) receives a line at the start and the end of each
	// project and every 50 issues (one line for a project with no new issue),
	// then for the issues changed in Plane every 50 and at the end of each
	// project.
	Progress io.Writer
	Now      func() time.Time
}

// Run imports a Plane workspace: it surveys it, plans the import against
// what earlier imports of the same workspace left, and then writes the base
// entities, the new issues and the new comments of issues changed in Plane.
// Once the workspace is surveyed, the report is returned with any error, so
// what was done can still be shown. Conflicts stop the run with a
// *ConflictError before anything is written; a dry run writes nothing at all.
func Run(ctx context.Context, opts RunOptions) (*Report, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	src := Source{URL: opts.URL, Workspace: opts.Workspace}
	ws, err := Survey(ctx, opts.Client, opts.Keys)
	if err != nil {
		return nil, err
	}
	if len(ws.Projects) == 0 {
		return &Report{Source: src, DryRun: opts.DryRun}, nil
	}
	planeWS := ws.Projects[0].Workspace
	if planeWS == "" {
		return nil, fmt.Errorf("plane: project %s names no workspace", ws.Projects[0].Identifier)
	}

	db := q.New(opts.Pool)
	source := uuid.Nil
	row, err := db.GetImportSource(ctx, q.GetImportSourceParams{Kind: sourceKind, SourceID: planeWS})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("import source: %w", err)
	default:
		source = row.ID
	}
	ex, err := LoadExisting(ctx, db, source)
	if err != nil {
		return nil, err
	}
	p, err := MakePlan(ws, ex)
	if err != nil {
		return nil, err
	}
	if err := nameChanged(ctx, db, &p, &ex); err != nil {
		return nil, err
	}
	r := p.Report
	r.Source, r.DryRun = src, opts.DryRun
	if !opts.DryRun {
		r.resetWritten()
	}
	if len(p.Conflicts) > 0 {
		return r, &ConflictError{Keys: p.Conflicts}
	}
	if opts.DryRun {
		return r, nil
	}

	row, err = db.UpsertImportSource(ctx, q.UpsertImportSourceParams{
		Kind: sourceKind, SourceID: planeWS, Url: opts.URL, Workspace: opts.Workspace, Now: now(),
	})
	if err != nil {
		return r, fmt.Errorf("import source: %w", err)
	}
	w := NewWriter(opts.Pool, row.ID, opts.Actor, now)
	if err := w.Base(ctx, p, &ex); err != nil {
		return r, err
	}
	issues, changed := 0, 0
	for _, pp := range p.Projects {
		issues += len(pp.Issues)
		changed += len(pp.Changed)
	}
	pr := newProgress(opts.Progress, opts.Client, now, "issues", issues)
	if err := w.Issues(ctx, opts.Client, opts.Store, opts.MaxUpload, p, &ex, pr.report); err != nil {
		return r, err
	}
	pr = newProgress(opts.Progress, opts.Client, now, "changed issues", changed)
	if err := w.NewComments(ctx, opts.Client, opts.Store, opts.MaxUpload, p, &ex, pr.report); err != nil {
		return r, err
	}
	return r, nil
}

// progress writes the progress lines of one part of a run (new issues or
// issues changed in Plane), with an estimate of the time that part still
// takes, worked out from the requests sent per minute and per item so far.
type progress struct {
	w        io.Writer
	c        *Client
	now      func() time.Time
	noun     string
	start    time.Time
	requests int
	total    int
	done     map[string]int
}

// newProgress starts measuring a part of total items, named noun in lines.
func newProgress(w io.Writer, c *Client, now func() time.Time, noun string, total int) *progress {
	return &progress{w: w, c: c, now: now, noun: noun, start: now(), requests: c.Requests(), total: total, done: map[string]int{}}
}

// report is the progress callback of Writer.Issues and Writer.NewComments:
// "KEY done/total noun", once per count, or "KEY nothing new" once for a
// project with nothing to do.
func (pr *progress) report(key string, done, total int) {
	prev, seen := pr.done[key]
	pr.done[key] = done
	if pr.w == nil || (seen && prev == done) {
		return
	}
	line := key + " nothing new"
	if total > 0 {
		line = fmt.Sprintf("%s %d/%d %s", key, done, total, pr.noun)
		if m, ok := pr.minutesLeft(); ok {
			line += fmt.Sprintf(", ~%dm left", m)
		}
	}
	// Progress is advisory: a failed write does not stop the import.
	_, _ = fmt.Fprintln(pr.w, line)
}

// minutesLeft estimates the minutes the remaining items take; false before
// there is anything to estimate from or when nothing is left.
func (pr *progress) minutesLeft() (int, bool) {
	done := 0
	for _, n := range pr.done {
		done += n
	}
	requests := float64(pr.c.Requests() - pr.requests)
	elapsed := pr.now().Sub(pr.start).Minutes()
	if done == 0 || done >= pr.total || requests <= 0 || elapsed <= 0 {
		return 0, false
	}
	perMinute := requests / elapsed
	perItem := requests / float64(done)
	left := float64(pr.total-done) * perItem / perMinute
	return max(int(math.Ceil(left)), 1), true
}
