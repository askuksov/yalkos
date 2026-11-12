package native_test

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAttachmentsAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	member := userBrowser(t, s, owner, "MEMBER")
	p := createProject(t, owner, "YLK", "Yalkos")
	i := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "t"})
	ctx := context.Background()
	users, err := s.svc.UsersByIDs(ctx, []uuid.UUID{uuid.MustParse(viewerID(t, owner))})
	if err != nil {
		t.Fatal(err)
	}
	iid := uuid.MustParse(i.ID)
	a, err := s.deps.Attachments.Upload(ctx, users[0], "shot.png", strings.NewReader("\x89PNG\r\n\x1a\nxxxx"), &iid)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Attachments []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Size        int    `json:"size"`
			ContentType string `json:"contentType"`
			URL         string `json:"url"`
			Inline      bool   `json:"inline"`
			Uploader    struct {
				ID string `json:"id"`
			} `json:"uploader"`
		} `json:"attachments"`
	}
	owner.do(`query($i: String!) { issue(id: $i) { attachments { id name size contentType url inline uploader { id } } } }`,
		map[string]any{"i": i.ID}).decode(t, "issue", &got)
	if len(got.Attachments) != 1 {
		t.Fatalf("attachments = %+v", got)
	}
	at := got.Attachments[0]
	if at.ID != a.ID.String() || at.URL != "/api/attachments/"+a.ID.String()+"/shot.png" || !at.Inline || at.ContentType != "image/png" ||
		at.Size != 12 || at.Uploader.ID != users[0].ID.String() {
		t.Errorf("attachment = %+v", at)
	}
	if r := member.do(`mutation($id: ID!) { deleteAttachment(id: $id) }`, map[string]any{"id": at.ID}); r.code() != "FORBIDDEN" {
		t.Errorf("member delete: %s", r.raw)
	}
	if r := owner.do(`mutation($id: ID!) { deleteAttachment(id: $id) }`, map[string]any{"id": at.ID}); len(r.Errors) > 0 {
		t.Errorf("owner delete: %s", r.raw)
	}
	// A file not linked yet is its uploader's alone.
	mu, err := s.svc.UsersByIDs(ctx, []uuid.UUID{uuid.MustParse(viewerID(t, member))})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.deps.Attachments.Upload(ctx, mu[0], "draft.png", strings.NewReader("\x89PNG\r\n\x1a\nyyyy"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := owner.do(`mutation($id: ID!) { deleteAttachment(id: $id) }`, map[string]any{"id": pending.ID}); r.code() != "NOT_FOUND" {
		t.Errorf("owner delete of a pending upload: %s", r.raw)
	}
}

func TestLimitsAPI(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	var l struct {
		MaxUploadMb int `json:"maxUploadMb"`
	}
	owner.do(`{ limits { maxUploadMb } }`, nil).decode(t, "limits", &l)
	if l.MaxUploadMb != 25 {
		t.Errorf("maxUploadMb = %d", l.MaxUploadMb)
	}
	if r := s.browser(t).do(`{ limits { maxUploadMb } }`, nil); r.code() != "AUTHENTICATION_ERROR" {
		t.Errorf("anonymous: %s", r.raw)
	}
}

func TestAttachmentUsedInTextAPI(t *testing.T) {
	s, queries := tracedServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")
	i := createIssue(t, owner, map[string]any{"projectId": p.ID, "title": "t"})
	ctx := context.Background()
	users, err := s.svc.UsersByIDs(ctx, []uuid.UUID{uuid.MustParse(viewerID(t, owner))})
	if err != nil {
		t.Fatal(err)
	}
	iid := uuid.MustParse(i.ID)
	upload := func(name string) string {
		a, err := s.deps.Attachments.Upload(ctx, users[0], name, strings.NewReader("\x89PNG\r\n\x1a\nxxxx"), &iid)
		if err != nil {
			t.Fatal(err)
		}
		return a.ID.String()
	}
	inComment, inDescription, unused := upload("c.png"), upload("d.png"), upload("n.png")
	owner.do(`mutation($id: ID!, $d: String!) { updateIssue(id: $id, input: { description: $d }) { id } }`,
		map[string]any{"id": i.ID, "d": "see ![d](/api/attachments/" + inDescription + "/d.png)"}).decode(t, "updateIssue", &struct{}{})
	// The only reference sits in the oldest of 60 comments: absolute, upper case.
	for n := range 60 {
		body := fmt.Sprintf("comment %d", n)
		if n == 0 {
			body = "![c](https://app.example.com/api/attachments/" + strings.ToUpper(inComment) + "/c.png)"
		}
		owner.do(`mutation($i: ID!, $b: String!) { createComment(issueId: $i, body: $b) { id } }`,
			map[string]any{"i": i.ID, "b": body}).decode(t, "createComment", &struct{}{})
	}

	used := func() map[string]bool {
		var got struct {
			Attachments []struct {
				ID         string `json:"id"`
				UsedInText bool   `json:"usedInText"`
			} `json:"attachments"`
		}
		owner.do(`query($i: String!) { issue(id: $i) { attachments { id usedInText } } }`,
			map[string]any{"i": i.ID}).decode(t, "issue", &got)
		out := map[string]bool{}
		for _, a := range got.Attachments {
			out[a.ID] = a.UsedInText
		}
		return out
	}
	want := map[string]bool{inComment: true, inDescription: true, unused: false}
	if got := used(); !maps.Equal(got, want) {
		t.Errorf("usedInText = %v, want %v", got, want)
	}

	// Fewest statements of three runs (see countList).
	count := func() int64 {
		var n int64
		for run := range 3 {
			queries.n.Store(0)
			used()
			if c := queries.n.Load(); run == 0 || c < n {
				n = c
			}
		}
		return n
	}
	few := count()
	for n := range 10 {
		upload(fmt.Sprintf("more-%d.png", n))
	}
	many := count()
	t.Logf("usedInText: %d SQL statements for 3 attachments, %d for 13", few, many)
	if many > few {
		t.Errorf("usedInText of 13 attachments ran %d SQL statements, of 3: %d", many, few)
	}
}
