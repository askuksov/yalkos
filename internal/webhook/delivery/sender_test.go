package delivery_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/askuksov/yalkos/internal/buildinfo"
	"github.com/askuksov/yalkos/internal/domain/webhook"
	"github.com/askuksov/yalkos/internal/webhook/delivery"
)

const testEnvelope = `{"action": "update", "type": "Issue", "data": {"id": "i-1", "title": "café <b>"}, "url": null}`

// request is what a receiver saw.
type request struct {
	Method string
	Header http.Header
	Body   []byte
}

// receiver is an httptest server that records requests.
type receiver struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []request
}

func newReceiver(t *testing.T, h http.HandlerFunc) *receiver {
	t.Helper()
	r := &receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.reqs = append(r.reqs, request{Method: req.Method, Header: req.Header.Clone(), Body: body})
		r.mu.Unlock()
		if h != nil {
			h(w, req)
		}
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) requests() []request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]request(nil), r.reqs...)
}

// plainClient follows redirects and sends the usual headers; it never reads
// the proxy variables.
var plainClient = &http.Client{Transport: &http.Transport{}}

func attempt(url, secret string) webhook.Attempt {
	return webhook.Attempt{
		DeliveryID: uuid.New(), URL: url, Secret: secret, Type: "Issue", Envelope: json.RawMessage(testEnvelope),
	}
}

