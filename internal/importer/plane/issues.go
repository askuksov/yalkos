package plane

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/store/q"
)

// progressEvery is how many issues pass between two progress calls.
const progressEvery = 50

// maxTitle is the Yalkos limit of an issue title.
const maxTitle = 255

// Issues creates the planned issues of every project after Base: per issue
// it reads the comments, files, links and relations from Plane, copies the
// files into store (at most maxUpload bytes each), and then writes the issue
// with its comments and attachments in one transaction, so a stop leaves no
// half issue. A Plane number taken in Yalkos gets the project's next free
// one. progress (may be nil) is called at the start and the end of each
// project and every 50 issues, with the project key and the issues done and
// planned.
func (w *Writer) Issues(ctx context.Context, c *Client, store blob.Store, maxUpload int64, p Plan, ex *Existing,
	progress func(key string, done, total int)) error {
	if progress == nil {
		progress = func(string, int, int) {}
	}
	pw := w.newProjectWriter(c, store, maxUpload, p, ex)
	for i := range p.Projects {
		if err := pw.write(ctx, &p.Projects[i], progress); err != nil {
			return fmt.Errorf("project %s: %w", p.Projects[i].Key, err)
		}
	}
	return nil
}

// newProjectWriter prepares the writing of issues and comments: ex gets its
// empty maps, and the labels of the import get their names for the report.
func (w *Writer) newProjectWriter(c *Client, store blob.Store, maxUpload int64, p Plan, ex *Existing) *projectWriter {
	r := p.Report
	if r == nil {
		r = &Report{}
	}
	ex.ready()
	pw := &projectWriter{
		w: w, c: c, store: store, limit: maxUpload, ex: ex, r: r, names: newIssueNamer(c, w.q, ex, p),
		labelNames: map[uuid.UUID]string{}, labelsGone: map[uuid.UUID]bool{},
	}
	for name, id := range ex.TypeNames {
		pw.labelNames[id] = name
	}
	for _, l := range p.Labels {
		if id, ok := w.labels[l.Key]; ok {
			pw.labelNames[id] = l.Name
		}
	}
	return pw
}

// projectWriter writes the issues of one project at a time.
type projectWriter struct {
	w     *Writer
	c     *Client
	store blob.Store
	limit int64
	ex    *Existing
	r     *Report
	names *issueNamer

	// labelNames names the labels of the import, for the report.
	labelNames map[uuid.UUID]string
	// labelsGone are the labels already reported as deleted in Yalkos.
	labelsGone map[uuid.UUID]bool

	pp      *ProjectPlan
	target  uuid.UUID
	rep     *ProjectReport
	states  map[uuid.UUID]q.State
	def     q.State
	modules map[uuid.UUID]bool
	// gone are the Plane states and modules already reported as deleted in Yalkos.
	gone map[string]bool
}

// issueOutcome is what one issue adds to the report, applied once its
// transaction has committed.
type issueOutcome struct {
	key           string
	cutTexts      []string
	unknown       map[string]int
	failed        []FailedAttachment
	tooLarge      []string
	deleted       []string
	unresolved    bool
	stateFallback bool
	parentDropped bool
	renumbered    *Renumbered
	comments      int
	deletedCmts   int
	files         int
}

