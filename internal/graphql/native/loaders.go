package native

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/webhook"
)

// loaders batch per-request lookups by id, so a list of N items costs one
// query per related entity type instead of N.
type loaders struct {
	users    *dataloadgen.Loader[uuid.UUID, account.User]
	projects *dataloadgen.Loader[uuid.UUID, project.Project]
	states   *dataloadgen.Loader[uuid.UUID, project.State]
	issues   *dataloadgen.Loader[uuid.UUID, issue.Issue]

	// By project id: a project's states and member ids.
	projectStates  *dataloadgen.Loader[uuid.UUID, []project.State]
	projectMembers *dataloadgen.Loader[uuid.UUID, []uuid.UUID]

	// By issue id: progress of its live direct children.
	subIssueProgress *dataloadgen.Loader[uuid.UUID, issue.Progress]

	milestones *dataloadgen.Loader[uuid.UUID, milestone.Milestone]
	// By project id: its milestones in manual order, all or open only.
	projectMilestones     *dataloadgen.Loader[uuid.UUID, []milestone.Milestone]
	projectOpenMilestones *dataloadgen.Loader[uuid.UUID, []milestone.Milestone]
	// By milestone id: counts of its live issues.
	milestoneProgress *dataloadgen.Loader[uuid.UUID, milestone.Progress]
	// By issue id: its sorted label ids.
	issueLabels *dataloadgen.Loader[uuid.UUID, []uuid.UUID]
	labels      *lazyLabels
	// By issue id: number of comments and the attachments.
	commentCounts    *dataloadgen.Loader[uuid.UUID, int]
	issueAttachments *dataloadgen.Loader[uuid.UUID, []attachment.Attachment]
	// By issue id: whether each attachment of the batch is used in its text.
	attachmentsUsed *dataloadgen.Loader[uuid.UUID, map[uuid.UUID]bool]
	// By webhook id and page size: its newest deliveries.
	webhookDeliveries *dataloadgen.Loader[deliveriesKey, []webhook.Delivery]

	// One page of sub-issues per parent and of history entries per issue, for
	// the page arguments (and order) of the selection.
	children *dataloadgen.Loader[childrenKey, page.Result[issue.Issue]]
	history  *dataloadgen.Loader[historyKey, page.Result[audit.Entry]]
}

// pageKey is page.Args in a comparable form, for loader keys: two selections
// share a batch only when their arguments are equal.
type pageKey struct {
	first, last         int
	hasFirst, hasLast   bool
	after, before       string
	hasAfter, hasBefore bool
}

func pageKeyOf(a page.Args) pageKey {
	var k pageKey
	if a.First != nil {
		k.first, k.hasFirst = *a.First, true
	}
	if a.Last != nil {
		k.last, k.hasLast = *a.Last, true
	}
	if a.After != nil {
		k.after, k.hasAfter = *a.After, true
	}
	if a.Before != nil {
		k.before, k.hasBefore = *a.Before, true
	}
	return k
}

func (k pageKey) args() page.Args {
	var a page.Args
	if k.hasFirst {
		a.First = &k.first
	}
	if k.hasLast {
		a.Last = &k.last
	}
	if k.hasAfter {
		a.After = &k.after
	}
	if k.hasBefore {
		a.Before = &k.before
	}
	return a
}

type childrenGroup struct {
	page  pageKey
	order issue.Order
}

type childrenKey struct {
	parent uuid.UUID
	childrenGroup
}

type historyKey struct {
	entity uuid.UUID
	page   pageKey
}

// pagedBatch adapts a fetch of one page per owner id to a loader fetch: keys
// are grouped by everything but their owner, one fetch runs per group, and an
// error fails only the keys of its group.
func pagedBatch[K comparable, G comparable, V any](owner func(K) uuid.UUID, group func(K) G,
	fetch func(context.Context, []uuid.UUID, G) (map[uuid.UUID]V, error)) func(context.Context, []K) ([]V, []error) {
	return func(ctx context.Context, keys []K) ([]V, []error) {
		values := make([]V, len(keys))
		errs := make([]error, len(keys))
		owners := map[G][]uuid.UUID{}
		for _, k := range keys {
			owners[group(k)] = append(owners[group(k)], owner(k))
		}
		found := make(map[G]map[uuid.UUID]V, len(owners))
		failed := make(map[G]error, len(owners))
		for g, ids := range owners {
			found[g], failed[g] = fetch(ctx, ids, g)
		}
		for i, k := range keys {
			g := group(k)
			errs[i] = failed[g]
			values[i] = found[g][owner(k)]
		}
		return values, errs
	}
}

