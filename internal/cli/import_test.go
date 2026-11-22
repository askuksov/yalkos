package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/importer/plane"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

const (
	importWorkspace = "acme"
	importKey       = "plane_api_testkey"
)

// miniPlane serves the hand-written fixtures of the importer package as a
// Plane API: lists in one page, attachment details as a redirect to a file.
type miniPlane struct {
	srv *httptest.Server

	mu       sync.Mutex
	key      string
	paged    map[string][]any
	plain    map[string]any
	files    map[string]struct{ Name, ContentType, Body string }
	requests []string
	failAll  bool
	// failFrom > 0 answers 500 to every per-issue request from the failFrom-th on.
	failFrom int
	// onItem runs on every per-issue request with its number (from 1).
	onItem func(n int)
	items  int
}

func newMiniPlane(t *testing.T) *miniPlane {
	t.Helper()
	m := &miniPlane{
		key:   importKey,
		paged: map[string][]any{},
		plain: map[string]any{},
		files: map[string]struct{ Name, ContentType, Body string }{},
	}
	names, err := filepath.Glob(filepath.Join("..", "importer", "plane", "testdata", "plane", "*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("fixtures: %v (%d files)", err, len(names))
	}
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Paged map[string][]any `json:"paged"`
			Plain map[string]any   `json:"plain"`
			Files map[string]struct {
				Name        string `json:"name"`
				ContentType string `json:"contentType"`
				Body        string `json:"body"`
			} `json:"files"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for p, v := range doc.Paged {
			m.paged[p] = v
		}
		for p, v := range doc.Plain {
			m.plain[p] = v
		}
		for p, f := range doc.Files {
			m.files[p] = struct{ Name, ContentType, Body string }{f.Name, f.ContentType, f.Body}
		}
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *miniPlane) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rest, ok := strings.CutPrefix(r.URL.Path, "/storage/"); ok {
		if f, ok := m.files[rest]; ok {
			if f.ContentType != "" {
				w.Header().Set("Content-Type", f.ContentType)
			}
			_, _ = w.Write([]byte(f.Body))
			return
		}
		http.NotFound(w, r)
		return
	}
	path, ok := strings.CutPrefix(r.URL.Path, "/api/v1/workspaces/"+importWorkspace+"/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	m.requests = append(m.requests, path)
	if r.Header.Get("X-API-Key") != m.key {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if m.failAll {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if isItemPath(path) {
		m.items++
		if m.onItem != nil {
			m.onItem(m.items)
		}
		if m.failFrom > 0 && m.items >= m.failFrom {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if _, ok := m.files[path]; ok {
		http.Redirect(w, r, m.srv.URL+"/storage/"+path, http.StatusFound)
		return
	}
	if v, ok := m.plain[path]; ok {
		_ = json.NewEncoder(w).Encode(v)
		return
	}
	if items, ok := m.paged[path]; ok {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": items, "next_cursor": "", "next_page_results": false, "total_count": len(items),
		})
		return
	}
	http.NotFound(w, r)
}

// isItemPath reports whether path asks for one work item or a part of it
// (not the list of work items).
func isItemPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range parts {
		if s == "work-items" && i+1 < len(parts) {
			return true
		}
	}
	return false
}

// itemRequests counts the requests made for one work item (not the lists).
func (m *miniPlane) itemRequests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, p := range m.requests {
		if isItemPath(p) {
			n++
		}
	}
	return n
}

type importEnv struct {
	pool  *pgxpool.Pool
	env   Env
	plane *miniPlane
	args  []string
}

// newImportEnv migrates a fresh database, sets the owner up and returns the
// environment of a run against the mini Plane; setup false leaves the
// database without an owner.
func newImportEnv(t *testing.T, setup bool, key string) *importEnv {
	t.Helper()
	url, pool := testdb.New(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	if setup {
		svc := account.New(pool, time.Now, "http://localhost:8080")
		_ = svc.RefreshSetupCode(ctx)
		if _, _, err := svc.Setup(ctx, account.SetupInput{Code: svc.SetupCode(), WorkspaceName: "W", Name: "Owner",
			Email: "owner@example.com", Password: "owner-password"}, "127.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	e := &importEnv{pool: pool, plane: newMiniPlane(t)}
	dataDir := t.TempDir()
	e.env = Env{Ctx: ctx, Getenv: func(k string) string {
		switch k {
		case "DATABASE_URL":
			return url
		case "DATA_DIR":
			return dataDir
		case "PLANE_API_KEY":
			return key
		}
		return ""
	}}
	e.args = []string{"import", "plane", "--url", e.plane.srv.URL, "--workspace", importWorkspace}
	return e
}

// run runs the command with no waits between retries.
func (e *importEnv) run(extra ...string) (int, string, string) {
	var out, errb bytes.Buffer
	env := e.env
	env.Stdout, env.Stderr = &out, &errb
	opts := plane.ClientOptions{Sleep: func(context.Context, time.Duration) error { return nil }}
	code := importPlaneWith(append(append([]string(nil), e.args[2:]...), extra...), env, opts)
	return code, out.String(), errb.String()
}

// lockFree reports whether the import lock can be taken from outside.
func (e *importEnv) lockFree(t *testing.T) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext('yalkos:import'))").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock(hashtext('yalkos:import'))"); err != nil {
			t.Fatal(err)
		}
	}
	return got
}

func (e *importEnv) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

var importTables = []string{"users", "projects", "states", "labels", "milestones", "issues", "comments", "attachments", "import_sources", "import_map"}

func (e *importEnv) counts(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tb := range importTables {
		out[tb] = e.count(t, tb)
	}
	return out
}

func TestImportNeedsKey(t *testing.T) {
	e := newImportEnv(t, true, "")
	code, _, errOut := e.run()
	if code != 2 || !strings.Contains(errOut, "PLANE_API_KEY") {
		t.Fatalf("exit %d, stderr %q; want 2 naming PLANE_API_KEY", code, errOut)
	}
}

func TestImportUnknownProject(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	code, _, errOut := e.run("--project", "ZZZ")
	if code != 2 || !strings.Contains(errOut, "ZZZ") {
		t.Fatalf("exit %d, stderr %q; want 2 naming ZZZ", code, errOut)
	}
	if n := e.count(t, "projects"); n != 0 {
		t.Errorf("projects = %d, want 0", n)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	before := e.counts(t)
	code, out, errOut := e.run("--dry-run")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"ALP", "BET", "ann@example.invalid"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	after := e.counts(t)
	for tb, n := range before {
		if after[tb] != n {
			t.Errorf("%s rows %d -> %d, want unchanged", tb, n, after[tb])
		}
	}
	if n := e.plane.itemRequests(); n != 0 {
		t.Errorf("%d per-issue requests in a dry run, want 0", n)
	}
	if !strings.Contains(out, "would create") {
		t.Errorf("dry-run report does not say what it would create:\n%s", out)
	}
	if !e.lockFree(t) {
		t.Error("the import lock is still held after the run")
	}
}

func TestImportRun(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	report := filepath.Join(t.TempDir(), "report.json")
	code, out, errOut := e.run("--report", report)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if !strings.Contains(out, "ALP") {
		t.Errorf("stdout lacks the report:\n%s", out)
	}
	if e.count(t, "projects") == 0 || e.count(t, "issues") == 0 {
		t.Fatalf("nothing imported: %v", e.counts(t))
	}
	if !strings.Contains(errOut, "ALP") {
		t.Errorf("stderr has no progress line for ALP:\n%s", errOut)
	}

	type counts struct {
		Milestones, States, Issues, Comments, Attachments int
	}
	type doc struct {
		Source   struct{ URL, Workspace string }
		DryRun   bool
		Projects []struct {
			Key     string
			Created counts
		}
		Users []struct {
			Email    string
			Created  bool
			Disabled bool
		}
		Labels      map[string]json.RawMessage
		Renumbered  []json.RawMessage
		Dropped     map[string]json.RawMessage
		Attachments map[string]json.RawMessage
	}
	read := func() doc {
		raw, err := os.ReadFile(report)
		if err != nil {
			t.Fatal(err)
		}
		var d doc
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("report JSON: %v\n%s", err, raw)
		}
		return d
	}
	d := read()
	if d.Source.Workspace != importWorkspace || d.DryRun || len(d.Projects) == 0 || d.Projects[0].Created.Issues == 0 {
		t.Errorf("report = %+v", d)
	}
	for _, k := range []string{"merged", "typeMapped"} {
		if _, ok := d.Labels[k]; !ok {
			t.Errorf("report labels lacks %q", k)
		}
	}
	for _, k := range []string{"startDates", "extraAssignees", "unknownTags", "cutTexts"} {
		if _, ok := d.Dropped[k]; !ok {
			t.Errorf("report dropped lacks %q", k)
		}
	}

	if !e.lockFree(t) {
		t.Error("the import lock is still held after the run")
	}
	before := e.counts(t)
	if code, _, errOut := e.run("--report", report); code != 0 {
		t.Fatalf("second run exit %d, stderr %q", code, errOut)
	}
	for _, p := range read().Projects {
		if p.Created != (counts{}) {
			t.Errorf("second run created in %s: %+v", p.Key, p.Created)
		}
	}
	if after := e.counts(t); len(after) != len(before) {
		t.Fatal("counts differ in shape")
	} else {
		for tb, n := range before {
			if after[tb] != n {
				t.Errorf("%s rows %d -> %d after the second run", tb, n, after[tb])
			}
		}
	}
}

func TestImportLockHeld(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	ctx := context.Background()
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext('yalkos:import'))"); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := e.run()
	if code != 1 || !strings.Contains(errOut, "another import is running") {
		t.Fatalf("exit %d, stderr %q; want 1 'another import is running'", code, errOut)
	}
	if n := e.count(t, "projects"); n != 0 {
		t.Errorf("projects = %d, want 0", n)
	}
}

func TestImportConflict(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	if _, err := e.pool.Exec(context.Background(), `
		INSERT INTO projects (workspace_id, key, name, created_by, created_at, updated_at)
		SELECT workspace_id, 'ALP', 'Native', id, now(), now() FROM users LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO project_keys (key, project_id) SELECT key, id FROM projects WHERE key = 'ALP'`); err != nil {
		t.Fatal(err)
	}
	before := e.counts(t)
	code, out, errOut := e.run()
	if code != 1 || !strings.Contains(errOut, "ALP") {
		t.Fatalf("exit %d, stderr %q; want 1 naming ALP", code, errOut)
	}
	if !strings.Contains(out, "ALP") {
		t.Errorf("stdout lacks the report:\n%s", out)
	}
	after := e.counts(t)
	for tb, n := range before {
		if after[tb] != n {
			t.Errorf("%s rows %d -> %d, want unchanged", tb, n, after[tb])
		}
	}
}

func TestImportKeyNotPrinted(t *testing.T) {
	e := newImportEnv(t, true, "plane_api_secretvalue")
	e.plane.mu.Lock()
	e.plane.failAll = true
	e.plane.mu.Unlock()
	report := filepath.Join(t.TempDir(), "report.json")
	// The fake checks the header first: the run must fail on the 500, not on a
	// refused key, so the key was sent and still never printed.
	e.plane.key = "plane_api_secretvalue"
	code, out, errOut := e.run("--report", report)
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr %q", code, errOut)
	}
	if strings.Contains(errOut, "401") {
		t.Errorf("the key was refused, the auth path was not exercised: %q", errOut)
	}
	file, _ := os.ReadFile(report)
	for name, text := range map[string]string{"stdout": out, "stderr": errOut, "report file": string(file)} {
		if strings.Contains(text, "secretvalue") {
			t.Errorf("key leaked in %s: %q", name, text)
		}
	}
}

func TestImportNotSetUp(t *testing.T) {
	e := newImportEnv(t, false, importKey)
	code, _, errOut := e.run()
	if code != 1 || !strings.Contains(errOut, "set up") {
		t.Fatalf("exit %d, stderr %q; want 1 'set up' hint", code, errOut)
	}
}

func TestImportUnknownActor(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	code, _, errOut := e.run("--as", "nobody@example.com")
	if code != 1 || !strings.Contains(errOut, "nobody@example.com") {
		t.Fatalf("exit %d, stderr %q; want 1 naming the email", code, errOut)
	}
}

func TestImportUsage(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	for name, args := range map[string][]string{
		"no subcommand":  {"import"},
		"bad subcommand": {"import", "linear"},
		"no url":         {"import", "plane", "--workspace", importWorkspace},
		"no workspace":   {"import", "plane", "--url", e.plane.srv.URL},
		"bad flag":       {"import", "plane", "--nope"},
	} {
		if code, _, _ := run(args, e.env); code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
}

func TestRunHelpListsImport(t *testing.T) {
	_, out, _ := run(nil, Env{})
	for _, s := range []string{"import plane", "--url", "--workspace", "--project", "--as", "--dry-run", "--report", "PLANE_API_KEY"} {
		if !strings.Contains(out, s) {
			t.Errorf("help lacks %q", s)
		}
	}
}

func TestImportDisabledActor(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	if _, err := e.pool.Exec(context.Background(), `UPDATE users SET disabled = true WHERE email = 'owner@example.com'`); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := e.run("--as", "owner@example.com")
	if code != 1 || !strings.Contains(errOut, "disabled") {
		t.Fatalf("exit %d, stderr %q; want 1 'disabled'", code, errOut)
	}
}

func TestImportURLWithUserinfo(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	var out, errb bytes.Buffer
	env := e.env
	env.Stdout, env.Stderr = &out, &errb
	code := importPlaneWith([]string{"--url", "http://someone:hunter2@127.0.0.1:1", "--workspace", importWorkspace}, env, plane.ClientOptions{})
	if code != 2 {
		t.Fatalf("exit %d, want 2; stderr %q", code, errb.String())
	}
	if strings.Contains(out.String()+errb.String(), "hunter2") {
		t.Errorf("password printed: %q %q", out.String(), errb.String())
	}
	if n := e.count(t, "import_sources"); n != 0 {
		t.Errorf("import_sources = %d, want 0", n)
	}
}

func TestImportReportFileBeforeRun(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	missing := filepath.Join(t.TempDir(), "no-such-dir", "report.json")
	code, _, errOut := e.run("--report", missing)
	if code != 2 || !strings.Contains(errOut, "report") {
		t.Fatalf("exit %d, stderr %q; want 2 naming the report file", code, errOut)
	}
	if n := len(e.plane.requests); n != 0 {
		t.Errorf("%d requests to Plane before an unwritable report file was refused, want 0", n)
	}
	if n := e.count(t, "projects"); n != 0 {
		t.Errorf("projects = %d, want 0", n)
	}

	// An existing file is truncated and made private.
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte("old content that is much longer than a report would need to be, to see truncation"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := e.run("--dry-run", "--report", path); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("report file mode %v, want 0600", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Errorf("report file is not the JSON report: %v\n%s", err, raw)
	}
}

func TestImportStoppedReportCountsWhatWasWritten(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	e.plane.mu.Lock()
	e.plane.failFrom = 12 // a few issues in, the Plane requests fail for good
	e.plane.mu.Unlock()
	report := filepath.Join(t.TempDir(), "report.json")
	code, out, errOut := e.run("--report", report)
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr %q", code, errOut)
	}
	if first, _, _ := strings.Cut(out, "\n"); first != "Stopped before the end; counts show what was written." {
		t.Errorf("first stdout line = %q", first)
	}
	issues := e.count(t, "issues")
	if issues == 0 {
		t.Fatal("no issue was written before the stop; the test needs a later failure")
	}
	raw, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var d struct {
		Projects []struct {
			Created struct{ Issues, States, Milestones int }
		}
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	var reported, states, milestones int
	for _, p := range d.Projects {
		reported += p.Created.Issues
		states += p.Created.States
		milestones += p.Created.Milestones
	}
	if reported != issues {
		t.Errorf("report says %d issues created, the database has %d", reported, issues)
	}
	if got := e.count(t, "milestones"); milestones != got {
		t.Errorf("report says %d milestones created, the database has %d", milestones, got)
	}
	if !e.lockFree(t) {
		t.Error("the import lock is still held after the stop")
	}
}

func TestImportInterrupted(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.env.Ctx = ctx
	e.plane.mu.Lock()
	e.plane.onItem = func(n int) {
		if n == 1 {
			cancel()
		}
	}
	e.plane.mu.Unlock()
	code, _, errOut := e.run()
	if code != 1 || !strings.Contains(errOut, "stopped; run again to continue") {
		t.Fatalf("exit %d, stderr %q; want 1 'stopped; run again to continue'", code, errOut)
	}
	if !e.lockFree(t) {
		t.Error("the import lock is still held after Ctrl+C")
	}
}

func TestImportInterruptedBeforeRun(t *testing.T) {
	e := newImportEnv(t, true, importKey)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.env.Ctx = ctx
	code, _, errOut := e.run()
	if code != 1 || !strings.Contains(errOut, "stopped; run again to continue") {
		t.Fatalf("exit %d, stderr %q; want 1 'stopped; run again to continue'", code, errOut)
	}
}

func TestImportReportFileKeptWithoutReport(t *testing.T) {
	// The workspace is not set up: the run stops before there is a report.
	e := newImportEnv(t, false, importKey)
	dir := t.TempDir()
	previous := filepath.Join(dir, "previous.json")
	if err := os.WriteFile(previous, []byte(`{"earlier": "report"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "fresh.json")
	for _, path := range []string{previous, fresh} {
		if code, _, errOut := e.run("--report", path); code != 1 {
			t.Fatalf("exit %d, stderr %q; want 1", code, errOut)
		}
	}
	if raw, err := os.ReadFile(previous); err != nil || string(raw) != `{"earlier": "report"}` {
		t.Errorf("previous report = %q, %v; want it untouched", raw, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	if len(names) != 1 || names[0] != "previous.json" {
		t.Errorf("files = %q, want only previous.json (no empty report, no temporary file)", names)
	}
}
