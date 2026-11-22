// Package plane reads a Plane workspace through Plane's REST API v1.
package plane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/askuksov/yalkos/internal/netguard"
)

const (
	apiKeyHeader     = "X-API-Key"
	pageSize         = "100"
	maxRedirects     = 5
	apiTimeout       = 30 * time.Second
	downloadTimeout  = 5 * time.Minute
	maxRateWait      = 61 * time.Second
	defaultRetryWait = 60 * time.Second
	maxRetryAfter    = 300 // seconds
	drainLimit       = 64 << 10
)

// serverBackoff is the wait before each retry of a 5xx answer or a network
// error; after the last one the error stops the caller.
var serverBackoff = [...]time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

// maxBody caps an API answer; a variable so tests can lower it.
var maxBody int64 = 64 << 20

var (
	errTooManyRedirects = errors.New("too many redirects")
	errRedirectScheme   = errors.New("redirect scheme not allowed")
)

// ErrTooLarge reports an attachment bigger than the allowed size.
var ErrTooLarge = errors.New("file too large")

// StatusError is an HTTP answer that the client does not retry or gave up on.
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string { return "HTTP status " + strconv.Itoa(e.Code) }

// ClientOptions are optional settings; zero values select the defaults.
type ClientOptions struct {
	// HTTP is the client to send requests with. Its redirect policy is replaced
	// with one that follows at most five redirects and never sends the API key on a
	// redirected request.
	HTTP *http.Client
	// Sleep waits for d or until ctx ends. Tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now returns the current time. Tests replace it.
	Now func() time.Time
}

// Client reads one Plane workspace. The API key is sent only in the
// X-API-Key header and never appears in errors.
type Client struct {
	base  string
	host  string
	key   string
	http  *http.Client
	sleep func(ctx context.Context, d time.Duration) error
	now   func() time.Time

	requests atomic.Int64
	mu       sync.Mutex
	pending  time.Duration // pacing wait owed before the next request
}

// ErrBaseURLCredentials reports a base URL with a user name or password.
var ErrBaseURLCredentials = errors.New("base URL must not contain a user name or password")

// CheckBaseURL returns baseURL without surrounding white space if it is an
// absolute http or https URL with no user name, password, query or fragment;
// the API paths are appended to it as they are.
func CheckBaseURL(baseURL string) (string, error) {
	s := strings.TrimSpace(baseURL)
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("base URL must be an absolute http or https URL")
	}
	if u.User != nil {
		return "", ErrBaseURLCredentials
	}
	if u.RawQuery != "" || u.ForceQuery || strings.Contains(s, "#") {
		return "", errors.New("base URL must not have a query or a fragment")
	}
	return s, nil
}

// NewClient returns a client for the workspace slug on the Plane instance at
// baseURL (for example https://plane.example.com), checked by CheckBaseURL.
func NewClient(baseURL, workspace, key string, opts ClientOptions) (*Client, error) {
	s, err := CheckBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("plane: %w", err)
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("plane: %w", err)
	}
	if workspace == "" {
		return nil, errors.New("plane: workspace is empty")
	}
	if key == "" {
		return nil, errors.New("plane: API key is empty")
	}
	c := &Client{
		base:  strings.TrimRight(u.String(), "/") + "/api/v1/workspaces/" + url.PathEscape(workspace),
		host:  u.Host,
		key:   key,
		sleep: opts.Sleep,
		now:   opts.Now,
	}
	var hc *http.Client
	if opts.HTTP != nil {
		clone := *opts.HTTP
		hc = &clone
	} else {
		hc = &http.Client{Transport: guardedTransport(u.Hostname())}
	}
	hc.CheckRedirect = c.checkRedirect
	c.http = hc
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c, nil
}