// hmacHex computes the signature without the code under test.
func hmacHex(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func TestSignMatchesIndependentHMAC(t *testing.T) {
	// RFC 4231 test case 2.
	const want = "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"
	if got := delivery.Sign("Jefe", []byte("what do ya want for nothing?")); got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
	for _, c := range []struct{ secret, body string }{
		{"yal_wh_secret", `{"a":1}`}, {"s", ""}, {"ключ", `{"t":"тест"}`},
	} {
		got := delivery.Sign(c.secret, []byte(c.body))
		if got != hmacHex(c.secret, []byte(c.body)) {
			t.Fatalf("Sign(%q, %q) = %s", c.secret, c.body, got)
		}
		if len(got) != 64 || got != strings.ToLower(got) {
			t.Fatalf("Sign = %q, want 64 lower-case hex characters", got)
		}
	}
	if delivery.Sign("a", []byte("x")) == delivery.Sign("b", []byte("x")) {
		t.Fatal("different secrets gave the same signature")
	}
}

func TestBodyCompactsAndStamps(t *testing.T) {
	at := time.UnixMilli(1793816585123)
	b, err := delivery.Body(json.RawMessage(testEnvelope), at)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"action":"update","type":"Issue","data":{"id":"i-1","title":"café <b>"},"url":null,"webhookTimestamp":1793816585123}`
	if string(b) != want {
		t.Fatalf("Body = %s\nwant   %s", b, want)
	}
	if b, err = delivery.Body(json.RawMessage(`{}`), at); err != nil || string(b) != `{"webhookTimestamp":1793816585123}` {
		t.Fatalf("Body({}) = %s, %v", b, err)
	}
	for _, bad := range []string{`[1]`, `"x"`, `{`, ``, `null`} {
		if _, err := delivery.Body(json.RawMessage(bad), at); err == nil {
			t.Fatalf("Body(%q) = nil error", bad)
		}
	}
}

func sender(allowPrivate bool, timeout time.Duration) *delivery.HTTPSender {
	return delivery.NewSender(delivery.SenderOptions{AllowPrivate: allowPrivate, Timeout: timeout, UserAgent: "Yalkos-Webhook/dev"})
}

func TestSenderIgnoresEnvProxy(t *testing.T) {
	// Go reads the proxy variables once per process, so the behaviour check
	// below only bites when no earlier code in the run consulted them; this
	// check does not depend on order.
	if tr := sender(true, 0).NewTransport(); tr.Proxy != nil {
		t.Fatal("transport has a proxy function")
	} else if !tr.DisableCompression {
		t.Fatal("transport asks for compressed answers")
	}
	proxy := newReceiver(t, nil)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	target := newReceiver(t, nil)
	// The name is not a loopback name, so the standard library would use a
	// proxy for it; the test resolver maps it to the receiver.
	u := hostURL(t, target, "hook.test")
	resolver := fixedResolver(map[string][]string{"hook.test": {"127.0.0.1"}})

	s := sender(true, 0)
	s.SetResolver(resolver)
	if res := s.Send(context.Background(), attempt(u, "s")); !res.OK() {
		t.Fatalf("result = %+v", res)
	}
	if n := len(target.requests()); n != 1 {
		t.Fatalf("target hits = %d, want 1", n)
	}
	if n := len(proxy.requests()); n != 0 {
		t.Fatalf("proxy hits = %d, want 0", n)
	}

	// Positive control: a client that does read the proxy variable sends the
	// same request to the proxy.
	ctl := &http.Client{Transport: &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return url.Parse(os.Getenv("HTTP_PROXY")) },
	}}
	resp, err := ctl.Post(u, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if n := len(proxy.requests()); n != 1 {
		t.Fatalf("proxy hits for the control = %d, want 1", n)
	}
}

func TestSenderRequest(t *testing.T) {
	rc := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	a := attempt(rc.URL+"/hook?x=1", "yal_wh_secret")
	res := sender(true, 0).Send(context.Background(), a)
	if !res.OK() || res.Status != 200 || res.Err != "" {
		t.Fatalf("result = %+v", res)
	}
	reqs := rc.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	got := reqs[0]
	if got.Method != http.MethodPost {
		t.Fatalf("method = %s", got.Method)
	}
	if bytes.HasPrefix(got.Body, []byte("\xef\xbb\xbf")) {
		t.Fatal("body starts with a BOM")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, got.Body); err != nil || !bytes.Equal(compact.Bytes(), got.Body) {
		t.Fatalf("body is not compact JSON: %s (%v)", got.Body, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(got.Body, &doc); err != nil {
		t.Fatal(err)
	}
	ts := string(doc["webhookTimestamp"])
	if _, err := strconv.ParseInt(ts, 10, 64); err != nil {
		t.Fatalf("webhookTimestamp = %s, want a JSON number of milliseconds", ts)
	}
	if ms, _ := strconv.ParseInt(ts, 10, 64); time.Since(time.UnixMilli(ms)) > 5*time.Second || ms > time.Now().UnixMilli() {
		t.Fatalf("webhookTimestamp = %d is not the send time", ms)
	}
	if res.At.UnixMilli() != mustInt(t, ts) {
		t.Fatalf("Result.At = %d, body stamp = %s", res.At.UnixMilli(), ts)
	}
	want := http.Header{
		"Content-Type":     {"application/json; charset=utf-8"},
		"Accept-Charset":   {"utf-8"},
		"Linear-Delivery":  {a.DeliveryID.String()},
		"Linear-Event":     {"Issue"},
		"Linear-Signature": {hmacHex("yal_wh_secret", got.Body)},
		"Linear-Timestamp": {ts},
		"User-Agent":       {"Yalkos-Webhook/dev"},
		// Added by the HTTP stack for every body, not set by the sender.
		"Content-Length": {strconv.Itoa(len(got.Body))},
	}
	if !reflect.DeepEqual(got.Header, want) {
		t.Fatalf("headers = %v\nwant     %v", got.Header, want)
	}
	if sig := got.Header.Get("Linear-Signature"); len(sig) != 64 || sig != strings.ToLower(sig) {
		t.Fatalf("signature = %q", sig)
	}

	// Positive control: the recorder does see an Accept-Encoding header when a
	// default client sends one, so its absence above means something.
	ctl := newReceiver(t, nil)
	resp, err := plainClient.Get(ctl.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if ctl.requests()[0].Header.Get("Accept-Encoding") == "" {
		t.Fatal("control request carried no Accept-Encoding; the recorder hides headers")
	}
}

func TestSenderDefaults(t *testing.T) {
	s := delivery.NewSender(delivery.SenderOptions{})
	if s.Timeout() != 5*time.Second {
		t.Fatalf("default timeout = %v, want 5s", s.Timeout())
	}
	rc := newReceiver(t, nil)
	delivery.NewSender(delivery.SenderOptions{AllowPrivate: true}).Send(context.Background(), attempt(rc.URL, "s"))
	if got, want := rc.requests()[0].Header.Get("User-Agent"), "Yalkos-Webhook/"+buildinfo.Version; got != want {
		t.Fatalf("default User-Agent = %q, want %q", got, want)
	}
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSenderRestampsEveryCall(t *testing.T) {
	rc := newReceiver(t, nil)
	a := attempt(rc.URL, "s")
	s := sender(true, 0)
	s.Send(context.Background(), a)
	time.Sleep(5 * time.Millisecond)
	s.Send(context.Background(), a)
	reqs := rc.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d", len(reqs))
	}
	t1, t2 := reqs[0].Header.Get("Linear-Timestamp"), reqs[1].Header.Get("Linear-Timestamp")
	if mustInt(t, t2) <= mustInt(t, t1) {
		t.Fatalf("timestamps %s, %s: want strictly increasing", t1, t2)
	}
	if reqs[0].Header.Get("Linear-Delivery") != reqs[1].Header.Get("Linear-Delivery") {
		t.Fatal("delivery id changed between calls")
	}
	if reqs[0].Header.Get("Linear-Signature") == reqs[1].Header.Get("Linear-Signature") {
		t.Fatal("signature did not change with the body")
	}
}

func TestSenderOutcomes(t *testing.T) {
	reply := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}
	}
	slow := func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}
	// A listener that is closed at once: nothing accepts connections there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + ln.Addr().String()
	_ = ln.Close()

	long := strings.Repeat("x", 2000)
	wide := strings.Repeat("€", 2000)
	for _, c := range []struct {
		name    string
		h       http.HandlerFunc
		url     string
		timeout time.Duration
		status  int
		ok      bool
		err     string // exact, or prefix when it ends with "…"
	}{
		{name: "200", h: reply(200, ""), status: 200, ok: true},
		{name: "204", h: reply(204, ""), status: 204, ok: true},
		{name: "299 with a large body", h: reply(299, strings.Repeat("y", 1<<20)), status: 299, ok: true},
		{name: "500", h: reply(500, "boom"), status: 500, err: "HTTP 500: boom"},
		{name: "500 empty body", h: reply(500, ""), status: 500, err: "HTTP 500"},
		{name: "404", h: reply(404, "no such hook\n"), status: 404, err: "HTTP 404: no such hook"},
		{name: "302 not followed", h: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "/elsewhere")
			w.WriteHeader(302)
		}, status: 302, err: "HTTP 302"},
		{name: "timeout", h: slow, timeout: 200 * time.Millisecond, err: "timeout after 200ms"},
		{name: "connection refused", url: closedURL, err: "connection failed: …"},
		{name: "long body cut", h: reply(500, long), status: 500, err: "HTTP 500: " + strings.Repeat("x", 990)},
		{name: "wide characters cut by characters", h: reply(500, wide), status: 500, err: "HTTP 500: " + strings.Repeat("€", 990)},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := c.url
			if u == "" {
				u = newReceiver(t, c.h).URL
			}
			res := sender(true, c.timeout).Send(context.Background(), attempt(u, "s"))
			if res.Status != c.status || res.OK() != c.ok {
				t.Fatalf("result = %+v, want status %d ok %v", res, c.status, c.ok)
			}
			if prefix, isPrefix := strings.CutSuffix(c.err, "…"); isPrefix {
				if !strings.HasPrefix(res.Err, prefix) || len(res.Err) == len(prefix) {
					t.Fatalf("Err = %q, want prefix %q and a cause", res.Err, prefix)
				}
			} else if res.Err != c.err {
				t.Fatalf("Err = %q, want %q", res.Err, c.err)
			}
			if utf8.RuneCountInString(res.Err) > 1000 {
				t.Fatalf("Err has %d characters", utf8.RuneCountInString(res.Err))
			}
			if res.Duration <= 0 {
				t.Fatalf("Duration = %v", res.Duration)
			}
		})
	}

	t.Run("body cut to a total of 1000 characters", func(t *testing.T) {
		res := sender(true, 0).Send(context.Background(), attempt(newReceiver(t, reply(500, long)).URL, "s"))
		if n := utf8.RuneCountInString(res.Err); n != 1000 || !strings.HasPrefix(res.Err, "HTTP 500: xxx") {
			t.Fatalf("Err has %d characters: %.30q", n, res.Err)
		}
	})
	t.Run("timeout stops the exchange", func(t *testing.T) {
		start := time.Now()
		res := sender(true, 150*time.Millisecond).Send(context.Background(), attempt(newReceiver(t, slow).URL, "s"))
		if took := time.Since(start); took > time.Second || res.Status != 0 {
			t.Fatalf("took %v, result %+v", took, res)
		}
	})
}

// TestSenderErrorIsClean covers receivers whose error body would not survive
// storage as JSON or text: NUL bytes, invalid UTF-8 and control characters.
func TestSenderErrorIsClean(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
	}{
		{"clean body keeps its text", "boom", "HTTP 500: boom"},
		{"NUL dropped", "a\x00b", "HTTP 500: ab"},
		{"invalid UTF-8 replaced", "\xff\xfe", "HTTP 500: \ufffd"},
		{"control characters dropped, newline and tab kept", "x\x01\x1b[0m\x7fy\tz\nw", "HTTP 500: x[0my\tz\nw"},
		{"NUL beyond the cut", strings.Repeat("x", 995) + "\x00\x00\x00\x00\x00\x00yy", "HTTP 500: " + strings.Repeat("x", 990)},
	} {
		t.Run(c.name, func(t *testing.T) {
			rc := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(500)
				_, _ = io.WriteString(w, c.body)
			})
			res := sender(true, 0).Send(context.Background(), attempt(rc.URL, "s"))
			if res.Err != c.want {
				t.Fatalf("Err = %q, want %q", res.Err, c.want)
			}
			if strings.ContainsRune(res.Err, 0) || !utf8.ValidString(res.Err) {
				t.Fatalf("Err is not clean: %q", res.Err)
			}
		})
	}
}

func fixedResolver(m map[string][]string) func(context.Context, string) ([]netip.Addr, error) {
	return func(_ context.Context, host string) ([]netip.Addr, error) {
		var out []netip.Addr
		for _, s := range m[host] {
			out = append(out, netip.MustParseAddr(s))
		}
		if out == nil {
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
		return out, nil
	}
}

func hostURL(t *testing.T, r *receiver, host string) string {
	t.Helper()
	u, err := url.Parse(r.URL)
	if err != nil {
		t.Fatal(err)
	}
	return "http://" + net.JoinHostPort(host, u.Port()) + "/"
}

func TestSenderBlocksResolvedMetadataIP(t *testing.T) {
	rc := newReceiver(t, nil)
	resolver := fixedResolver(map[string][]string{
		"metadata.test": {"169.254.169.254"},
		"mapped.test":   {"::ffff:169.254.169.254"},
		"loopback.test": {"127.0.0.1"},
		"mixed.test":    {"169.254.169.254", "127.0.0.1"},
	})
	port := func(host string) string { return hostURL(t, rc, host) }
	for _, c := range []struct {
		name         string
		allowPrivate bool
		url          string
		wantErr      string
		hits         int
	}{
		{"metadata name, private allowed", true, port("metadata.test"), "address not allowed: 169.254.169.254", 0},
		{"metadata name, private denied", false, port("metadata.test"), "address not allowed: 169.254.169.254", 0},
		{"mapped metadata name", true, port("mapped.test"), "address not allowed: 169.254.169.254", 0},
		{"metadata literal", true, port("169.254.169.254"), "address not allowed: 169.254.169.254", 0},
		{"loopback name, private denied", false, port("loopback.test"), "address not allowed: 127.0.0.1", 0},
		{"loopback literal, private denied", false, port("127.0.0.1"), "address not allowed: 127.0.0.1", 0},
		// Positive controls: the same receiver answers when the address is allowed.
		{"loopback name, private allowed", true, port("loopback.test"), "", 1},
		{"loopback literal, private allowed", true, port("127.0.0.1"), "", 1},
		// A blocked address in the answer is never dialed; an allowed one is used.
		{"blocked and allowed answers", true, port("mixed.test"), "", 1},
		{"unknown name", true, port("nowhere.test"), "connection failed: ", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := len(rc.requests())
			s := sender(c.allowPrivate, 0)
			s.SetResolver(resolver)
			res := s.Send(context.Background(), attempt(c.url, "s"))
			if c.wantErr == "" {
				if !res.OK() {
					t.Fatalf("result = %+v, want success", res)
				}
			} else if res.Status != 0 || (c.wantErr == "connection failed: " && !strings.HasPrefix(res.Err, c.wantErr)) ||
				(c.wantErr != "connection failed: " && res.Err != c.wantErr) {
				t.Fatalf("result = %+v, want error %q", res, c.wantErr)
			}
			if got := len(rc.requests()) - before; got != c.hits {
				t.Fatalf("receiver hits = %d, want %d", got, c.hits)
			}
		})
	}
}

func TestSenderDoesNotFollowRedirect(t *testing.T) {
	target := newReceiver(t, nil)
	redirect := func(to string) *receiver {
		return newReceiver(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", to)
			w.WriteHeader(302)
		})
	}

	// Redirect to a receiver the sender could reach: still not followed.
	r1 := redirect(target.URL)
	res := sender(true, 0).Send(context.Background(), attempt(r1.URL, "s"))
	if res.Status != 302 || res.OK() || res.Err != "HTTP 302" {
		t.Fatalf("result = %+v", res)
	}
	if n := len(target.requests()); n != 0 {
		t.Fatalf("redirect target got %d requests, want 0", n)
	}
	// Positive control: a client that follows redirects does reach the target.
	resp, err := plainClient.Post(r1.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if n := len(target.requests()); n != 1 {
		t.Fatalf("following client reached the target %d times, want 1", n)
	}

	// Redirect to the metadata address: one request only, nothing dialed there.
	r2 := redirect("http://169.254.169.254/latest/meta-data/")
	res = sender(true, 0).Send(context.Background(), attempt(r2.URL, "s"))
	if res.Status != 302 || res.Err != "HTTP 302" {
		t.Fatalf("result = %+v", res)
	}
	if n := len(r2.requests()); n != 1 {
		t.Fatalf("redirecting receiver got %d requests, want 1", n)
	}
}

func TestSenderSecretNotInResult(t *testing.T) {
	const secret = "yal_wh_TOPSECRETVALUE"
	rc := newReceiver(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedURL := "http://" + closed.Addr().String()
	_ = closed.Close()
	for _, u := range []string{rc.URL, closedURL, "http://169.254.169.254/"} {
		res := sender(true, 0).Send(context.Background(), attempt(u, secret))
		if s := fmt.Sprintf("%+v", res); strings.Contains(s, secret) {
			t.Fatalf("result shows the secret: %s", s)
		}
	}
	// Positive control: the secret is used, as a signature key.
	reqs := rc.requests()
	if len(reqs) != 1 || reqs[0].Header.Get("Linear-Signature") != hmacHex(secret, reqs[0].Body) {
		t.Fatalf("requests = %v", reqs)
	}
}
