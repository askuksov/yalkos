package linear

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/page"
	"github.com/askuksov/yalkos/internal/domain/webhook"
)

type resourceType struct {
	name     string
	resource event.Resource
}

// resourceTypes pairs the event type names clients use with the resources
// webhooks subscribe to, in the order they are listed.
var resourceTypes = []resourceType{
	{"Issue", event.Issue},
	{"Comment", event.Comment},
	{"IssueLabel", event.Label},
	{"Project", event.Milestone},
}

// webhookFieldNames maps domain field names of webhooks to the names used
// here; it applies to webhook operations only.
var webhookFieldNames = map[string]string{"resources": "resourceTypes", "name": "label"}

// webhookErr renames the field of a domain input error.
func webhookErr(err error) error {
	var ie *kit.InputError
	if errors.As(err, &ie) {
		if f, ok := webhookFieldNames[ie.Field]; ok {
			renamed := *ie
			renamed.Field = f
			return &renamed
		}
	}
	return err
}

// webhookActor is the user making the request, who must manage webhooks:
// anyone else is refused before their input is looked at.
func (r *Resolver) webhookActor(ctx context.Context) (account.User, error) {
	me, err := r.actor(ctx)
	if err != nil {
		return account.User{}, err
	}
	if !me.IsAdmin() {
		return account.User{}, kit.ErrForbidden
	}
	return me, nil
}

// webhookOut maps a webhook; a team-less one covers all teams.
func webhookOut(w webhook.Webhook) *Webhook {
	types := make([]string, 0, len(w.Resources))
	for _, t := range resourceTypes {
		if slices.Contains(w.Resources, t.resource) {
			types = append(types, t.name)
		}
	}
	return &Webhook{
		ID: w.ID.String(), Label: nonEmpty(w.Name), Secret: &w.Secret, URL: &w.URL, UpdatedAt: w.UpdatedAt,
		ResourceTypes: types, CreatedAt: w.CreatedAt, Enabled: w.Enabled, AllPublicTeams: w.ProjectID == nil, Src: w,
	}
}

// resourcesIn maps event type names to resources.
func resourcesIn(names []string) ([]event.Resource, error) {
	out := make([]event.Resource, 0, len(names))
	for _, n := range names {
		i := slices.IndexFunc(resourceTypes, func(t resourceType) bool { return t.name == n })
		if i < 0 {
			return nil, badArg("resourceTypes", "Invalid resourceTypes: "+n+" is not supported; use "+supportedTypes())
		}
		out = append(out, resourceTypes[i].resource)
	}
	return out, nil
}