// guardedTransport dials like the default transport but never connects to
// link-local, metadata or other special addresses. Private and loopback
// addresses are allowed only for baseHost, the Plane host itself (a self-hosted
// Plane usually lives on a private network); any other host, such as a redirect
// target, may not reach them.
func guardedTransport(baseHost string) *http.Transport {
	dial := func(allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   netguard.Control(allowPrivate),
		}).DialContext
	}
	own, other := dial(true), dial(false)
	t := http.DefaultTransport.(*http.Transport).Clone()
	// No proxy from the environment: the dial guard would see only the proxy,
	// which then fetches any redirect target, internal hosts included.
	t.Proxy = nil
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// addr still holds the host name, before resolution.
		if host, _, err := net.SplitHostPort(addr); err == nil && strings.EqualFold(host, baseHost) {
			return own(ctx, network, addr)
		}
		return other(ctx, network, addr)
	}
	return t
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return errTooManyRedirects
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return errRedirectScheme
	}
	// Plane's storage redirect may stay on the same host (and even drop to plain
	// http behind a proxy), so the key never follows a redirect.
	req.Header.Del(apiKeyHeader)
	if req.Response != nil {
		c.noteLimits(req.Response)
	}
	return nil
}

// Requests is the number of HTTP requests sent so far, retries included.
func (c *Client) Requests() int { return int(c.requests.Load()) }

// permanent marks a consume error that a retry cannot fix.
type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// do sends GET rel and hands a 2xx answer to consume, which must close the body
// and call cancel (the per-request timeout) when it is done. 429 waits for
// Retry-After and repeats without limit; 5xx, network errors and consume
// errors are retried after the serverBackoff waits; other statuses stop at once.
func (c *Client) do(ctx context.Context, rel string, query url.Values, timeout time.Duration,
	consume func(resp *http.Response, cancel context.CancelFunc) error) error {
	target := c.base + "/" + rel
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	for retries := 0; ; {
		if err := c.pace(ctx); err != nil {
			return fmt.Errorf("plane %s: %w", rel, err)
		}
		rctx, cancel := context.WithTimeout(ctx, timeout)
		req, err := http.NewRequestWithContext(rctx, http.MethodGet, target, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("plane %s: %w", rel, errors.New("bad request"))
		}
		req.Header.Set(apiKeyHeader, c.key)
		req.Header.Set("Accept", "application/json")
		c.requests.Add(1)

		var failure error
		resp, err := c.http.Do(req)
		switch {
		case err != nil:
			cancel()
			failure = unwrapURLError(err)
			if errors.Is(err, errTooManyRedirects) || errors.Is(err, errRedirectScheme) || errors.Is(err, netguard.ErrBlocked) {
				return fmt.Errorf("plane %s: %w", rel, failure)
			}
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := retryAfter(resp.Header)
			closeBody(resp)
			cancel()
			if err := c.sleep(ctx, wait); err != nil {
				return fmt.Errorf("plane %s: %w", rel, err)
			}
			continue
		case resp.StatusCode >= 500:
			c.noteLimits(resp)
			failure = &StatusError{Code: resp.StatusCode}
			closeBody(resp)
			cancel()
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			c.noteLimits(resp)
			cerr := consume(resp, cancel)
			if cerr == nil {
				return nil
			}
			var perm permanent
			if errors.As(cerr, &perm) {
				return fmt.Errorf("plane %s: %w", rel, cerr)
			}
			failure = cerr
		default:
			c.noteLimits(resp)
			closeBody(resp)
			cancel()
			return fmt.Errorf("plane %s: %w", rel, &StatusError{Code: resp.StatusCode})
		}

		if ctx.Err() != nil {
			return fmt.Errorf("plane %s: %w", rel, ctx.Err())
		}
		if retries >= len(serverBackoff) {
			return fmt.Errorf("plane %s: %w", rel, failure)
		}
		if err := c.sleep(ctx, serverBackoff[retries]); err != nil {
			return fmt.Errorf("plane %s: %w", rel, err)
		}
		retries++
	}
}

// unwrapURLError drops the URL (a redirect target may carry a signature).
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func closeBody(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit))
	_ = resp.Body.Close()
}

// retryAfter reads Retry-After as seconds, from one to 300; missing or invalid
// means 60 s.
func retryAfter(h http.Header) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After")))
	if err != nil {
		return defaultRetryWait
	}
	return time.Duration(min(max(n, 1), maxRetryAfter)) * time.Second
}

