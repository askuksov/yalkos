package issue

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/askuksov/yalkos/internal/domain/kit"
	"github.com/askuksov/yalkos/internal/domain/project"
)

const maxTitle = 255

func validTitle(v string) (string, error) {
	v = strings.TrimSpace(v)
	switch n := utf8.RuneCountInString(v); {
	case n == 0:
		return "", kit.Input("title", kit.ReasonRequired, "enter a title")
	case n > maxTitle:
		return "", kit.Input("title", kit.ReasonTooLong, "title must be at most 255 characters").With("max", maxTitle)
	case strings.ContainsAny(v, "\r\n"):
		return "", kit.Input("title", kit.ReasonInvalidFormat, "title must be a single line")
	}
	return v, nil
}

func validDescription(v string) error { return project.Description("description", v) }

func validPriority(p int) error {
	if p < 0 || p > 4 {
		return kit.Input("priority", kit.ReasonOutOfRange, "priority must be 0–4").With("min", 0).With("max", 4)
	}
	return nil
}

// dateOnly keeps the calendar day of t (a GraphQL Date) at UTC midnight.
func dateOnly(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return &d
}
