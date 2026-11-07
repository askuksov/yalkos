package webhook

import (
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/netguard"
)

const (
	maxURL       = 2048
	maxName      = 100
	maxSecret    = 255
	maxWebhooks  = 50
	msgTooLong   = "is too long"
	msgBadURL    = "use an absolute http or https URL"
	msgNoAddress = "this address is not allowed"
	msgBadChar   = "contains an invalid character"
)

// hasNUL reports a NUL character, which Postgres text cannot store.
func hasNUL(v string) bool { return strings.ContainsRune(v, 0) }

// validURL checks a destination URL: absolute http or https, a host, no user
// info, no fragment, at most 2048 characters. A host that is an IP literal in
// a forbidden range is refused here; host names are checked when dialing.
func validURL(raw string, allowPrivate bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if hasNUL(raw) {
		return "", kit.Input("url", kit.ReasonInvalidFormat, msgBadURL)
	}
	if utf8.RuneCountInString(raw) > maxURL {
		return "", kit.Input("url", kit.ReasonTooLong, msgTooLong).With("max", maxURL)
	}
	bad := kit.Input("url", kit.ReasonInvalidFormat, msgBadURL)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || strings.Contains(raw, "#") {
		return "", bad
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && netguard.Check(ip, allowPrivate) != nil {
		return "", kit.Input("url", kit.ReasonNotAllowed, msgNoAddress)
	}
	return raw, nil
}

// validName trims a webhook name and checks its length.
func validName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if hasNUL(v) {
		return "", kit.Input("name", kit.ReasonInvalidFormat, msgBadChar)
	}
	if utf8.RuneCountInString(v) > maxName {
		return "", kit.Input("name", kit.ReasonTooLong, msgTooLong).With("max", maxName)
	}
	return v, nil
}

// validSecret checks a client-chosen secret; it is used verbatim.
func validSecret(v string) error {
	if hasNUL(v) {
		return kit.Input("secret", kit.ReasonInvalidFormat, msgBadChar)
	}
	switch n := utf8.RuneCountInString(v); {
	case n == 0:
		return kit.Input("secret", kit.ReasonTooShort, "is too short")
	case n > maxSecret:
		return kit.Input("secret", kit.ReasonTooLong, msgTooLong).With("max", maxSecret)
	}
	return nil
}

// validResources checks a non-empty subset of the resources and returns it
// without duplicates, in canonical order.
func validResources(in []event.Resource) ([]event.Resource, error) {
	if len(in) == 0 {
		return nil, kit.Input("resources", kit.ReasonRequired, "choose at least one event type")
	}
	all := event.Resources()
	for _, r := range in {
		if !slices.Contains(all, r) {
			return nil, kit.Input("resources", kit.ReasonInvalidFormat, "unknown event type")
		}
	}
	out := make([]event.Resource, 0, len(in))
	for _, r := range all {
		if slices.Contains(in, r) {
			out = append(out, r)
		}
	}
	return out, nil
}
