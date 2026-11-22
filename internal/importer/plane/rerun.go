package plane

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/store/q"
)

// NewComments adds the comments written in Plane since an earlier import to
// the imported issues Plane changed since (ProjectPlan.Changed), after Base:
// one comments request per issue, then the comments not in the import map
// are created with their images, like Issues creates them, and the Plane
// updated_at of the issue is recorded, all in one transaction per issue. The
// issue's own fields stay as they are. Each changed issue's report entry
// gets the number added; an issue (or project) deleted in Yalkos is skipped,
// keeps its recorded updated_at, is reported as deleted and leaves the
// changed list. progress (may be nil) is called every 50 changed issues and
// at the end of each project that has any, with the project key and the
// changed issues done and in total.
func (w *Writer) NewComments(ctx context.Context, c *Client, store blob.Store, maxUpload int64, p Plan, ex *Existing,
	progress func(key string, done, total int)) error {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	pw := w.newProjectWriter(c, store, maxUpload, p, ex)
	for i := range p.Projects {
		pp := &p.Projects[i]
		if len(pp.Changed) == 0 {
			continue
		}
		if _, ok := ex.mapped(kindProject, pp.Data.ID); !ok {
			// Deleted in Yalkos since an earlier import; Base reported it.
			for _, ch := range pp.Changed {
				pw.r.dropChanged(ch.Key)
			}
			continue
		}
		pw.pp, pw.rep = pp, pw.r.project(pp.Key)
		items := make(map[string]WorkItem, len(pp.Data.Items))
		for _, it := range pp.Data.Items {
			items[it.ID] = it
		}
		total := len(pp.Changed)
		for n, ch := range pp.Changed {
			if it, ok := items[ch.PlaneID]; ok {
				if err := pw.addComments(ctx, it, ch.Key); err != nil {
					return fmt.Errorf("project %s: %s: %w", pp.Key, ch.Key, err)
				}
			}
			if done := n + 1; done%progressEvery == 0 && done < total {
				progress(pp.Key, done, total)
			}
		}
		progress(pp.Key, total, total)
	}
	return nil
}

// nameChanged names the issues changed in Plane by their current Yalkos
// keys, in p and its report: an issue renumbered on import, or moved
// or renumbered in Yalkos since, is listed as Yalkos shows it. One deleted in
// Yalkos keeps its Plane key.
func nameChanged(ctx context.Context, db *q.Queries, p *Plan, ex *Existing) error {
	var ids []uuid.UUID
	for _, pp := range p.Projects {
		for _, ch := range pp.Changed {
			if id, ok := ex.mapped(kindIssue, ch.PlaneID); ok {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := db.ListIssuesByIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("changed issues: %w", err)
	}
	current := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		current[row.Issue.ID] = issueKey(row.ProjectKey, int(row.Issue.Number))
	}
	renamed := map[string]string{}
	for i := range p.Projects {
		for j := range p.Projects[i].Changed {
			ch := &p.Projects[i].Changed[j]
			id, _ := ex.mapped(kindIssue, ch.PlaneID)
			if k, ok := current[id]; ok && k != ch.Key {
				renamed[ch.Key] = k
				ch.Key = k
			}
		}
	}
	if p.Report != nil {
		// One pass, so a key that another issue now has is not renamed twice.
		for i, c := range p.Report.ChangedInPlane {
			if k, ok := renamed[c.Key]; ok {
				p.Report.ChangedInPlane[i].Key = k
			}
		}
	}
	return nil
}

// addComments adds the new comments of the imported work item it, named key.
func (pw *projectWriter) addComments(ctx context.Context, it WorkItem, key string) error {
	issueID, ok := pw.ex.mapped(kindIssue, it.ID)
	if !ok {
		return nil
	}
	if _, err := pw.w.q.GetIssue(ctx, issueID); errors.Is(err, pgx.ErrNoRows) {
		pw.deletedIssue(key)
		return nil
	} else if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	comments, err := pw.c.Comments(ctx, pw.pp.Data.ID, it.ID)
	if err != nil {
		return err
	}
	live, _ := liveComments(comments)
	var added []Comment
	for _, cm := range live {
		if _, done := pw.ex.mapped(kindComment, cm.ID); !done {
			added = append(added, cm)
		}
	}
	out := &issueOutcome{key: key, unknown: map[string]int{}}
	created := orTime(it.CreatedAt, pw.w.now())
	files := pw.newFiles(it.ID, key, out)
	if err := pw.commentImages(ctx, files, added, created); err != nil {
		return err
	}
	bodies, err := pw.commentBodies(ctx, added, files, out)
	if err != nil {
		return err
	}

	gone := false
	updated := it.UpdatedAt
	err = pw.w.inTx(ctx, pw.ex, func(tx *q.Queries, mapTo mapFunc) error {
		// The row lock keeps the issue from being deleted until the commit.
		row, err := tx.GetIssueForUpdate(ctx, issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			gone = true
			return nil
		}
		gone = false
		if err != nil {
			return fmt.Errorf("get issue: %w", err)
		}
		events, err := pw.insertComments(ctx, tx, mapTo, issueID, row.Issue.ProjectID, added, bodies, created)
		if err != nil {
			return err
		}
		if err := pw.insertFiles(ctx, tx, mapTo, issueID, files.files); err != nil {
			return err
		}
		if err := tx.SetImportMapUpdated(ctx, q.SetImportMapUpdatedParams{
			SourceUpdatedAt: &updated, SourceID: pw.w.source, Kind: kindIssue, PlaneID: it.ID,
		}); err != nil {
			return fmt.Errorf("record updated_at: %w", err)
		}
		return audit.RecordMany(ctx, tx, events)
	})
	if err != nil {
		return err
	}
	if gone {
		pw.deletedIssue(key)
		return nil
	}
	pw.ex.Updated[it.ID] = updated
	out.comments, out.files = len(added), len(files.files)
	pw.apply(out)
	pw.r.commentsAdded(key, len(added))
	return nil
}

// commentsAdded records the comments added to a changed issue.
func (r *Report) commentsAdded(key string, n int) {
	for i := range r.ChangedInPlane {
		if r.ChangedInPlane[i].Key == key {
			r.ChangedInPlane[i].CommentsAdded += n
			return
		}
	}
	r.ChangedInPlane = append(r.ChangedInPlane, ChangedIssue{Key: key, CommentsAdded: n})
}

// deletedIssue reports a changed issue deleted in Yalkos: listed as deleted,
// not as changed.
func (pw *projectWriter) deletedIssue(key string) {
	pw.r.Dropped.DeletedInYalkos = append(pw.r.Dropped.DeletedInYalkos, "issue "+key)
	pw.r.dropChanged(key)
}

// dropChanged removes an issue from the changed list.
func (r *Report) dropChanged(key string) {
	r.ChangedInPlane = slices.DeleteFunc(r.ChangedInPlane, func(c ChangedIssue) bool { return c.Key == key })
}
