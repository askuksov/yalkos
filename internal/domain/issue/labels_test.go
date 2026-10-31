package issue_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/page"
)

type labelEnv struct {
	env
	labels                  *label.Service
	bug, feature, task      uuid.UUID
	perf, security, typeGrp uuid.UUID
}

func newLabelEnv(t *testing.T) labelEnv {
	t.Helper()
	ctx := context.Background()
	e := labelEnv{env: newEnv(t), labels: nil}
	e.labels = label.New(e.pool, e.clock)
	all, err := e.labels.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range all {
		switch l.Name {
		case "Bug":
			e.bug = l.ID
		case "Feature":
			e.feature = l.ID
		case "Task":
			e.task = l.ID
		case "Type":
			e.typeGrp = l.ID
		}
	}
	for _, n := range []string{"Perf", "Security"} {
		l, err := e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: n, Color: "#000000"})
		if err != nil {
			t.Fatal(err)
		}
		if n == "Perf" {
			e.perf = l.ID
		} else {
			e.security = l.ID
		}
	}
	return e
}

func sorted(ids ...uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out
}

func (e labelEnv) createWith(t *testing.T, in issue.CreateInput) issue.Issue {
	t.Helper()
	if in.ProjectID == nil && in.ParentID == nil {
		in.ProjectID = &e.ylk.ID
	}
	if in.Title == "" {
		in.Title = "t"
	}
	i, err := e.issues.Create(context.Background(), e.users.Member, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return i
}

func (e labelEnv) labelsOf(t *testing.T, id uuid.UUID) []uuid.UUID {
	t.Helper()
	m, err := e.issues.LabelIDs(context.Background(), []uuid.UUID{id})
	if err != nil {
		t.Fatal(err)
	}
	return m[id]
}

func TestCreateGetsDefaultType(t *testing.T) {
	e := newLabelEnv(t)
	ctx := context.Background()
	plain := e.createWith(t, issue.CreateInput{})
	if !slices.Equal(plain.LabelIDs, []uuid.UUID{e.task}) || !slices.Equal(e.labelsOf(t, plain.ID), []uuid.UUID{e.task}) {
		t.Errorf("default = %v / %v, want [Task]", plain.LabelIDs, e.labelsOf(t, plain.ID))
	}
	bug := e.createWith(t, issue.CreateInput{LabelIDs: []uuid.UUID{e.bug, e.perf}})
	if !slices.Equal(bug.LabelIDs, sorted(e.bug, e.perf)) {
		t.Errorf("explicit = %v", bug.LabelIDs)
	}
	got, err := e.issues.Get(ctx, bug.ID.String())
	if err != nil || !slices.Equal(got.LabelIDs, sorted(e.bug, e.perf)) {
		t.Errorf("Get labels = %v, %v", got.LabelIDs, err)
	}
	h := e.history(t, bug.ID)
	var created []uuid.UUID
	for _, c := range h[0].Changes {
		if c.Field == "labelIds" {
			_ = json.Unmarshal(c.To, &created)
		}
	}
	if !slices.Equal(created, sorted(e.bug, e.perf)) {
		t.Errorf("create audit labelIds = %v", created)
	}
	if err := e.labels.SetDefaultType(ctx, e.users.Admin, nil); err != nil {
		t.Fatal(err)
	}
	none := e.createWith(t, issue.CreateInput{})
	if len(none.LabelIDs) != 0 {
		t.Errorf("without default = %v", none.LabelIDs)
	}
}

func TestSubIssueGetsDefaultNotParentType(t *testing.T) {
	e := newLabelEnv(t)
	parent := e.createWith(t, issue.CreateInput{LabelIDs: []uuid.UUID{e.bug}})
	child := e.createWith(t, issue.CreateInput{ParentID: &parent.ID})
	if !slices.Equal(child.LabelIDs, []uuid.UUID{e.task}) {
		t.Errorf("child labels = %v, want [Task]", child.LabelIDs)
	}
}

func TestAddExclusiveReplaces(t *testing.T) {
	e := newLabelEnv(t)
	ctx := context.Background()
	i := e.createWith(t, issue.CreateInput{LabelIDs: []uuid.UUID{e.bug, e.perf}})
	u, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{e.feature}})
	if err != nil || !slices.Equal(u.LabelIDs, sorted(e.feature, e.perf)) {
		t.Fatalf("add = %v, %v", u.LabelIDs, err)
	}
	h := e.history(t, i.ID)
	last := h[len(h)-1].Changes
	var from, to []uuid.UUID
	if len(last) == 1 && last[0].Field == "labelIds" {
		_ = json.Unmarshal(last[0].From, &from)
		_ = json.Unmarshal(last[0].To, &to)
	}
	if !slices.Equal(from, sorted(e.bug, e.perf)) || !slices.Equal(to, sorted(e.feature, e.perf)) {
		t.Errorf("audit = %+v", last)
	}
	u, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{RemoveLabelIDs: []uuid.UUID{e.perf, uuid.New()}})
	if err != nil || !slices.Equal(u.LabelIDs, []uuid.UUID{e.feature}) {
		t.Fatalf("remove = %v, %v", u.LabelIDs, err)
	}
	set := []uuid.UUID{e.security}
	u, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{LabelIDs: &set})
	if err != nil || !slices.Equal(u.LabelIDs, []uuid.UUID{e.security}) {
		t.Fatalf("set = %v, %v", u.LabelIDs, err)
	}
	// An unchanged label set records nothing.
	before := len(e.history(t, i.ID))
	if _, err := e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{e.security}}); err != nil {
		t.Fatal(err)
	}
	if after := len(e.history(t, i.ID)); after != before {
		t.Errorf("no-op label update recorded %d events", after-before)
	}
}

