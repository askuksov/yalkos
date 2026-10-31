package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/askuksov/yalkos/internal/authn"
	"github.com/askuksov/yalkos/internal/blob"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/domain/attachment"
	"github.com/askuksov/yalkos/internal/domain/issue"
	"github.com/askuksov/yalkos/internal/domain/project"
	"github.com/askuksov/yalkos/internal/testfx"
)

const testBase = "http://yalkos.test"

type fileEnv struct {
	h      http.Handler
	pool   *pgxpool.Pool
	dir    string
	cookie string
	issue  issue.Issue
}

func newFileEnv(t *testing.T, maxBytes int64) fileEnv {
	t.Helper()
	ctx := context.Background()
	pool, users, _ := testfx.New(t)
	quiet := slog.New(slog.NewJSONHandler(io.Discard, nil))
	accounts := account.New(pool, time.Now, testBase)
	projects := project.New(pool, time.Now)
	issues := issue.New(pool, projects, time.Now)
	dir := t.TempDir()
	store, err := blob.NewLocalFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	atts := attachment.New(pool, issues, store, &blob.Locks{}, maxBytes, time.Now)
	base, _ := url.Parse(testBase)
	h := newTestRouter(t, func(d *Deps) {
		d.Attachments = atts
		d.Auth = authn.Middleware(accounts, base, quiet)
	})
	_, token, err := accounts.Login(ctx, "owner@example.com", "owner-password", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	p, err := projects.Create(ctx, users.Admin, project.CreateInput{Key: "YLK", Name: "Yalkos"})
	if err != nil {
		t.Fatal(err)
	}
	i, err := issues.Create(ctx, users.Member, issue.CreateInput{ProjectID: &p.ID, Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	return fileEnv{h: h, pool: pool, dir: dir, cookie: authn.CookieName + "=" + token, issue: i}
}

// uploadReq builds a multipart upload; issueID "" leaves the field out.
func uploadReq(t *testing.T, name string, data []byte, issueID string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if issueID != "" {
		_ = mw.WriteField("issueId", issueID)
	}
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

func (e fileEnv) send(r *http.Request, withCookie bool) *httptest.ResponseRecorder {
	if withCookie {
		r.Header.Set("Cookie", e.cookie)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

type uploaded struct {
	ID, URL, Name, ContentType string
	Size                       int64
	Inline                     bool
}

func (e fileEnv) upload(t *testing.T, name string, data []byte) uploaded {
	t.Helper()
	rec := e.send(uploadReq(t, name, data, e.issue.ID.String()), true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %s = %d %s", name, rec.Code, rec.Body)
	}
	var u uploaded
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func (e fileEnv) download(t *testing.T, u string) *httptest.ResponseRecorder {
	t.Helper()
	return e.send(httptest.NewRequest(http.MethodGet, u, nil), true)
}

var pngData = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestUploadAndDownload(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	u := e.upload(t, "shot one.png", pngData)
	if u.URL != "/api/attachments/"+u.ID+"/shot%20one.png" || u.Name != "shot one.png" || u.ContentType != "image/png" ||
		u.Size != int64(len(pngData)) || !u.Inline {
		t.Fatalf("response = %+v", u)
	}
	rec := e.download(t, u.URL)
	h := rec.Header()
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pngData) {
		t.Fatalf("GET = %d %q", rec.Code, rec.Body)
	}
	if h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Content-Security-Policy") != "sandbox" || h.Get("Content-Disposition") != `inline; filename*=UTF-8''shot%20one.png` ||
		h.Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Errorf("headers = %v", h)
	}
	// The name in the URL is ignored.
	if rec := e.download(t, "/api/attachments/"+u.ID+"/other.png"); rec.Code != http.StatusOK {
		t.Errorf("other name = %d", rec.Code)
	}
	// Ranges are served.
	r := httptest.NewRequest(http.MethodGet, u.URL, nil)
	r.Header.Set("Range", "bytes=0-3")
	if rec := e.send(r, true); rec.Code != http.StatusPartialContent || rec.Body.Len() != 4 {
		t.Errorf("range = %d %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := e.download(t, "/api/attachments/not-a-uuid/x"); rec.Code != http.StatusNotFound {
		t.Errorf("bad id = %d", rec.Code)
	}
	if rec := e.download(t, "/api/attachments/01a19370-363e-70ca-88e0-7433cda3a849/x"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id = %d", rec.Code)
	}
}

func TestDisguisedHTMLServedAsDownload(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	u := e.upload(t, "shot.png", []byte("<html><script>alert(document.cookie)</script></html>"))
	if u.Inline || u.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("response = %+v", u)
	}
	h := e.download(t, u.URL).Header()
	if h.Get("Content-Disposition") != `attachment; filename*=UTF-8''shot.png` || h.Get("Content-Security-Policy") != "sandbox" ||
		h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", h)
	}
}

func TestSVGServedAsDownload(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	for _, svg := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`,
	} {
		u := e.upload(t, "logo.svg", []byte(svg))
		h := e.download(t, u.URL).Header()
		if u.Inline || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") || h.Get("Content-Security-Policy") != "sandbox" {
			t.Errorf("%q: inline %v headers %v", svg, u.Inline, h)
		}
	}
}

func TestPDFInlineWithoutSandbox(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	u := e.upload(t, "doc.pdf", []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"))
	h := e.download(t, u.URL).Header()
	if !u.Inline || h.Get("Content-Type") != "application/pdf" || h.Get("Content-Disposition") != `inline; filename*=UTF-8''doc.pdf` ||
		h.Get("Content-Security-Policy") != "" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("inline %v headers %v", u.Inline, h)
	}
}

func TestUploadTooLarge(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	rec := e.send(uploadReq(t, "big.bin", bytes.Repeat([]byte("x"), 2<<20), ""), true)
	var body struct {
		Error, Reason string
		Params        map[string]any
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusRequestEntityTooLarge || body.Reason != "too_large" || body.Params["maxMb"] != float64(1) {
		t.Fatalf("= %d %s", rec.Code, rec.Body)
	}
	// Just over the file limit but within the body allowance: the service refuses it.
	rec = e.send(uploadReq(t, "big.bin", bytes.Repeat([]byte("x"), 1<<20+10), ""), true)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("slightly over = %d %s", rec.Code, rec.Body)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.dir, "tmp")); len(entries) != 0 {
		t.Errorf("tmp has %d files", len(entries))
	}
}

func TestDownloadRequiresAuth(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	u := e.upload(t, "shot.png", pngData)
	for _, r := range []*http.Request{
		httptest.NewRequest(http.MethodGet, u.URL, nil),
		uploadReq(t, "x.png", pngData, ""),
	} {
		rec := e.send(r, false)
		if rec.Code != http.StatusUnauthorized || strings.TrimSpace(rec.Body.String()) != `{"error":"authentication required"}` {
			t.Errorf("%s = %d %s", r.Method, rec.Code, rec.Body)
		}
	}
	r := httptest.NewRequest(http.MethodGet, u.URL, nil)
	r.Header.Set("Cookie", authn.CookieName+"=bogus")
	if rec := e.send(r, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad session = %d", rec.Code)
	}
}

func TestUploadRejectsForeignOrigin(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	r := uploadReq(t, "x.png", pngData, "")
	r.Header.Set("Origin", "https://evil.example")
	if rec := e.send(r, true); rec.Code != http.StatusForbidden {
		t.Errorf("foreign origin = %d", rec.Code)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM attachments`).Scan(&n); err != nil || n != 0 {
		t.Errorf("attachments stored = %d, %v", n, err)
	}
	r = uploadReq(t, "x.png", pngData, "")
	r.Header.Set("Origin", testBase)
	if rec := e.send(r, true); rec.Code != http.StatusCreated {
		t.Errorf("own origin = %d %s", rec.Code, rec.Body)
	}
}

func TestUploadNameSanitized(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	u := e.upload(t, "../../a\tb.png", pngData)
	if u.Name != "ab.png" {
		t.Errorf("name = %q, want ab.png", u.Name)
	}
}

func TestUploadMalformedMultipart(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	body := "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a\x01.png\"\r\n\r\nxx\r\n--b--\r\n"
	r := httptest.NewRequest(http.MethodPost, "/api/attachments", strings.NewReader(body))
	r.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	if rec := e.send(r, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"reason":"invalid_format"`) {
		t.Errorf("malformed = %d %s", rec.Code, rec.Body)
	}
}

func TestUploadBadInput(t *testing.T) {
	e := newFileEnv(t, 1<<20)
	if rec := e.send(uploadReq(t, "x.png", pngData, "nope"), true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"issueId"`) {
		t.Errorf("bad issueId = %d %s", rec.Code, rec.Body)
	}
	if rec := e.send(uploadReq(t, "x.png", pngData, "01a19370-363e-70ca-88e0-7433cda3a849"), true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"reason":"not_found"`) {
		t.Errorf("unknown issue = %d %s", rec.Code, rec.Body)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/attachments", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	if rec := e.send(r, true); rec.Code != http.StatusBadRequest {
		t.Errorf("not multipart = %d", rec.Code)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("issueId", e.issue.ID.String())
	_ = mw.Close()
	r = httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if rec := e.send(r, true); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"reason":"required"`) {
		t.Errorf("no file = %d %s", rec.Code, rec.Body)
	}
}
