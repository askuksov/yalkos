package native

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/webhook"
)

// webhookDeliveriesDefault is the page size of Webhook.deliveries when first
// is omitted (the schema default, which is also the maximum).
const webhookDeliveriesDefault = 50

// Webhook is the GraphQL Webhook; its project and creator resolve through
// loaders and its deliveries are read on demand.
type Webhook struct {
	ID         string
	Name       string
	URL        string
	Secret     string
	Resources  []WebhookResource
	Enabled    bool
	DisabledAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time

	DisabledReason *WebhookDisabledReason

	uid       uuid.UUID
	projectID *uuid.UUID
	createdBy *uuid.UUID
}

func webhookOut(w webhook.Webhook) *Webhook {
	out := &Webhook{
		ID: w.ID.String(), Name: w.Name, URL: w.URL, Secret: w.Secret, Resources: webhookResourcesOut(w.Resources),
		Enabled: w.Enabled, DisabledAt: w.DisabledAt, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
		uid: w.ID, projectID: w.ProjectID, createdBy: w.CreatedBy,
	}
	if w.DisabledReason != nil {
		reason := WebhookDisabledReason(strings.ToUpper(*w.DisabledReason))
		out.DisabledReason = &reason
	}
	return out
}

func webhooksOut(in []webhook.Webhook) []Webhook {
	out := make([]Webhook, 0, len(in))
	for _, w := range in {
		out = append(out, *webhookOut(w))
	}
	return out
}

func webhookResourcesOut(in []event.Resource) []WebhookResource {
	out := make([]WebhookResource, 0, len(in))
	for _, r := range in {
		out = append(out, WebhookResource(strings.ToUpper(string(r))))
	}
	return out
}

func webhookResourcesIn(in []WebhookResource) []event.Resource {
	out := make([]event.Resource, 0, len(in))
	for _, r := range in {
		out = append(out, event.Resource(strings.ToLower(string(r))))
	}
	return out
}

// WebhookDelivery is the GraphQL WebhookDelivery; resource and action are
// reported as stored.
type WebhookDelivery struct {
	ID             string
	Resource       string
	Action         string
	EntityID       *string
	Status         WebhookDeliveryStatus
	Attempts       int
	ResponseStatus *int
	Error          *string
	DurationMs     *int
	CreatedAt      time.Time
	LastAttemptAt  *time.Time
	NextAttemptAt  *time.Time
}

func webhookDeliveryOut(d webhook.Delivery) *WebhookDelivery {
	out := &WebhookDelivery{
		ID: d.ID.String(), Resource: d.Resource, Action: d.Action,
		Status:   WebhookDeliveryStatus(strings.ToUpper(d.Status)),
		Attempts: d.Attempts, ResponseStatus: d.ResponseStatus, Error: d.LastError, DurationMs: d.DurationMS,
		CreatedAt: d.CreatedAt, LastAttemptAt: d.LastAttemptAt, NextAttemptAt: d.NextAttemptAt,
	}
	if d.EntityID != nil {
		id := d.EntityID.String()
		out.EntityID = &id
	}
	return out
}

func webhookDeliveriesOut(in []webhook.Delivery) []WebhookDelivery {
	out := make([]WebhookDelivery, 0, len(in))
	for _, d := range in {
		out = append(out, *webhookDeliveryOut(d))
	}
	return out
}
