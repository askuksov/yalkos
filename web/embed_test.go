package web

import (
	"io/fs"
	"testing"
)

// The committed dist/.gitkeep must be reachable even before any frontend build,
// so a fresh clone compiles and tests without Node.js.
func TestDistAvailableWithoutBuild(t *testing.T) {
	if _, err := fs.Stat(Dist(), ".gitkeep"); err != nil {
		t.Fatalf("Dist() has no .gitkeep: %v", err)
	}
}
