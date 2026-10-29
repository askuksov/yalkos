package account

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// failureLimiter allows `burst` failures per key, refilling one every `every`;
// only failures are counted. Idle keys are dropped.
type failureLimiter struct {
	mu    sync.Mutex
	every time.Duration
	burst int
	keys  map[string]*limiterEntry
}

type limiterEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

// newFailureLimiter: 10 failures per 15 minutes per key.
func newFailureLimiter() *failureLimiter {
	return &failureLimiter{every: 90 * time.Second, burst: 10, keys: map[string]*limiterEntry{}}
}

func (f *failureLimiter) entry(key string) *limiterEntry {
	now := time.Now()
	for k, e := range f.keys {
		if now.Sub(e.seen) > 15*time.Minute {
			delete(f.keys, k)
		}
	}
	e, ok := f.keys[key]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(rate.Every(f.every), f.burst)}
		f.keys[key] = e
	}
	e.seen = now
	return e
}

// Blocked reports whether key has used up its failures.
func (f *failureLimiter) Blocked(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entry(key).lim.Tokens() < 1
}

// Fail records one failure for key.
func (f *failureLimiter) Fail(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entry(key).lim.Allow()
}
