package native_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type gqlWebhook struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	URL            string   `json:"url"`
	Secret         string   `json:"secret"`
	Resources      []string `json:"resources"`
	Enabled        bool     `json:"enabled"`
	DisabledReason *string  `json:"disabledReason"`
	DisabledAt     *string  `json:"disabledAt"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
	Project        *struct {
		Key string `json:"key"`
	} `json:"project"`
	CreatedBy *struct {
		ID string `json:"id"`
	} `json:"createdBy"`
}

type gqlDelivery struct {
	ID             string  `json:"id"`
	Resource       string  `json:"resource"`
	Action         string  `json:"action"`
	EntityID       *string `json:"entityId"`
	Status         string  `json:"status"`
	Attempts       int     `json:"attempts"`
	ResponseStatus *int    `json:"responseStatus"`
	Error          *string `json:"error"`
	DurationMs     *int    `json:"durationMs"`
	CreatedAt      string  `json:"createdAt"`
	LastAttemptAt  *string `json:"lastAttemptAt"`
	NextAttemptAt  *string `json:"nextAttemptAt"`
}

const (
	webhookFields  = `id name url secret resources enabled disabledReason disabledAt createdAt updatedAt project { key } createdBy { id }`
	deliveryFields = `id resource action entityId status attempts responseStatus error durationMs createdAt lastAttemptAt nextAttemptAt`
)

func createWebhook(t *testing.T, c *client, in map[string]any) gqlWebhook {
	t.Helper()
	var w gqlWebhook
	c.do(`mutation($in: CreateWebhookInput!) { createWebhook(input: $in) { `+webhookFields+` } }`,
		map[string]any{"in": in}).decode(t, "createWebhook", &w)
	return w
}

func getWebhook(t *testing.T, c *client, id string) gqlWebhook {
	t.Helper()
	var w gqlWebhook
	c.do(`query($id: ID!) { webhook(id: $id) { `+webhookFields+` } }`, map[string]any{"id": id}).decode(t, "webhook", &w)
	return w
}

// receiver is a loopback HTTP endpoint that answers with status.
func receiver(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(srv.Close)
	return srv
}

func TestNativeWebhookCRUD(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	p := createProject(t, owner, "YLK", "Yalkos")

	// Created without a secret: a generated one comes back.
	w := createWebhook(t, owner, map[string]any{"url": "https://example.com/hook", "resources": []string{"ISSUE", "COMMENT"}})
	if !strings.HasPrefix(w.Secret, "yal_wh_") || len(w.Secret) != len("yal_wh_")+40 {
		t.Errorf("generated secret = %q", w.Secret)
	}
	if !w.Enabled || w.Project != nil || w.Name != "" || w.DisabledReason != nil || w.DisabledAt != nil ||
		strings.Join(w.Resources, ",") != "ISSUE,COMMENT" || w.CreatedBy == nil || w.CreatedBy.ID != viewerID(t, owner) {
		t.Errorf("created = %+v", w)
	}

	// A client secret is used verbatim and scope can name a project.
	scoped := createWebhook(t, owner, map[string]any{
		"url": "https://example.com/scoped", "name": "Scoped", "projectId": p.ID, "resources": []string{"LABEL"},
		"secret": "  my secret  ", "enabled": false,
	})
	if scoped.Secret != "  my secret  " || scoped.Project == nil || scoped.Project.Key != "YLK" || scoped.Enabled || scoped.Name != "Scoped" {
		t.Errorf("scoped = %+v", scoped)
	}

	var list struct {
		Webhooks []gqlWebhook `json:"webhooks"`
	}
	r := owner.do(`{ webhooks { `+webhookFields+` } }`, nil)
	r.decode(t, "webhooks", &list.Webhooks)
	if len(list.Webhooks) != 2 || list.Webhooks[0].ID != scoped.ID || list.Webhooks[1].ID != w.ID {
		t.Fatalf("webhooks (newest first) = %s", r.raw)
	}
	if list.Webhooks[1].Secret != w.Secret {
		t.Errorf("listed secret = %q, want %q", list.Webhooks[1].Secret, w.Secret)
	}

	// Update: absent projectId keeps the scope, null clears it.
	var up gqlWebhook
	owner.do(`mutation($id: ID!, $in: UpdateWebhookInput!) { updateWebhook(id: $id, input: $in) { `+webhookFields+` } }`,
		map[string]any{"id": scoped.ID, "in": map[string]any{"name": "Renamed", "resources": []string{"MILESTONE", "ISSUE"}, "enabled": true}}).
		decode(t, "updateWebhook", &up)
	if up.Name != "Renamed" || up.Project == nil || up.Project.Key != "YLK" || !up.Enabled || strings.Join(up.Resources, ",") != "ISSUE,MILESTONE" ||
		up.Secret != scoped.Secret {
		t.Errorf("updated (project kept) = %+v", up)
	}
	owner.do(`mutation($id: ID!, $in: UpdateWebhookInput!) { updateWebhook(id: $id, input: $in) { `+webhookFields+` } }`,
		map[string]any{"id": scoped.ID, "in": map[string]any{"projectId": nil}}).decode(t, "updateWebhook", &up)
	if up.Project != nil || up.Name != "Renamed" {
		t.Errorf("updated (project cleared) = %+v", up)
	}
	owner.do(`mutation($id: ID!, $in: UpdateWebhookInput!) { updateWebhook(id: $id, input: $in) { `+webhookFields+` } }`,
		map[string]any{"id": scoped.ID, "in": map[string]any{"projectId": p.ID}}).decode(t, "updateWebhook", &up)
	if up.Project == nil || up.Project.Key != "YLK" {
		t.Errorf("updated (project set) = %+v", up)
	}

	// Rotation replaces the secret with a generated one.
	var rot gqlWebhook
	owner.do(`mutation($id: ID!) { rotateWebhookSecret(id: $id) { `+webhookFields+` } }`, map[string]any{"id": w.ID}).decode(t, "rotateWebhookSecret", &rot)
	if rot.Secret == w.Secret || !strings.HasPrefix(rot.Secret, "yal_wh_") {
		t.Errorf("rotated secret = %q (was %q)", rot.Secret, w.Secret)
	}
	if got := getWebhook(t, owner, w.ID); got.Secret != rot.Secret {
		t.Errorf("stored secret = %q, want %q", got.Secret, rot.Secret)
	}

	// Delete, then the webhook is gone.
	var ok bool
	owner.do(`mutation($id: ID!) { deleteWebhook(id: $id) }`, map[string]any{"id": w.ID}).decode(t, "deleteWebhook", &ok)
	if !ok {
		t.Error("deleteWebhook = false")
	}
	gone := owner.do(`query($id: ID!) { webhook(id: $id) { id } }`, map[string]any{"id": w.ID})
	if gone.code() != "NOT_FOUND" || gone.Errors[0].Message != "Webhook not found" {
		t.Errorf("webhook after delete = %s", gone.raw)
	}
}

func TestNativeWebhookAPIKeyAdmin(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	adminKey := s.apiClient(t, userBrowser(t, s, owner, "ADMIN").do(`mutation { createApiKey(name: "hooks") { key } }`, nil).str(t, "createApiKey", "key"))
	memberKey := s.apiClient(t, userBrowser(t, s, owner, "MEMBER").do(`mutation { createApiKey(name: "hooks") { key } }`, nil).str(t, "createApiKey", "key"))
	in := map[string]any{"url": "https://example.com/hook", "resources": []string{"ISSUE"}}
	const mut = `mutation($in: CreateWebhookInput!) { createWebhook(input: $in) { id secret } }`

	// Positive control: an admin key manages webhooks, an exception to the session-only rule.
	r := adminKey.do(mut, map[string]any{"in": in})
	id := r.str(t, "createWebhook", "id")
	if len(r.Errors) > 0 || id == "" || !strings.HasPrefix(r.str(t, "createWebhook", "secret"), "yal_wh_") {
		t.Fatalf("admin key create: %s", r.raw)
	}
	if r := adminKey.do(`{ webhooks { id } }`, nil); len(r.Errors) > 0 || !strings.Contains(r.raw, id) {
		t.Errorf("admin key list: %s", r.raw)
	}

	// A member key gets FORBIDDEN and no data.
	if r := memberKey.do(mut, map[string]any{"in": in}); r.code() != "FORBIDDEN" || len(r.Data) != 0 {
		t.Errorf("member key create: %s", r.raw)
	}
	if r := memberKey.do(`{ webhooks { id secret } }`, nil); r.code() != "FORBIDDEN" || len(r.Data) != 0 {
		t.Errorf("member key list: %s", r.raw)
	}
	if r := memberKey.do(`query($id: ID!) { webhook(id: $id) { id secret } }`, map[string]any{"id": id}); r.code() != "FORBIDDEN" {
		t.Errorf("member key webhook: %s", r.raw)
	}
}

func TestNativeWebhooksMemberForbidden(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	admin := userBrowser(t, s, owner, "ADMIN")
	member := userBrowser(t, s, owner, "MEMBER")
	recv := receiver(t, 200)
	const secret = "super-secret-value"
	fresh := func() string {
		return createWebhook(t, owner, map[string]any{"url": recv.URL, "resources": []string{"ISSUE"}, "secret": secret}).ID
	}

	cases := map[string]func(id string) string{
		"webhooks":    func(string) string { return `{ webhooks { id name url secret } }` },
		"webhook":     func(id string) string { return `{ webhook(id: "` + id + `") { id secret } }` },
		"deliveries":  func(id string) string { return `{ webhook(id: "` + id + `") { deliveries(first: 5) { id status } } }` },
		"testWebhook": func(id string) string { return `mutation { testWebhook(id: "` + id + `") { id status } }` },
		"createWebhook": func(string) string {
			return `mutation { createWebhook(input: {url: "https://example.com/x", resources: [ISSUE]}) { id secret } }`
		},
		"updateWebhook": func(id string) string {
			return `mutation { updateWebhook(id: "` + id + `", input: {name: "x"}) { id secret } }`
		},
		"rotateWebhookSecret": func(id string) string { return `mutation { rotateWebhookSecret(id: "` + id + `") { id secret } }` },
		"deleteWebhook":       func(id string) string { return `mutation { deleteWebhook(id: "` + id + `") }` },
	}
	for name, query := range cases {
		// Positive control: an admin passes the same operation.
		if r := admin.do(query(fresh()), nil); len(r.Errors) > 0 {
			t.Errorf("%s as admin: %s", name, r.raw)
		}
		id := fresh()
		r := member.do(query(id), nil)
		if r.code() != "FORBIDDEN" {
			t.Errorf("%s as member: %s", name, r.raw)
		}
		if len(r.Data) != 0 {
			t.Errorf("%s as member: data present: %s", name, r.raw)
		}
		if strings.Contains(r.raw, secret) || strings.Contains(r.raw, "yal_wh_") {
			t.Errorf("%s as member leaked a secret: %s", name, r.raw)
		}
		// The member's attempt changed nothing.
		if got := getWebhook(t, owner, id); got.Name != "" || got.Secret != secret {
			t.Errorf("%s as member changed the webhook: %+v", name, got)
		}
	}
}

func TestNativeWebhookErrors(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	const mut = `mutation($in: CreateWebhookInput!) { createWebhook(input: $in) { id } }`
	ok := func(over map[string]any) map[string]any {
		in := map[string]any{"url": "https://example.com/hook", "resources": []string{"ISSUE"}}
		for k, v := range over {
			in[k] = v
		}
		return in
	}
	cases := []struct {
		name          string
		in            map[string]any
		field, reason string
		message       string
	}{
		{"bad URL", ok(map[string]any{"url": "ftp://example.com"}), "url", "invalid_format", "use an absolute http or https URL"},
		{"URL too long", ok(map[string]any{"url": "https://example.com/" + strings.Repeat("a", 2048)}), "url", "too_long", "is too long"},
		{"blocked IP", ok(map[string]any{"url": "http://169.254.169.254/hook"}), "url", "not_allowed", "this address is not allowed"},
		{"no resources", ok(map[string]any{"resources": []string{}}), "resources", "required", "choose at least one event type"},
		{"name too long", ok(map[string]any{"name": strings.Repeat("n", 101)}), "name", "too_long", "is too long"},
		{"secret empty", ok(map[string]any{"secret": ""}), "secret", "too_short", "is too short"},
		{"secret too long", ok(map[string]any{"secret": strings.Repeat("s", 256)}), "secret", "too_long", "is too long"},
		{"unknown project", ok(map[string]any{"projectId": uuid.NewString()}), "projectId", "not_found", "no such project"},
	}
	for _, c := range cases {
		r := owner.do(mut, map[string]any{"in": c.in})
		if r.code() != "INVALID_INPUT" || r.field() != c.field || r.reason() != c.reason || len(r.Errors) != 1 || r.Errors[0].Message != c.message {
			t.Errorf("%s: %s", c.name, r.raw)
		}
	}
	// The limits carry max.
	r := owner.do(mut, map[string]any{"in": cases[1].in})
	if params, _ := r.Errors[0].Extensions["params"].(map[string]any); params["max"] != float64(2048) {
		t.Errorf("url too_long params: %s", r.raw)
	}
	// Positive control: the same input without the fault is accepted.
	if r := owner.do(mut, map[string]any{"in": ok(nil)}); len(r.Errors) > 0 {
		t.Fatalf("valid input: %s", r.raw)
	}

	// Update validates the same way.
	w := createWebhook(t, owner, ok(nil))
	const upd = `mutation($id: ID!, $in: UpdateWebhookInput!) { updateWebhook(id: $id, input: $in) { id } }`
	if r := owner.do(upd, map[string]any{"id": w.ID, "in": map[string]any{"url": "nope"}}); r.field() != "url" || r.reason() != "invalid_format" {
		t.Errorf("update bad URL: %s", r.raw)
	}
	if r := owner.do(upd, map[string]any{"id": w.ID, "in": map[string]any{"projectId": uuid.NewString()}}); r.field() != "projectId" || r.reason() != "not_found" {
		t.Errorf("update unknown project: %s", r.raw)
	}
	if r := owner.do(upd, map[string]any{"id": w.ID, "in": map[string]any{"resources": []string{}}}); r.field() != "resources" || r.reason() != "required" {
		t.Errorf("update no resources: %s", r.raw)
	}
	if r := owner.do(upd, map[string]any{"id": w.ID, "in": map[string]any{"name": nil}}); r.field() != "name" || r.reason() != "required" {
		t.Errorf("update null name: %s", r.raw)
	}
	if r := owner.do(upd, map[string]any{"id": w.ID, "in": map[string]any{"name": "ok"}}); len(r.Errors) > 0 {
		t.Errorf("update valid: %s", r.raw)
	}

	// Unknown id: NOT_FOUND with the entity message, on every operation.
	missing := uuid.NewString()
	for name, q := range map[string]string{
		"webhook":             `{ webhook(id: "` + missing + `") { id deliveries { id } } }`,
		"updateWebhook":       `mutation { updateWebhook(id: "` + missing + `", input: {name: "x"}) { id } }`,
		"deleteWebhook":       `mutation { deleteWebhook(id: "` + missing + `") }`,
		"rotateWebhookSecret": `mutation { rotateWebhookSecret(id: "` + missing + `") { id } }`,
		"testWebhook":         `mutation { testWebhook(id: "` + missing + `") { id } }`,
	} {
		r := owner.do(q, nil)
		if r.code() != "NOT_FOUND" || r.Errors[0].Message != "Webhook not found" {
			t.Errorf("%s unknown id: %s", name, r.raw)
		}
	}

	// deliveries(first) outside 1-50.
	for _, first := range []int{0, 51, -1} {
		r := owner.do(fmt.Sprintf(`{ webhook(id: %q) { deliveries(first: %d) { id } } }`, w.ID, first), nil)
		if r.code() != "INVALID_INPUT" || r.field() != "first" || r.reason() != "out_of_range" {
			t.Errorf("deliveries(first: %d): %s", first, r.raw)
		}
	}
	if r := owner.do(fmt.Sprintf(`{ webhook(id: %q) { deliveries(first: 50) { id } } }`, w.ID), nil); len(r.Errors) > 0 {
		t.Errorf("deliveries(first: 50): %s", r.raw)
	}
}

func TestNativeWebhookLimit(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	const mut = `mutation($in: CreateWebhookInput!) { createWebhook(input: $in) { id } }`
	in := map[string]any{"url": "https://example.com/hook", "resources": []string{"ISSUE"}}
	for i := range 50 {
		if r := owner.do(mut, map[string]any{"in": in}); len(r.Errors) > 0 {
			t.Fatalf("webhook %d: %s", i+1, r.raw)
		}
	}
	r := owner.do(mut, map[string]any{"in": in})
	if r.code() != "INVALID_INPUT" || r.field() != "url" || r.reason() != "too_many" ||
		r.Errors[0].Message != "a workspace can have at most 50 webhooks" {
		t.Fatalf("51st webhook: %s", r.raw)
	}
	if params, _ := r.Errors[0].Extensions["params"].(map[string]any); params["max"] != float64(50) {
		t.Errorf("too_many params: %s", r.raw)
	}
}

func TestNativeTestWebhook(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	ok := receiver(t, 200)
	bad := receiver(t, 500)
	w := createWebhook(t, owner, map[string]any{"url": ok.URL, "resources": []string{"ISSUE"}})

	var d gqlDelivery
	owner.do(`mutation($id: ID!) { testWebhook(id: $id) { `+deliveryFields+` } }`, map[string]any{"id": w.ID}).decode(t, "testWebhook", &d)
	if d.Status != "SUCCEEDED" || d.ResponseStatus == nil || *d.ResponseStatus != 200 || d.Resource != "test" || d.Action != "test" ||
		d.Attempts != 1 || d.EntityID != nil || d.Error != nil || d.NextAttemptAt != nil || d.LastAttemptAt == nil || d.DurationMs == nil {
		t.Errorf("test delivery = %+v", d)
	}

	var got struct {
		Deliveries []gqlDelivery `json:"deliveries"`
	}
	owner.do(`query($id: ID!) { webhook(id: $id) { deliveries(first: 1) { `+deliveryFields+` } } }`, map[string]any{"id": w.ID}).decode(t, "webhook", &got)
	// The stored row and the returned delivery agree (timestamps may differ in precision).
	if len(got.Deliveries) != 1 {
		t.Fatalf("deliveries(first: 1) = %+v", got.Deliveries)
	}
	if l := got.Deliveries[0]; l.ID != d.ID || l.Status != "SUCCEEDED" || l.Resource != "test" || l.Action != "test" ||
		l.Attempts != 1 || l.ResponseStatus == nil || *l.ResponseStatus != 200 || l.LastAttemptAt == nil || l.DurationMs == nil {
		t.Errorf("deliveries(first: 1) = %+v, want %+v", l, d)
	}
	// The default is the newest 50; the list is newest first.
	var d2 gqlDelivery
	owner.do(`mutation($id: ID!) { testWebhook(id: $id) { id } }`, map[string]any{"id": w.ID}).decode(t, "testWebhook", &d2)
	owner.do(`query($id: ID!) { webhook(id: $id) { deliveries { id } } }`, map[string]any{"id": w.ID}).decode(t, "webhook", &got)
	if len(got.Deliveries) != 2 || got.Deliveries[0].ID != d2.ID || got.Deliveries[1].ID != d.ID {
		t.Errorf("deliveries = %+v", got.Deliveries)
	}

	// A failing receiver gives FAILED with the status and the error text.
	wb := createWebhook(t, owner, map[string]any{"url": bad.URL, "resources": []string{"ISSUE"}})
	owner.do(`mutation($id: ID!) { testWebhook(id: $id) { `+deliveryFields+` } }`, map[string]any{"id": wb.ID}).decode(t, "testWebhook", &d)
	if d.Status != "FAILED" || d.ResponseStatus == nil || *d.ResponseStatus != 500 || d.Error == nil || *d.Error == "" {
		t.Errorf("failed test delivery = %+v", d)
	}
}

func TestNativeWebhookComplexity(t *testing.T) {
	s := newServer(t)
	owner := ownerBrowser(t, s)
	w := createWebhook(t, owner, map[string]any{"url": "https://example.com/hook", "resources": []string{"ISSUE"}})
	// The full delivery selection of every webhook, 50 each, exceeds the budget...
	r := owner.do(`{ webhooks { id deliveries(first: 50) { `+deliveryFields+` } } }`, nil)
	if r.code() != "COMPLEXITY_LIMIT_EXCEEDED" {
		t.Errorf("deep selection: %s", r.raw)
	}
	// ...while the detail view and a small page are fine.
	if r := owner.do(fmt.Sprintf(`{ webhook(id: %q) { deliveries(first: 50) { %s } } }`, w.ID, deliveryFields), nil); len(r.Errors) > 0 {
		t.Errorf("detail view: %s", r.raw)
	}
	if r := owner.do(`{ webhooks { id deliveries(first: 1) { id status } } }`, nil); len(r.Errors) > 0 {
		t.Errorf("list with last delivery: %s", r.raw)
	}
}
