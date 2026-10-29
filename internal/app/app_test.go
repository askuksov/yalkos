package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/askuksov/yalkos/internal/app"
	"github.com/askuksov/yalkos/internal/config"
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

	s := start(ctx, t, config.Config{DatabaseURL: url, ListenAddr: "127.0.0.1:0"})
	if code := get(t, "http://"+s.addr+"/readyz"); code != 200 {
		t.Errorf("/readyz = %d, want 200", code)
	}
	if code := get(t, "http://"+s.addr+"/healthz"); code != 200 {
		t.Errorf("/healthz = %d, want 200", code)
	}
}

func TestRunStopsOnContextCancel(t *testing.T) {
	url, _ := testdb.New(t)
	ctx, cancel := context.WithCancel(context.Background())

	s := start(ctx, t, config.Config{DatabaseURL: url, ListenAddr: "127.0.0.1:0"})
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
	cfg := config.Config{DatabaseURL: "postgres://u:p@127.0.0.1:1/x?sslmode=disable", ListenAddr: "127.0.0.1:0"}
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
