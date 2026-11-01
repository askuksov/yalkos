// Package ratelimit limits requests per user with an in-memory token bucket.
package ratelimit

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Result is the outcome of one Take.
type Result struct {
	Allowed          bool
	Limit, Remaining int
	// Reset is when the bucket is full again.
	Reset time.Time
	// RetryAfter is the wait until one token is back (denied requests only).
	RetryAfter time.Duration
}

// Limiter holds one bucket per user: capacity perHour, refilled continuously
// at perHour tokens per hour; each request takes one token.
type Limiter struct {
	limit     int
	interval  time.Duration // refill time of one token
	now       func() time.Time
	mu        sync.Mutex
	buckets   map[uuid.UUID]*bucket
	lastSweep time.Time
}

// bucket stores its deficit as a time: full is when no token is missing.
type bucket struct {
	full, seen time.Time
}

const (
	window     = time.Hour
	sweepEvery = time.Minute
)

// New returns a limiter allowing perHour (> 0) requests per hour and user.
func New(perHour int, now func() time.Time) *Limiter {
	return &Limiter{limit: perHour, interval: window / time.Duration(perHour), now: now, buckets: map[uuid.UUID]*bucket{}}
}

// Take spends one token of the user's bucket when there is one.
func (l *Limiter) Take(user uuid.UUID) Result {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	b := l.buckets[user]
	if b == nil {
		b = &bucket{full: now}
		l.buckets[user] = b
	}
	b.seen = now
	// missing is the time to refill the tokens already spent.
	missing := max(b.full.Sub(now), 0)
	r := Result{Limit: l.limit}
	if missing+l.interval <= window {
		missing += l.interval
		b.full = now.Add(missing)
		r.Allowed = true
	} else {
		r.RetryAfter = missing + l.interval - window
	}
	r.Remaining = int((window - missing) / l.interval)
	r.Reset = now.Add(missing)
	return r
}

// sweep drops buckets idle for over an hour (they are full again), at most
// once a minute.
func (l *Limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepEvery {
		return
	}
	l.lastSweep = now
	for id, b := range l.buckets {
		if now.Sub(b.seen) > window {
			delete(l.buckets, id)
		}
	}
}

func (l *Limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
