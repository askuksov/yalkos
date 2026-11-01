package linear

import (
	"hash/fnv"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/account"
)

var avatarColors = [8]string{"#5E6AD2", "#26B5CE", "#F2C94C", "#4CB782", "#F2994A", "#EB5757", "#BB87FC", "#95A2B3"}

// avatarColor picks a palette colour by a hash of the user id.
func avatarColor(id uuid.UUID) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id.String()))
	return avatarColors[h.Sum32()%uint32(len(avatarColors))]
}

// initials are the upper-cased first letters of the first two words.
func initials(name string) string {
	var b strings.Builder
	for i, w := range strings.Fields(name) {
		if i == 2 {
			break
		}
		r := []rune(w)[0]
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// userOut maps a workspace user; me is the request's user.
func (r *Resolver) userOut(u account.User, me uuid.UUID) *User {
	active := !u.Disabled
	return &User{
		ID: u.ID.String(), Name: u.Name, DisplayName: u.Name, Email: u.Email,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		Active: active, IsAssignable: active, IsMentionable: active,
		Admin: u.IsAdmin(), Owner: u.Role == account.RoleOwner, IsMe: u.ID == me,
		CanAccessAnyPublicTeam: true,
		Initials:               initials(u.Name), AvatarBackgroundColor: avatarColor(u.ID),
		URL: r.d.base(),
	}
}
