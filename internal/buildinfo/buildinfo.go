// Package buildinfo holds version metadata injected at build time via -ldflags -X.
package buildinfo

var (
	// Version is the release version, e.g. "v0.1.0" or a git describe string.
	Version = "dev"
	// Commit is the short git commit hash.
	Commit = "none"
	// Date is the build time in "YYYY-MM-DD HH:mm" (UTC).
	Date = "unknown"
)
