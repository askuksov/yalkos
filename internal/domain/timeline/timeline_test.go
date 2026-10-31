package timeline_test

import (
	"context"
	"slices"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/timeline"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

func ptr[T any](v T) *T { return &v }

func TestTimelineMerge(t *testing.T) {
	ctx := context.Background()
	pool, users, clock := testfx.New(t)
	projects := project.New(pool, clock)
	issues := issue.New(pool, projects, clock)
	comments := comment.New(pool, issues, clock)
	p, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "YLK", Name: "Yalkos"})
	if err != nil {
		t.Fatal(err)
	}
	i, err := issues.Create(ctx, users.Member, issue.CreateInput{ProjectID: &p.ID, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := comments.Create(ctx, users.Member, i.ID, "one", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := issues.Update(ctx, users.Member, i.ID, issue.Patch{Title: ptr("t2")}); err != nil {
		t.Fatal(err)
	}
	if _, err := comments.Create(ctx, users.Member, i.ID, "two", nil); err != nil {
		t.Fatal(err)
	}
	db := q.New(pool)
	kinds := func(items []timeline.Item) []string {
		out := make([]string, len(items))
		for n, it := range items {
			out[n] = it.Kind
		}
		return out
	}
	all, err := timeline.List(ctx, db, i.ID, page.Args{First: ptr(10)})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{timeline.Event, timeline.Comment, timeline.Event, timeline.Comment}
	if !slices.Equal(kinds(all.Items), want) {
		t.Fatalf("kinds = %v, want %v", kinds(all.Items), want)
	}
	// Forward pages of 2.
	p1, err := timeline.List(ctx, db, i.ID, page.Args{First: ptr(2)})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := timeline.List(ctx, db, i.ID, page.Args{First: ptr(2), After: p1.Info.End})
	if err != nil {
		t.Fatal(err)
	}
	fwd := append(slices.Clone(p1.Items), p2.Items...)
	if !slices.Equal(fwd, all.Items) || !p1.Info.HasNext || p2.Info.HasNext {
		t.Errorf("forward pages = %+v / %+v", p1, p2)
	}
	// Backward pages of 2 from the end.
	b1, err := timeline.List(ctx, db, i.ID, page.Args{Last: ptr(2)})
	if err != nil {
		t.Fatal(err)
	}
	b2, err := timeline.List(ctx, db, i.ID, page.Args{Last: ptr(2), Before: b1.Info.Start})
	if err != nil {
		t.Fatal(err)
	}
	back := append(slices.Clone(b2.Items), b1.Items...)
	if !slices.Equal(back, all.Items) || !b1.Info.HasPrev || b2.Info.HasPrev {
		t.Errorf("backward pages = %+v / %+v", b1, b2)
	}
	if _, err := timeline.List(ctx, db, i.ID, page.Args{First: ptr(2), After: ptr("bogus")}); err == nil {
		t.Error("bad cursor accepted")
	}
}
