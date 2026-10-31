package native_test

import (
	"context"
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
