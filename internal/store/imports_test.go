package store_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/store/q"
	"github.com/askuksov/yalkos/internal/testfx"
)

func utc(y int, m time.Month, d, h, mi int) time.Time {
	return time.Date(y, m, d, h, mi, 0, 0, time.UTC)
}

func TestImportCreateIssueKeepsTimes(t *testing.T) {
	ctx := context.Background()
	pool, users, _ := testfx.New(t)
	qs := q.New(pool)

	projectAt := utc(2025, 1, 2, 3, 4)
	p, err := qs.ImportCreateProject(ctx, q.ImportCreateProjectParams{
		Key: "ALP", Name: "Alpha", CreatedBy: &users.Owner.ID, CreatedAt: projectAt, UpdatedAt: projectAt,
	})
	if err != nil {
		t.Fatalf("ImportCreateProject: %v", err)
	}
	if !p.CreatedAt.Equal(projectAt) || !p.UpdatedAt.Equal(projectAt) {
		t.Errorf("project times = %v / %v, want %v", p.CreatedAt, p.UpdatedAt, projectAt)
	}
	st, err := qs.ImportCreateState(ctx, q.ImportCreateStateParams{
		ProjectID: p.ID, Name: "Todo", Type: "unstarted", Color: "#95a2b3", Position: 0, IsDefault: true,
		CreatedAt: projectAt, UpdatedAt: projectAt,
	})
	if err != nil {
		t.Fatalf("ImportCreateState: %v", err)
	}

	createdAt, updatedAt := utc(2025, 3, 1, 10, 0), utc(2025, 3, 5, 11, 0)
	issue, err := qs.ImportCreateIssue(ctx, q.ImportCreateIssueParams{
		ProjectID: p.ID, Number: 62, Title: "Imported", StateID: st.ID, CreatorID: users.Owner.ID,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	})
	if err != nil {
		t.Fatalf("ImportCreateIssue: %v", err)
	}
	if issue.Number != 62 {
		t.Errorf("number = %d, want 62", issue.Number)
	}
	if !issue.CreatedAt.Equal(createdAt) || !issue.UpdatedAt.Equal(updatedAt) {
		t.Errorf("times = %v / %v, want %v / %v", issue.CreatedAt, issue.UpdatedAt, createdAt, updatedAt)
	}
}

