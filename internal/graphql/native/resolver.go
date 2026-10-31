package native

// This file will not be regenerated automatically.

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/audit"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/domain/timeline"
)

// Resolver is the root resolver.
type Resolver struct {
	Accounts    *account.Service
	Projects    *project.Service
	Issues      *issue.Service
	Audit       *audit.Service
	Milestones  *milestone.Service
	Labels      *label.Service
	Comments    *comment.Service
	Attachments *attachment.Service
	Timeline    *timeline.Service
	Logger      *slog.Logger
}

func actor(ctx context.Context) (*authn.Actor, error) {
	a, ok := authn.ActorFrom(ctx)
	if !ok {
		return nil, account.ErrUnauthenticated
	}
	return a, nil
}

// sessionActor is actor() restricted to browser sessions: API keys cannot manage
// credentials (keys, password, invites), so a leaked key cannot entrench itself.
func sessionActor(ctx context.Context) (*authn.Actor, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if a.Via != "session" {
		return nil, account.ErrSessionRequired
	}
	return a, nil
}

func parseID(field, id string) (uuid.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, kit.Input(field, kit.ReasonInvalidFormat, "invalid ID")
	}
	return u, nil
}

func roleOut(r account.Role) Role { return Role(strings.ToUpper(string(r))) }
func roleIn(r Role) account.Role  { return account.Role(strings.ToLower(string(r))) }

func userOut(u account.User) *User {
	return &User{ID: u.ID.String(), Name: u.Name, Email: u.Email, Role: roleOut(u.Role), Disabled: u.Disabled, CreatedAt: u.CreatedAt}
}

func inviteOut(i account.Invite) *Invite {
	return &Invite{ID: i.ID.String(), Role: roleOut(i.Role), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
}

func apiKeyOut(k account.APIKey) *APIKey {
	return &APIKey{ID: k.ID.String(), Name: k.Name, DisplayPrefix: k.DisplayPrefix, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt}
}

func (r *mutationResolver) setArchived(ctx context.Context, id string, archived bool) (*Project, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	pid, err := parseID("id", id)
	if err != nil {
		return nil, err
	}
	p, err := r.Projects.SetArchived(ctx, a.User, pid, archived)
	if err != nil {
		return nil, err
	}
	return projectOut(p), nil
}

type memberChange func(ctx context.Context, actor account.User, projectID, userID uuid.UUID) error

func (r *mutationResolver) changeMember(ctx context.Context, projectID, userID string, change memberChange) (*Project, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	pid, err := parseID("projectId", projectID)
	if err != nil {
		return nil, err
	}
	uid, err := parseID("userId", userID)
	if err != nil {
		return nil, err
	}
	if err := change(ctx, a.User, pid, uid); err != nil {
		return nil, err
	}
	p, err := r.Projects.Get(ctx, pid.String())
	if err != nil {
		return nil, err
	}
	return projectOut(p), nil
}

// user loads an optional user reference.
func (r *Resolver) user(ctx context.Context, id *uuid.UUID) (*User, error) {
	if id == nil {
		return nil, nil
	}
	u, err := loadersFrom(ctx).users.Load(ctx, *id)
	if err != nil {
		return nil, err
	}
	return userOut(u), nil
}

// optID turns an omittable ID input into a patch field (explicit null clears).
func optID(field string, valueOK func() (*string, bool)) (kit.Opt[uuid.UUID], error) {
	v, set := valueOK()
	if !set {
		return kit.Opt[uuid.UUID]{}, nil
	}
	id, err := parseOptID(field, v)
	if err != nil {
		return kit.Opt[uuid.UUID]{}, err
	}
	return kit.Opt[uuid.UUID]{Set: true, Value: id}, nil
}
