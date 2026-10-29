package account_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askuksov/yalkos/internal/domain/account"
)

var memberSeq atomic.Int64

func tokenFromURL(t *testing.T, url string) string {
	t.Helper()
	const prefix = "http://yalkos.test/invite/"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("invite URL %q lacks prefix %q", url, prefix)
	}
	return strings.TrimPrefix(url, prefix)
}

// addMember invites a user with role through actor and accepts the invite.
func (e env) addMember(t *testing.T, actor account.User, role account.Role) (account.User, string) {
	t.Helper()
	ctx := context.Background()
	inv, err := e.svc.CreateInvite(ctx, actor, role, 7)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	email := fmt.Sprintf("m%d@example.com", memberSeq.Add(1))
	u, tok, err := e.svc.AcceptInvite(ctx, tokenFromURL(t, inv.URL), "Member", email, "member-password")
	if err != nil {
		t.Fatalf("AcceptInvite: %v", err)
	}
	return u, tok
}

func TestInvitePermissions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	admin, _ := e.addMember(t, owner, account.RoleAdmin)
	member, _ := e.addMember(t, owner, account.RoleMember)

	if _, err := e.svc.CreateInvite(ctx, member, account.RoleMember, 7); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("member invites: err = %v, want ErrForbidden", err)
	}
	if _, err := e.svc.CreateInvite(ctx, admin, account.RoleAdmin, 7); !errors.Is(err, account.ErrForbidden) {
		t.Errorf("admin invites admin: err = %v, want ErrForbidden", err)
	}
	if _, err := e.svc.CreateInvite(ctx, admin, account.RoleMember, 7); err != nil {
		t.Errorf("admin invites member: %v", err)
	}
	if _, err := e.svc.CreateInvite(ctx, owner, account.RoleAdmin, 7); err != nil {
		t.Errorf("owner invites admin: %v", err)
	}
	for _, days := range []int{0, 31} {
		_, err := e.svc.CreateInvite(ctx, owner, account.RoleMember, days)
		var ie *account.InputError
		if !errors.As(err, &ie) || ie.Field != "expiresInDays" {
			t.Errorf("days=%d: err = %v, want InputError{expiresInDays}", days, err)
		}
	}
}

func TestInviteURL(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.setupOwner(t)
	inv, err := e.svc.CreateInvite(context.Background(), owner, account.RoleMember, 7)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(tokenFromURL(t, inv.URL))
	if err != nil || len(raw) != 32 {
		t.Errorf("token decodes to %d bytes (err %v), want 32", len(raw), err)
	}
	if got := inv.ExpiresAt.Sub(inv.CreatedAt).Round(time.Hour); got != 7*24*time.Hour {
		t.Errorf("expiry = %v, want 7 days", got)
	}
}

func TestAcceptInvite(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	inv, _ := e.svc.CreateInvite(ctx, owner, account.RoleAdmin, 7)
	tok := tokenFromURL(t, inv.URL)

	u, session, err := e.svc.AcceptInvite(ctx, tok, "Ann", "ann@example.com", "ann-password")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != account.RoleAdmin {
		t.Errorf("role = %s, want admin", u.Role)
	}
	if got, err := e.svc.UserBySession(ctx, session); err != nil || got.ID != u.ID {
		t.Errorf("session of new user: %+v, %v", got, err)
	}
	if _, _, err := e.svc.AcceptInvite(ctx, tok, "Bob", "bob@example.com", "bob-password"); !errors.Is(err, account.ErrInviteInvalid) {
		t.Errorf("second accept: err = %v, want ErrInviteInvalid", err)
	}
	if _, _, err := e.svc.AcceptInvite(ctx, "not-a-token", "Bob", "bob@example.com", "bob-password"); !errors.Is(err, account.ErrInviteInvalid) {
		t.Errorf("unknown token: err = %v, want ErrInviteInvalid", err)
	}
}

func TestAcceptInviteExpired(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	inv, _ := e.svc.CreateInvite(ctx, owner, account.RoleMember, 7)
	e.clock.add(8 * 24 * time.Hour)
	if _, _, err := e.svc.AcceptInvite(ctx, tokenFromURL(t, inv.URL), "Ann", "ann@example.com", "ann-password"); !errors.Is(err, account.ErrInviteInvalid) {
		t.Errorf("err = %v, want ErrInviteInvalid", err)
	}
}

func TestAcceptInviteConcurrent(t *testing.T) {
	e := newEnv(t)
	owner, _ := e.setupOwner(t)
	inv, _ := e.svc.CreateInvite(context.Background(), owner, account.RoleMember, 7)
	tok := tokenFromURL(t, inv.URL)
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Go(func() {
			_, _, errs[i] = e.svc.AcceptInvite(context.Background(), tok, "N", fmt.Sprintf("c%d@example.com", i), "same-password")
		})
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, account.ErrInviteInvalid):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d accepts succeeded, want 1", ok)
	}
}

func TestAcceptInviteDuplicateEmail(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	inv, _ := e.svc.CreateInvite(ctx, owner, account.RoleMember, 7)
	_, _, err := e.svc.AcceptInvite(ctx, tokenFromURL(t, inv.URL), "Dup", strings.ToUpper(ownerEmail), "dup-password")
	var ie *account.InputError
	if !errors.As(err, &ie) || ie.Field != "email" {
		t.Fatalf("err = %v, want InputError{email}", err)
	}
	// The invite stays usable after a validation failure.
	if _, _, err := e.svc.AcceptInvite(ctx, tokenFromURL(t, inv.URL), "Ok", "ok@example.com", "ok-password"); err != nil {
		t.Errorf("retry with a free email: %v", err)
	}
}

func TestRevokeInvite(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, _ := e.setupOwner(t)
	inv, _ := e.svc.CreateInvite(ctx, owner, account.RoleMember, 7)
	if err := e.svc.RevokeInvite(ctx, owner, inv.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.AcceptInvite(ctx, tokenFromURL(t, inv.URL), "A", "a@example.com", "a-password"); !errors.Is(err, account.ErrInviteInvalid) {
		t.Errorf("err = %v, want ErrInviteInvalid", err)
	}
	list, err := e.svc.ListInvites(ctx, owner)
	if err != nil || len(list) != 0 {
		t.Errorf("ListInvites = %v, %v; want empty", list, err)
	}
}
