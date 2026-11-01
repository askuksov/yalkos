package linear

import (
	"cmp"
	"encoding/base64"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
)

// cursorOf is the cursor of an item of an in-memory list.
func cursorOf(id uuid.UUID) string { return base64.RawURLEncoding.EncodeToString(id[:]) }

func cursorID(field, s string) (uuid.UUID, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil && len(b) == 16 {
		return uuid.UUID(b), nil
	}
	return uuid.Nil, kit.Input(field, kit.ReasonInvalidFormat, "cursor is invalid")
}

// pageOf cuts one Relay page out of a list that is already filtered and
// ordered; cursors hold item ids.
func pageOf[T any](items []T, id func(T) uuid.UUID, a page.Args) ([]T, *PageInfo, error) {
	w, err := a.Window()
	if err != nil {
		return nil, nil, err
	}
	pos := -1
	if w.Cursor != nil {
		field := "after"
		if !w.Forward {
			field = "before"
		}
		cid, err := cursorID(field, *w.Cursor)
		if err != nil {
			return nil, nil, err
		}
		pos = slices.IndexFunc(items, func(t T) bool { return id(t) == cid })
		if pos < 0 {
			return nil, nil, kit.Input(field, kit.ReasonInvalidFormat, "cursor is invalid")
		}
	}
	var from, to int
	if w.Forward {
		from = pos + 1
		to = min(from+w.Limit, len(items))
	} else {
		to = len(items)
		if pos >= 0 {
			to = pos
		}
		from = max(to-w.Limit, 0)
	}
	out := items[from:to]
	info := &PageInfo{HasPreviousPage: from > 0, HasNextPage: to < len(items)}
	if len(out) > 0 {
		s, e := cursorOf(id(out[0])), cursorOf(id(out[len(out)-1]))
		info.StartCursor, info.EndCursor = &s, &e
	}
	return out, info, nil
}

// orderByTime sorts newest first by createdAt, or by updatedAt when asked;
// ties by id, newest first.
func orderByTime[T any](items []T, id func(T) uuid.UUID, created, updated func(T) time.Time, by *PaginationOrderBy) {
	key := created
	if by != nil && *by == PaginationOrderByUpdatedAt {
		key = updated
	}
	slices.SortStableFunc(items, func(a, b T) int {
		if c := key(b).Compare(key(a)); c != 0 {
			return c
		}
		return cmp.Compare(id(b).String(), id(a).String())
	})
}

func pageArgs(first *int, after *string, last *int, before *string) page.Args {
	return page.Args{First: first, After: after, Last: last, Before: before}
}
