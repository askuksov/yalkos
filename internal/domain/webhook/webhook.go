// Package webhook implements webhook registration for Owners and Admins:
// destinations, secrets, scopes and event types.
package webhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/auth"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/store/q"
)

// ReasonFailures is the disabled reason of a webhook turned off automatically
// after repeated failed deliveries.
const ReasonFailures = "failures"

// Webhook is a registered destination. ProjectID nil means all projects.
type Webhook struct {
	ID                   uuid.UUID
	Name, URL, Secret    string
	ProjectID            *uuid.UUID
	Resources            []event.Resource
	Enabled              bool
	DisabledReason       *string
	DisabledAt           *time.Time
	CreatedBy            *uuid.UUID
	CreatedAt, UpdatedAt time.Time
}

// CreateInput is a new webhook. A nil Secret asks for a generated one.
type CreateInput struct {
	URL, Name string
	ProjectID *uuid.UUID
	Resources []event.Resource
	Secret    *string
	Enabled   bool
}

// Patch changes a webhook; nil fields keep their value. ProjectID set with a
// nil value means all projects.
type Patch struct {
	URL, Name, Secret *string
	ProjectID         kit.Opt[uuid.UUID]
	Resources         *[]event.Resource
	Enabled           *bool
}

// Options configure the service. Now is the clock (time.Now when nil);
// AllowPrivate lets webhooks target private and loopback networks; BaseURL is
// the public URL of the app. Builder and Queue are needed by Emit: the payload
// builder and the River client that queues deliveries.
type Options struct {
	Now          func() time.Time
	AllowPrivate bool
	BaseURL      string
	Builder      Builder
	Queue        Inserter
}

// Service holds webhook rules.
type Service struct {
	pool *pgxpool.Pool
	q    *q.Queries
	opts Options
	// sender sends test deliveries (SetSender).
	sender Sender
}

// New builds the service.
func New(pool *pgxpool.Pool, o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{pool: pool, q: q.New(pool), opts: o}
}

var errNotFound = kit.NotFoundError{Entity: "Webhook"}

func (s *Service) inTx(ctx context.Context, fn func(*q.Queries) error) error {
	return kit.InTx(ctx, s.pool, s.q, fn)
}

func authorize(actor account.User) error {
	if !actor.IsAdmin() {
		return kit.ErrForbidden
	}
	return nil
}

