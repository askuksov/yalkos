package account_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/account"
)

// Parallel wrong logins must not get more than 10 verifications per (email, IP).
func TestLoginLimiterParallel(t *testing.T) {
	e := newEnv(t)
	e.setupOwner(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	verified := 0
	for range 40 {
		wg.Go(func() {
			_, _, err := e.svc.Login(context.Background(), ownerEmail, "wrong-password", ip)
			if errors.Is(err, account.ErrInvalidCredentials) {
				mu.Lock()
				verified++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if verified > 10 {
		t.Errorf("%d parallel guesses were verified, want ≤ 10", verified)
	}
}

func TestLoginEmailTooLong(t *testing.T) {
	e := newEnv(t)
	e.setupOwner(t)
	_, _, err := e.svc.Login(context.Background(), strings.Repeat("a", 300)+"@x.io", "whatever-pass", ip)
	var ie *account.InputError
	if !errors.As(err, &ie) || ie.Field != "email" {
		t.Errorf("err = %v, want InputError{email}", err)
	}
}

// IPv6 addresses of one /64 share a budget.
func TestLoginLimiterIPv6Subnet(t *testing.T) {
	e := newEnv(t)
	e.setupOwner(t)
	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		_, _, _ = e.svc.Login(ctx, ownerEmail, "wrong-password", fmt.Sprintf("2001:db8::%x", i))
	}
	if _, _, err := e.svc.Login(ctx, ownerEmail, ownerPass, "2001:db8::ffff"); !errors.Is(err, account.ErrRateLimited) {
		t.Errorf("same /64: err = %v, want ErrRateLimited", err)
	}
	if _, _, err := e.svc.Login(ctx, ownerEmail, ownerPass, "2001:db8:0:1::1"); err != nil {
		t.Errorf("other /64: err = %v", err)
	}
}

// Many IPs together get at most 100 failures per email per hour.
func TestLoginPerEmailCap(t *testing.T) {
	e := newEnv(t)
	e.setupOwner(t)
	ctx := context.Background()
	for i := range 100 {
		_, _, _ = e.svc.Login(ctx, ownerEmail, "wrong-password", fmt.Sprintf("198.51.%d.%d", i/250, i%250+1))
	}
	if _, _, err := e.svc.Login(ctx, ownerEmail, ownerPass, "203.0.113.200"); !errors.Is(err, account.ErrRateLimited) {
		t.Errorf("101st failure from a new IP: err = %v, want ErrRateLimited", err)
	}
}

func TestChangePasswordRateLimited(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, keep := e.setupOwner(t)
	for range 10 {
		_ = e.svc.ChangePassword(ctx, owner.ID, "wrong-password", "new-password-1", keep)
	}
	if err := e.svc.ChangePassword(ctx, owner.ID, ownerPass, "new-password-1", keep); !errors.Is(err, account.ErrRateLimited) {
		t.Errorf("11th attempt: err = %v, want ErrRateLimited", err)
	}
}

// Setup-code guesses are capped across all clients (IP rotation).
func TestSetupCodeGlobalCap(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for i := range 100 {
		_, _, _ = e.svc.Setup(ctx, setupInput("WRONGCOD"), fmt.Sprintf("198.51.%d.%d", i/250, i%250+1))
	}
	if _, _, err := e.svc.Setup(ctx, setupInput(e.svc.SetupCode()), "203.0.113.250"); !errors.Is(err, account.ErrRateLimited) {
		t.Errorf("after 100 failures from many IPs: err = %v, want ErrRateLimited", err)
	}
}
