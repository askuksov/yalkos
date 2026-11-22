package plane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	fakeWorkspace = "acme"
	fakeKey       = "plane_api_testkey"
)

// fakeFile is an attachment body served behind the 302 of an attachment detail.
type fakeFile struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	// Length announces the body size in Content-Length (a large body is
	// otherwise sent chunked).
	Length bool `json:"length"`
}

// fakeFailure makes the next Times requests to a path answer with Status.
// Times < 0 means every request.
type fakeFailure struct {
	status int
	times  int
}

// fakePlane is an in-memory Plane API v1. Data is keyed by the path relative
// to the workspace root, with a trailing slash (for example
// "projects/p1/work-items/"). It is shared by the tests of the importer.
type fakePlane struct {
	t   *testing.T
	srv *httptest.Server
	key string

	mu         sync.Mutex
	paged      map[string][]any
	plain      map[string]any
	files      map[string]fakeFile
	failures   map[string]*fakeFailure
	limited    map[string]string
	extra      map[string]http.Header
	redirectTo string
	requests   map[string]int
	queries    map[string][]url.Values
	keys       map[string][]string
	hooks      map[string]func()
}

func newFakePlane(t *testing.T) *fakePlane {
	t.Helper()
	f := &fakePlane{
		t:        t,
		key:      fakeKey,
		paged:    map[string][]any{},
		plain:    map[string]any{},
		files:    map[string]fakeFile{},
		failures: map[string]*fakeFailure{},
		limited:  map[string]string{},
		extra:    map[string]http.Header{},
		requests: map[string]int{},
		queries:  map[string][]url.Values{},
		keys:     map[string][]string{},
		hooks:    map[string]func(){},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// URL is the base URL of the fake server.
func (f *fakePlane) URL() string { return f.srv.URL }

// Requests counts the requests the fake saw for a relative path.
func (f *fakePlane) Requests(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[path]
}

// Queries returns the query strings of the requests for a relative path.
func (f *fakePlane) Queries(path string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.queries[path]...)
}

// setPaged serves items from a list endpoint with Plane's pagination envelope.
func (f *fakePlane) setPaged(path string, items ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paged[path] = items
}

// setPlain serves v as is: a plain array or a single object.
func (f *fakePlane) setPlain(path string, v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plain[path] = v
}

// setFile makes the attachment detail at path answer 302 to a file.
func (f *fakePlane) setFile(path string, file fakeFile) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = file
}

// failOn makes the next times requests to path answer with status.
func (f *fakePlane) failOn(path string, status, times int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[path] = &fakeFailure{status: status, times: times}
}

// rateLimitOnce makes the first request to path answer 429, with a Retry-After
// header when retryAfter is not empty.
func (f *fakePlane) rateLimitOnce(path, retryAfter string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limited[path] = retryAfter
	f.failures[path] = &fakeFailure{status: http.StatusTooManyRequests, times: 1}
}

// headerOn adds a response header to every answer for path.
func (f *fakePlane) headerOn(path, name, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.extra[path] == nil {
		f.extra[path] = http.Header{}
	}
	f.extra[path].Set(name, value)
}

// onRequest runs fn once, when the next request for path arrives (before it
// is answered); it simulates a change in Yalkos in the middle of a run.
func (f *fakePlane) onRequest(path string, fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks[path] = fn
}

// redirectAttachment sends attachment details to a full URL instead of the
// fake's own storage path.
func (f *fakePlane) redirectAttachment(to string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.redirectTo = to
}

func (f *fakePlane) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if rest, ok := strings.CutPrefix(r.URL.Path, "/storage/"); ok {
		f.keys["storage/"+rest] = append(f.keys["storage/"+rest], r.Header.Get("X-API-Key"))
		f.serveStorage(w, rest)
		return
	}
	prefix := "/api/v1/workspaces/" + fakeWorkspace + "/"
	path, ok := strings.CutPrefix(r.URL.Path, prefix)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f.requests[path]++
	if hook := f.hooks[path]; hook != nil {
		delete(f.hooks, path)
		hook()
	}
	f.queries[path] = append(f.queries[path], r.URL.Query())
	f.keys[path] = append(f.keys[path], r.Header.Get("X-API-Key"))
	for name, vals := range f.extra[path] {
		for _, v := range vals {
			w.Header().Add(name, v)
		}
	}
	if r.Header.Get("X-API-Key") != f.key {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if fl := f.failures[path]; fl != nil && fl.times != 0 {
		if fl.times > 0 {
			fl.times--
		}
		if ra, limited := f.limited[path]; limited && fl.status == http.StatusTooManyRequests && ra != "" {
			w.Header().Set("Retry-After", ra)
		}
		w.WriteHeader(fl.status)
		return
	}
	if _, ok := f.files[path]; ok {
		to := f.redirectTo
		if to == "" {
			to = f.srv.URL + "/storage/" + path
		}
		http.Redirect(w, r, to, http.StatusFound)
		return
	}
	if v, ok := f.plain[path]; ok {
		writeJSON(w, v)
		return
	}
	if items, ok := f.paged[path]; ok {
		f.servePage(w, r, items)
		return
	}
	http.NotFound(w, r)
}

func (f *fakePlane) serveStorage(w http.ResponseWriter, path string) {
	file, ok := f.files[path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if file.ContentType != "" {
		w.Header().Set("Content-Type", file.ContentType)
	}
	if file.Length {
		w.Header().Set("Content-Length", strconv.Itoa(len(file.Body)))
	}
	if file.Name != "" {
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(file.Name))
	}
	_, _ = w.Write([]byte(file.Body))
}

// servePage cuts items by the per_page and cursor ("<per_page>:<page>:0")
// query parameters like Plane does.
func (f *fakePlane) servePage(w http.ResponseWriter, r *http.Request, items []any) {
	per := 1000
	if v, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && v > 0 {
		per = v
	}
	page := 0
	if parts := strings.Split(r.URL.Query().Get("cursor"), ":"); len(parts) == 3 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v >= 0 {
			page = v
		}
	}
	from := min(page*per, len(items))
	to := min(from+per, len(items))
	more := to < len(items)
	next := ""
	if more {
		next = fmt.Sprintf("%d:%d:0", per, page+1)
	}
	writeJSON(w, map[string]any{
		"results":           append([]any{}, items[from:to]...),
		"next_cursor":       next,
		"next_page_results": more,
		"total_count":       len(items),
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// loadFixture returns a fake filled from every *.json file of dir. A file is an
// object:
//
//	{"paged": {"projects/": [...]}, "plain": {"members/": [...]},
//	 "files": {"projects/p/work-items/i/attachments/a/": {"name": "...", "body": "..."}}}
func loadFixture(t *testing.T, dir string) *fakePlane {
	t.Helper()
	f := newFakePlane(t)
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Paged map[string][]any    `json:"paged"`
			Plain map[string]any      `json:"plain"`
			Files map[string]fakeFile `json:"files"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for p, items := range doc.Paged {
			f.setPaged(p, items...)
		}
		for p, v := range doc.Plain {
			f.setPlain(p, v)
		}
		for p, file := range doc.Files {
			f.setFile(p, file)
		}
	}
	return f
}
