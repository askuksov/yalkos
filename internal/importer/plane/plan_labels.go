package plane

import (
	"cmp"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const maxIssueLabels = 20

// typeOrder is the precedence of the Plane labels that become Type labels,
// with the Type label each maps to.
var typeOrder = []struct{ plane, yalkos string }{
	{"bug", "Bug"}, {"feature", "Feature"}, {"improvement", "Improvement"},
}

// labelKey identifies a Yalkos label: its lower-case name, prefixed by its
// group's for a label in a group.
func labelKey(group, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if group == "" {
		return name
	}
	return strings.ToLower(strings.TrimSpace(group)) + "\x1f" + name
}

// labelRes is what a Plane label turns into.
type labelRes struct {
	// key is the ordinary label (or group) the Plane label merges into. For a
	// Type mapping it is the ordinary label used when another Type label wins.
	key string
	// rank is the Type precedence (index into typeOrder); -1 for an ordinary label.
	rank   int
	typeID uuid.UUID
	name   string
	color  string
	// order is the label's position in its project's label order.
	order int
}

// labelPlanner merges the labels of all projects into workspace labels.
type labelPlanner struct {
	ex       *Existing
	r        *Report
	labels   []LabelPlan
	byKey    map[string]int
	projects map[string][]string
	colors   map[string][]string
	byPlane  map[string]labelRes
	typeRow  map[string]int
	// losers holds, by key, the ordinary label a Type-mapped Plane label
	// becomes when another Type label wins: first name and colour seen.
	losers map[string]LabelPlan
	// topNames are the lower-case names of the top-level labels planned so far.
	topNames map[string]bool
}

func newLabelPlanner(ex *Existing, r *Report) *labelPlanner {
	return &labelPlanner{
		ex: ex, r: r,
		byKey:    map[string]int{},
		projects: map[string][]string{},
		colors:   map[string][]string{},
		byPlane:  map[string]labelRes{},
		typeRow:  map[string]int{},
		losers:   map[string]LabelPlan{},
		topNames: map[string]bool{},
	}
}

// sortedLabels returns a project's labels in Plane order (sort_order).
func sortedLabels(labels []Label) []Label {
	out := slices.Clone(labels)
	slices.SortStableFunc(out, func(a, b Label) int { return cmp.Compare(a.SortOrder, b.SortOrder) })
	return out
}

// addProject merges one project's labels. Top-level labels with children
// become groups; deeper labels go into their top-level group.
func (lp *labelPlanner) addProject(projectKey string, labels []Label) {
	sorted := sortedLabels(labels)
	byID := make(map[string]Label, len(sorted))
	for _, l := range sorted {
		byID[l.ID] = l
	}
	// top returns a label's top-level ancestor and its depth (0 = top level).
	top := func(l Label) (Label, int) {
		depth := 0
		for l.Parent != nil && depth <= len(sorted) {
			parent, ok := byID[*l.Parent]
			if !ok {
				break
			}
			l = parent
			depth++
		}
		return l, depth
	}
	hasChildren := map[string]bool{}
	for _, l := range sorted {
		if t, depth := top(l); depth > 0 {
			hasChildren[t.ID] = true
		}
	}

	// Groups and top-level labels first, so a group precedes its labels.
	for i, l := range sorted {
		if _, depth := top(l); depth > 0 {
			continue
		}
		color := validColor(l.Color, fallbackLabelColor)
		res := labelRes{key: labelKey("", l.Name), rank: -1, name: strings.TrimSpace(l.Name), color: color, order: i}
		if rank := typeRank(l.Name); rank >= 0 && !hasChildren[l.ID] {
			if id, ok := lp.ex.TypeNames[typeOrder[rank].plane]; ok {
				res.rank, res.typeID = rank, id
				lp.byPlane[l.ID] = res
				if _, seen := lp.losers[res.key]; !seen {
					lp.losers[res.key] = LabelPlan{Key: res.key, Name: res.name, Color: color}
				}
				lp.seen(res.key, projectKey, color)
				if _, seen := lp.typeRow[typeOrder[rank].plane]; !seen {
					lp.typeRow[typeOrder[rank].plane] = len(lp.r.Labels.TypeMapped)
					lp.r.Labels.TypeMapped = append(lp.r.Labels.TypeMapped,
						TypeMapped{Name: res.name, Type: typeOrder[rank].yalkos})
				}
				continue
			}
		}
		lp.add(projectKey, l.ID, res.key, "", res.name, color, hasChildren[l.ID])
		lp.byPlane[l.ID] = res
	}
	for i, l := range sorted {
		t, depth := top(l)
		if depth == 0 {
			continue
		}
		color := validColor(l.Color, fallbackLabelColor)
		group := labelKey("", t.Name)
		res := labelRes{key: labelKey(t.Name, l.Name), rank: -1, name: strings.TrimSpace(l.Name), color: color, order: i}
		lp.add(projectKey, l.ID, res.key, group, res.name, color, false)
		lp.byPlane[l.ID] = res
		if depth > 1 {
			f := FlattenedLabel{Name: res.name, Group: strings.TrimSpace(t.Name)}
			if !slices.Contains(lp.r.Labels.Flattened, f) {
				lp.r.Labels.Flattened = append(lp.r.Labels.Flattened, f)
			}
		}
	}
}

func typeRank(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	for i, t := range typeOrder {
		if t.plane == n {
			return i
		}
	}
	return -1
}

// add merges a Plane label into the label with key, creating it on first sight.
func (lp *labelPlanner) add(projectKey, planeID, key, group, name, color string, isGroup bool) {
	i, ok := lp.byKey[key]
	if !ok {
		i = lp.newLabel(LabelPlan{Key: key, Name: name, Color: color, Group: group})
	}
	l := &lp.labels[i]
	l.IsGroup = l.IsGroup || isGroup
	l.PlaneIDs = append(l.PlaneIDs, planeID)
	lp.seen(key, projectKey, color)
}

// seen records a project and a colour of the label with key.
func (lp *labelPlanner) seen(key, projectKey, color string) {
	if !slices.Contains(lp.projects[key], projectKey) {
		lp.projects[key] = append(lp.projects[key], projectKey)
	}
	if !slices.Contains(lp.colors[key], color) {
		lp.colors[key] = append(lp.colors[key], color)
	}
}

func (lp *labelPlanner) newLabel(l LabelPlan) int {
	lp.labels = append(lp.labels, l)
	lp.byKey[l.Key] = len(lp.labels) - 1
	if l.Group == "" {
		lp.topNames[strings.ToLower(l.Name)] = true
	}
	return len(lp.labels) - 1
}

// finish resolves the labels that already exist, in creation order so a
// group is resolved before its labels.
func (lp *labelPlanner) finish() {
	for i := range lp.labels {
		lp.resolve(&lp.labels[i])
	}
}

// report lists the labels found in more than one project.
func (lp *labelPlanner) report() {
	for _, l := range lp.labels {
		if ps := lp.projects[l.Key]; len(ps) > 1 {
			lp.r.Labels.Merged = append(lp.r.Labels.Merged,
				MergedLabel{Name: l.Name, Projects: ps, Colors: lp.colors[l.Key]})
		}
	}
}

// resolve sets the Yalkos label an earlier import mapped one of the Plane
// labels to, else the one with the same name in the same place. A top-level
// label whose name is taken by a Yalkos label of the other kind (plain or
// group), or by an exclusive group, gets the next free name instead.
func (lp *labelPlanner) resolve(l *LabelPlan) {
	for _, id := range l.PlaneIDs {
		if t, ok := lp.ex.mapped(kindLabel, id); ok {
			l.Target = t
			return
		}
	}
	name := strings.ToLower(l.Name)
	if l.Group != "" {
		// A group that is created (new or renamed) has no labels yet.
		if group := lp.labels[lp.byKey[l.Group]]; group.Target != uuid.Nil {
			l.Target = lp.ex.GroupLabels[strings.ToLower(group.Name)][name]
		}
		return
	}
	id, ok := lp.ex.Labels[name]
	if !ok {
		return
	}
	if kind := lp.ex.LabelKinds[name]; kind.Group == l.IsGroup && !kind.Exclusive {
		l.Target = id
		return
	}
	final := freeName(l.Name, func(n string) bool {
		_, inYalkos := lp.ex.Labels[n]
		return inYalkos || lp.topNames[n]
	})
	lp.r.Labels.Renamed = append(lp.r.Labels.Renamed, RenamedLabel{Plane: l.Name, Yalkos: final})
	l.Name = final
	lp.topNames[strings.ToLower(final)] = true
}

// ordinary returns the ordinary label for a Type-mapped Plane label that lost
// to another Type label, creating it on first use with the name and colour
// of its first project.
func (lp *labelPlanner) ordinary(res labelRes) LabelPlan {
	if i, ok := lp.byKey[res.key]; ok {
		return lp.labels[i]
	}
	l, ok := lp.losers[res.key]
	if !ok {
		l = LabelPlan{Key: res.key, Name: res.name, Color: res.color}
	}
	i := lp.newLabel(l)
	lp.resolve(&lp.labels[i])
	return lp.labels[i]
}

// forIssue returns the Type label and the ordinary labels of an issue, and
// reports Type clashes, group labels and labels over the limit.
func (lp *labelPlanner) forIssue(key string, planeIDs []string) (*uuid.UUID, []string) {
	var res []labelRes
	for _, id := range planeIDs {
		if r, ok := lp.byPlane[id]; ok {
			res = append(res, r)
		}
	}
	slices.SortStableFunc(res, func(a, b labelRes) int { return cmp.Compare(a.order, b.order) })

	win := -1
	types := 0
	for i, r := range res {
		if r.rank < 0 {
			continue
		}
		types++
		if win < 0 || r.rank < res[win].rank {
			win = i
		}
	}
	var typ *uuid.UUID
	if win >= 0 {
		id := res[win].typeID
		typ = &id
		lp.r.Labels.TypeMapped[lp.typeRow[typeOrder[res[win].rank].plane]].Issues++
	} else if lp.ex.Type.DefaultID != nil {
		id := *lp.ex.Type.DefaultID
		typ = &id
	}
	if types > 1 {
		lp.r.Labels.TypeConflicts = append(lp.r.Labels.TypeConflicts, key)
	}

	var keys []string
	groupDropped := false
	for i, r := range res {
		if i == win {
			continue
		}
		var l LabelPlan
		if r.rank >= 0 {
			l = lp.ordinary(r)
		} else {
			l = lp.labels[lp.byKey[r.key]]
		}
		if l.IsGroup {
			groupDropped = true
			continue
		}
		if !slices.Contains(keys, l.Key) {
			keys = append(keys, l.Key)
		}
	}
	if groupDropped {
		lp.r.Dropped.GroupLabels = append(lp.r.Dropped.GroupLabels, key)
	}
	// The Type label counts towards the limit.
	limit := maxIssueLabels
	if typ != nil {
		limit--
	}
	if len(keys) > limit {
		keys = keys[:limit]
		lp.r.Dropped.LabelsOver20 = append(lp.r.Dropped.LabelsOver20, key)
	}
	return typ, keys
}
