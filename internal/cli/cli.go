// Package cli implements the yalkos command-line entry point.
package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/app"
	"github.com/askuksov/yalkos/internal/buildinfo"
	"github.com/askuksov/yalkos/internal/config"
	"github.com/askuksov/yalkos/internal/store"
)

// Env carries the process environment the CLI writes to and reads from.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// UI is the embedded web build; nil or without index.html means "not embedded".
	UI fs.FS
	// Ctx is cancelled on SIGINT/SIGTERM.
	Ctx context.Context
	// Getenv reads environment variables (os.Getenv in production).
	Getenv func(string) string
}

const usage = `Usage: yalkos <command>

Commands:
  serve           Run the server (applies migrations first)
  migrate up      Apply pending database migrations
  migrate status  Show applied and latest migration versions
  version         Print version information
  help            Show this help
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
	case "serve":
		return serve(env)
	case "migrate":
		if len(args) < 2 || (args[1] != "up" && args[1] != "status") {
			_, _ = fmt.Fprintf(env.Stderr, "migrate: expected \"up\" or \"status\"\n\n%s", usage)
			return 2
		}
		return migrate(args[1], env)
	default:
		_, _ = fmt.Fprintf(env.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func serve(env Env) int {
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(env.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	if err := app.Run(ctxOf(env), cfg, env.UI, logger, nil); err != nil {
		logger.Error("server stopped", slog.String("error", err.Error()))
		return 1
	}
	return 0
}

func migrate(sub string, env Env) int {
	cfg, err := config.Load(env.Getenv)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, err)
		return 1
	}
	ctx := ctxOf(env)
	pool, err := store.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, err)
		return 1
	}
	defer pool.Close()

	if sub == "up" {
		if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
			_, _ = fmt.Fprintln(env.Stderr, err)
			return 1
		}
	}
	current, latest, err := store.MigrationStatus(ctx, pool, db.Migrations())
	if err != nil {
		_, _ = fmt.Fprintln(env.Stderr, err)
		return 1
	}
	_, _ = fmt.Fprintf(env.Stdout, "current %d, latest %d\n", current, latest)
	return 0
}

func ctxOf(env Env) context.Context {
	if env.Ctx != nil {
		return env.Ctx
	}
	return context.Background()
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
