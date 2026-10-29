// Command yalkos is the Yalkos server and CLI.
package main

import (
	"os"

	"github.com/askuksov/yalkos/internal/cli"
	"github.com/askuksov/yalkos/web"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		UI:     web.Dist(),
	}))
}
