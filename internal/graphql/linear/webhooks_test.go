package linear_test

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/webhook"
)

const webhookFields = `id label secret url updatedAt resourceTypes team { id } archivedAt createdAt creator { id } enabled allPublicTeams`

type webhookNode struct {
	ID             string
	Label          *string
	Secret         *string
	URL            *string
	UpdatedAt      string
	ResourceTypes  []string
	Team           *struct{ ID string }
	ArchivedAt     *string
	CreatedAt      string
	Creator        *struct{ ID string }
	Enabled        bool
	AllPublicTeams bool
}

const (
	createWebhookMutation = `mutation($input: WebhookCreateInput!) { webhookCreate(input: $input) { success lastSyncId webhook { id enabled } } }`
	updateWebhookMutation = `mutation($id: String!, $input: WebhookUpdateInput!) { webhookUpdate(id: $id, input: $input) { success lastSyncId webhook { ` + webhookFields + ` } } }`
	deleteWebhookMutation = `mutation($id: String!) { webhookDelete(id: $id) { success entityId lastSyncId } }`
	rotateWebhookMutation = `mutation($id: String!) { webhookRotateSecret(id: $id) { success secret lastSyncId } }`
)

var generatedSecret = regexp.MustCompile(`^yal_wh_[0-9A-Za-z]{40}$`)

// webhookFix is the issue fixture with an Admin (and the Member from the
// fixture) as API key holders.
type webhookFix struct {
	issueFix
	admin    account.User
	adminKey string
}

func newWebhookFix(t *testing.T) webhookFix {
	t.Helper()
	f := webhookFix{issueFix: newIssueFix(t)}
	f.admin = f.user(t, "Ada", "ada@example.com", account.RoleAdmin)
	f.adminKey = f.key(t, f.admin)
	return f
}

