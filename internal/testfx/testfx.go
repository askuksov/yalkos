// Package testfx builds a migrated test database with a set-up workspace and
// three users (Owner, Admin, Member) for domain tests.
package testfx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

const baseURL = "http://yalkos.test"

// Users are the workspace users created by New.
type Users struct {
	Owner, Admin, Member account.User
}

var seq atomic.Int64

// New returns a pool to a fresh migrated database with a workspace, its Owner,
// an Admin and a Member, plus a clock that starts at 2026-01-01 00:00 UTC and
// advances one second per call.
func New(t *testing.T) (*pgxpool.Pool, Users, func() time.Time) {
	t.Helper()
	ctx := context.Background()
	_, pool := testdb.New(t)
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatalf("testfx: migrate: %v", err)
	}
	svc := account.New(pool, time.Now, baseURL)
	if err := svc.RefreshSetupCode(ctx); err != nil {
		t.Fatalf("testfx: setup code: %v", err)
	}
	owner, _, err := svc.Setup(ctx, account.SetupInput{
		Code: svc.SetupCode(), WorkspaceName: "Acme", Name: "Owner",
		Email: "owner@example.com", Password: "owner-password",
	}, "203.0.113.7")
	if err != nil {
		t.Fatalf("testfx: setup: %v", err)
	}
	u := Users{Owner: owner}
	u.Admin = AddUser(t, pool, owner, account.RoleAdmin)
	u.Member = AddUser(t, pool, owner, account.RoleMember)
	return pool, u, Clock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
}

// Clock returns a time source that yields start, start+1s, start+2s, ...
func Clock(start time.Time) func() time.Time {
	var mu sync.Mutex
	next := start
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := next
		next = next.Add(time.Second)
		return t
	}
}

// AddUser invites a user with role through owner and accepts the invite.
func AddUser(t *testing.T, pool *pgxpool.Pool, owner account.User, role account.Role) account.User {
	t.Helper()
	ctx := context.Background()
	svc := account.New(pool, time.Now, baseURL)
	inv, err := svc.CreateInvite(ctx, owner, role, 7)
	if err != nil {
		t.Fatalf("testfx: invite: %v", err)
	}
	n := seq.Add(1)
	user, _, err := svc.AcceptInvite(ctx, strings.TrimPrefix(inv.URL, baseURL+"/invite/"),
		fmt.Sprintf("User %d", n), fmt.Sprintf("user%d@example.com", n), "user-password")
	if err != nil {
		t.Fatalf("testfx: accept invite: %v", err)
	}
	return user
}

// DisabledUser adds a member and disables it.
func DisabledUser(t *testing.T, pool *pgxpool.Pool, owner account.User) account.User {
	t.Helper()
	u := AddUser(t, pool, owner, account.RoleMember)
	if err := account.New(pool, time.Now, baseURL).SetMemberDisabled(context.Background(), owner, u.ID, true); err != nil {
		t.Fatalf("testfx: disable: %v", err)
	}
	u.Disabled = true
	return u
}