// noteLimits records a pacing wait when Plane says the request budget is spent.
// Answers from another host (a storage redirect) are not Plane's.
func (c *Client) noteLimits(resp *http.Response) {
	if resp.Request == nil || resp.Request.URL.Host != c.host {
		return
	}
	if strings.TrimSpace(resp.Header.Get("X-Ratelimit-Remaining")) != "0" {
		return
	}
	wait := maxRateWait
	if reset, err := strconv.ParseFloat(strings.TrimSpace(resp.Header.Get("X-Ratelimit-Reset")), 64); err == nil {
		until := time.Unix(int64(reset), 0).Sub(c.now())
		wait = until.Round(time.Second)
		if wait < until {
			wait += time.Second // round up: never wake before the reset
		}
		wait = min(wait, maxRateWait)
	}
	if wait <= 0 {
		return
	}
	c.mu.Lock()
	c.pending = max(c.pending, wait)
	c.mu.Unlock()
}

// pace spends the wait owed from an exhausted request budget.
func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	wait := c.pending
	c.pending = 0
	c.mu.Unlock()
	if wait <= 0 {
		return ctx.Err()
	}
	return c.sleep(ctx, wait)
}

// page is Plane's pagination envelope.
type page[T any] struct {
	Results         []T    `json:"results"`
	NextCursor      string `json:"next_cursor"`
	NextPageResults bool   `json:"next_page_results"`
}

// decodePage accepts the envelope and, for endpoints that are not paginated, a
// plain array.
func decodePage[T any](body []byte) (page[T], error) {
	var p page[T]
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		return p, json.Unmarshal(body, &p.Results)
	}
	return p, json.Unmarshal(body, &p)
}

// readBody reads an API answer of at most maxBody bytes; a bigger one fails
// with a permanent ErrTooLarge.
func readBody(resp *http.Response, cancel context.CancelFunc) ([]byte, error) {
	defer cancel()
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBody {
		return nil, permanent{fmt.Errorf("answer over %d bytes: %w", maxBody, ErrTooLarge)}
	}
	return body, nil
}

