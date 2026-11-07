package linearpayload

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/domain/event"
	"github.com/askuksov/yalkos/internal/domain/milestone"
	"github.com/askuksov/yalkos/internal/store/q"
)

// A rule copies the previous value of a diff key to an updatedFrom key.
type rule struct {
	from, to string
	conv     func(any) any
}

// apply puts the previous values of the diff's keys the rules know into uf;
// other keys produce nothing.
func apply(uf map[string]any, c event.Change, rules []rule) {
	for _, r := range rules {
		if d, ok := c.Diff[r.from]; ok {
			uf[r.to] = r.conv(d.From)
		}
	}
}

// start is updatedFrom of a change: always the previous update time.
func start(c event.Change) map[string]any {
	return map[string]any{"updatedAt": ts(c.PrevUpdatedAt)}
}

// plain keeps a previous value; ids, times and lists get their payload formats.
func plain(v any) any {
	switch v := v.(type) {
	case uuid.UUID:
		return v.String()
	case *uuid.UUID:
		return idp(v)
	case []uuid.UUID:
		return idStrings(v)
	case time.Time:
		return ts(v)
	case *time.Time:
		return tsp(v)
	}
	return v
}

// ids is a previous list of ids; none is [] and never null.
func ids(v any) any {
	if out, ok := plain(v).([]string); ok {
		return out
	}
	if out, ok := v.([]any); ok {
		return out
	}
	return []string{}
}

// blankNull is a previous text where "" reads as null.
func blankNull(v any) any {
	if s, ok := v.(string); ok && s == "" {
		return nil
	}
	return v
}

// when is a previous date-time; the textual form of a stored change is
// formatted too.
func when(v any) any {
	if s, ok := v.(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return ts(t)
		}
	}
	return plain(v)
}

// day is a previous date.
func day(v any) any {
	switch v := v.(type) {
	case time.Time:
		return v.Format(dateLayout)
	case *time.Time:
		return datep(v)
	}
	return plain(v)
}

var issueRules = []rule{
	{"title", "title", plain},
	{"description", "description", blankNull},
	{"stateId", "stateId", plain},
	{"priority", "priority", plain},
	{"assigneeId", "assigneeId", plain},
	{"dueDate", "dueDate", day},
	{"parentId", "parentId", plain},
	{"sortOrder", "sortOrder", plain},
	{"projectId", "teamId", plain},
	{"number", "number", plain},
	{"milestoneId", "projectId", plain},
	{"labelIds", "labelIds", ids},
	{"completedAt", "completedAt", when},
	{"canceledAt", "canceledAt", when},
}

// issueUpdatedFrom is the previous values of an issue's changed fields. A
// changed number or project also gives the previous identifier, with the key
// the old project has now; currentProject and currentNumber are the issue's now.
func issueUpdatedFrom(ctx context.Context, db *q.Queries, c event.Change, currentProject uuid.UUID, currentNumber int) (map[string]any, error) {
	uf := start(c)
	apply(uf, c, issueRules)
	_, numberChanged := c.Diff["number"]
	_, projectChanged := c.Diff["projectId"]
	if !numberChanged && !projectChanged {
		return uf, nil
	}
	number := currentNumber
	if n, ok := c.Diff["number"]; ok {
		var isInt bool
		if number, isInt = integer(n.From); !isInt {
			return nil, fmt.Errorf("previous number %v is not a number", n.From)
		}
	}
	old := currentProject
	if d, ok := c.Diff["projectId"]; ok {
		s, _ := plain(d.From).(string)
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("previous project %v: %w", d.From, err)
		}
		old = id
	}
	p, err := getProject(ctx, db, old)
	if err != nil {
		return nil, err
	}
	uf["identifier"] = p.Key + "-" + strconv.Itoa(number)
	return uf, nil
}

// integer reads a whole number from a previous value.
func integer(v any) (int, bool) {
	switch v := v.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), v == float64(int(v))
	}
	return 0, false
}

// commentUpdatedFrom is the previous body and edit time.
func commentUpdatedFrom(c event.Change) map[string]any {
	uf := start(c)
	apply(uf, c, []rule{{"body", "body", plain}, {"editedAt", "editedAt", when}})
	return uf
}

// labelUpdatedFrom is the previous values of the fields a Linear-compatible label has;
// a change of only exclusive or position gives the update time alone.
func labelUpdatedFrom(c event.Change) map[string]any {
	uf := start(c)
	apply(uf, c, []rule{
		{"name", "name", plain}, {"color", "color", plain},
		{"description", "description", blankNull}, {"parentId", "parentId", plain},
	})
	return uf
}

// milestoneUpdatedFrom is the previous values of a milestone's changed
// fields. The status before is the effective one: the pinned status, or for
// "automatic" the one prog (the issues, which the change did not touch)
// computes.
func milestoneUpdatedFrom(c event.Change, prog milestone.Progress) map[string]any {
	uf := start(c)
	apply(uf, c, []rule{
		{"name", "name", plain},
		{"description", "description", plain},
		{"description", "content", blankNull},
		{"targetDate", "targetDate", day},
		{"leadId", "leadId", plain},
		{"sortOrder", "sortOrder", plain},
	})
	if d, ok := c.Diff["status"]; ok {
		if s, ok := d.From.(string); ok {
			before := milestone.AutoStatus(prog)
			if s != "automatic" {
				before = milestone.Status(s)
			}
			// A status the domain does not know gives no key rather than a wrong one.
			if slices.Contains(statusOrder[:], before) {
				uf["statusId"] = StatusOf(before).ID
			}
		}
	}
	return uf
}
