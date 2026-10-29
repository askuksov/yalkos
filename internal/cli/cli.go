// Package cli implements the yalkos command-line entry point.
package cli

import (
	"fmt"
	"io"
	"io/fs"

	"github.com/askuksov/yalkos/internal/buildinfo"
)

// Env carries the process environment the CLI writes to and reads from.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// UI is the embedded web build; nil or without index.html means "not embedded".
	UI fs.FS
}

const usage = `Usage: yalkos <command>

Commands:
  version  Print version information
  help     Show this help
`

// Run executes the command in args and returns the process exit code.
func Run(args []string, env Env) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(env.Stdout, usage)
		return 0
	}

	switch args[0] {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(env.Stdout, usage)
		return 0
	case "version":
		_, _ = fmt.Fprintf(env.Stdout, "yalkos %s (commit %s, built %s, ui %s)\n",
			buildinfo.Version, buildinfo.Commit, buildinfo.Date, uiStatus(env.UI))
		return 0
	default:
		_, _ = fmt.Fprintf(env.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func uiStatus(ui fs.FS) string {
	if ui == nil {
		return "missing"
	}
	if _, err := fs.Stat(ui, "index.html"); err != nil {
		return "missing"
	}
	return "embedded"
}
