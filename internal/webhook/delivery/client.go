package delivery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/askuksov/yalkos/internal/buildinfo"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/netguard"
)

const (
	defaultTimeout = 5 * time.Second
	// maxResponse is how much of a response is read.
	maxResponse = 64 << 10
	// maxErrorChars is the longest last_error.
	maxErrorChars = 1000
)

// SenderOptions configure an HTTPSender. Timeout (5 s when zero) covers the
// whole exchange. UserAgent defaults to Yalkos-Webhook/<version>. Now is the
// clock that stamps the body (time.Now when nil).
type SenderOptions struct {
	AllowPrivate bool
	Timeout      time.Duration
	UserAgent    string
	Now          func() time.Time
}

// HTTPSender posts attempts over HTTP. It implements webhook.Sender.
type HTTPSender struct {
	allowPrivate bool
	timeout      time.Duration
	userAgent    string
	now          func() time.Time
	// resolve looks a host name up; a field so that tests can answer with
	// chosen addresses.
	resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

var _ webhook.Sender = (*HTTPSender)(nil)

// NewSender builds a sender.
func NewSender(o SenderOptions) *HTTPSender {
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.UserAgent == "" {
		o.UserAgent = "Yalkos-Webhook/" + buildinfo.Version
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &HTTPSender{
		allowPrivate: o.AllowPrivate, timeout: o.Timeout, userAgent: o.UserAgent, now: o.Now,
		resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
	}
}

// dial resolves the host itself and dials a resolved address, so the address
// checked by the dialer's control hook is the address connected to.
func (s *HTTPSender) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []netip.Addr
	if ip, perr := netip.ParseAddr(host); perr == nil {
		ips = []netip.Addr{ip}
	} else if ips, err = s.resolve(ctx, host); err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	d := &net.Dialer{Control: netguard.Control(s.allowPrivate)}
	var first error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		if errors.Is(err, netguard.ErrBlocked) {
			// Keep the guard's own error: "address not allowed: <ip>".
			var oe *net.OpError
			if errors.As(err, &oe) {
				err = oe.Err
			}
		}
		if first == nil {
			first = err
		}
	}
	return nil, first
}

// newTransport builds the transport of one attempt. Proxy stays nil, so the
// environment is not consulted (a proxy would hide the dialed address);
// compression stays off so that no Accept-Encoding header is added.
func (s *HTTPSender) newTransport() *http.Transport {
	return &http.Transport{DialContext: s.dial, DisableCompression: true}
}

// Send posts one attempt. The body gets a fresh webhookTimestamp and is
// signed as sent; redirects and proxies are never followed.
func (s *HTTPSender) Send(ctx context.Context, a webhook.Attempt) webhook.Result {
	at := s.now()
	started := time.Now()
	res := webhook.Result{At: at}
	finish := func() webhook.Result {
		res.Duration = time.Since(started)
		res.Err = cut(res.Err, maxErrorChars)
		return res
	}

	body, err := Body(a.Envelope, at)
	if err != nil {
		res.Err = "invalid payload: " + err.Error()
		return finish()
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytes.NewReader(body))
	if err != nil {
		res.Err = "connection failed: " + unwrapURL(err).Error()
		return finish()
	}
	h := req.Header
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Accept-Charset", "utf-8")
	h.Set("Linear-Delivery", a.DeliveryID.String())
	h.Set("Linear-Event", a.Type)
	h.Set("Linear-Signature", Sign(a.Secret, body))
	h.Set("Linear-Timestamp", strconv.FormatInt(at.UnixMilli(), 10))
	h.Set("User-Agent", s.userAgent)

	// A transport per attempt: nothing is reused, so every attempt dials and
	// is checked anew.
	tr := s.newTransport()
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		res.Err = s.failure(err)
		return finish()
	}
	defer func() { _ = resp.Body.Close() }()
	res.Status = resp.StatusCode
	// A cut or slow body does not change the answer: the status is known.
	text, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if !res.OK() {
		res.Err = httpError(resp.StatusCode, string(text))
	}
	return finish()
}

// failure words an error of the exchange.
func (s *HTTPSender) failure(err error) string {
	err = unwrapURL(err)
	var ne net.Error
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		return err.Error()
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return "timeout after " + s.timeout.String()
	}
	return "connection failed: " + err.Error()
}

func unwrapURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// httpError is the last_error of a non-2xx answer: the status and the start
// of the body.
func httpError(status int, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, body)
}

// cut limits s to limit characters.
func cut(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit])
	}
	return s
}
