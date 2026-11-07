package delivery

import (
	"context"
	"net/http"
	"net/netip"
	"time"
)

// SetResolver replaces the host name lookup of the sender.
func (s *HTTPSender) SetResolver(f func(ctx context.Context, host string) ([]netip.Addr, error)) {
	s.resolve = f
}

// Timeout returns the time allowed for one exchange.
func (s *HTTPSender) Timeout() time.Duration { return s.timeout }

// NewTransport returns the transport an attempt would use.
func (s *HTTPSender) NewTransport() *http.Transport { return s.newTransport() }
