package linear

import (
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/99designs/gqlgen/graphql"

	"github.com/askuksov/yalkos/internal/domain/kit"
)

const (
	dateTimeLayout = "2006-01-02T15:04:05.000Z"
	dateLayout     = "2006-01-02"
)

// MarshalDateTime writes t in UTC with milliseconds.
func MarshalDateTime(t time.Time) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, strconv.Quote(t.UTC().Format(dateTimeLayout)))
	})
}

// UnmarshalDateTime reads an RFC 3339 date-time.
func UnmarshalDateTime(v any) (time.Time, error) {
	s, ok := v.(string)
	if ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, kit.Input("", kit.ReasonInvalidFormat, "use an ISO 8601 date-time")
}

// MarshalTimelessDate writes a calendar date as YYYY-MM-DD.
func MarshalTimelessDate(t time.Time) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, strconv.Quote(t.Format(dateLayout)))
	})
}

// UnmarshalTimelessDate reads YYYY-MM-DD.
func UnmarshalTimelessDate(v any) (time.Time, error) {
	if s, ok := v.(string); ok {
		if t, err := time.Parse(dateLayout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, kit.Input("", kit.ReasonInvalidFormat, "use a date YYYY-MM-DD")
}

// MarshalJSONText writes JSON text as a string.
func MarshalJSONText(s string) graphql.Marshaler { return graphql.MarshalString(s) }

// UnmarshalJSONText reads JSON text.
func UnmarshalJSONText(v any) (string, error) {
	if s, ok := v.(string); ok && json.Valid([]byte(s)) {
		return s, nil
	}
	return "", kit.Input("", kit.ReasonInvalidFormat, "use JSON text")
}

// JSONObject is any JSON value; the zero value is written as {}.
type JSONObject struct{ V any }

// MarshalGQL writes the value, or {} when it is nil.
func (j JSONObject) MarshalGQL(w io.Writer) {
	if j.V == nil {
		_, _ = io.WriteString(w, "{}")
		return
	}
	b, err := json.Marshal(j.V)
	if err != nil {
		_, _ = io.WriteString(w, "{}")
		return
	}
	_, _ = w.Write(b)
}

// UnmarshalGQL keeps any value.
func (j *JSONObject) UnmarshalGQL(v any) error {
	j.V = v
	return nil
}