func (pw *projectWriter) write(ctx context.Context, pp *ProjectPlan, progress func(string, int, int)) error {
	target, ok := pw.ex.mapped(kindProject, pp.Data.ID)
	if !ok {
		// Deleted in Yalkos since an earlier import; Base reported it.
		return nil
	}
	pw.pp, pw.target, pw.rep, pw.gone = pp, target, pw.r.project(pp.Key), map[string]bool{}
	if err := pw.loadTargets(ctx); err != nil {
		return err
	}
	// The counter goes above every Plane number first, so a native issue
	// created during the run, or a renumbered item, never takes the number
	// of a later Plane item.
	if pp.MaxNumber > 0 {
		if err := pw.w.q.RaiseSequenceCounter(ctx, q.RaiseSequenceCounterParams{Number: int32(pp.MaxNumber), ID: target}); err != nil {
			return fmt.Errorf("raise counter: %w", err)
		}
	}
	total := len(pp.Issues)
	progress(pp.Key, 0, total)
	for n := range pp.Issues {
		is := &pp.Issues[n]
		if _, done := pw.ex.mapped(kindIssue, is.Item.ID); !done {
			if err := pw.writeIssue(ctx, is); err != nil {
				return fmt.Errorf("%s: %w", is.Key, err)
			}
		}
		if done := n + 1; done%progressEvery == 0 && done < total {
			progress(pp.Key, done, total)
		}
	}
	progress(pp.Key, total, total)
	return nil
}

// issueText is an issue's text and files, read from Plane before its
// transaction.
type issueText struct {
	title, description string
	comments           []Comment
	bodies             []string
	files              *issueFiles
}

// read fetches what belongs to a work item and copies its files.
func (pw *projectWriter) read(ctx context.Context, is *IssuePlan, out *issueOutcome) (issueText, error) {
	pid, it := pw.pp.Data.ID, is.Item
	comments, err := pw.c.Comments(ctx, pid, it.ID)
	if err != nil {
		return issueText{}, err
	}
	atts, err := pw.c.Attachments(ctx, pid, it.ID)
	if err != nil {
		return issueText{}, err
	}
	links, err := pw.c.Links(ctx, pid, it.ID)
	if err != nil {
		return issueText{}, err
	}
	rels, err := pw.c.Relations(ctx, pid, it.ID)
	if err != nil {
		return issueText{}, err
	}
	relLines, unresolved, err := pw.names.relationLines(ctx, rels)
	if err != nil {
		return issueText{}, err
	}
	out.unresolved = unresolved

	live, deleted := liveComments(comments)
	out.deletedCmts = deleted

	created := orTime(it.CreatedAt, pw.w.now())
	files := pw.newFiles(it.ID, is.Key, out)
	for _, a := range atts {
		if !a.IsUploaded {
			files.notUploaded(a.ID, a.Name)
			continue
		}
		if err := files.fetch(ctx, a.ID, a.Name, a.Size, *pw.w.creator(a.CreatedBy), orTime(a.CreatedAt, created)); err != nil {
			return issueText{}, err
		}
	}
	for _, id := range imagesOf(it.DescriptionHTML) {
		if err := files.fetch(ctx, id, "", 0, *pw.w.creator(it.CreatedBy), created); err != nil {
			return issueText{}, err
		}
	}
	if err := pw.commentImages(ctx, files, live, created); err != nil {
		return issueText{}, err
	}

	t := issueText{comments: live, files: files}
	title, cut := cutRunes(oneLine(it.Name), maxTitle)
	title = strings.TrimSpace(title)
	if title == "" {
		// Yalkos needs a title: the key stands in for a blank one.
		title, cut = is.Key, true
	}
	if cut {
		out.cutTexts = append(out.cutTexts, is.Key+" (title)")
	}
	t.title = title
	body, err := pw.w.convert(ctx, it.DescriptionHTML, pw.names, files, out)
	if err != nil {
		return issueText{}, err
	}
	if t.description, cut = describe(body, linkLines(links), relLines); cut {
		out.cutTexts = append(out.cutTexts, is.Key+" (description)")
	}
	if t.bodies, err = pw.commentBodies(ctx, live, files, out); err != nil {
		return issueText{}, err
	}
	return t, nil
}

