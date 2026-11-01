package ratelimit

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeClock is a settable time source.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestTakeUntilEmpty(t *testing.T) {
	c := newClock()
	l := New(3, c.now)
	u := uuid.New()
	for i, want := range []int{2, 1, 0} {
		r := l.Take(u)
		if !r.Allowed || r.Remaining != want || r.Limit != 3 || r.RetryAfter != 0 {
			t.Fatalf("take %d = %+v, want allowed, remaining %d", i+1, r, want)
		}
	}
	r := l.Take(u)
	if r.Allowed || r.Remaining != 0 || r.RetryAfter != 20*time.Minute {
		t.Fatalf("4th = %+v, want denied, retry after 20m", r)
	}
	if !r.Reset.Equal(c.now().Add(time.Hour)) {
		t.Fatalf("Reset = %v, want now+1h", r.Reset)
	}
}

func TestRefillContinuous(t *testing.T) {
	c := newClock()
	l := New(3, c.now)
	u := uuid.New()
	for range 3 {
		l.Take(u)
	}
	c.add(19 * time.Minute)
	if r := l.Take(u); r.Allowed {
		t.Fatalf("after 19m = %+v, want denied", r)
	}
	if r := l.Take(u); r.RetryAfter != time.Minute {
		t.Fatalf("RetryAfter = %v, want 1m", r.RetryAfter)
	}
	c.add(time.Minute)
	if r := l.Take(u); !r.Allowed || r.Remaining != 0 {
		t.Fatalf("after 20m = %+v, want one more allowed", r)
	}
	c.add(5 * time.Hour)
	if r := l.Take(u); !r.Allowed || r.Remaining != 2 {
		t.Fatalf("after a long pause = %+v, want capacity capped at 3", r)
	}
}

func TestUsersSeparate(t *testing.T) {
	c := newClock()
	l := New(1, c.now)
	a, b := uuid.New(), uuid.New()
	l.Take(a)
	if r := l.Take(a); r.Allowed {
		t.Fatal("A not limited")
	}
	if r := l.Take(b); !r.Allowed {
		t.Fatal("B limited by A")
	}
}

func TestIdleBucketsDropped(t *testing.T) {
	c := newClock()
	l := New(3, c.now)
	a, b := uuid.New(), uuid.New()
	l.Take(a)
	c.add(30 * time.Minute)
	l.Take(b)
	if n := l.size(); n != 2 {
		t.Fatalf("buckets = %d, want 2", n)
	}
	c.add(31 * time.Minute)
	l.Take(b)
	if n := l.size(); n != 1 {
		t.Fatalf("buckets after A idle 61m = %d, want 1", n)
	}
	if r := l.Take(a); !r.Allowed || r.Remaining != 2 {
		t.Fatalf("A after drop = %+v, want a full bucket", r)
	}
}
