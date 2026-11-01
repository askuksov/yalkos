package linear

import (
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/comment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/label"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/domain/project"
)

// Deps are the services behind the Linear-compatible API.
type Deps struct {
	Accounts    *account.Service
	Projects    *project.Service
	Issues      *issue.Service
	Milestones  *milestone.Service
	Labels      *label.Service
	Comments    *comment.Service
	Attachments *attachment.Service
	// BaseURL is the public URL used in links.
	BaseURL *url.URL
	Logger  *slog.Logger
	// Now is the clock (time.Now when nil).
	Now func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// base is BASE_URL without a trailing slash.
func (d Deps) base() string {
	if d.BaseURL == nil {
		return ""
	}
	return strings.TrimSuffix(d.BaseURL.String(), "/")
}

// parseID reads a UUID argument.
func parseID(field, s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, kit.Input(field, kit.ReasonInvalidFormat, "not a valid id")
	}
	return id, nil
}

// parseIDs reads a list of UUID arguments.
func parseIDs(field string, ss []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, len(ss))
	for i, s := range ss {
		id, err := parseID(field, s)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}
