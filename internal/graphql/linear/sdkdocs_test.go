package linear_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2"

	"github.com/askuksov/yalkos/internal/graphql/linear"
)

// TestSDKDocuments validates every <operation>.graphql in LINEAR_SDK_DOCUMENTS
// (documents a Linear-compatible client sends, kept outside the repository)
// against the schema.
func TestSDKDocuments(t *testing.T) {
	dir := os.Getenv("LINEAR_SDK_DOCUMENTS")
	if dir == "" {
		t.Skip("LINEAR_SDK_DOCUMENTS not set")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.graphql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no documents in %s (%v)", dir, err)
	}
	schema := linear.NewExecutableSchema(linear.Config{Resolvers: &linear.Resolver{}}).Schema()
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".graphql")
		t.Run(name, func(t *testing.T) {
			doc, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if _, errs := gqlparser.LoadQueryWithRules(schema, string(doc), nil); len(errs) > 0 {
				for _, e := range errs {
					t.Error(e.Message)
				}
			}
		})
	}
}
