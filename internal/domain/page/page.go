// Package page implements Relay-style pagination: argument
// checks, opaque keyset cursors and page assembly.
package page

import (
	"encoding/base64"
	"encoding/json"
	"slices"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
)

// Page sizes.
const (
	Default = 50
	Max     = 250
)

// Args are the Relay connection arguments.
type Args struct {
	First, Last   *int
	After, Before *string
}

// Window is what a query needs: how many rows, which direction, from where.
// Queries fetch Limit+1 rows to learn whether more exist.
type Window struct {
	Limit   int
	Forward bool
	Cursor  *string
}

// Window validates the arguments.
func (a Args) Window() (Window, error) {
	if a.First != nil && a.Last != nil {
		return Window{}, kit.Input("first", kit.ReasonNotAllowed, "use either first or last, not both")
	}
	if err := checkSize("first", a.First); err != nil {
		return Window{}, err
	}
	if err := checkSize("last", a.Last); err != nil {
		return Window{}, err
	}
	if a.Last != nil && a.After != nil {
		return Window{}, kit.Input("after", kit.ReasonNotAllowed, "after cannot be used with last")
	}
	if a.Before != nil && (a.First != nil || a.After != nil) {
		return Window{}, kit.Input("before", kit.ReasonNotAllowed, "before cannot be used with first or after")
	}
	if a.Last != nil || a.Before != nil {
		return Window{Limit: size(a.Last), Forward: false, Cursor: a.Before}, nil
	}
	return Window{Limit: size(a.First), Forward: true, Cursor: a.After}, nil
}

func checkSize(field string, n *int) error {
	if n != nil && (*n < 1 || *n > Max) {
		return kit.Input(field, kit.ReasonOutOfRange, "must be 1–250").With("min", 1).With("max", Max)
	}
	return nil
}

func size(n *int) int {
	if n == nil {
		return Default
	}
	return *n
}

// Cursor is a keyset position: order field F, direction D ("asc"/"desc"),
// the sort key value V and the row id as tie-break.
type Cursor struct {
	F  string          `json:"f"`
	D  string          `json:"d"`
	V  json.RawMessage `json:"v"`
	ID uuid.UUID       `json:"id"`
}

// Encode returns the opaque cursor string.
func Encode(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Decode parses a cursor; field ("after"/"before") names the argument in the error.
func Decode(s, field string) (Cursor, error) {
	bad := kit.Input(field, kit.ReasonInvalidFormat, "invalid cursor")
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, bad
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil || c.F == "" || (c.D != "asc" && c.D != "desc") || c.ID == uuid.Nil {
		return Cursor{}, bad
	}
	return c, nil
}

// Info is the Relay pageInfo.
type Info struct {
	HasNext, HasPrev bool
	Start, End       *string
}

// Result is one page in natural order with a cursor per item.
type Result[T any] struct {
	Items   []T
	Cursors []string
	Info    Info
}

// Finish turns rows fetched with LIMIT w.Limit+1 in query direction into a page:
// drops the probe row, restores natural order for backward pages, fills Info.
func Finish[T any](rows []T, w Window, cursor func(T) string) Result[T] {
	more := len(rows) > w.Limit
	if more {
		rows = rows[:w.Limit]
	}
	items := slices.Clone(rows)
	var info Info
	if w.Forward {
		info.HasNext, info.HasPrev = more, w.Cursor != nil
	} else {
		slices.Reverse(items)
		info.HasPrev, info.HasNext = more, w.Cursor != nil
	}
	cursors := make([]string, len(items))
	for i, it := range items {
		cursors[i] = cursor(it)
	}
	if len(cursors) > 0 {
		info.Start, info.End = &cursors[0], &cursors[len(cursors)-1]
	}
	return Result[T]{Items: items, Cursors: cursors, Info: info}
}
