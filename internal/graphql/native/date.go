package native

import (
	"io"
	"strconv"
	"time"

	"github.com/askuksov/yalkos/internal/domain/kit"
)

const dateLayout = "2006-01-02"

// Date is the GraphQL Date scalar: a calendar date written as YYYY-MM-DD.
type Date time.Time

// MarshalGQL writes the date as a quoted YYYY-MM-DD string.
func (d Date) MarshalGQL(w io.Writer) {
	_, _ = io.WriteString(w, strconv.Quote(time.Time(d).Format(dateLayout)))
}

// UnmarshalGQL accepts only YYYY-MM-DD strings. The error has no field:
// the presenter takes it from the input path.
func (d *Date) UnmarshalGQL(v any) error {
	s, ok := v.(string)
	if !ok {
		return badDate()
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return badDate()
	}
	*d = Date(t)
	return nil
}

func badDate() error {
	return kit.Input("", kit.ReasonInvalidFormat, "invalid date, expected YYYY-MM-DD")
}