// liveComments returns the comments Plane has not deleted, oldest first, and
// the number of deleted ones.
func liveComments(comments []Comment) (live []Comment, deleted int) {
	for _, cm := range comments {
		if cm.DeletedAt != nil {
			deleted++
			continue
		}
		live = append(live, cm)
	}
	slices.SortStableFunc(live, func(a, b Comment) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	return live, deleted
}

// newFiles starts copying the files of the work item iid of the current
// project; key names it in the report.
func (pw *projectWriter) newFiles(iid, key string, out *issueOutcome) *issueFiles {
	return &issueFiles{
		c: pw.c, store: pw.store, limit: pw.limit, pid: pw.pp.Data.ID, iid: iid, key: key, ex: pw.ex, out: out,
		byPlane: map[string]*storedFile{},
	}
}

// commentImages copies the images of comments; fallback is the time of a
// comment Plane gives none (the issue's creation).
func (pw *projectWriter) commentImages(ctx context.Context, files *issueFiles, comments []Comment, fallback time.Time) error {
	for _, cm := range comments {
		for _, id := range imagesOf(cm.CommentHTML) {
			if err := files.fetch(ctx, id, "", 0, *pw.w.creator(cm.Actor), orTime(cm.CreatedAt, fallback)); err != nil {
				return err
			}
		}
	}
	return nil
}

// commentBodies converts comments to Markdown, after their images are copied.
func (pw *projectWriter) commentBodies(ctx context.Context, comments []Comment, files *issueFiles, out *issueOutcome) ([]string, error) {
	bodies := make([]string, 0, len(comments))
	for _, cm := range comments {
		body, err := pw.w.convert(ctx, cm.CommentHTML, pw.names, files, out)
		if err != nil {
			return nil, err
		}
		if body == "" {
			body = emptyComment
		}
		body, cut := cutText(body)
		if cut {
			out.cutTexts = append(out.cutTexts, out.key+" (comment)")
		}
		bodies = append(bodies, body)
	}
	return bodies, nil
}

func (pw *projectWriter) writeIssue(ctx context.Context, is *IssuePlan) error {
	out := &issueOutcome{key: is.Key, unknown: map[string]int{}}
	t, err := pw.read(ctx, is, out)
	if err != nil {
		return err
	}
	it := is.Item
	created := orTime(it.CreatedAt, pw.w.now())
	updated := laterOf(orTime(it.UpdatedAt, created), created)
	st := pw.state(is.State, out)
	var assignee *uuid.UUID
	if id, ok := pw.w.users[is.Assignee]; ok && is.Assignee != "" {
		assignee = &id
	}
	creator := pw.w.creator(it.CreatedBy)
	// A sub-issue keeps its own Plane module; it does not inherit its parent's.
	milestoneID := pw.milestone(is.Milestone, out)
	labels := pw.labelIDs(is)

	var issueID uuid.UUID
	err = pw.w.inTx(ctx, pw.ex, func(tx *q.Queries, mapTo mapFunc) error {
		out.renumbered, out.parentDropped = nil, false
		number, err := pw.number(ctx, tx, is, out)
		if err != nil {
			return err
		}
		// The counter update holds the project row, which deleting a state or
		// a milestone needs too: what is read now stays until the commit.
		st, err := pw.liveState(ctx, tx, st, is.State, out)
		if err != nil {
			return err
		}
		milestoneID, err := pw.liveMilestone(ctx, tx, milestoneID, is.Milestone, out)
		if err != nil {
			return err
		}
		var completed, canceled *time.Time
		switch st.Type {
		case "completed":
			completed = finishedAt(it, updated)
		case "canceled":
			canceled = finishedAt(it, updated)
		}
		parent, err := pw.parent(ctx, tx, is, out)
		if err != nil {
			return err
		}
		row, err := tx.ImportCreateIssue(ctx, q.ImportCreateIssueParams{
			ProjectID: pw.target, Number: number, Title: t.title, Description: t.description, StateID: st.ID,
			Priority: int16(is.Priority), AssigneeID: assignee, CreatorID: *creator, ParentID: parent,
			DueDate: planeDate(it.TargetDate), SortOrder: it.SortOrder, CreatedAt: created, UpdatedAt: updated,
			CompletedAt: completed, CanceledAt: canceled, MilestoneID: milestoneID,
		})
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		if err := tx.InsertIssueNumber(ctx, q.InsertIssueNumberParams{ProjectID: pw.target, Number: number, IssueID: row.ID}); err != nil {
			return fmt.Errorf("issue number: %w", err)
		}
		labelIDs, missing, err := insertLabels(ctx, tx, row.ID, labels)
		if err != nil {
			return err
		}
		for _, id := range missing {
			if !pw.labelsGone[id] {
				pw.labelsGone[id] = true
				out.deleted = append(out.deleted, "label "+pw.labelNames[id])
			}
		}
		if err := tx.InsertImportMap(ctx, q.InsertImportMapParams{
			SourceID: pw.w.source, Kind: kindIssue, PlaneID: it.ID, TargetID: row.ID, SourceUpdatedAt: &it.UpdatedAt, Now: pw.w.now(),
		}); err != nil {
			return fmt.Errorf("import map issue %s: %w", it.ID, err)
		}
		events := []audit.Event{issue.CreatedEvent(row, pw.pp.Key, labelIDs, creator)}
		cmEvents, err := pw.insertComments(ctx, tx, mapTo, row.ID, pw.target, t.comments, t.bodies, created)
		if err != nil {
			return err
		}
		events = append(events, cmEvents...)
		if err := pw.insertFiles(ctx, tx, mapTo, row.ID, t.files.files); err != nil {
			return err
		}
		issueID = row.ID
		return audit.RecordMany(ctx, tx, events)
	})
	if err != nil {
		return err
	}
	pw.ex.Map[kindIssue][it.ID] = issueID
	pw.ex.Updated[it.ID] = it.UpdatedAt
	out.comments, out.files = len(t.comments), len(t.files.files)
	pw.rep.Created.Issues++
	pw.apply(out)
	return nil
}

// finishedAt is when an issue in a completed or canceled state was finished.
func finishedAt(it WorkItem, updated time.Time) *time.Time {
	if it.CompletedAt != nil && !it.CompletedAt.IsZero() {
		at := *it.CompletedAt
		return &at
	}
	return &updated
}

// number is the Plane number of the issue, or the project's next free one
// when Yalkos has given that number to another issue. The counter is raised
// first: its row lock keeps a native issue from taking the number meanwhile.
func (pw *projectWriter) number(ctx context.Context, tx *q.Queries, is *IssuePlan, out *issueOutcome) (int32, error) {
	n := int32(is.Number)
	if err := tx.RaiseSequenceCounter(ctx, q.RaiseSequenceCounterParams{Number: n, ID: pw.target}); err != nil {
		return 0, fmt.Errorf("raise counter: %w", err)
	}
	taken, err := tx.IssueNumberTaken(ctx, q.IssueNumberTakenParams{ProjectID: pw.target, Number: n})
	if err != nil {
		return 0, fmt.Errorf("issue number taken: %w", err)
	}
	if !taken {
		return n, nil
	}
	next, err := tx.NextIssueNumber(ctx, pw.target)
	if err != nil {
		return 0, fmt.Errorf("next issue number: %w", err)
	}
	out.renumbered = &Renumbered{Plane: is.Key, Yalkos: issueKey(pw.pp.Key, int(next))}
	return next, nil
}

// parent is the Yalkos parent of the issue: nil when there is none, or when
// it was deleted in Yalkos or moved to another project.
func (pw *projectWriter) parent(ctx context.Context, tx *q.Queries, is *IssuePlan, out *issueOutcome) (*uuid.UUID, error) {
	if is.Parent == "" {
		return nil, nil
	}
	id, ok := pw.ex.mapped(kindIssue, is.Parent)
	if !ok {
		out.parentDropped = true
		return nil, nil
	}
	row, err := tx.GetIssue(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.Issue.ProjectID != pw.target) {
		out.parentDropped = true
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("parent: %w", err)
	}
	return &id, nil
}

// insertComments creates the comments of an issue in project projectID with
// their converted bodies and import map rows, and returns their create
// events. fallback is the time of a comment Plane gives none.
func (pw *projectWriter) insertComments(ctx context.Context, tx *q.Queries, mapTo mapFunc, issueID, projectID uuid.UUID,
	comments []Comment, bodies []string, fallback time.Time) ([]audit.Event, error) {
	events := make([]audit.Event, 0, len(comments))
	for i, cm := range comments {
		at := orTime(cm.CreatedAt, fallback)
		c, err := tx.ImportCreateComment(ctx, q.ImportCreateCommentParams{
			IssueID: issueID, AuthorID: *pw.w.creator(cm.Actor), Body: bodies[i],
			CreatedAt: at, UpdatedAt: laterOf(orTime(cm.UpdatedAt, at), at), EditedAt: cm.EditedAt,
		})
		if err != nil {
			return nil, fmt.Errorf("create comment: %w", err)
		}
		if err := mapTo(kindComment, cm.ID, c.ID); err != nil {
			return nil, err
		}
		events = append(events, comment.CreatedEvent(c, projectID, &c.AuthorID))
	}
	return events, nil
}

// insertFiles writes the blob rows and attachments of the copied files.
func (pw *projectWriter) insertFiles(ctx context.Context, tx *q.Queries, mapTo mapFunc, issueID uuid.UUID, files []*storedFile) error {
	for _, f := range files {
		if err := tx.InsertBlob(ctx, q.InsertBlobParams{Sha256: f.sum, Size: f.size, ContentType: f.ctype, Now: pw.w.now()}); err != nil {
			return fmt.Errorf("insert blob: %w", err)
		}
		if _, err := tx.ImportCreateAttachment(ctx, q.ImportCreateAttachmentParams{
			ID: f.id, IssueID: issueID, UploaderID: f.uploader, BlobSha256: f.sum, Name: f.name, CreatedAt: f.created,
		}); err != nil {
			return fmt.Errorf("create attachment: %w", err)
		}
		if !f.mapped {
			if err := mapTo(kindAttachment, f.planeID, f.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// apply adds what a committed issue brought to the report.
func (pw *projectWriter) apply(out *issueOutcome) {
	r := pw.r
	r.Dropped.CutTexts = append(r.Dropped.CutTexts, out.cutTexts...)
	for tag, n := range out.unknown {
		if r.Dropped.UnknownTags == nil {
			r.Dropped.UnknownTags = map[string]int{}
		}
		r.Dropped.UnknownTags[tag] += n
	}
	r.Dropped.DeletedInYalkos = append(r.Dropped.DeletedInYalkos, out.deleted...)
	r.Attachments.Failed = append(r.Attachments.Failed, out.failed...)
	r.Attachments.TooLarge = append(r.Attachments.TooLarge, out.tooLarge...)
	if out.unresolved {
		r.Dropped.UnresolvedRelations = append(r.Dropped.UnresolvedRelations, out.key)
	}
	if out.stateFallback && !slices.Contains(r.StateFallback, out.key) {
		r.StateFallback = append(r.StateFallback, out.key)
	}
	if out.parentDropped && !slices.Contains(r.Dropped.Parents, out.key) {
		r.Dropped.Parents = append(r.Dropped.Parents, out.key)
	}
	if out.renumbered != nil {
		r.Renumbered = append(r.Renumbered, *out.renumbered)
	}
	pw.rep.Created.Comments += out.comments
	pw.rep.Skipped.Comments += out.deletedCmts
	pw.rep.Created.Attachments += out.files
	pw.rep.Skipped.Attachments += len(out.failed) + len(out.tooLarge)
}

// project is the report of the project with key, added when missing.
func (r *Report) project(key string) *ProjectReport {
	for i := range r.Projects {
		if r.Projects[i].Key == key {
			return &r.Projects[i]
		}
	}
	r.Projects = append(r.Projects, ProjectReport{Key: key})
	return &r.Projects[len(r.Projects)-1]
}
