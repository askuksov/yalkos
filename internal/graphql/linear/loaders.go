package linear

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/vikstrous/dataloadgen"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/kit"
)

// loaders batch per-request lookups by id, so a list of N items costs one
// query per related entity type instead of N.
type loaders struct {
	users *dataloadgen.Loader[uuid.UUID, account.User]
	// By user id: live issues they created.
	createdIssues *dataloadgen.Loader[uuid.UUID, int]
}

type loadersKey struct{}

const loaderWait = time.Millisecond

func newLoaders(d Deps) *loaders {
	opt := dataloadgen.WithWait(loaderWait)
	return &loaders{
		users: dataloadgen.NewLoader(byID("User", func(ctx context.Context, ids []uuid.UUID) ([]account.User, error) {
			return d.Accounts.UsersByIDs(ctx, ids)
		}, func(u account.User) uuid.UUID { return u.ID }), opt),
		createdIssues: dataloadgen.NewLoader(mapped(d.Issues.CountByCreators), opt),
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