func TestRaiseSequenceCounterNeverLowers(t *testing.T) {
	ctx := context.Background()
	pool, _, _ := testfx.New(t)
	qs := q.New(pool)

	at := utc(2025, 1, 2, 3, 4)
	p, err := qs.ImportCreateProject(ctx, q.ImportCreateProjectParams{Key: "BET", Name: "Beta", CreatedAt: at, UpdatedAt: at})
	if err != nil {
		t.Fatalf("ImportCreateProject: %v", err)
	}
	counter := func() int32 {
		t.Helper()
		got, err := qs.GetProject(ctx, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.SequenceCounter
	}
	if got := counter(); got != 0 {
		t.Fatalf("initial counter = %d, want 0", got)
	}
	if err := qs.RaiseSequenceCounter(ctx, q.RaiseSequenceCounterParams{ID: p.ID, Number: 145}); err != nil {
		t.Fatal(err)
	}
	if got := counter(); got != 145 {
		t.Errorf("after raise 145 = %d, want 145", got)
	}
	if err := qs.RaiseSequenceCounter(ctx, q.RaiseSequenceCounterParams{ID: p.ID, Number: 10}); err != nil {
		t.Fatal(err)
	}
	if got := counter(); got != 145 {
		t.Errorf("after raise 10 = %d, want 145", got)
	}
	next, err := qs.NextIssueNumber(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next != 146 {
		t.Errorf("next number = %d, want 146", next)
	}
}

func TestImportMapUnique(t *testing.T) {
	ctx := context.Background()
	pool, _, _ := testfx.New(t)
	qs := q.New(pool)

	now := utc(2025, 6, 1, 12, 0)
	src, err := qs.UpsertImportSource(ctx, q.UpsertImportSourceParams{
		Kind: "plane", SourceID: "ws-1", Url: "https://plane.example.invalid", Workspace: "acme", Now: now,
	})
	if err != nil {
		t.Fatalf("UpsertImportSource: %v", err)
	}
	p := q.InsertImportMapParams{SourceID: src.ID, Kind: "project", PlaneID: "p-1", TargetID: src.ID, Now: now}
	if err := qs.InsertImportMap(ctx, p); err != nil {
		t.Fatalf("first InsertImportMap: %v", err)
	}
	err = qs.InsertImportMap(ctx, p)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second InsertImportMap error = %v, want code 23505", err)
	}
}

func TestImportUserCannotLogIn(t *testing.T) {
	ctx := context.Background()
	pool, _, _ := testfx.New(t)
	qs := q.New(pool)

	ws, err := qs.GetWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword(ctx, base64.StdEncoding.EncodeToString(secret))
	if err != nil {
		t.Fatal(err)
	}
	u, err := qs.ImportCreateUser(ctx, q.ImportCreateUserParams{
		WorkspaceID: ws.ID, Email: "ann@example.invalid", Name: "Ann", PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("ImportCreateUser: %v", err)
	}
	if !u.Disabled || u.Role != "member" {
		t.Errorf("user = disabled %v, role %q; want true, member", u.Disabled, u.Role)
	}
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") {
		t.Errorf("stored hash = %q, want an argon2id hash", u.PasswordHash)
	}
	ok, err := auth.VerifyPassword(ctx, "", u.PasswordHash)
	if err != nil || ok {
		t.Errorf("VerifyPassword with empty password = %v, %v; want false, nil", ok, err)
	}
}

func TestIssueNumberTaken(t *testing.T) {
	ctx := context.Background()
	pool, users, _ := testfx.New(t)
	qs := q.New(pool)

	at := utc(2025, 1, 2, 3, 4)
	p, err := qs.ImportCreateProject(ctx, q.ImportCreateProjectParams{Key: "ALP", Name: "Alpha", CreatedAt: at, UpdatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	st, err := qs.ImportCreateState(ctx, q.ImportCreateStateParams{
		ProjectID: p.ID, Name: "Todo", Type: "unstarted", Color: "#95a2b3", IsDefault: true, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	issue, err := qs.ImportCreateIssue(ctx, q.ImportCreateIssueParams{
		ProjectID: p.ID, Number: 62, Title: "Imported", StateID: st.ID, CreatorID: users.Owner.ID, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := qs.InsertIssueNumber(ctx, q.InsertIssueNumberParams{ProjectID: p.ID, Number: 62, IssueID: issue.ID}); err != nil {
		t.Fatal(err)
	}
	for number, want := range map[int32]bool{62: true, 63: false} {
		got, err := qs.IssueNumberTaken(ctx, q.IssueNumberTakenParams{ProjectID: p.ID, Number: number})
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("IssueNumberTaken(%d) = %v, want %v", number, got, want)
		}
	}
}

func TestImportCreateLabelGroup(t *testing.T) {
	ctx := context.Background()
	pool, users, _ := testfx.New(t)
	qs := q.New(pool)

	at := utc(2025, 2, 3, 4, 5)
	group, err := qs.ImportCreateLabel(ctx, q.ImportCreateLabelParams{
		IsGroup: true, Name: "Area", Color: "#6e7781", Position: 1000, CreatedBy: &users.Owner.ID, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatalf("group: %v", err)
	}
	child, err := qs.ImportCreateLabel(ctx, q.ImportCreateLabelParams{
		ParentID: &group.ID, Name: "Backend", Color: "#30a46c", Position: 1000, CreatedAt: at, UpdatedAt: at,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	if !group.IsGroup || group.Exclusive || group.ParentID != nil || !group.CreatedAt.Equal(at) {
		t.Errorf("group = isGroup %v, exclusive %v, parent %v, created %v", group.IsGroup, group.Exclusive, group.ParentID, group.CreatedAt)
	}
	if child.IsGroup || child.Exclusive || child.ParentID == nil || *child.ParentID != group.ID {
		t.Errorf("child = isGroup %v, exclusive %v, parent %v; want a plain label under the group", child.IsGroup, child.Exclusive, child.ParentID)
	}
}
