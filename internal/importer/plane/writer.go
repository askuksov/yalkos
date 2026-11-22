package plane

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/store/q"
)

const (
	// maxText is where a description or comment is cut; with cutNote it stays
	// within the 100,000 runes Yalkos accepts.
	maxText = 99_950
	cutNote = "\n\n_(cut on import)_"
)

// Writer writes a planned import into Yalkos. Every entity goes in with its
// import map row and its create event in one transaction, with the times and
// the creator Plane recorded. Nothing is reported to webhooks: an import is
// history, not a change.
type Writer struct {
	pool   *pgxpool.Pool
	q      *q.Queries
	source uuid.UUID
	actor  account.User
	now    func() time.Time

	// users maps a Plane member id to its Yalkos user.
	users map[string]uuid.UUID
	// names maps a Plane member id to the name a mention of it shows.
	names map[string]string
	// labels maps a LabelPlan key to its Yalkos label.
	labels map[string]uuid.UUID
}

// NewWriter returns a writer for the import source with id source (an
// import_sources row). actor is recorded for what Plane's seed bot or an
// unknown user created; now is the clock of the import map.
func NewWriter(pool *pgxpool.Pool, source uuid.UUID, actor account.User, now func() time.Time) *Writer {
	return &Writer{
		pool: pool, q: q.New(pool), source: source, actor: actor, now: now,
		users:  map[string]uuid.UUID{},
		names:  map[string]string{},
		labels: map[string]uuid.UUID{},
	}
}

// Base creates the users, then the labels, then per project the project with
// its key and members, its states and its milestones. A Plane id already in
// ex.Map is skipped; what is created enters ex.Map. An entity an earlier
// import created and Yalkos has deleted since is not brought back; it is
// listed in the report, and a deleted project leaves ex.Map so nothing is
// written into it.
func (w *Writer) Base(ctx context.Context, p Plan, ex *Existing) error {
	if len(p.Conflicts) > 0 {
		return &ConflictError{Keys: p.Conflicts}
	}
	r := p.Report
	if r == nil {
		r = &Report{}
	}
	if ex.Map == nil {
		ex.Map = map[string]map[string]uuid.UUID{}
	}
	for _, kind := range mapKinds {
		if ex.Map[kind] == nil {
			ex.Map[kind] = map[string]uuid.UUID{}
		}
	}
	if err := w.writeUsers(ctx, p.Users, ex); err != nil {
		return err
	}
	if err := w.writeLabels(ctx, p, ex, r); err != nil {
		return err
	}
	for i := range p.Projects {
		if err := w.writeProject(ctx, &p.Projects[i], ex, r); err != nil {
			return fmt.Errorf("project %s: %w", p.Projects[i].Key, err)
		}
	}
	return nil
}

type mapRow struct {
	kind, planeID string
	target        uuid.UUID
}

// mapFunc records that a Plane id became target, in the running transaction.
type mapFunc func(kind, planeID string, target uuid.UUID) error

// inTx runs fn in a transaction. The import map rows fn adds are written in
// that transaction and enter ex.Map once it commits.
func (w *Writer) inTx(ctx context.Context, ex *Existing, fn func(tx *q.Queries, mapTo mapFunc) error) error {
	var added []mapRow
	err := kit.InTx(ctx, w.pool, w.q, func(tx *q.Queries) error {
		added = added[:0]
		return fn(tx, func(kind, planeID string, target uuid.UUID) error {
			if err := tx.InsertImportMap(ctx, q.InsertImportMapParams{
				SourceID: w.source, Kind: kind, PlaneID: planeID, TargetID: target, Now: w.now(),
			}); err != nil {
				return fmt.Errorf("import map %s %s: %w", kind, planeID, err)
			}
			added = append(added, mapRow{kind, planeID, target})
			return nil
		})
	})
	if err != nil {
		return err
	}
	for _, m := range added {
		ex.Map[m.kind][m.planeID] = m.target
	}
	return nil
}

// creator is the user a Plane user id maps to; the actor for the seed bot,
// an empty id or a user that is not a workspace member.
func (w *Writer) creator(planeID string) *uuid.UUID {
	if id, ok := w.users[planeID]; ok {
		return &id
	}
	id := w.actor.ID
	return &id
}