// deliveriesKey is a webhook and how many of its newest deliveries to load.
type deliveriesKey struct {
	webhook uuid.UUID
	limit   int
}

type loadersKey struct{}

// loaderWait is how long a loader collects keys before it runs one batch.
// Tests widen it: a slow runner can need longer to reach every Load of a
// page, which splits the batch.
var loaderWait = time.Millisecond

func newLoaders(d Deps) *loaders {
	opt := dataloadgen.WithWait(loaderWait)
	// Sub-issues and history change with the mutations of the same request
	// (two updateIssue fields, each selecting its history): these loaders batch
	// but keep no results, so a read after a write sees it.
	fresh := dataloadgen.WithoutCache()
	return &loaders{
		users: dataloadgen.NewLoader(byID("User", func(ctx context.Context, ids []uuid.UUID) ([]account.User, error) {
			return d.Accounts.UsersByIDs(ctx, ids)
		}, func(u account.User) uuid.UUID { return u.ID }), opt),
		projects: dataloadgen.NewLoader(byID("Project", func(ctx context.Context, ids []uuid.UUID) ([]project.Project, error) {
			return d.Projects.ByIDs(ctx, ids)
		}, func(p project.Project) uuid.UUID { return p.ID }), opt),
		states: dataloadgen.NewLoader(byID("State", func(ctx context.Context, ids []uuid.UUID) ([]project.State, error) {
			return d.Projects.StatesByIDs(ctx, ids)
		}, func(s project.State) uuid.UUID { return s.ID }), opt),
		issues: dataloadgen.NewLoader(byID("Issue", func(ctx context.Context, ids []uuid.UUID) ([]issue.Issue, error) {
			return d.Issues.ByIDs(ctx, ids)
		}, func(i issue.Issue) uuid.UUID { return i.ID }), opt),
		projectStates:  dataloadgen.NewLoader(grouped(d.Projects.StatesByProjects), opt),
		projectMembers: dataloadgen.NewLoader(grouped(d.Projects.MemberIDsByProjects), opt),
		subIssueProgress: dataloadgen.NewLoader(func(ctx context.Context, ids []uuid.UUID) ([]issue.Progress, []error) {
			values := make([]issue.Progress, len(ids))
			errs := make([]error, len(ids))
			byID, err := d.Issues.SubIssueProgress(ctx, ids)
			for i, id := range ids {
				if err != nil {
					errs[i] = err
					continue
				}
				values[i] = byID[id] // no children: zero progress
			}
			return values, errs
		}, opt),
		milestones: dataloadgen.NewLoader(byID("Milestone", func(ctx context.Context, ids []uuid.UUID) ([]milestone.Milestone, error) {
			return d.Milestones.ByIDs(ctx, ids)
		}, func(m milestone.Milestone) uuid.UUID { return m.ID }), opt),
		projectMilestones: dataloadgen.NewLoader(grouped(func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]milestone.Milestone, error) {
			return d.Milestones.ByProjects(ctx, ids, true)
		}), opt),
		projectOpenMilestones: dataloadgen.NewLoader(grouped(func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]milestone.Milestone, error) {
			return d.Milestones.ByProjects(ctx, ids, false)
		}), opt),
		milestoneProgress: dataloadgen.NewLoader(mapped(d.Milestones.Progress), opt),
		issueLabels:       dataloadgen.NewLoader(grouped(d.Issues.LabelIDs), opt),
		labels:            &lazyLabels{load: loadLabelIndex(d.Labels)},
		commentCounts:     dataloadgen.NewLoader(mapped(d.Comments.Counts), opt),
		issueAttachments:  dataloadgen.NewLoader(grouped(d.Attachments.ByIssues), opt),
		attachmentsUsed:   dataloadgen.NewLoader(shared(d.Attachments.UsedInText), opt),
		webhookDeliveries: dataloadgen.NewLoader(webhookDeliveries(d), opt),
		children: dataloadgen.NewLoader(pagedBatch(
			func(k childrenKey) uuid.UUID { return k.parent },
			func(k childrenKey) childrenGroup { return k.childrenGroup },
			func(ctx context.Context, ids []uuid.UUID, g childrenGroup) (map[uuid.UUID]page.Result[issue.Issue], error) {
				return d.Issues.ChildrenOf(ctx, ids, g.order, g.page.args())
			}), opt, fresh),
		history: dataloadgen.NewLoader(pagedBatch(
			func(k historyKey) uuid.UUID { return k.entity },
			func(k historyKey) pageKey { return k.page },
			func(ctx context.Context, ids []uuid.UUID, p pageKey) (map[uuid.UUID]page.Result[audit.Entry], error) {
				return d.Audit.ListMany(ctx, ids, p.args())
			}), opt, fresh),
	}
}

