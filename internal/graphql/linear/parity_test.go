package linear

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2/ast"
)

// issueOnlyFields are fields that Issue has and IssueSearchResult
// deliberately does not: the paginated relations, which a search hit does not
// carry.
var issueOnlyFields = map[string]string{
	"comments":    "paginated relation, not part of a search hit",
	"children":    "paginated relation, not part of a search hit",
	"labels":      "paginated relation, not part of a search hit (labelIds is)",
	"relations":   "paginated relation, not part of a search hit",
	"attachments": "paginated relation, not part of a search hit",
}

// searchOnlyFields are fields that IssueSearchResult has and Issue does not.
var searchOnlyFields = map[string]string{
	"metadata": "search metadata; always an empty object",
}

// fieldSignature renders a field as name(argNames): Type, so that any change
// of nullability, list-ness or arguments shows up in a diff.
func fieldSignature(f *ast.FieldDefinition) string {
	args := make([]string, 0, len(f.Arguments))
	for _, a := range f.Arguments {
		args = append(args, a.Name)
	}
	sort.Strings(args)
	sig := f.Name
	if len(args) > 0 {
		sig += "(" + strings.Join(args, ", ") + ")"
	}
	return sig + ": " + f.Type.String()
}

func schemaSignatures(t *testing.T, def *ast.Definition) map[string]string {
	t.Helper()
	if def == nil {
		t.Fatal("type missing from the schema")
	}
	out := map[string]string{}
	for _, f := range def.Fields {
		if strings.HasPrefix(f.Name, "__") {
			continue
		}
		out[f.Name] = fieldSignature(f)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestIssueSearchResultParity(t *testing.T) {
	schema := NewExecutableSchema(Config{Resolvers: &Resolver{}}).Schema()
	issueSig := schemaSignatures(t, schema.Types["Issue"])
	searchSig := schemaSignatures(t, schema.Types["IssueSearchResult"])

	t.Run("allow-lists are consistent", func(t *testing.T) {
		for _, name := range sortedKeys(issueOnlyFields) {
			if _, ok := issueSig[name]; !ok {
				t.Errorf("allow-listed %q is not an Issue field", name)
			}
			if _, ok := searchSig[name]; ok {
				t.Errorf("%q is allow-listed as Issue-only but IssueSearchResult has it", name)
			}
		}
		for _, name := range sortedKeys(searchOnlyFields) {
			if _, ok := searchSig[name]; !ok {
				t.Errorf("allow-listed %q is not an IssueSearchResult field", name)
			}
			if _, ok := issueSig[name]; ok {
				t.Errorf("%q is allow-listed as search-only but Issue has it", name)
			}
		}
	})

	t.Run("schema fields match", func(t *testing.T) {
		var diffs []string
		for _, name := range sortedKeys(issueSig) {
			if _, skip := issueOnlyFields[name]; skip {
				continue
			}
			s, ok := searchSig[name]
			switch {
			case !ok:
				diffs = append(diffs, fmt.Sprintf("only on Issue:             %s", issueSig[name]))
			case s != issueSig[name]:
				diffs = append(diffs, fmt.Sprintf("differs:\n    Issue:             %s\n    IssueSearchResult: %s", issueSig[name], s))
			}
		}
		for _, name := range sortedKeys(searchSig) {
			if _, skip := searchOnlyFields[name]; skip {
				continue
			}
			if _, ok := issueSig[name]; !ok {
				diffs = append(diffs, fmt.Sprintf("only on IssueSearchResult: %s", searchSig[name]))
			}
		}
		if len(diffs) > 0 {
			t.Errorf("Issue and IssueSearchResult drifted apart:\n  %s", strings.Join(diffs, "\n  "))
		}
	})

	// searchResultOut copies model fields by name and type from the Issue
	// model; every other field must be served by a resolver of both types.
	t.Run("search result fields are populated from Issue", func(t *testing.T) {
		issueT := reflect.TypeOf(Issue{})
		searchT := reflect.TypeOf(IssueSearchResult{})
		issueResolver := reflect.TypeFor[IssueResolver]()
		searchResolver := reflect.TypeFor[IssueSearchResultResolver]()

		byJSON := func(rt reflect.Type) map[string]reflect.StructField {
			m := map[string]reflect.StructField{}
			for i := range rt.NumField() {
				f := rt.Field(i)
				if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
					m[name] = f
				}
			}
			return m
		}
		issueFields, searchFields := byJSON(issueT), byJSON(searchT)
		hasMethod := func(rt reflect.Type, field string) bool {
			for i := range rt.NumMethod() {
				if strings.EqualFold(rt.Method(i).Name, field) {
					return true
				}
			}
			return false
		}

		var problems []string
		for _, name := range sortedKeys(searchSig) {
			if _, skip := searchOnlyFields[name]; skip {
				continue
			}
			if sf, ok := searchFields[name]; ok {
				if f, ok := issueFields[name]; !ok || f.Name != sf.Name || f.Type != sf.Type {
					problems = append(problems, fmt.Sprintf("%s: IssueSearchResult.%s (%s) has no same-named, same-typed field on the Issue model, so searchResultOut leaves it empty", name, sf.Name, sf.Type))
				}
				continue
			}
			if !hasMethod(searchResolver, name) {
				problems = append(problems, fmt.Sprintf("%s: neither a model field nor a resolver method of IssueSearchResult, it would always resolve to null", name))
			}
			if !hasMethod(issueResolver, name) {
				problems = append(problems, fmt.Sprintf("%s: IssueSearchResult has a resolver but Issue has none", name))
			}
		}
		if len(problems) > 0 {
			t.Errorf("IssueSearchResult fields not populated like Issue:\n  %s", strings.Join(problems, "\n  "))
		}
	})
}
