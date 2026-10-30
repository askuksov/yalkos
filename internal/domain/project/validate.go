package project

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/askuksov/yalkos/internal/domain/kit"
)

var (
	keyRe   = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,6}$`)
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

const (
	maxKey         = 7
	maxName        = 100
	maxDescription = 100_000
	maxIcon        = 32
)

// NormalizeKey upper-cases and trims a project key and checks its format.
func NormalizeKey(field, k string) (string, error) {
	k = strings.ToUpper(strings.TrimSpace(k))
	switch {
	case k == "":
		return "", kit.Input(field, kit.ReasonRequired, "enter a key")
	case utf8.RuneCountInString(k) > maxKey:
		return "", kit.Input(field, kit.ReasonTooLong, "key must be at most 7 characters").With("max", maxKey)
	case !keyRe.MatchString(k):
		return "", kit.Input(field, kit.ReasonInvalidFormat, "key must start with a letter and contain only letters and digits")
	}
	return k, nil
}

// Text checks a trimmed required text of 1..maxRunes runes.
func Text(field, v string, maxRunes int) (string, error) {
	v = strings.TrimSpace(v)
	switch n := utf8.RuneCountInString(v); {
	case n == 0:
		return "", kit.Input(field, kit.ReasonRequired, "must not be empty")
	case n > maxRunes:
		return "", kit.Input(field, kit.ReasonTooLong, "is too long").With("max", maxRunes)
	}
	return v, nil
}

// Description checks a Markdown description (stored as is).
func Description(field, v string) error {
	if utf8.RuneCountInString(v) > maxDescription {
		return kit.Input(field, kit.ReasonTooLong, "is too long").With("max", maxDescription)
	}
	return nil
}

// Color checks #rrggbb and returns it lower-cased.
func Color(field, c string) (string, error) {
	if !colorRe.MatchString(c) {
		return "", kit.Input(field, kit.ReasonInvalidFormat, "color must look like #aabbcc")
	}
	return strings.ToLower(c), nil
}

func optIcon(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s, err := Text("icon", *v, maxIcon)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func optColor(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	c, err := Color("color", *v)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