// webhookDeliveries batches the newest deliveries per webhook: one query per
// distinct page size. The actor must be allowed to read webhooks.
func webhookDeliveries(d Deps) func(context.Context, []deliveriesKey) ([][]webhook.Delivery, []error) {
	return func(ctx context.Context, keys []deliveriesKey) ([][]webhook.Delivery, []error) {
		values := make([][]webhook.Delivery, len(keys))
		errs := make([]error, len(keys))
		a, err := actor(ctx)
		byLimit := map[int][]uuid.UUID{}
		for _, k := range keys {
			byLimit[k.limit] = append(byLimit[k.limit], k.webhook)
		}
		found := make(map[deliveriesKey][]webhook.Delivery, len(keys))
		failed := make(map[int]error, len(byLimit))
		for limit, ids := range byLimit {
			if err != nil {
				failed[limit] = err
				continue
			}
			byWebhook, ferr := d.Webhooks.LatestDeliveries(ctx, a.User, ids, limit)
			if ferr != nil {
				failed[limit] = ferr
				continue
			}
			for id, ds := range byWebhook {
				found[deliveriesKey{id, limit}] = ds
			}
		}
		for i, k := range keys {
			errs[i] = failed[k.limit]
			values[i] = found[k]
		}
		return values, errs
	}
}

// mapped adapts a fetch returning one value per key (zero value when absent).
func mapped[T any](fetch func(context.Context, []uuid.UUID) (map[uuid.UUID]T, error)) func(context.Context, []uuid.UUID) ([]T, []error) {
	return func(ctx context.Context, keys []uuid.UUID) ([]T, []error) {
		values := make([]T, len(keys))
		errs := make([]error, len(keys))
		byKey, err := fetch(ctx, keys)
		for i, k := range keys {
			if err != nil {
				errs[i] = err
				continue
			}
			values[i] = byKey[k]
		}
		return values, errs
	}
}

// grouped adapts a fetch returning lists per key; a key without rows gets an
// empty list.
func grouped[T any](fetch func(context.Context, []uuid.UUID) (map[uuid.UUID][]T, error)) func(context.Context, []uuid.UUID) ([][]T, []error) {
	return func(ctx context.Context, keys []uuid.UUID) ([][]T, []error) {
		values := make([][]T, len(keys))
		errs := make([]error, len(keys))
		byKey, err := fetch(ctx, keys)
		for i, k := range keys {
			if err != nil {
				errs[i] = err
				continue
			}
			values[i] = byKey[k]
		}
		return values, errs
	}
}

// shared adapts a fetch returning one value for the whole batch (a map keyed
// by unique child ids): every key gets that value, read-only.
func shared[T any](fetch func(context.Context, []uuid.UUID) (T, error)) func(context.Context, []uuid.UUID) ([]T, []error) {
	return func(ctx context.Context, keys []uuid.UUID) ([]T, []error) {
		values := make([]T, len(keys))
		errs := make([]error, len(keys))
		v, err := fetch(ctx, keys)
		for i := range keys {
			values[i], errs[i] = v, err
		}
		return values, errs
	}
}

// byID adapts a ByIDs-style fetch to a loader fetch; a missing id is NOT_FOUND.
func byID[T any](entity string, fetch func(context.Context, []uuid.UUID) ([]T, error), id func(T) uuid.UUID) func(context.Context, []uuid.UUID) ([]T, []error) {
	return func(ctx context.Context, keys []uuid.UUID) ([]T, []error) {
		values := make([]T, len(keys))
		errs := make([]error, len(keys))
		rows, err := fetch(ctx, keys)
		if err != nil {
			for i := range errs {
				errs[i] = err
			}
			return values, errs
		}
		found := make(map[uuid.UUID]T, len(rows))
		for _, r := range rows {
			found[id(r)] = r
		}
		for i, k := range keys {
			v, ok := found[k]
			if !ok {
				errs[i] = kit.NotFoundError{Entity: entity}
				continue
			}
			values[i] = v
		}
		return values, errs
	}
}

func withLoaders(d Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), loadersKey{}, newLoaders(d))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func loadersFrom(ctx context.Context) *loaders {
	return ctx.Value(loadersKey{}).(*loaders)
}
