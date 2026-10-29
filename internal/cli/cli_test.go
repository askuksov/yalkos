package cli

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/testdb"
)

func run(args []string, env Env) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	env.Stdout, env.Stderr = &out, &errb
	code = Run(args, env)
	return code, out.String(), errb.String()
}

func TestRunVersionWithUI(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	code, out, errOut := run([]string{"version"}, Env{UI: ui})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	want := "yalkos dev (commit none, built unknown, ui embedded)\n"
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
}

func TestRunVersionWithoutUI(t *testing.T) {
	want := "yalkos dev (commit none, built unknown, ui missing)\n"
	cases := map[string]Env{
		"nil fs":       {UI: nil},
		"only gitkeep": {UI: fstest.MapFS{".gitkeep": {}}},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			code, out, _ := run([]string{"version"}, env)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestRunNoArgs(t *testing.T) {
	code, out, _ := run(nil, Env{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, s := range []string{"Usage: yalkos <command>", "version"} {
		if !strings.Contains(out, s) {
			t.Errorf("stdout %q does not contain %q", out, s)
		}
	}
}

func TestRunHelp(t *testing.T) {
	_, want, _ := run(nil, Env{})
	for _, arg := range []string{"help", "-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			code, out, _ := run([]string{arg}, Env{})
			if code != 0 {
				t.Fatalf("exit code = %d, want 0", code)
			}
			if out != want {
				t.Errorf("stdout = %q, want %q", out, want)
			}
		})
	}
}

func TestRunUnknownCommand(t *testing.T) {
	code, out, errOut := run([]string{"frobnicate"}, Env{})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	for _, s := range []string{`unknown command "frobnicate"`, "Usage: yalkos <command>"} {
		if !strings.Contains(errOut, s) {
			t.Errorf("stderr %q does not contain %q", errOut, s)
		}
	}
}

func TestRunServeMissingDatabaseURL(t *testing.T) {
	code, _, errOut := run([]string{"serve"}, Env{Ctx: context.Background(), Getenv: func(string) string { return "" }})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "DATABASE_URL is required") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestRunMigrateStatus(t *testing.T) {
	url, _ := testdb.New(t)
	env := Env{Ctx: context.Background(), Getenv: func(k string) string {
		if k == "DATABASE_URL" {
			return url
		}
		return ""
	}}
	if code, _, errOut := run([]string{"migrate", "up"}, env); code != 0 {
		t.Fatalf("migrate up exit = %d, stderr %q", code, errOut)
	}
	files, _ := fs.Glob(db.Migrations(), "*.sql")
	want := fmt.Sprintf("current %d, latest %d\n", len(files), len(files))
	code, out, _ := run([]string{"migrate", "status"}, env)
	if code != 0 || out != want {
		t.Errorf("migrate status → %d %q, want 0 %q", code, out, want)
	}
}

func TestRunMigrateUnknown(t *testing.T) {
	env := Env{Ctx: context.Background(), Getenv: func(string) string { return "postgres://x" }}
	for _, args := range [][]string{{"migrate"}, {"migrate", "sideways"}} {
		code, _, errOut := run(args, env)
		if code != 2 || !strings.Contains(errOut, "Usage: yalkos <command>") {
			t.Errorf("%v → %d %q, want 2 + usage", args, code, errOut)
		}
	}
}
