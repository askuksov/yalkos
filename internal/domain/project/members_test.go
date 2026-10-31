package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/testfx"
)

func TestMembers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	for range 2 {
		if err := e.svc.AddMember(ctx, e.users.Admin, p.ID, e.users.Member.ID); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := e.svc.MemberIDs(ctx, p.ID)
	if err != nil || len(ids) != 2 {
		t.Fatalf("members = %v, %v", ids, err)
	}
	h := e.history(t, p.ID)
	if len(h) != 2 || changeFields(h[1]) != "members" {
		t.Fatalf("history = %+v (want create + one members update)", h)
	}

	if err := e.svc.AddMember(ctx, e.users.Member, p.ID, e.users.Owner.ID); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("member adds: %v", err)
	}
	disabled := testfx.DisabledUser(t, e.pool, e.users.Owner)
	err = e.svc.AddMember(ctx, e.users.Admin, p.ID, disabled.ID)
	wantInput(t, err, "userId", kit.ReasonNotAllowed)
	err = e.svc.AddMember(ctx, e.users.Admin, p.ID, uuid.New())
	wantInput(t, err, "userId", kit.ReasonNotFound)

	if err := e.svc.RemoveMember(ctx, e.users.Admin, p.ID, e.users.Member.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.RemoveMember(ctx, e.users.Admin, p.ID, e.users.Member.ID); err != nil {
		t.Fatalf("remove twice: %v", err)
	}
	ids, err = e.svc.MemberIDs(ctx, p.ID)
	if err != nil || len(ids) != 1 || ids[0] != e.users.Admin.ID {
		t.Fatalf("after remove = %v, %v", ids, err)
	}
	if n := len(e.history(t, p.ID)); n != 3 {
		t.Fatalf("history len = %d, want 3", n)
	}
}

func TestSelfJoinLeave(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	p := e.create(t, "YLK")
	if err := e.svc.AddMember(ctx, e.users.Member, p.ID, e.users.Member.ID); err != nil {
		t.Fatalf("member joins: %v", err)
	}
	if err := e.svc.AddMember(ctx, e.users.Member, p.ID, e.users.Owner.ID); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("member adds someone else: %v", err)
	}
	if err := e.svc.RemoveMember(ctx, e.users.Member, p.ID, e.users.Admin.ID); !errors.Is(err, kit.ErrForbidden) {
		t.Fatalf("member removes someone else: %v", err)
	}
	if err := e.svc.RemoveMember(ctx, e.users.Member, p.ID, e.users.Member.ID); err != nil {
		t.Fatalf("member leaves: %v", err)
	}
	ids, err := e.svc.MemberIDs(ctx, p.ID)
	if err != nil || len(ids) != 1 || ids[0] != e.users.Admin.ID {
		t.Fatalf("members = %v, %v", ids, err)
	}
	h := e.history(t, p.ID)
	if len(h) != 3 || h[1].ActorID == nil || *h[1].ActorID != e.users.Member.ID {
		t.Fatalf("history = %+v (want create, join, leave by the member)", h)
	}
}
