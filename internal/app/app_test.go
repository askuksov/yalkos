package app_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/askuksov/yalkos/db"
	"github.com/askuksov/yalkos/internal/app"
	"github.com/askuksov/yalkos/internal/config"
	"github.com/askuksov/yalkos/internal/domain/account"
	"github.com/askuksov/yalkos/internal/store"
	"github.com/askuksov/yalkos/internal/testdb"
)

var quiet = slog.New(slog.NewJSONHandler(io.Discard, nil))

type started struct {
	addr  string
	errCh chan error
}

// start runs app.Run in the background and waits until it listens.
func start(ctx context.Context, t *testing.T, cfg config.Config) started {
	t.Helper()
	addrCh := make(chan string, 1)
	errCh := make(chan error, 1)
	ui := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	go func() { errCh <- app.Run(ctx, cfg, ui, quiet, func(a string) { addrCh <- a }) }()
	select {
	case a := <-addrCh:
		return started{addr: a, errCh: errCh}
	case err := <-errCh:
		t.Fatalf("Run returned before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not start listening within 5s")
	}
	return started{}
}

func get(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestRunServesAndReady(t *testing.T) {
	url, _ := testdb.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := start(ctx, t, config.Config{DatabaseURL: url, ListenAddr: "127.0.0.1:0", DataDir: t.TempDir()})
	if code := get(t, "http://"+s.addr+"/readyz"); code != 200 {
		t.Errorf("/readyz = %d, want 200", code)
	}
	if code := get(t, "http://"+s.addr+"/healthz"); code != 200 {
		t.Errorf("/healthz = %d, want 200", code)
	}
}

func TestRunServesAttachments(t *testing.T) {
	url, _ := testdb.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := filepath.Join(t.TempDir(), "data")
	s := start(ctx, t, config.Config{DatabaseURL: url, ListenAddr: "127.0.0.1:0", DataDir: dir, MaxUploadMB: 1})
	resp, err := http.Post("http://"+s.addr+"/api/attachments", "multipart/form-data; boundary=x", strings.NewReader("--x--"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /api/attachments = %d, want 401", resp.StatusCode)
	}
	for _, d := range []string{dir, filepath.Join(dir, "blobs"), filepath.Join(dir, "tmp")} {
		if info, err := os.Stat(d); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", d, info, err)
		}
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	url, _ := testdb.New(t)
	ctx, cancel := context.WithCancel(context.Background())

	s := start(ctx, t, config.Config{DatabaseURL: url, ListenAddr: "127.0.0.1:0", DataDir: t.TempDir()})
	cancel()
	select {
	case err := <-s.errCh:
		if err != nil {
			t.Errorf("Run = %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop within 10s")
	}
}

func TestRunFailsOnBadDatabase(t *testing.T) {
	var listened atomic.Bool
	done := make(chan error, 1)
	cfg := config.Config{DatabaseURL: "postgres://u:p@127.0.0.1:1/x?sslmode=disable", ListenAddr: "127.0.0.1:0", DataDir: t.TempDir()}
	go func() {
		done <- app.Run(context.Background(), cfg, fstest.MapFS{}, quiet, func(string) { listened.Store(true) })
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Run = nil, want error")
		}
		if listened.Load() {
			t.Error("onListen called despite DB failure")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Run did not fail within 6s")
	}
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *syncBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buf.String() }

func runAndCaptureLogs(t *testing.T, cfg config.Config) string {
	t.Helper()
	var logs syncBuffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() { errCh <- app.Run(ctx, cfg, fstest.MapFS{}, logger, func(a string) { addrCh <- a }) }()
	select {
	case <-addrCh:
	case err := <-errCh:
		t.Fatalf("Run: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("not listening within 5s")
	}
	cancel()
	<-errCh
	return logs.String()
}

func TestRunLogsSetupCode(t *testing.T) {
	url, _ := testdb.New(t)
	logs := runAndCaptureLogs(t, config.Config{DatabaseURL: url, ListenAddr: "127.0.0.1:0", DataDir: t.TempDir()})
	if !strings.Contains(logs, `"msg":"setup required"`) {
		t.Fatalf("no setup log: %s", logs)
	}
	if !regexp.MustCompile(`/setup\?code=[ABCDEFGHJKMNPQRSTUVWXYZ2-9]{8}"`).MatchString(logs) {
		t.Errorf("setup URL with 8-char code not logged: %s", logs)
	}
}

func TestRunNoSetupLogAfterSetup(t *testing.T) {
	url, pool := testdb.New(t)
	ctx := context.Background()
	if err := store.Migrate(ctx, pool, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	svc := account.New(pool, time.Now, "http://localhost:8080")
	_ = svc.RefreshSetupCode(ctx)
	if _, _, err := svc.Setup(ctx, account.SetupInput{Code: svc.SetupCode(), WorkspaceName: "W", Name: "O",
		Email: "o@example.com", Password: "owner-password"}, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if logs := runAndCaptureLogs(t, config.Config{DatabaseURL: url, ListenAddr: "127.0.0.1:0", DataDir: t.TempDir()}); strings.Contains(logs, "setup required") {
		t.Errorf("setup log after setup: %s", logs)
	}
}
