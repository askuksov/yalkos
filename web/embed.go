// Package web embeds the built frontend (web/dist) into the Go binary.
package web

import (
	"embed"
	"io/fs"
)

// all: keeps dist/.gitkeep, so the pattern matches even before a frontend build.
//
//go:embed all:dist
var distFS embed.FS

// Dist returns the contents of web/dist. After `pnpm build` it contains index.html.
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Only possible if the embed directive above is changed incorrectly.
		panic(err)
	}
	return sub
}
