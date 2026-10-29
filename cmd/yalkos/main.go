// Command yalkos is the Yalkos server and CLI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/askuksov/yalkos/internal/cli"
	"github.com/askuksov/yalkos/web"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(os.Args[1:], cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		UI:     web.Dist(),
		Ctx:    ctx,
		Getenv: os.Getenv,
	})
	stop()
	os.Exit(code)
}