// supportedTypes lists the event type names: "Issue, Comment, IssueLabel or Project".
func supportedTypes() string {
	names := make([]string, len(resourceTypes))
	for i, t := range resourceTypes {
		names[i] = t.name
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// webhookConn orders and pages webhooks; includeArchived is ignored, there
// are no archived webhooks.
func webhookConn(ws []webhook.Webhook, by *PaginationOrderBy, a page.Args) (*WebhookConnection, error) {
	id := func(w webhook.Webhook) uuid.UUID { return w.ID }
	orderByTime(ws, id, func(w webhook.Webhook) time.Time { return w.CreatedAt }, func(w webhook.Webhook) time.Time { return w.UpdatedAt }, by)
	items, info, err := pageOf(ws, id, a)
	if err != nil {
		return nil, err
	}
	out := &WebhookConnection{PageInfo: info, Nodes: make([]Webhook, len(items)), Edges: make([]WebhookEdge, len(items))}
	for i, w := range items {
		n := webhookOut(w)
		out.Nodes[i] = *n
		out.Edges[i] = WebhookEdge{Node: n, Cursor: cursorOf(w.ID)}
	}
	return out, nil
}

// webhooks lists the webhooks of the workspace, or only those limited to one
// team.
func (r *Resolver) webhooks(ctx context.Context, team *uuid.UUID, by *PaginationOrderBy, a page.Args) (*WebhookConnection, error) {
	me, err := r.webhookActor(ctx)
	if err != nil {
		return nil, err
	}
	all, err := r.d.Webhooks.List(ctx, me)
	if err != nil {
		return nil, err
	}
	if team != nil {
		all = slices.DeleteFunc(all, func(w webhook.Webhook) bool { return w.ProjectID == nil || *w.ProjectID != *team })
	}
	return webhookConn(all, by, a)
}

func (r *Resolver) webhookByID(ctx context.Context, id string) (*Webhook, error) {
	me, err := r.webhookActor(ctx)
	if err != nil {
		return nil, err
	}
	wid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	w, err := r.d.Webhooks.Get(ctx, me, wid)
	if err != nil {
		return nil, err
	}
	return webhookOut(w), nil
}

func webhookPayload(w webhook.Webhook) *WebhookPayload {
	return &WebhookPayload{Success: true, LastSyncID: lastSync(w.UpdatedAt), Webhook: webhookOut(w)}
}

func (r *Resolver) createWebhook(ctx context.Context, in WebhookCreateInput) (*WebhookPayload, error) {
	me, err := r.webhookActor(ctx)
	if err != nil {
		return nil, err
	}
	all := isTrue(in.AllPublicTeams)
	switch {
	case in.TeamID != nil && all:
		return nil, badArg("allPublicTeams", "Invalid allPublicTeams: use either teamId or allPublicTeams")
	case in.TeamID == nil && !all:
		return nil, badArg("teamId", "Invalid teamId: give teamId or allPublicTeams: true")
	}
	ci := webhook.CreateInput{URL: in.URL, Secret: in.Secret, Enabled: true}
	if ci.Resources, err = resourcesIn(in.ResourceTypes); err != nil {
		return nil, err
	}
	if in.TeamID != nil {
		p, err := r.projectByRef(ctx, "teamId", *in.TeamID)
		if err != nil {
			return nil, err
		}
		ci.ProjectID = &p.ID
	}
	if in.Label != nil {
		ci.Name = *in.Label
	}
	if in.Enabled != nil {
		ci.Enabled = *in.Enabled
	}
	w, err := r.d.Webhooks.Create(ctx, me, ci)
	if err != nil {
		return nil, webhookErr(err)
	}
	return webhookPayload(w), nil
}

func (r *Resolver) updateWebhook(ctx context.Context, id string, in WebhookUpdateInput) (*WebhookPayload, error) {
	me, err := r.webhookActor(ctx)
	if err != nil {
		return nil, err
	}
	wid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	p := webhook.Patch{URL: in.URL, Secret: in.Secret, Enabled: in.Enabled, Name: clearable(in.Label)}
	if in.ResourceTypes != nil {
		res, err := resourcesIn(in.ResourceTypes)
		if err != nil {
			return nil, err
		}
		p.Resources = &res
	}
	w, err := r.d.Webhooks.Update(ctx, me, wid, p)
	if err != nil {
		return nil, webhookErr(err)
	}
	return webhookPayload(w), nil
}

func (r *Resolver) deleteWebhook(ctx context.Context, id string) (*DeletePayload, error) {
	me, err := r.webhookActor(ctx)
	if err != nil {
		return nil, err
	}
	wid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	if err := r.d.Webhooks.Delete(ctx, me, wid); err != nil {
		return nil, err
	}
	return &DeletePayload{Success: true, EntityID: id, LastSyncID: lastSync(r.d.now())}, nil
}

func (r *Resolver) rotateWebhookSecret(ctx context.Context, id string) (*WebhookRotateSecretPayload, error) {
	me, err := r.webhookActor(ctx)
	if err != nil {
		return nil, err
	}
	wid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	w, err := r.d.Webhooks.RotateSecret(ctx, me, wid)
	if err != nil {
		return nil, err
	}
	return &WebhookRotateSecretPayload{Success: true, LastSyncID: lastSync(w.UpdatedAt), Secret: w.Secret}, nil
}