// writeUsers creates, disabled, the members no Yalkos user matches, and maps
// the members matched by email too, so a later email change in Yalkos does
// not make a re-run create them again.
func (w *Writer) writeUsers(ctx context.Context, users []UserPlan, ex *Existing) error {
	var workspace uuid.UUID
	for _, u := range users {
		w.names[u.PlaneID] = u.Name
		if id, ok := ex.mapped(kindUser, u.PlaneID); ok {
			w.users[u.PlaneID] = id
			continue
		}
		if !u.Create {
			err := w.inTx(ctx, ex, func(_ *q.Queries, mapTo mapFunc) error {
				return mapTo(kindUser, u.PlaneID, u.Target)
			})
			if err != nil {
				return err
			}
			w.users[u.PlaneID] = u.Target
			continue
		}
		if workspace == uuid.Nil {
			ws, err := w.q.GetWorkspace(ctx)
			if err != nil {
				return fmt.Errorf("workspace: %w", err)
			}
			workspace = ws.ID
		}
		hash, err := unusableHash(ctx)
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = w.inTx(ctx, ex, func(tx *q.Queries, mapTo mapFunc) error {
			row, err := tx.ImportCreateUser(ctx, q.ImportCreateUserParams{
				WorkspaceID: workspace, Email: u.Email, Name: u.Name, PasswordHash: hash,
			})
			if err != nil {
				return fmt.Errorf("create user %s: %w", u.Email, err)
			}
			id = row.ID
			return mapTo(kindUser, u.PlaneID, row.ID)
		})
		if err != nil {
			return err
		}
		w.users[u.PlaneID] = id
		if ex.Users != nil {
			ex.Users[strings.ToLower(u.Email)] = id
		}
	}
	return nil
}

// unusableHash hashes 32 random bytes that are thrown away: nobody can log
// in with it, and a login attempt costs as long as for any other account.
func unusableHash(ctx context.Context) (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("random password: %w", err)
	}
	hash, err := auth.HashPassword(ctx, base64.StdEncoding.EncodeToString(secret))
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// writeLabels creates the planned labels, a group before its labels. A
// label that already exists gets the import map rows of the Plane labels
// merged into it that are not mapped yet.
func (w *Writer) writeLabels(ctx context.Context, p Plan, ex *Existing, r *Report) error {
	byID := map[string]Label{}
	byKey := map[string][]Label{}
	for _, pp := range p.Projects {
		for _, l := range pp.Data.Labels {
			byID[l.ID] = l
			if l.Parent == nil {
				byKey[labelKey("", l.Name)] = append(byKey[labelKey("", l.Name)], l)
			}
		}
	}

	names := make(map[string]string, len(p.Labels))
	for _, l := range p.Labels {
		names[l.Key] = l.Name
	}

	targets := make([]uuid.UUID, len(p.Labels))
	var ids []uuid.UUID
	for i, l := range p.Labels {
		targets[i] = l.Target
		for _, id := range l.PlaneIDs {
			if t, ok := ex.mapped(kindLabel, id); ok && targets[i] == uuid.Nil {
				targets[i] = t
			}
		}
		if targets[i] == uuid.Nil && len(l.PlaneIDs) == 0 && l.Group == "" {
			// A label without Plane ids has no map row; one this writer created
			// before is found by name.
			targets[i] = ex.Labels[strings.ToLower(l.Name)]
		}
		if targets[i] != uuid.Nil {
			ids = append(ids, targets[i])
		}
	}
	live := map[uuid.UUID]bool{}
	if len(ids) > 0 {
		rows, err := w.q.ListLabelsByIDs(ctx, ids)
		if err != nil {
			return fmt.Errorf("labels: %w", err)
		}
		for _, l := range rows {
			live[l.ID] = true
		}
	}

	for i, l := range p.Labels {
		if t := targets[i]; t != uuid.Nil {
			if !live[t] {
				r.Dropped.DeletedInYalkos = append(r.Dropped.DeletedInYalkos, "label "+l.Name)
				continue
			}
			w.labels[l.Key] = t
			if err := w.mapLabel(ctx, l, t, ex); err != nil {
				return err
			}
			continue
		}
		var parent *uuid.UUID
		if l.Group != "" {
			g, ok := w.labels[l.Group]
			if !ok {
				r.Dropped.DeletedInYalkos = append(r.Dropped.DeletedInYalkos,
					"label "+names[l.Group]+"/"+l.Name+" (group deleted)")
				continue
			}
			parent = &g
		}
		var src []Label
		for _, id := range l.PlaneIDs {
			if pl, ok := byID[id]; ok {
				src = append(src, pl)
			}
		}
		if len(src) == 0 {
			// A Plane label kept as an ordinary label next to the Type label
			// of its issue: it has no Plane id of its own.
			src = byKey[l.Key]
		}
		at, by := w.now(), ""
		if first, ok := earliest(src); ok {
			at, by = first.CreatedAt, first.CreatedBy
		}
		updated := lastUpdated(src, at)
		creator := w.creator(by)
		name := l.Name
		var id uuid.UUID
		err := w.inTx(ctx, ex, func(tx *q.Queries, mapTo mapFunc) error {
			pos, err := tx.NextLabelPosition(ctx, parent)
			if err != nil {
				return fmt.Errorf("label position: %w", err)
			}
			row, err := tx.ImportCreateLabel(ctx, q.ImportCreateLabelParams{
				ParentID: parent, IsGroup: l.IsGroup, Name: name, Color: l.Color, Position: pos,
				CreatedBy: creator, CreatedAt: at, UpdatedAt: updated,
			})
			if err != nil {
				return fmt.Errorf("create label %s: %w", name, err)
			}
			for _, pid := range l.PlaneIDs {
				if err := mapTo(kindLabel, pid, row.ID); err != nil {
					return err
				}
			}
			id = row.ID
			return audit.Record(ctx, tx, label.CreatedEvent(row, creator))
		})
		if err != nil {
			return err
		}
		w.labels[l.Key] = id
		if parent == nil && ex.Labels != nil {
			ex.Labels[strings.ToLower(name)] = id
		}
	}
	return nil
}