// create registers a webhook through the service.
func (f webhookFix) create(t *testing.T, mod func(*webhook.CreateInput)) webhook.Webhook {
	t.Helper()
	in := webhook.CreateInput{URL: "https://example.com/hook", Resources: []event.Resource{event.Issue}, Enabled: true}
	if mod != nil {
		mod(&in)
	}
	w, err := f.deps.Webhooks.Create(context.Background(), f.owner, in)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func (f webhookFix) stored(t *testing.T, id string) webhook.Webhook {
	t.Helper()
	all, err := f.deps.Webhooks.List(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range all {
		if w.ID.String() == id {
			return w
		}
	}
	t.Fatalf("webhook %s is not stored", id)
	return webhook.Webhook{}
}

func (f webhookFix) count(t *testing.T) int {
	t.Helper()
	all, err := f.deps.Webhooks.List(context.Background(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	return len(all)
}

func TestLinearWebhookCreateLikeN8n(t *testing.T) {
	f := newWebhookFix(t)
	var out struct {
		WebhookCreate struct {
			Success    bool
			LastSyncID float64 `json:"lastSyncId"`
			Webhook    struct {
				ID      string
				Enabled bool
			}
		}
	}
	input := map[string]any{"url": "https://example.com/hook", "teamId": "YLK", "resourceTypes": []string{"Issue", "Comment"}}
	f.gql(t, f.adminKey, createWebhookMutation, map[string]any{"input": input}).ok(t, &out)
	got := out.WebhookCreate
	if !got.Success || !got.Webhook.Enabled || got.Webhook.ID == "" {
		t.Fatalf("payload = %+v", got)
	}
	w := f.stored(t, got.Webhook.ID)
	if !generatedSecret.MatchString(w.Secret) {
		t.Errorf("secret = %q, want a generated one", w.Secret)
	}
	if w.ProjectID == nil || *w.ProjectID != f.ylk.ID || w.Name != "" || !w.Enabled || w.CreatedBy == nil || *w.CreatedBy != f.admin.ID {
		t.Errorf("stored = %+v", w)
	}
	if !slices.Equal(w.Resources, []event.Resource{event.Issue, event.Comment}) {
		t.Errorf("resources = %v", w.Resources)
	}
	if got.LastSyncID != float64(w.UpdatedAt.UnixMilli()) {
		t.Errorf("lastSyncId = %v, want %d", got.LastSyncID, w.UpdatedAt.UnixMilli())
	}

	// The same webhook reads back with the fields an SDK client selects.
	var read struct{ Webhook webhookNode }
	f.gql(t, f.adminKey, `query($id: String!) { webhook(id: $id) { `+webhookFields+` } }`, map[string]any{"id": got.Webhook.ID}).ok(t, &read)
	n := read.Webhook
	if n.ID != w.ID.String() || n.Secret == nil || *n.Secret != w.Secret || n.URL == nil || *n.URL != "https://example.com/hook" ||
		n.Team == nil || n.Team.ID != f.ylk.ID.String() || n.AllPublicTeams || n.Label != nil || n.ArchivedAt != nil ||
		n.Creator == nil || n.Creator.ID != f.admin.ID.String() || !n.Enabled || n.CreatedAt == "" || n.UpdatedAt == "" ||
		!slices.Equal(n.ResourceTypes, []string{"Issue", "Comment"}) {
		t.Fatalf("webhook = %+v", n)
	}

	// A team id works as well as its key.
	input = map[string]any{"url": "https://example.com/other", "teamId": f.web.ID.String(), "resourceTypes": []string{"Project"}}
	f.gql(t, f.adminKey, createWebhookMutation, map[string]any{"input": input}).ok(t, &out)
	if w := f.stored(t, out.WebhookCreate.Webhook.ID); w.ProjectID == nil || *w.ProjectID != f.web.ID {
		t.Errorf("team id scope = %+v", w)
	}
}

func TestLinearWebhookCreateOwnSecret(t *testing.T) {
	f := newWebhookFix(t)
	secret := strings.Repeat("a1b2c3d4", 8)
	input := map[string]any{
		"url": "https://example.com/hook", "allPublicTeams": true, "secret": secret, "label": "Production",
		"resourceTypes": []string{"Project", "IssueLabel", "Issue", "Project"}, "enabled": false,
	}
	var out struct {
		WebhookCreate struct{ Webhook struct{ ID string } }
	}
	f.gql(t, f.ownerKey, createWebhookMutation, map[string]any{"input": input}).ok(t, &out)
	id := out.WebhookCreate.Webhook.ID
	w := f.stored(t, id)
	if w.Secret != secret || w.Name != "Production" || w.ProjectID != nil || w.Enabled {
		t.Errorf("stored = %+v", w)
	}
	var read struct{ Webhook webhookNode }
	f.gql(t, f.ownerKey, `query($id: String!) { webhook(id: $id) { `+webhookFields+` } }`, map[string]any{"id": id}).ok(t, &read)
	n := read.Webhook
	if n.Team != nil || !n.AllPublicTeams || n.Label == nil || *n.Label != "Production" || n.Secret == nil || *n.Secret != secret || n.Enabled {
		t.Fatalf("webhook = %+v", n)
	}
	// Duplicates collapse and the order is the canonical one.
	if !slices.Equal(n.ResourceTypes, []string{"Issue", "IssueLabel", "Project"}) {
		t.Errorf("resourceTypes = %v", n.ResourceTypes)
	}
}

func TestLinearWebhookInputErrors(t *testing.T) {
	f := newWebhookFix(t)
	valid := func() map[string]any {
		return map[string]any{"url": "https://example.com/hook", "allPublicTeams": true, "resourceTypes": []string{"Issue"}}
	}
	with := func(kv ...any) map[string]any {
		in := valid()
		for i := 0; i < len(kv); i += 2 {
			in[kv[i].(string)] = kv[i+1]
		}
		return in
	}
	run := func(in map[string]any) result {
		return f.gql(t, f.adminKey, createWebhookMutation, map[string]any{"input": in})
	}
	// Positive control: the base input is accepted.
	run(valid()).ok(t, nil)
	n := f.count(t)

	const unsupported = "use Issue, Comment, IssueLabel or Project"
	cases := []struct {
		name  string
		input map[string]any
		msg   string
		field string
	}{
		{"neither", with("allPublicTeams", nil), "Invalid teamId: give teamId or allPublicTeams: true", "teamId"},
		{"allPublicTeams false", with("allPublicTeams", false), "Invalid teamId: give teamId or allPublicTeams: true", "teamId"},
		{"both", with("teamId", "YLK"), "Invalid allPublicTeams: use either teamId or allPublicTeams", "allPublicTeams"},
		{"unknown type", with("resourceTypes", []string{"Issue", "Cycle"}), "Invalid resourceTypes: Cycle is not supported; " + unsupported, "resourceTypes"},
		{"lower case type", with("resourceTypes", []string{"issue"}), "Invalid resourceTypes: issue is not supported; " + unsupported, "resourceTypes"},
		{"empty types", with("resourceTypes", []string{}), "Invalid resourceTypes: choose at least one event type", "resourceTypes"},
		{"unknown team", with("allPublicTeams", nil, "teamId", "NOPE"), "Invalid teamId: no such team", "teamId"},
		{"bad url", with("url", "ftp://example.com/x"), "Invalid url: use an absolute http or https URL", "url"},
		{"empty secret", with("secret", ""), "Invalid secret: is too short", "secret"},
		{"long secret", with("secret", strings.Repeat("s", 256)), "Invalid secret: is too long", "secret"},
		{"long label", with("label", strings.Repeat("l", 101)), "Invalid label: is too long", "label"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run(c.input).fails(t).want(t, "INVALID_INPUT", "invalid input", c.msg, c.field)
		})
	}
	if f.count(t) != n {
		t.Fatalf("a webhook was created: %d, want %d", f.count(t), n)
	}

	// The input has no id field.
	g := run(with("id", "6d8f2f6c-7c0b-4a39-b4b9-2d1a7d7a1c11")).fails(t)
	if g.Extensions["type"] != "graphql error" || g.Message != `Field "id" is not defined by type "WebhookCreateInput"` {
		t.Errorf("id field: %+v", g)
	}

	// The same domain rules reach updates, in this API's names.
	w := f.create(t, nil)
	update := func(in map[string]any) result {
		return f.gql(t, f.adminKey, updateWebhookMutation, map[string]any{"id": w.ID.String(), "input": in})
	}
	update(map[string]any{"label": "ok"}).ok(t, nil)
	update(map[string]any{"resourceTypes": []string{}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid resourceTypes: choose at least one event type", "resourceTypes")
	update(map[string]any{"resourceTypes": []string{"Cycle"}}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid resourceTypes: Cycle is not supported; "+unsupported, "resourceTypes")
	update(map[string]any{"label": strings.Repeat("l", 101)}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid label: is too long", "label")
	update(map[string]any{"url": "nope"}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid url: use an absolute http or https URL", "url")
	update(map[string]any{"teamId": "YLK"}).fails(t) // the scope cannot change
	if got := f.stored(t, w.ID.String()); got.Name != "ok" || got.URL != w.URL {
		t.Errorf("a refused update changed the webhook: %+v", got)
	}
}

func TestLinearWebhooksQueries(t *testing.T) {
	f := newWebhookFix(t)
	a := f.create(t, func(in *webhook.CreateInput) { in.Name = "first" })
	b := f.create(t, func(in *webhook.CreateInput) { in.ProjectID = &f.ylk.ID; in.Name = "ylk" })
	c := f.create(t, func(in *webhook.CreateInput) { in.ProjectID = &f.web.ID; in.Name = "web" })
	d := f.create(t, func(in *webhook.CreateInput) { in.ProjectID = &f.ylk.ID; in.Name = "ylk again" })

	type conn struct {
		Nodes    []webhookNode
		PageInfo struct {
			HasNextPage bool
			EndCursor   *string
		}
	}
	ids := func(c conn) []string {
		out := []string{}
		for _, n := range c.Nodes {
			out = append(out, n.ID)
		}
		return out
	}
	list := `query($first: Int, $after: String, $orderBy: PaginationOrderBy) {
		webhooks(first: $first, after: $after, orderBy: $orderBy) { nodes { ` + webhookFields + ` } pageInfo { hasNextPage endCursor } }
	}`
	var out struct{ Webhooks conn }
	f.gql(t, f.ownerKey, list, nil).ok(t, &out)
	if got, want := ids(out.Webhooks), []string{d.ID.String(), c.ID.String(), b.ID.String(), a.ID.String()}; !slices.Equal(got, want) {
		t.Fatalf("webhooks = %v, want newest first %v", got, want)
	}
	if out.Webhooks.Nodes[0].Secret == nil || *out.Webhooks.Nodes[0].Secret != d.Secret {
		t.Errorf("listed secret = %v", out.Webhooks.Nodes[0].Secret)
	}

	// Paging.
	f.gql(t, f.ownerKey, list, map[string]any{"first": 3}).ok(t, &out)
	if got, want := ids(out.Webhooks), []string{d.ID.String(), c.ID.String(), b.ID.String()}; !slices.Equal(got, want) || !out.Webhooks.PageInfo.HasNextPage {
		t.Fatalf("first page = %v next %v", got, out.Webhooks.PageInfo.HasNextPage)
	}
	f.gql(t, f.ownerKey, list, map[string]any{"first": 3, "after": *out.Webhooks.PageInfo.EndCursor}).ok(t, &out)
	if got := ids(out.Webhooks); !slices.Equal(got, []string{a.ID.String()}) || out.Webhooks.PageInfo.HasNextPage {
		t.Fatalf("second page = %v", got)
	}
	f.gql(t, f.ownerKey, list, map[string]any{"first": 1, "after": "not-a-cursor"}).fails(t)

	// A change moves a webhook to the front of an updatedAt order.
	if _, err := f.deps.Webhooks.Update(context.Background(), f.owner, a.ID, webhook.Patch{Name: new("renamed")}); err != nil {
		t.Fatal(err)
	}
	f.gql(t, f.ownerKey, list, map[string]any{"orderBy": "updatedAt"}).ok(t, &out)
	if got := ids(out.Webhooks); got[0] != a.ID.String() {
		t.Errorf("updatedAt order = %v, want %s first", got, a.ID)
	}

	// By id.
	var one struct{ Webhook webhookNode }
	f.gql(t, f.adminKey, `query($id: String!) { webhook(id: $id) { `+webhookFields+` } }`, map[string]any{"id": b.ID.String()}).ok(t, &one)
	if one.Webhook.ID != b.ID.String() || one.Webhook.Team == nil || one.Webhook.Team.ID != f.ylk.ID.String() || one.Webhook.Label == nil || *one.Webhook.Label != "ylk" {
		t.Fatalf("webhook = %+v", one.Webhook)
	}
	const missing = "0190f7aa-0000-7000-8000-000000000001"
	f.gql(t, f.adminKey, `query($id: String!) { webhook(id: $id) { id } }`, map[string]any{"id": missing}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Webhook not found", "")
	f.gql(t, f.adminKey, `query($id: String!) { webhook(id: $id) { id } }`, map[string]any{"id": "nope"}).fails(t).
		want(t, "INVALID_INPUT", "invalid input", "Invalid id: not a valid id", "id")

	// Team.webhooks lists that team's own webhooks only, by key or id.
	teamList := `query($id: String!, $first: Int) { team(id: $id) { webhooks(first: $first) { nodes { id team { id } allPublicTeams } pageInfo { hasNextPage } } } }`
	var team struct{ Team struct{ Webhooks conn } }
	for _, ref := range []string{"YLK", f.ylk.ID.String()} {
		f.gql(t, f.adminKey, teamList, map[string]any{"id": ref}).ok(t, &team)
		if got, want := ids(team.Team.Webhooks), []string{d.ID.String(), b.ID.String()}; !slices.Equal(got, want) {
			t.Errorf("team %s webhooks = %v, want %v", ref, got, want)
		}
	}
	f.gql(t, f.adminKey, teamList, map[string]any{"id": "WEB", "first": 5}).ok(t, &team)
	if got := ids(team.Team.Webhooks); !slices.Equal(got, []string{c.ID.String()}) {
		t.Errorf("WEB webhooks = %v", got)
	}
	f.gql(t, f.adminKey, teamList, map[string]any{"id": "YLK", "first": 1}).ok(t, &team)
	if got := ids(team.Team.Webhooks); !slices.Equal(got, []string{d.ID.String()}) || !team.Team.Webhooks.PageInfo.HasNextPage {
		t.Errorf("YLK first page = %v", got)
	}
}

func TestLinearWebhookUpdateDeleteRotate(t *testing.T) {
	f := newWebhookFix(t)
	w := f.create(t, func(in *webhook.CreateInput) {
		in.Name, in.ProjectID, in.Resources = "Original", &f.ylk.ID, []event.Resource{event.Issue}
	})
	id := w.ID.String()
	var out struct {
		WebhookUpdate struct {
			Success    bool
			LastSyncID float64 `json:"lastSyncId"`
			Webhook    webhookNode
		}
	}
	update := func(in map[string]any) {
		t.Helper()
		f.gql(t, f.adminKey, updateWebhookMutation, map[string]any{"id": id, "input": in}).ok(t, &out)
	}

	update(map[string]any{"enabled": false})
	n := out.WebhookUpdate.Webhook
	if !out.WebhookUpdate.Success || n.Enabled || n.Label == nil || *n.Label != "Original" || n.Team == nil || !slices.Equal(n.ResourceTypes, []string{"Issue"}) {
		t.Fatalf("enabled false: %+v", out.WebhookUpdate)
	}
	if n.Secret == nil || *n.Secret != w.Secret {
		t.Errorf("an update without a secret changed it: %v", n.Secret)
	}
	if got := f.stored(t, id); got.Enabled || out.WebhookUpdate.LastSyncID != float64(got.UpdatedAt.UnixMilli()) {
		t.Errorf("stored = %+v, lastSyncId %v", got, out.WebhookUpdate.LastSyncID)
	}

	// Null clears the label; an absent label keeps it.
	update(map[string]any{"label": nil})
	if n := out.WebhookUpdate.Webhook; n.Label != nil {
		t.Errorf("label after null = %q", *n.Label)
	}
	if got := f.stored(t, id); got.Name != "" {
		t.Errorf("stored name = %q", got.Name)
	}
	update(map[string]any{"label": "Again"})
	update(map[string]any{"enabled": true})
	if n := out.WebhookUpdate.Webhook; n.Label == nil || *n.Label != "Again" || !n.Enabled {
		t.Errorf("absent label: %+v", n)
	}

	update(map[string]any{"resourceTypes": []string{"Project", "Comment"}, "url": "https://example.com/new", "secret": "own-secret"})
	n = out.WebhookUpdate.Webhook
	if !slices.Equal(n.ResourceTypes, []string{"Comment", "Project"}) || n.URL == nil || *n.URL != "https://example.com/new" || n.Secret == nil || *n.Secret != "own-secret" {
		t.Errorf("resources/url/secret: %+v", n)
	}

	// Rotation: a new generated secret, a sync id from the change.
	var rot struct {
		WebhookRotateSecret struct {
			Success    bool
			Secret     string
			LastSyncID float64 `json:"lastSyncId"`
		}
	}
	f.gql(t, f.adminKey, rotateWebhookMutation, map[string]any{"id": id}).ok(t, &rot)
	r := rot.WebhookRotateSecret
	got := f.stored(t, id)
	if !r.Success || r.Secret == "own-secret" || !generatedSecret.MatchString(r.Secret) || got.Secret != r.Secret {
		t.Errorf("rotated = %+v, stored secret %q", r, got.Secret)
	}
	if r.LastSyncID != float64(got.UpdatedAt.UnixMilli()) || r.LastSyncID <= 0 {
		t.Errorf("rotate lastSyncId = %v, want %d", r.LastSyncID, got.UpdatedAt.UnixMilli())
	}
	f.gql(t, f.adminKey, rotateWebhookMutation, map[string]any{"id": id}).ok(t, &rot)
	if rot.WebhookRotateSecret.Secret == r.Secret {
		t.Error("a second rotation returned the same secret")
	}

	// Delete returns the id and removes the webhook.
	var del struct {
		WebhookDelete struct {
			Success    bool
			EntityID   string  `json:"entityId"`
			LastSyncID float64 `json:"lastSyncId"`
		}
	}
	f.gql(t, f.adminKey, deleteWebhookMutation, map[string]any{"id": id}).ok(t, &del)
	if d := del.WebhookDelete; !d.Success || d.EntityID != id || d.LastSyncID <= 0 {
		t.Errorf("delete = %+v", d)
	}
	if f.count(t) != 0 {
		t.Error("the webhook is still stored")
	}
	for _, q := range []string{deleteWebhookMutation, rotateWebhookMutation} {
		f.gql(t, f.adminKey, q, map[string]any{"id": id}).fails(t).
			want(t, "NOT_FOUND", "invalid input", "Webhook not found", "")
	}
	f.gql(t, f.adminKey, updateWebhookMutation, map[string]any{"id": id, "input": map[string]any{"enabled": true}}).fails(t).
		want(t, "NOT_FOUND", "invalid input", "Webhook not found", "")
}

func TestLinearWebhooksMemberKeyForbidden(t *testing.T) {
	f := newWebhookFix(t)
	const secret = "member-must-not-see-this-secret"
	fresh := func() string {
		return f.create(t, func(in *webhook.CreateInput) { in.Secret = new(secret) }).ID.String()
	}
	const forbidden = "You do not have permission to do this"
	cases := map[string]struct {
		query string
		vars  func(id string) map[string]any
	}{
		"webhooks": {`{ webhooks { nodes { id secret url } } }`, func(string) map[string]any { return nil }},
		"webhook": {`query($id: String!) { webhook(id: $id) { id secret } }`, func(id string) map[string]any {
			return map[string]any{"id": id}
		}},
		"Team.webhooks": {`{ team(id: "YLK") { webhooks { nodes { id secret } } } }`, func(string) map[string]any { return nil }},
		"webhookCreate": {createWebhookMutation, func(string) map[string]any {
			return map[string]any{"input": map[string]any{"url": "https://example.com/x", "allPublicTeams": true, "resourceTypes": []string{"Issue"}}}
		}},
		"webhookCreate invalid": {createWebhookMutation, func(string) map[string]any {
			return map[string]any{"input": map[string]any{"url": "nope", "resourceTypes": []string{}}}
		}},
		"webhookUpdate": {updateWebhookMutation, func(id string) map[string]any {
			return map[string]any{"id": id, "input": map[string]any{"label": "x"}}
		}},
		"webhookRotateSecret": {rotateWebhookMutation, func(id string) map[string]any { return map[string]any{"id": id} }},
		"webhookDelete":       {deleteWebhookMutation, func(id string) map[string]any { return map[string]any{"id": id} }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Positive control: an Admin key passes the same operation.
			if r := f.gql(t, f.adminKey, c.query, c.vars(fresh())); name != "webhookCreate invalid" {
				r.ok(t, nil)
			} else {
				r.fails(t)
			}
			id := fresh()
			before := f.stored(t, id)
			r := f.gql(t, f.miaKey, c.query, c.vars(id))
			r.fails(t).want(t, "FORBIDDEN", "forbidden", forbidden, "")
			if strings.Contains(r.Raw, secret) || strings.Contains(r.Raw, "yal_wh_") || strings.Contains(r.Raw, `"secret"`) {
				t.Errorf("member response leaked a secret: %s", r.Raw)
			}
			if string(r.Data) != "null" && r.Data != nil {
				t.Errorf("member response has data: %s", r.Data)
			}
			if got := f.stored(t, id); got.Secret != before.Secret || got.Name != before.Name || !got.UpdatedAt.Equal(before.UpdatedAt) {
				t.Errorf("a member changed the webhook: %+v", got)
			}
		})
	}
}
