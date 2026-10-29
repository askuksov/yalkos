package account_test

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/account"
)

func TestCreateAndUseAPIKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)

	key, plain, err := e.svc.CreateAPIKey(ctx, owner, "CI")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^yal_api_[0-9A-Za-z]{40}$`).MatchString(plain) {
		t.Errorf("plain key %q has wrong format", plain)
	}
	if key.DisplayPrefix != plain[:12]+"…" || key.Name != "CI" {
		t.Errorf("key = %+v", key)
	}
	u, err := e.svc.UserByAPIKey(ctx, plain)
	if err != nil || u.ID != owner.ID {
		t.Fatalf("UserByAPIKey = %+v, %v", u, err)
	}
	list, err := e.svc.ListAPIKeys(ctx, owner)
	if err != nil || len(list) != 1 || list[0].ID != key.ID {
		t.Fatalf("ListAPIKeys = %+v, %v", list, err)
	}
	var ie *account.InputError
	if _, _, err := e.svc.CreateAPIKey(ctx, owner, " "); !errors.As(err, &ie) || ie.Field != "name" {
		t.Errorf("empty name: err = %v", err)
	}
}

func TestRevokedKeyRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	key, plain, _ := e.svc.CreateAPIKey(ctx, owner, "temp")
	if err := e.svc.RevokeAPIKey(ctx, owner, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UserByAPIKey(ctx, plain); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
	if _, err := e.svc.UserByAPIKey(ctx, "yal_api_doesnotexist"); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("unknown key: err = %v", err)
	}
	if list, _ := e.svc.ListAPIKeys(ctx, owner); len(list) != 0 {
		t.Errorf("revoked key still listed: %+v", list)
	}
}

func TestKeyOfDisabledUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	member, _ := e.addMember(t, owner, account.RoleMember)
	_, plain, _ := e.svc.CreateAPIKey(ctx, member, "bot")
	if err := e.svc.SetMemberDisabled(ctx, owner, member.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UserByAPIKey(ctx, plain); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestRevokeOthersKey(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	a, _ := e.addMember(t, owner, account.RoleMember)
	b, _ := e.addMember(t, owner, account.RoleMember)
	key, _, _ := e.svc.CreateAPIKey(ctx, a, "a-key")
	if err := e.svc.RevokeAPIKey(ctx, b, key.ID); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("member revokes other's key: err = %v, want ErrForbidden", err)
	}
	if err := e.svc.RevokeAPIKey(ctx, owner, key.ID); err != nil {
		t.Errorf("owner revokes: %v", err)
	}
}

func TestLastUsedThrottled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	_, plain, _ := e.svc.CreateAPIKey(ctx, owner, "k")
	if _, err := e.svc.UserByAPIKey(ctx, plain); err != nil {
		t.Fatal(err)
	}
	first, _ := e.svc.ListAPIKeys(ctx, owner)
	if first[0].LastUsedAt == nil {
		t.Fatal("last_used_at not set after first use")
	}
	if _, err := e.svc.UserByAPIKey(ctx, plain); err != nil {
		t.Fatal(err)
	}
	second, _ := e.svc.ListAPIKeys(ctx, owner)
	if !second[0].LastUsedAt.Equal(*first[0].LastUsedAt) {
		t.Errorf("last_used_at changed within 60s: %v → %v", first[0].LastUsedAt, second[0].LastUsedAt)
	}
}
