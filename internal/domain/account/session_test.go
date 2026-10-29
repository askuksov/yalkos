package account_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

const (
	ownerEmail = "owner@example.com"
	ownerPass  = "owner-password"
	ip         = "203.0.113.7"
)

// clock is a settable time source shared with the service.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	svc   *account.Service
	clock *clock
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	_, pool := testdb.New(t)
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Now()}
	svc := account.New(pool, c.now, "http://yalkos.test")
	if err := svc.RefreshSetupCode(ctx); err != nil {
		t.Fatal(err)
	}
	return env{svc: svc, clock: c}
}

func setupInput(code string) account.SetupInput {
	return account.SetupInput{Code: code, WorkspaceName: "Acme", Name: "Olga", Email: ownerEmail, Password: ownerPass}
}

// setupOwner claims the instance and returns the owner and its session token.
func (e env) setupOwner(t *testing.T) (account.User, string) {
	t.Helper()
	u, tok, err := e.svc.Setup(context.Background(), setupInput(e.svc.SetupCode()), ip)
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	return u, tok
}

func TestSetupRequiresCode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, _, err := e.svc.Setup(ctx, setupInput("WRONGCOD"), ip); !errors.Is(err, account.ErrInvalidSetupCode) {
		t.Fatalf("err = %v, want ErrInvalidSetupCode", err)
	}
	if needed, _ := e.svc.SetupNeeded(ctx); !needed {
		t.Error("workspace created despite wrong code")
	}
}

func TestSetupCreatesOwnerAndSession(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	u, tok := e.setupOwner(t)
	if u.Role != account.RoleOwner || u.Email != ownerEmail || u.Name != "Olga" {
		t.Errorf("user = %+v", u)
	}
	got, err := e.svc.UserBySession(ctx, tok)
	if err != nil || got.ID != u.ID {
		t.Fatalf("UserBySession = %+v, %v", got, err)
	}
	if needed, _ := e.svc.SetupNeeded(ctx); needed {
		t.Error("SetupNeeded still true")
	}
}

func TestSetupTwice(t *testing.T) {
	e := newEnv(t)
	code := e.svc.SetupCode()
	e.setupOwner(t)
	in := setupInput(code)
	in.Email = "second@example.com"
	if _, _, err := e.svc.Setup(context.Background(), in, ip); !errors.Is(err, account.ErrAlreadySetUp) {
		t.Fatalf("err = %v, want ErrAlreadySetUp", err)
	}
}

func TestSetupConcurrent(t *testing.T) {
	e := newEnv(t)
	code := e.svc.SetupCode()
	var wg sync.WaitGroup
	errs := make([]error, 5)
	for i := range errs {
		wg.Go(func() {
			in := setupInput(code)
			in.Email = []string{"a@x.io", "b@x.io", "c@x.io", "d@x.io", "e@x.io"}[i]
			_, _, errs[i] = e.svc.Setup(context.Background(), in, ip)
		})
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, account.ErrAlreadySetUp):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d setups succeeded, want 1", ok)
	}
}

func TestLoginOK(t *testing.T) {
	e := newEnv(t)
	owner, first := e.setupOwner(t)
	u, tok, err := e.svc.Login(context.Background(), "OWNER@Example.com", ownerPass, ip)
	if err != nil || u.ID != owner.ID {
		t.Fatalf("Login = %+v, %v", u, err)
	}
	if tok == "" || tok == first {
		t.Error("Login did not issue a new session token")
	}
}

func TestLoginWrong(t *testing.T) {
	e := newEnv(t)
	e.setupOwner(t)
	ctx := context.Background()
	if _, _, err := e.svc.Login(ctx, ownerEmail, "wrong-password", ip); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Errorf("wrong password: err = %v", err)
	}
	if _, _, err := e.svc.Login(ctx, "nobody@example.com", ownerPass, ip); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Errorf("unknown email: err = %v", err)
	}
}

func TestLoginRateLimited(t *testing.T) {
	e := newEnv(t)
	e.setupOwner(t)
	ctx := context.Background()
	for range 10 {
		_, _, _ = e.svc.Login(ctx, ownerEmail, "wrong-password", ip)
	}
	if _, _, err := e.svc.Login(ctx, ownerEmail, ownerPass, ip); !errors.Is(err, account.ErrRateLimited) {
		t.Errorf("11th attempt: err = %v, want ErrRateLimited", err)
	}
	if _, _, err := e.svc.Login(ctx, ownerEmail, ownerPass, "198.51.100.1"); err != nil {
		t.Errorf("other IP: err = %v, want nil", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	e := newEnv(t)
	_, tok := e.setupOwner(t)
	e.clock.add(30*24*time.Hour + time.Second)
	if _, err := e.svc.UserBySession(context.Background(), tok); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestLogout(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	_, tok := e.setupOwner(t)
	if err := e.svc.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UserBySession(ctx, tok); !errors.Is(err, account.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestChangePassword(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, keep := e.setupOwner(t)
	_, other, _ := e.svc.Login(ctx, ownerEmail, ownerPass, ip)

	if err := e.svc.ChangePassword(ctx, owner.ID, "wrong-password", "new-password-1", keep); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Fatalf("wrong current: err = %v", err)
	}
	if err := e.svc.ChangePassword(ctx, owner.ID, ownerPass, "new-password-1", keep); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.Login(ctx, ownerEmail, ownerPass, ip); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Errorf("old password still works: %v", err)
	}
	if _, _, err := e.svc.Login(ctx, ownerEmail, "new-password-1", ip); err != nil {
		t.Errorf("new password: %v", err)
	}
	if _, err := e.svc.UserBySession(ctx, other); !errors.Is(err, account.ErrUnauthenticated) {
		t.Error("other session survived password change")
	}
	if _, err := e.svc.UserBySession(ctx, keep); err != nil {
		t.Errorf("kept session lost: %v", err)
	}
}

func TestSetupValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cases := map[string]func(*account.SetupInput){
		"email":         func(in *account.SetupInput) { in.Email = "nope" },
		"password":      func(in *account.SetupInput) { in.Password = "short" },
		"name":          func(in *account.SetupInput) { in.Name = "  " },
		"workspaceName": func(in *account.SetupInput) { in.WorkspaceName = "" },
	}
	for field, mod := range cases {
		in := setupInput(e.svc.SetupCode())
		mod(&in)
		_, _, err := e.svc.Setup(ctx, in, ip)
		var ie *account.InputError
		if !errors.As(err, &ie) || ie.Field != field {
			t.Errorf("%s: err = %v, want InputError{Field:%q}", field, err, field)
		}
	}
}
