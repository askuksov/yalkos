package account_test

import (
	"context"
	"errors"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/account"
)

func TestRoleChangeRules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	admin, _ := e.addMember(t, owner, account.RoleAdmin)
	member, _ := e.addMember(t, owner, account.RoleMember)

	if err := e.svc.UpdateMemberRole(ctx, owner, member.ID, account.RoleAdmin); err != nil {
		t.Errorf("owner promotes member: %v", err)
	}
	if err := e.svc.UpdateMemberRole(ctx, admin, member.ID, account.RoleMember); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("admin changes role: err = %v, want ErrForbidden", err)
	}
	if err := e.svc.UpdateMemberRole(ctx, owner, owner.ID, account.RoleAdmin); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("owner changes own role: err = %v, want ErrForbidden", err)
	}
	var ie *account.InputError
	if err := e.svc.UpdateMemberRole(ctx, owner, member.ID, account.RoleOwner); !errors.As(err, &ie) {
		t.Errorf("target role owner: err = %v, want InputError", err)
	}
	members, err := e.svc.ListMembers(ctx, member)
	if err != nil || len(members) != 3 {
		t.Errorf("ListMembers = %d, %v; want 3", len(members), err)
	}
}

func TestDisabledUserLosesSessionsAndKeys(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	member, session := e.addMember(t, owner, account.RoleMember)

	if err := e.svc.SetMemberDisabled(ctx, owner, member.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UserBySession(ctx, session); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("session after disable: err = %v", err)
	}
	if _, _, err := e.svc.Login(ctx, member.Email, "member-password", ip); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Errorf("login after disable: err = %v", err)
	}
	if err := e.svc.SetMemberDisabled(ctx, owner, member.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.Login(ctx, member.Email, "member-password", ip); err != nil {
		t.Errorf("login after enable: %v", err)
	}
}

func TestCannotDisableOwnerOrSelf(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	admin, _ := e.addMember(t, owner, account.RoleAdmin)
	member, _ := e.addMember(t, owner, account.RoleMember)

	if err := e.svc.SetMemberDisabled(ctx, admin, owner.ID, true); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("admin disables owner: err = %v", err)
	}
	if err := e.svc.SetMemberDisabled(ctx, admin, admin.ID, true); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("admin disables self: err = %v", err)
	}
	if err := e.svc.SetMemberDisabled(ctx, member, admin.ID, true); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("member disables admin: err = %v", err)
	}
}