func getList[T any](ctx context.Context, c *Client, rel string) ([]T, error) {
	var all []T
	cursor := ""
	seen := map[string]bool{}
	for {
		q := url.Values{"per_page": {pageSize}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var p page[T]
		err := c.do(ctx, rel, q, apiTimeout, func(resp *http.Response, cancel context.CancelFunc) error {
			body, err := readBody(resp, cancel)
			if err != nil {
				return unwrapURLError(err)
			}
			p, err = decodePage[T](body)
			if err != nil {
				return permanent{fmt.Errorf("decode answer: %w", err)}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		if !p.NextPageResults {
			return all, nil
		}
		if p.NextCursor == "" {
			return nil, fmt.Errorf("plane %s: more results announced but no next cursor", rel)
		}
		if seen[p.NextCursor] {
			return nil, fmt.Errorf("plane %s: cursor %q repeats", rel, p.NextCursor)
		}
		seen[p.NextCursor] = true
		cursor = p.NextCursor
	}
}

func getObject[T any](ctx context.Context, c *Client, rel string) (T, error) {
	var v T
	err := c.do(ctx, rel, nil, apiTimeout, func(resp *http.Response, cancel context.CancelFunc) error {
		body, err := readBody(resp, cancel)
		if err != nil {
			return unwrapURLError(err)
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return permanent{fmt.Errorf("decode answer: %w", err)}
		}
		return nil
	})
	return v, err
}

// Members lists the workspace members.
func (c *Client) Members(ctx context.Context) ([]Member, error) {
	return getList[Member](ctx, c, "members/")
}

// Projects lists the projects the key's owner can see.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	return getList[Project](ctx, c, "projects/")
}

func projectPath(pid, rest string) string {
	return "projects/" + url.PathEscape(pid) + "/" + rest
}

func issuePath(pid, iid, rest string) string {
	return projectPath(pid, "work-items/"+url.PathEscape(iid)+"/"+rest)
}

// ProjectMembers lists the user ids of the project's members.
func (c *Client) ProjectMembers(ctx context.Context, pid string) ([]string, error) {
	members, err := getList[struct {
		ID string `json:"id"`
	}](ctx, c, projectPath(pid, "members/"))
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	return ids, nil
}

// States lists the project's states.
func (c *Client) States(ctx context.Context, pid string) ([]State, error) {
	return getList[State](ctx, c, projectPath(pid, "states/"))
}

// Labels lists the project's labels.
func (c *Client) Labels(ctx context.Context, pid string) ([]Label, error) {
	return getList[Label](ctx, c, projectPath(pid, "labels/"))
}

// Modules lists the project's modules.
func (c *Client) Modules(ctx context.Context, pid string) ([]Module, error) {
	return getList[Module](ctx, c, projectPath(pid, "modules/"))
}

// ModuleIssues lists the ids of the work items in a module.
func (c *Client) ModuleIssues(ctx context.Context, pid, mid string) ([]string, error) {
	entries, err := getList[struct {
		ID    string `json:"id"`
		Issue string `json:"issue"`
	}](ctx, c, projectPath(pid, "modules/"+url.PathEscape(mid)+"/module-issues/"))
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.Issue
		if ids[i] == "" {
			ids[i] = e.ID
		}
	}
	return ids, nil
}

// WorkItems lists the project's work items.
func (c *Client) WorkItems(ctx context.Context, pid string) ([]WorkItem, error) {
	return getList[WorkItem](ctx, c, projectPath(pid, "work-items/"))
}

// WorkItem reads one work item.
func (c *Client) WorkItem(ctx context.Context, pid, iid string) (WorkItem, error) {
	return getObject[WorkItem](ctx, c, issuePath(pid, iid, ""))
}

// Comments lists the comments of a work item.
func (c *Client) Comments(ctx context.Context, pid, iid string) ([]Comment, error) {
	return getList[Comment](ctx, c, issuePath(pid, iid, "comments/"))
}

// Attachments lists the files attached to a work item.
func (c *Client) Attachments(ctx context.Context, pid, iid string) ([]Attachment, error) {
	return getList[Attachment](ctx, c, issuePath(pid, iid, "attachments/"))
}

// Links lists the web links of a work item.
func (c *Client) Links(ctx context.Context, pid, iid string) ([]Link, error) {
	return getList[Link](ctx, c, issuePath(pid, iid, "links/"))
}

// Relations returns the relations of a work item, by kind.
func (c *Client) Relations(ctx context.Context, pid, iid string) (Relations, error) {
	return getObject[Relations](ctx, c, issuePath(pid, iid, "relations/"))
}

// Download is an attachment body. The caller closes Body.
type Download struct {
	Name        string
	ContentType string
	Body        io.ReadCloser
}

// Download fetches an attachment (or an image of a description) by following
// Plane's redirect to the file. A file bigger than limit bytes (limit > 0)
// fails with ErrTooLarge, up front when its size is announced, otherwise while
// the body is read.
func (c *Client) Download(ctx context.Context, pid, iid, assetID string, limit int64) (Download, error) {
	var d Download
	err := c.do(ctx, issuePath(pid, iid, "attachments/"+url.PathEscape(assetID)+"/"), nil, downloadTimeout,
		func(resp *http.Response, cancel context.CancelFunc) error {
			if limit > 0 && resp.ContentLength > limit {
				_ = resp.Body.Close()
				cancel()
				return permanent{ErrTooLarge}
			}
			var body io.ReadCloser = &cancelCloser{ReadCloser: resp.Body, cancel: cancel}
			if limit > 0 {
				body = &limitBody{rc: body, left: limit}
			}
			d = Download{Name: fileName(resp), ContentType: resp.Header.Get("Content-Type"), Body: body}
			return nil
		})
	return d, err
}

// fileName takes the name from Content-Disposition, else from the URL path.
func fileName(resp *http.Response) string {
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		return params["filename"]
	}
	if resp.Request != nil && resp.Request.URL != nil {
		if base := path.Base(resp.Request.URL.Path); base != "." && base != "/" {
			return base
		}
	}
	return ""
}

type cancelCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// limitBody fails with ErrTooLarge when more than left bytes can be read.
type limitBody struct {
	rc   io.ReadCloser
	left int64
}

func (l *limitBody) Read(p []byte) (int, error) {
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.rc.Read(p)
	if int64(n) > l.left {
		n = int(l.left)
		l.left = 0
		return n, ErrTooLarge
	}
	l.left -= int64(n)
	return n, err
}

func (l *limitBody) Close() error { return l.rc.Close() }
