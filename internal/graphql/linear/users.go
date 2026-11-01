package linear

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/page"
)

// userMatch applies a user filter in memory; me answers isMe.
func userMatch(f *UserFilter, u account.User, me uuid.UUID) (bool, error) {
	if f == nil {
		return true, nil
	}
	for _, sub := range f.And {
		if ok, err := userMatch(&sub, u, me); err != nil || !ok {
			return false, err
		}
	}
	if len(f.Or) > 0 {
		matched := false
		for _, sub := range f.Or {
			ok, err := userMatch(&sub, u, me)
			if err != nil {
				return false, err
			}
			matched = matched || ok
		}
		if !matched {
			return false, nil
		}
	}
	ok, err := matchID("id", f.ID, u.ID)
	if err != nil || !ok {
		return false, err
	}
	return matchString(f.Name, u.Name) && matchString(f.DisplayName, u.Name) && matchString(f.Email, u.Email) &&
		matchBool(f.Active, !u.Disabled) && matchBool(f.Admin, u.IsAdmin()) &&
		matchBool(f.Owner, u.Role == account.RoleOwner) && matchBool(f.IsMe, u.ID == me), nil
}

// sortKey checks a one-element, one-key sort input and returns its order.
func sortKey(nulls *PaginationNulls, order *PaginationSortOrder) (desc bool, err error) {
	if nulls != nil && *nulls == PaginationNullsFirst {
		return false, badArg("sort", "Invalid sort: nulls first is not supported")
	}
	return order != nil && *order == PaginationSortOrderDescending, nil
}

func tooManySortKeys() error { return badArg("sort", "Invalid sort: use one sort key") }

// sortUsers orders by name when sort asks for it.
func sortUsers(users []account.User, sort []UserSortInput) error {
	if len(sort) == 0 {
		return nil
	}
	if len(sort) > 1 {
		return tooManySortKeys()
	}
	s := sort[0]
	var desc bool
	var err error
	switch {
	case s.Name != nil && s.DisplayName != nil:
		return tooManySortKeys()
	case s.Name != nil:
		desc, err = sortKey(s.Name.Nulls, s.Name.Order)
	case s.DisplayName != nil:
		desc, err = sortKey(s.DisplayName.Nulls, s.DisplayName.Order)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	slices.SortStableFunc(users, func(a, b account.User) int {
		c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		if desc {
			return -c
		}
		return c
	})
	return nil
}

// userConn filters, orders and pages users in memory.
func (r *Resolver) userConn(users []account.User, me uuid.UUID, f *UserFilter, includeDisabled *bool, by *PaginationOrderBy, sort []UserSortInput, a page.Args) (*UserConnection, error) {
	var kept []account.User
	for _, u := range users {
		if u.Disabled && (includeDisabled == nil || !*includeDisabled) {
			continue
		}
		ok, err := userMatch(f, u, me)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, u)
		}
	}
	id := func(u account.User) uuid.UUID { return u.ID }
	orderByTime(kept, id, func(u account.User) time.Time { return u.CreatedAt }, func(u account.User) time.Time { return u.UpdatedAt }, by)
	if err := sortUsers(kept, sort); err != nil {
		return nil, err
	}
	items, info, err := pageOf(kept, id, a)
	if err != nil {
		return nil, err
	}
	out := &UserConnection{PageInfo: info, Nodes: make([]User, len(items)), Edges: make([]UserEdge, len(items))}
	for i, u := range items {
		n := r.userOut(u, me)
		out.Nodes[i] = *n
		out.Edges[i] = UserEdge{Node: n, Cursor: cursorOf(u.ID)}
	}
	return out, nil
}

// userByID loads one user through the request's loader.
func (r *Resolver) userByID(ctx context.Context, field, id string) (*User, error) {
	me, err := r.actor(ctx)
	if err != nil {
		return nil, err
	}
	uid, err := parseID(field, id)
	if err != nil {
		return nil, err
	}
	u, err := loadersFrom(ctx).users.Load(ctx, uid)
	if err != nil {
		return nil, err
	}
	return r.userOut(u, me.ID), nil
}