// mapLabel maps the Plane labels of l that are not mapped yet onto the
// existing label target.
func (w *Writer) mapLabel(ctx context.Context, l LabelPlan, target uuid.UUID, ex *Existing) error {
	var todo []string
	for _, id := range l.PlaneIDs {
		if _, ok := ex.mapped(kindLabel, id); !ok {
			todo = append(todo, id)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	return w.inTx(ctx, ex, func(_ *q.Queries, mapTo mapFunc) error {
		for _, id := range todo {
			if err := mapTo(kindLabel, id, target); err != nil {
				return err
			}
		}
		return nil
	})
}

// lastUpdated is the latest Plane update of labels merged into one, not
// before created.
func lastUpdated(labels []Label, created time.Time) time.Time {
	out := created
	for _, l := range labels {
		out = laterOf(out, l.UpdatedAt)
	}
	return out
}

// earliest returns the label Plane created first; labels without a time
// come last.
func earliest(labels []Label) (Label, bool) {
	var out Label
	found := false
	for _, l := range labels {
		if l.CreatedAt.IsZero() {
			continue
		}
		if !found || l.CreatedAt.Before(out.CreatedAt) {
			out, found = l, true
		}
	}
	return out, found
}

// writeProject creates the project (with its states, in one transaction) or
// adds the new states to the one an earlier import created, then creates the
// milestones.
func (w *Writer) writeProject(ctx context.Context, pp *ProjectPlan, ex *Existing, r *Report) error {
	pd := pp.Data
	target := pp.Target
	if t, ok := ex.mapped(kindProject, pd.ID); ok {
		target = t
	}
	created := orTime(pd.CreatedAt, w.now())
	if target != uuid.Nil {
		if _, err := w.q.GetProject(ctx, target); errors.Is(err, pgx.ErrNoRows) {
			r.Dropped.DeletedInYalkos = append(r.Dropped.DeletedInYalkos, "project "+pp.Key)
			delete(ex.Map[kindProject], pd.ID)
			return nil
		} else if err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		err := w.inTx(ctx, ex, func(tx *q.Queries, mapTo mapFunc) error {
			return w.writeStates(ctx, tx, mapTo, pp, target, created, ex)
		})
		if err != nil {
			return err
		}
	} else {
		id, err := w.createProject(ctx, pp, created, ex, r)
		if err != nil {
			return err
		}
		target = id
	}
	for _, mp := range pp.Milestones {
		if err := w.writeMilestone(ctx, pp, mp, target, ex, r); err != nil {
			return err
		}
	}
	return nil
}

func (w *Writer) createProject(ctx context.Context, pp *ProjectPlan, created time.Time, ex *Existing, r *Report) (uuid.UUID, error) {
	pd := pp.Data
	desc := w.description(pd.DescriptionHTML, pd.Description, r, pp.Key+" (project description)")
	updated := laterOf(orTime(pd.UpdatedAt, created), created)
	creator := w.creator(pd.CreatedBy)
	var members []uuid.UUID
	for _, m := range pp.Members {
		if id, ok := w.users[m]; ok && !slices.Contains(members, id) {
			members = append(members, id)
		}
	}
	if !slices.Contains(members, w.actor.ID) {
		members = append(members, w.actor.ID)
	}

	var id uuid.UUID
	err := w.inTx(ctx, ex, func(tx *q.Queries, mapTo mapFunc) error {
		row, err := tx.ImportCreateProject(ctx, q.ImportCreateProjectParams{
			Key: pp.Key, Name: pp.Name, Description: desc, CreatedBy: creator, CreatedAt: created, UpdatedAt: updated,
		})
		if project.IsUniqueViolation(err) {
			return keyTaken(pp.Key)
		}
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		if err := project.ClaimKey(ctx, tx, pp.Key, row.ID); err != nil {
			if in := (*kit.InputError)(nil); errors.As(err, &in) && in.Reason == kit.ReasonTaken {
				return keyTaken(pp.Key)
			}
			return err
		}
		for _, m := range members {
			if err := tx.AddProjectMember(ctx, q.AddProjectMemberParams{ProjectID: row.ID, UserID: m, Now: created}); err != nil {
				return fmt.Errorf("add member: %w", err)
			}
		}
		if err := mapTo(kindProject, pd.ID, row.ID); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, project.CreatedEvent(row, members, creator)); err != nil {
			return err
		}
		id = row.ID
		return w.writeStates(ctx, tx, mapTo, pp, row.ID, created, ex)
	})
	return id, err
}

func keyTaken(key string) error {
	return fmt.Errorf("project key %s was taken by another Yalkos project meanwhile", key)
}

// writeStates creates the planned states of a project after its current
// ones. A Plane state planned onto an existing state only gets its import
// map row. fallback is the time of a state Plane gives none.
func (w *Writer) writeStates(ctx context.Context, tx *q.Queries, mapTo mapFunc, pp *ProjectPlan, projectID uuid.UUID, fallback time.Time, ex *Existing) error {
	byID := make(map[string]State, len(pp.Data.States))
	for _, s := range pp.Data.States {
		byID[s.ID] = s
	}
	current, err := tx.ListStates(ctx, projectID)
	if err != nil {
		return fmt.Errorf("list states: %w", err)
	}
	pos := int32(0)
	for _, s := range current {
		pos = max(pos, s.Position+1)
	}
	var events []audit.Event
	for _, sp := range pp.States {
		if _, ok := ex.mapped(kindState, sp.PlaneID); ok {
			continue
		}
		if sp.Target != uuid.Nil {
			if err := mapTo(kindState, sp.PlaneID, sp.Target); err != nil {
				return err
			}
			continue
		}
		ps := byID[sp.PlaneID]
		at := orTime(ps.CreatedAt, fallback)
		row, err := tx.ImportCreateState(ctx, q.ImportCreateStateParams{
			ProjectID: projectID, Name: sp.Name, Type: sp.Type, Color: sp.Color, Position: pos,
			IsDefault: sp.Default, CreatedAt: at, UpdatedAt: laterOf(orTime(ps.UpdatedAt, at), at),
		})
		if err != nil {
			return fmt.Errorf("create state %s: %w", sp.Name, err)
		}
		pos++
		if err := mapTo(kindState, sp.PlaneID, row.ID); err != nil {
			return err
		}
		events = append(events, project.StateCreatedEvent(row, w.creator(ps.CreatedBy)))
	}
	return audit.RecordMany(ctx, tx, events)
}

func (w *Writer) writeMilestone(ctx context.Context, pp *ProjectPlan, mp MilestonePlan, projectID uuid.UUID, ex *Existing, r *Report) error {
	m := mp.Module
	if _, ok := ex.mapped(kindMilestone, m.ID); ok {
		return nil
	}
	name := mp.Name
	desc := w.description(m.DescriptionHTML, m.Description, r, pp.Key+": "+name+" (milestone description)")
	created := orTime(m.CreatedAt, w.now())
	updated := laterOf(orTime(m.UpdatedAt, created), created)
	var lead *uuid.UUID
	if m.Lead != nil {
		if id, ok := w.users[*m.Lead]; ok {
			lead = &id
		}
	}
	creator := w.creator(m.CreatedBy)
	return w.inTx(ctx, ex, func(tx *q.Queries, mapTo mapFunc) error {
		row, err := tx.ImportCreateMilestone(ctx, q.ImportCreateMilestoneParams{
			ProjectID: projectID, Name: name, Description: desc, TargetDate: planeDate(m.TargetDate),
			LeadID: lead, SortOrder: m.SortOrder, StatusManual: mp.Status, CreatedBy: creator,
			CreatedAt: created, UpdatedAt: updated,
		})
		if err != nil {
			return fmt.Errorf("create milestone %s: %w", name, err)
		}
		if err := mapTo(kindMilestone, m.ID, row.ID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, milestone.CreatedEvent(row, creator))
	})
}

// description is the Markdown description of a project or module: its HTML
// converted when Plane has any, else its plain text as it is (Plane often
// leaves description_html empty and keeps the text in description). A text
// over the limit is cut and reported under what.
func (w *Writer) description(htmlText, plain string, r *Report, what string) string {
	if strings.TrimSpace(htmlText) != "" {
		return w.markdown(htmlText, r, what)
	}
	if strings.TrimSpace(plain) == "" {
		return ""
	}
	text, cut := cutText(plain)
	if cut {
		r.Dropped.CutTexts = append(r.Dropped.CutTexts, what)
	}
	return text
}

// markdown converts Plane HTML to a Markdown description: mentions show the
// member's name; images and work item embeds are not resolved here. Unknown
// tags are counted and a text over the limit is cut, both reported (what
// names the text in the report).
func (w *Writer) markdown(htmlText string, r *Report, what string) string {
	if strings.TrimSpace(htmlText) == "" {
		return ""
	}
	// A conversion error still leaves the plain text, which is kept.
	res, _ := ToMarkdown(htmlText, Refs{User: func(id string) (string, bool) {
		n, ok := w.names[id]
		return n, ok
	}})
	for tag, n := range res.Unknown {
		if r.Dropped.UnknownTags == nil {
			r.Dropped.UnknownTags = map[string]int{}
		}
		r.Dropped.UnknownTags[tag] += n
	}
	text, cut := cutText(res.Markdown)
	if cut {
		r.Dropped.CutTexts = append(r.Dropped.CutTexts, what)
	}
	return text
}

// cutText cuts a description or comment to the Yalkos limit and says so at
// the end.
func cutText(s string) (string, bool) {
	out, cut := cutRunes(s, maxText)
	if !cut {
		return s, false
	}
	return out + cutNote, true
}

// cutRunes keeps the first n runes of s.
func cutRunes(s string, n int) (string, bool) {
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos], true
		}
		i++
	}
	return s, false
}

// oneLine turns line breaks into spaces and trims s.
func oneLine(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s))
}

// planeDate reads a Plane date (YYYY-MM-DD, possibly followed by a time) as
// UTC midnight of that day; nil when missing or unreadable.
func planeDate(s *string) *time.Time {
	if s == nil || len(*s) < len(time.DateOnly) {
		return nil
	}
	d, err := time.Parse(time.DateOnly, (*s)[:len(time.DateOnly)])
	if err != nil {
		return nil
	}
	return &d
}

func orTime(t, fallback time.Time) time.Time {
	if t.IsZero() {
		return fallback
	}
	return t
}

func laterOf(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}
