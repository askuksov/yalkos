package native

import (
	"testing"
	"time"
)

// SetLoaderWait sets the loader batch window until t ends.
func SetLoaderWait(t testing.TB, d time.Duration) {
	old := loaderWait
	loaderWait = d
	t.Cleanup(func() { loaderWait = old })
}
