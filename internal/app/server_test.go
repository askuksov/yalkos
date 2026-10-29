package app

import (
	"net/http"
	"testing"
	"time"
)

func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 10s", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout = %v, want 30s", srv.ReadTimeout)
	}
	if srv.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %v, want 120s", srv.IdleTimeout)
	}
}
