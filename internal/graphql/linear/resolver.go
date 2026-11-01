package linear

// This file will not be regenerated automatically.

import (
	"context"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/domain/account"
)

// Resolver is the root resolver of the Linear-compatible API. Resolvers call
// domain services and map Yalkos values to this API's shapes; the Yalkos ↔
// Linear-compatible name mapping lives in this package only.
type Resolver struct {
	d Deps
}

// actor is the user making the request.
func (r *Resolver) actor(ctx context.Context) (account.User, error) {
	a, ok := authn.ActorFrom(ctx)
	if !ok {
		return account.User{}, account.ErrUnauthenticated
	}
	return a.User, nil
}
