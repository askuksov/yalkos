package gqlerr_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/graphql/gqlerr"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

func TestFromNotFound(t *testing.T) {
	e := gqlerr.From(context.Background(), kit.NotFoundError{Entity: "Issue"})
	if e.Extensions["code"] != "NOT_FOUND" || e.Message != "Issue not found" {
		t.Fatalf("got %q %v", e.Message, e.Extensions)
	}
	if e.Extensions["type"] != "not found" || e.Extensions["userError"] != true {
		t.Fatalf("extensions = %v", e.Extensions)
	}
	if !errors.Is(kit.NotFoundError{Entity: "Issue"}, kit.ErrNotFound) {
		t.Fatal("NotFoundError is not ErrNotFound")
	}
}

func TestAccountAliases(t *testing.T) {
	if !errors.Is(account.ErrForbidden, kit.ErrForbidden) {
		t.Fatal("account.ErrForbidden is not kit.ErrForbidden")
	}
	var err error = &account.InputError{Field: "email", Reason: kit.ReasonInvalidFormat, Msg: "bad"}
	e := gqlerr.From(context.Background(), err)
	if e.Extensions["code"] != "INVALID_INPUT" || e.Extensions["field"] != "email" || e.Extensions["reason"] != "invalid_format" {
		t.Fatalf("extensions = %v", e.Extensions)
	}
}

func TestInputErrorExtensions(t *testing.T) {
	e := gqlerr.From(context.Background(), kit.Input("title", "too_long", "must be at most 255 characters").With("max", 255))
	if e.Extensions["field"] != "title" || e.Extensions["reason"] != "too_long" || e.Message != "must be at most 255 characters" {
		t.Fatalf("got %q %v", e.Message, e.Extensions)
	}
	if !reflect.DeepEqual(e.Extensions["params"], map[string]any{"max": 255}) {
		t.Fatalf("params = %#v", e.Extensions["params"])
	}
	e = gqlerr.From(context.Background(), kit.Input("title", kit.ReasonRequired, "required"))
	if _, ok := e.Extensions["params"]; ok {
		t.Fatalf("params present without params: %v", e.Extensions)
	}
}

func TestAccountReasons(t *testing.T) {
	ctx := context.Background()
	_, pool := testdb.New(t)
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	svc := account.New(pool, time.Now, "http://yalkos.test")
	if err := svc.RefreshSetupCode(ctx); err != nil {
		t.Fatal(err)
	}
	in := account.SetupInput{Code: svc.SetupCode(), WorkspaceName: "Acme", Name: "Olga", Email: "nope", Password: "owner-password"}

	check := func(err error, field, reason string, params map[string]any) {
		t.Helper()
		e := gqlerr.From(ctx, err)
		if e.Extensions["code"] != "INVALID_INPUT" || e.Extensions["field"] != field || e.Extensions["reason"] != reason {
			t.Fatalf("err %v → %v, want field %s reason %s", err, e.Extensions, field, reason)
		}
		if params != nil && !reflect.DeepEqual(e.Extensions["params"], params) {
			t.Fatalf("params = %#v, want %#v", e.Extensions["params"], params)
		}
	}

	_, _, err := svc.Setup(ctx, in, "203.0.113.7")
	check(err, "email", "invalid_format", nil)

	in.Email, in.Password = "owner@example.com", "short"
	_, _, err = svc.Setup(ctx, in, "203.0.113.7")
	check(err, "password", "too_short", map[string]any{"min": 8})

	in.Password, in.Code = "owner-password", "WRONGCOD"
	_, _, err = svc.Setup(ctx, in, "203.0.113.7")
	check(err, "code", "mismatch", nil)
}
