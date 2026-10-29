package cli

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"
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
