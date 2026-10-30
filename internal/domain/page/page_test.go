package page_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
)

func ptr[T any](v T) *T { return &v }

func wantInput(t *testing.T, err error, field string, reason kit.Reason) {
	t.Helper()
	var ie *kit.InputError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want InputError field %q", err, field)
	}
	if ie.Field != field || ie.Reason != reason {
		t.Fatalf("InputError = {%s %s}, want {%s %s}", ie.Field, ie.Reason, field, reason)
	}
}

func TestWindowDefaults(t *testing.T) {
	w, err := page.Args{}.Window()
	if err != nil {
		t.Fatal(err)
	}
	if w.Limit != 50 || !w.Forward || w.Cursor != nil {
		t.Fatalf("window = %+v", w)
	}
}

func TestWindowLimits(t *testing.T) {
	cases := []struct {
		name   string
		args   page.Args
		field  string
		reason kit.Reason
	}{
		{"first 0", page.Args{First: ptr(0)}, "first", kit.ReasonOutOfRange},
		{"first 251", page.Args{First: ptr(251)}, "first", kit.ReasonOutOfRange},
		{"last 251", page.Args{Last: ptr(251)}, "last", kit.ReasonOutOfRange},
		{"first+last", page.Args{First: ptr(1), Last: ptr(1)}, "first", kit.ReasonNotAllowed},
		{"first+before", page.Args{First: ptr(1), Before: ptr("x")}, "before", kit.ReasonNotAllowed},
		{"last+after", page.Args{Last: ptr(1), After: ptr("x")}, "after", kit.ReasonNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.args.Window()
			wantInput(t, err, c.field, c.reason)
		})
	}
	var ie *kit.InputError
	_, err := page.Args{First: ptr(0)}.Window()
	if errors.As(err, &ie) && (ie.Params["min"] != 1 || ie.Params["max"] != 250) {
		t.Errorf("params = %v, want min 1 max 250", ie.Params)
	}

	w, err := page.Args{First: ptr(250)}.Window()
	if err != nil || w.Limit != 250 || !w.Forward {
		t.Fatalf("first 250: %+v, %v", w, err)
	}
	w, err = page.Args{Last: ptr(3), Before: ptr("c")}.Window()
	if err != nil || w.Limit != 3 || w.Forward || w.Cursor == nil || *w.Cursor != "c" {
		t.Fatalf("last 3 before c: %+v, %v", w, err)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	c := page.Cursor{F: "priority", D: "desc", V: json.RawMessage(`3`), ID: uuid.New()}
	got, err := page.Decode(page.Encode(c), "after")
	if err != nil {
		t.Fatal(err)
	}
	if got.F != c.F || got.D != c.D || string(got.V) != string(c.V) || got.ID != c.ID {
		t.Fatalf("round trip: %+v != %+v", got, c)
	}
}

func TestCursorGarbage(t *testing.T) {
	for _, s := range []string{"zz", base64.RawURLEncoding.EncodeToString([]byte("{}"))} {
		_, err := page.Decode(s, "after")
		wantInput(t, err, "after", kit.ReasonInvalidFormat)
	}
	_, err := page.Decode("zz", "before")
	wantInput(t, err, "before", kit.ReasonInvalidFormat)
}

func rows(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func cur(i int) string { return fmt.Sprintf("c%d", i) }

func TestFinishForward(t *testing.T) {
	r := page.Finish(rows(6), page.Window{Limit: 5, Forward: true}, cur)
	if len(r.Items) != 5 || !r.Info.HasNext || r.Info.HasPrev {
		t.Fatalf("result = %+v", r)
	}
	if *r.Info.Start != "c0" || *r.Info.End != cur(r.Items[4]) || len(r.Cursors) != 5 {
		t.Fatalf("cursors = %v start %v end %v", r.Cursors, *r.Info.Start, *r.Info.End)
	}
	r = page.Finish(rows(3), page.Window{Limit: 5, Forward: true, Cursor: ptr("x")}, cur)
	if len(r.Items) != 3 || r.Info.HasNext || !r.Info.HasPrev {
		t.Fatalf("with after: %+v", r.Info)
	}
	r = page.Finish([]int{}, page.Window{Limit: 5, Forward: true}, cur)
	if len(r.Items) != 0 || r.Info.Start != nil || r.Info.End != nil {
		t.Fatalf("empty: %+v", r)
	}
}

func TestFinishBackward(t *testing.T) {
	// Backward pages come from the query in reverse order: 5,4,3,2,1,0.
	in := []int{5, 4, 3, 2, 1, 0}
	r := page.Finish(in, page.Window{Limit: 5, Forward: false}, cur)
	want := []int{1, 2, 3, 4, 5}
	if fmt.Sprint(r.Items) != fmt.Sprint(want) {
		t.Fatalf("items = %v, want %v", r.Items, want)
	}
	if !r.Info.HasPrev || r.Info.HasNext || *r.Info.Start != "c1" || *r.Info.End != "c5" {
		t.Fatalf("info = %+v", r.Info)
	}
	r = page.Finish([]int{2, 1}, page.Window{Limit: 5, Forward: false, Cursor: ptr("x")}, cur)
	if r.Info.HasPrev || !r.Info.HasNext {
		t.Fatalf("with before: %+v", r.Info)
	}
}