func fromRow(r q.Webhook) Webhook {
	res := make([]event.Resource, len(r.Resources))
	for i, v := range r.Resources {
		res[i] = event.Resource(v)
	}
	return Webhook{
		ID: r.ID, Name: r.Name, URL: r.Url, Secret: r.Secret, ProjectID: r.ProjectID, Resources: res,
		Enabled: r.Enabled, DisabledReason: r.DisabledReason, DisabledAt: r.DisabledAt, CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func resourceStrings(in []event.Resource) []string {
	out := make([]string, len(in))
	for i, r := range in {
		out[i] = string(r)
	}
	return out
}

// errNoProject builds a fresh error each time: an InputError is mutable (With).
func errNoProject() error { return kit.Input("projectId", kit.ReasonNotFound, "no such project") }

// checkProject requires an existing project; an archived one is accepted.
func checkProject(ctx context.Context, db *q.Queries, id uuid.UUID) error {
	if _, err := db.GetProject(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return errNoProject()
	} else if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	return nil
}

// projectGone reports a foreign key violation on webhooks.project_id: the
// project was deleted between the check and the write.
func projectGone(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "webhooks_project_id_fkey"
}

// List returns all webhooks, newest first.
func (s *Service) List(ctx context.Context, actor account.User) ([]Webhook, error) {
	if err := authorize(actor); err != nil {
		return nil, err
	}
	rows, err := s.q.ListWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	out := make([]Webhook, len(rows))
	for i, r := range rows {
		out[i] = fromRow(r)
	}
	return out, nil
}

// Get returns one webhook with its secret.
func (s *Service) Get(ctx context.Context, actor account.User, id uuid.UUID) (Webhook, error) {
	if err := authorize(actor); err != nil {
		return Webhook{}, err
	}
	r, err := s.q.GetWebhook(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Webhook{}, errNotFound
	}
	if err != nil {
		return Webhook{}, fmt.Errorf("get webhook: %w", err)
	}
	return fromRow(r), nil
}

// Create registers a webhook. The secret is the given one, verbatim, or a
// generated one.
func (s *Service) Create(ctx context.Context, actor account.User, in CreateInput) (Webhook, error) {
	if err := authorize(actor); err != nil {
		return Webhook{}, err
	}
	u, err := validURL(in.URL, s.opts.AllowPrivate)
	if err != nil {
		return Webhook{}, err
	}
	name, err := validName(in.Name)
	if err != nil {
		return Webhook{}, err
	}
	resources, err := validResources(in.Resources)
	if err != nil {
		return Webhook{}, err
	}
	secret := ""
	if in.Secret != nil {
		if err := validSecret(*in.Secret); err != nil {
			return Webhook{}, err
		}
		secret = *in.Secret
	} else {
		secret = auth.NewWebhookSecret()
	}
	var out q.Webhook
	err = s.inTx(ctx, func(tx *q.Queries) error {
		// Lock first: the count and the insert must not interleave with another creation.
		if err := tx.LockWorkspaceForWebhooks(ctx); err != nil {
			return fmt.Errorf("lock workspace: %w", err)
		}
		if in.ProjectID != nil {
			if err := checkProject(ctx, tx, *in.ProjectID); err != nil {
				return err
			}
		}
		n, err := tx.CountWebhooks(ctx)
		if err != nil {
			return fmt.Errorf("count webhooks: %w", err)
		}
		if n >= maxWebhooks {
			return kit.Input("url", kit.ReasonTooMany, "a workspace can have at most 50 webhooks").With("max", maxWebhooks)
		}
		out, err = tx.CreateWebhook(ctx, q.CreateWebhookParams{
			Name: name, Url: u, Secret: secret, ProjectID: in.ProjectID, Resources: resourceStrings(resources),
			Enabled: in.Enabled, CreatedBy: &actor.ID, Now: s.opts.Now(),
		})
		if projectGone(err) {
			return errNoProject()
		}
		if err != nil {
			return fmt.Errorf("create webhook: %w", err)
		}
		return nil
	})
	if err != nil {
		return Webhook{}, err
	}
	return fromRow(out), nil
}

// change locks a webhook, lets mutate edit it and stores the result.
func (s *Service) change(ctx context.Context, actor account.User, id uuid.UUID, mutate func(tx *q.Queries, p *q.UpdateWebhookParams) error) (Webhook, error) {
	if err := authorize(actor); err != nil {
		return Webhook{}, err
	}
	var out q.Webhook
	err := s.inTx(ctx, func(tx *q.Queries) error {
		// NO KEY UPDATE: an edit never changes the key, and event writers key-share the row.
		cur, err := tx.GetWebhookForNoKeyUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return fmt.Errorf("lock webhook: %w", err)
		}
		p := q.UpdateWebhookParams{
			ID: id, Name: cur.Name, Url: cur.Url, Secret: cur.Secret, ProjectID: cur.ProjectID, Resources: cur.Resources,
			Enabled: cur.Enabled, DisabledReason: cur.DisabledReason, DisabledAt: cur.DisabledAt, Now: s.opts.Now(),
		}
		if err := mutate(tx, &p); err != nil {
			return err
		}
		out, err = tx.UpdateWebhook(ctx, p)
		if projectGone(err) {
			return errNoProject()
		}
		if err != nil {
			return fmt.Errorf("update webhook: %w", err)
		}
		return nil
	})
	if err != nil {
		return Webhook{}, err
	}
	return fromRow(out), nil
}

// Update applies a patch. Enabling clears the automatic disable reason;
// disabling by hand sets none.
func (s *Service) Update(ctx context.Context, actor account.User, id uuid.UUID, patch Patch) (Webhook, error) {
	return s.change(ctx, actor, id, func(tx *q.Queries, p *q.UpdateWebhookParams) error {
		var err error
		if patch.URL != nil {
			if p.Url, err = validURL(*patch.URL, s.opts.AllowPrivate); err != nil {
				return err
			}
		}
		if patch.Name != nil {
			if p.Name, err = validName(*patch.Name); err != nil {
				return err
			}
		}
		if patch.Secret != nil {
			if err := validSecret(*patch.Secret); err != nil {
				return err
			}
			p.Secret = *patch.Secret
		}
		if patch.Resources != nil {
			res, err := validResources(*patch.Resources)
			if err != nil {
				return err
			}
			p.Resources = resourceStrings(res)
		}
		if patch.ProjectID.Set {
			if patch.ProjectID.Value != nil {
				if err := checkProject(ctx, tx, *patch.ProjectID.Value); err != nil {
					return err
				}
			}
			p.ProjectID = patch.ProjectID.Value
		}
		if patch.Enabled != nil {
			p.Enabled = *patch.Enabled
			p.DisabledReason, p.DisabledAt = nil, nil
		}
		return nil
	})
}

// RotateSecret replaces the secret with a newly generated one.
func (s *Service) RotateSecret(ctx context.Context, actor account.User, id uuid.UUID) (Webhook, error) {
	return s.change(ctx, actor, id, func(_ *q.Queries, p *q.UpdateWebhookParams) error {
		p.Secret = auth.NewWebhookSecret()
		return nil
	})
}

// Delete removes a webhook with its deliveries.
func (s *Service) Delete(ctx context.Context, actor account.User, id uuid.UUID) error {
	if err := authorize(actor); err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *q.Queries) error {
		if _, err := tx.GetWebhookForUpdate(ctx, id); errors.Is(err, pgx.ErrNoRows) {
			return errNotFound
		} else if err != nil {
			return fmt.Errorf("lock webhook: %w", err)
		}
		if err := tx.DeleteWebhook(ctx, id); err != nil {
			return fmt.Errorf("delete webhook: %w", err)
		}
		return nil
	})
}
