package native

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
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
}

type loadersKey struct{}

const loaderWait = time.Millisecond

func newLoaders(d Deps) *loaders {
	opt := dataloadgen.WithWait(loaderWait)
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