func TestTwoTypesInOneInput(t *testing.T) {
	e := newLabelEnv(t)
	ctx := context.Background()
	_, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "t", LabelIDs: []uuid.UUID{e.bug, e.feature}})
	wantInput(t, err, "labelIds", kit.ReasonNotAllowed)
	i := e.createWith(t, issue.CreateInput{})
	set := []uuid.UUID{e.bug, e.feature}
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{LabelIDs: &set})
	wantInput(t, err, "labelIds", kit.ReasonNotAllowed)
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{e.bug, e.feature}})
	wantInput(t, err, "labelIds", kit.ReasonNotAllowed)
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{e.typeGrp}})
	wantInput(t, err, "labelIds", kit.ReasonNotAllowed)
	_, err = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{uuid.New()}})
	wantInput(t, err, "labelIds", kit.ReasonNotFound)
}

func TestLabelLimit(t *testing.T) {
	e := newLabelEnv(t)
	ctx := context.Background()
	var ids []uuid.UUID
	for n := range 21 {
		l, err := e.labels.Create(ctx, e.users.Member, label.CreateInput{Name: "L" + string(rune('a'+n)), Color: "#000000"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, l.ID)
	}
	_, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "t", LabelIDs: ids})
	var ie *kit.InputError
	wantInput(t, err, "labelIds", kit.ReasonTooMany)
	if errors.As(err, &ie); ie.Params["max"] != 20 {
		t.Errorf("params = %v", ie.Params)
	}
	// 20 labels including the default type are fine.
	if _, err := e.issues.Create(ctx, e.users.Member, issue.CreateInput{ProjectID: &e.ylk.ID, Title: "t", LabelIDs: ids[:19]}); err != nil {
		t.Errorf("19 + default type: %v", err)
	}
}

func TestExclusiveGroupConcurrent(t *testing.T) {
	e := newLabelEnv(t)
	ctx := context.Background()
	for round := range 10 {
		i := e.createWith(t, issue.CreateInput{LabelIDs: []uuid.UUID{e.perf}})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for n, l := range []uuid.UUID{e.bug, e.feature} {
			wg.Go(func() {
				_, errs[n] = e.issues.Update(ctx, e.users.Member, i.ID, issue.Patch{AddLabelIDs: []uuid.UUID{l}})
			})
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		types := 0
		for _, l := range e.labelsOf(t, i.ID) {
			if l == e.bug || l == e.feature || l == e.task {
				types++
			}
		}
		if types != 1 {
			t.Fatalf("round %d: %d types on the issue", round, types)
		}
	}
}

func TestFilterLabelAndType(t *testing.T) {
	e := newLabelEnv(t)
	ctx := context.Background()
	perfBug := e.createWith(t, issue.CreateInput{Title: "perfBug", LabelIDs: []uuid.UUID{e.perf, e.bug}})
	sec := e.createWith(t, issue.CreateInput{Title: "sec", LabelIDs: []uuid.UUID{e.security}})
	task := e.createWith(t, issue.CreateInput{Title: "task"})
	if err := e.labels.SetDefaultType(ctx, e.users.Admin, nil); err != nil {
		t.Fatal(err)
	}
	bare := e.createWith(t, issue.CreateInput{Title: "bare"})
	check := func(name string, f issue.Filter, want ...issue.Issue) {
		t.Helper()
		r, err := e.issues.List(ctx, f, issue.Order{}, page.Args{First: ptr(50)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var got, exp []string
		for _, i := range r.Items {
			got = append(got, i.Title)
		}
		for _, i := range want {
			exp = append(exp, i.Title)
		}
		slices.Sort(got)
		slices.Sort(exp)
		if !slices.Equal(got, exp) {
			t.Errorf("%s: got %v want %v", name, got, exp)
		}
	}
	check("label in perf", issue.Filter{Label: &issue.LabelCond{In: []uuid.UUID{e.perf}}}, perfBug)
	check("label nin security", issue.Filter{Label: &issue.LabelCond{Nin: []uuid.UUID{e.security}}}, perfBug, task, bare)
	check("no labels", issue.Filter{Label: &issue.LabelCond{Null: ptr(true)}}, bare)
	check("has labels", issue.Filter{Label: &issue.LabelCond{Null: ptr(false)}}, perfBug, sec, task)
	check("type bug", issue.Filter{Type: &issue.TypeCond{In: []uuid.UUID{e.bug}}}, perfBug)
	check("no type", issue.Filter{Type: &issue.TypeCond{Null: ptr(true)}}, bare)
	check("has type", issue.Filter{Type: &issue.TypeCond{Null: ptr(false)}}, perfBug, sec, task)
	check("label in []", issue.Filter{Label: &issue.LabelCond{In: []uuid.UUID{}}})
}
