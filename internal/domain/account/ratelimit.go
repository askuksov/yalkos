package account

import (
	"crypto/sha256"
	"net"
	"sync"
	"time"
)

// attemptLimiter allows at most max failures per key within window. A slot is
// reserved before the (slow) check runs, so parallel requests cannot all pass
// the limit at once. Keys are hashed to a fixed size.
type attemptLimiter struct {
	mu        sync.Mutex
	max       int
	window    time.Duration
	keys      map[[32]byte]*attempts
	lastSweep time.Time
	now       func() time.Time
}

type attempts struct {
	fails    []time.Time
	inflight int
}

func newAttemptLimiter(limit int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{max: limit, window: window, keys: map[[32]byte]*attempts{}, now: time.Now}
}

// reserve takes a slot for one attempt; false means the key is rate limited.
func (l *attemptLimiter) reserve(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	k := sha256.Sum256([]byte(key))
	a := l.keys[k]
	if a == nil {
		a = &attempts{}
		l.keys[k] = a
	}
	a.prune(now, l.window)
	if len(a.fails)+a.inflight >= l.max {
		return false
	}
	a.inflight++
	return true
}

// finish releases a reserved slot; failed attempts are counted for window.
func (l *attemptLimiter) finish(key string, failed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.keys[sha256.Sum256([]byte(key))]
	if a == nil {
		return
	}
	if a.inflight > 0 {
		a.inflight--
	}
	if failed {
		a.fails = append(a.fails, l.now())
	}
}

func (a *attempts) prune(now time.Time, window time.Duration) {
	i := 0
	for i < len(a.fails) && now.Sub(a.fails[i]) >= window {
		i++
	}
	a.fails = a.fails[i:]
}

// sweep drops idle keys, at most once a minute.
func (l *attemptLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, a := range l.keys {
		a.prune(now, l.window)
		if len(a.fails) == 0 && a.inflight == 0 {
			delete(l.keys, k)
		}
	}
}

// clientNet groups IPv6 clients by /64 (one customer usually owns a whole /64).
func clientNet(ip string) string {
	p := net.ParseIP(ip)
	if p == nil {
		return ip
	}
	if v4 := p.To4(); v4 != nil {
		return v4.String()
	}
	return p.Mask(net.CIDRMask(64, 128)).String() + "/64"
}
