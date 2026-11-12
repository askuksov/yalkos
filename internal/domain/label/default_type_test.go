package label_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
)

// wantDefault asserts the workspace's default type.
func wantDefault(t *testing.T, e env, want uuid.UUID) {
	t.Helper()
	ti, err := e.labels.TypeInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ti.DefaultID == nil || *ti.DefaultID != want {
		t.Errorf("default type = %v, want %v", ti.DefaultID, want)
	}
}

// wantParent asserts the group of the label named name.
func wantParent(t *testing.T, e env, name string, want *uuid.UUID) {
	t.Helper()
	l := e.byName(t, name)
	if (l.ParentID == nil) != (want == nil) || (want != nil && *l.ParentID != *want) {
		t.Errorf("%s group = %v, want %v", name, l.ParentID, want)
	}
}

func moveTo(g *uuid.UUID) label.Patch {
	return label.Patch{ParentID: kit.Opt[uuid.UUID]{Set: true, Value: g}}
}

// outcome waits for a background call, with a bound.
func outcome(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", what)
		return nil
	}
}

func TestDefaultTypeStaysInTypeGroup(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	task := e.byName(t, "Task")
	if e.types.DefaultID == nil || *e.types.DefaultID != task.ID {
		t.Fatal("fixture: Task must be the default type")
	}
	prio := e.create(t, e.users.Admin, label.CreateInput{Name: "Prio", IsGroup: true, Exclusive: ptr(true)})
	area := e.create(t, e.users.Admin, label.CreateInput{Name: "Area", IsGroup: true, Exclusive: ptr(false)})

	for name, g := range map[string]*uuid.UUID{"no group": nil, "exclusive group": &prio.ID, "other group": &area.ID} {
		_, err := e.labels.Update(ctx, e.users.Admin, task.ID, moveTo(g))
		wantInput(t, err, "parentId", kit.ReasonNotAllowed)
		var ie *kit.InputError
		if errors.As(err, &ie) && ie.Msg != "the default type must stay in the Type group" {
			t.Errorf("move to %s: message = %q", name, ie.Msg)
		}
		wantParent(t, e, "Task", &e.types.GroupID)
		wantDefault(t, e, task.ID)
	}

	// Moving the default within Type or renaming it is fine.
	if _, err := e.labels.Update(ctx, e.users.Admin, task.ID, label.Patch{
		Name: ptr("Chore"), ParentID: kit.Opt[uuid.UUID]{Set: true, Value: &e.types.GroupID},
	}); err != nil {
		t.Errorf("rename the default: %v", err)
	}

	// Control: other type labels may leave the group.
	bug := e.byName(t, "Bug")
	if _, err := e.labels.Update(ctx, e.users.Admin, bug.ID, moveTo(nil)); err != nil {
		t.Errorf("move a non-default type out: %v", err)
	}
	wantParent(t, e, "Bug", nil)
	wantDefault(t, e, task.ID)
}

// setDefault runs SetDefaultType in the background.
func setDefault(e env, id uuid.UUID) <-chan error {
	done := make(chan error, 1)
	go func() { done <- e.labels.SetDefaultType(context.Background(), e.users.Admin, &id) }()
	return done
}

// A label that leaves Type while SetDefaultType waits for it cannot become
// the default.
func TestSetDefaultTypeWaitsForMove(t *testing.T) {
	e := newEnv(t)
	bug := e.byName(t, "Bug")

	move := hold(t, e) // a move of Bug out of Type in progress
	if err := move.lock(bug.ID, "UPDATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := move.tx.Exec(context.Background(), `UPDATE labels SET parent_id = NULL WHERE id = $1`, bug.ID); err != nil {
		t.Fatal(err)
	}
	done := settle(t, e, setDefault(e, bug.ID))
	if !blocked(t, e) {
		t.Fatalf("SetDefaultType did not wait: %v", outcome(t, done, "SetDefaultType"))
	}
	move.commit()
	wantInput(t, outcome(t, done, "SetDefaultType"), "labelId", kit.ReasonNotAllowed)
	wantDefault(t, e, *e.types.DefaultID)
	wantParent(t, e, "Bug", nil)
}

// A label that becomes the default while its move waits for it stays in Type.
func TestMoveWaitsForSetDefaultType(t *testing.T) {
	e := newEnv(t)
	bug := e.byName(t, "Bug")

	set := hold(t, e) // SetDefaultType(Bug) in progress
	if err := set.lock(bug.ID, "SHARE"); err != nil {
		t.Fatal(err)
	}
	if _, err := set.tx.Exec(context.Background(), `UPDATE workspaces SET default_type_label_id = $1`, bug.ID); err != nil {
		t.Fatal(err)
	}
	done := settle(t, e, update(e, bug.ID, moveTo(nil)))
	if !blocked(t, e) {
		t.Fatalf("the move did not wait: %v", outcome(t, done, "move"))
	}
	set.commit()
	wantInput(t, outcome(t, done, "move"), "parentId", kit.ReasonNotAllowed)
	wantParent(t, e, "Bug", &e.types.GroupID)
	wantDefault(t, e, bug.ID)
}
