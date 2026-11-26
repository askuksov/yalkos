package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthcheckTarget(t *testing.T) {
	cases := []struct {
		listen string
		want   string
	}{
		{"", "http://127.0.0.1:8080/readyz"},
		{":8080", "http://127.0.0.1:8080/readyz"},
		{"0.0.0.0:9000", "http://127.0.0.1:9000/readyz"},
		{"[::]:9000", "http://127.0.0.1:9000/readyz"},
		{"127.0.0.1:18765", "http://127.0.0.1:18765/readyz"},
		{"[::1]:9000", "http://[::1]:9000/readyz"},
		{"localhost:9000", "http://localhost:9000/readyz"},
	}
	for _, c := range cases {
		t.Run(c.listen, func(t *testing.T) {
			got, err := healthcheckTarget(c.listen)
			if err != nil {
				t.Fatalf("healthcheckTarget(%q): %v", c.listen, err)
			}
			if got != c.want {
				t.Errorf("healthcheckTarget(%q) = %q, want %q", c.listen, got, c.want)
			}
		})
	}

	for _, bad := range []string{"8080", "localhost:", ":"} {
		t.Run("error "+bad, func(t *testing.T) {
			if got, err := healthcheckTarget(bad); err == nil {
				t.Errorf("healthcheckTarget(%q) = %q, want error", bad, got)
			}
		})
	}
}

func healthcheckEnv(t *testing.T, vars map[string]string) Env {
	t.Helper()
	return Env{
		Ctx:    t.Context(),
		Getenv: func(k string) string { return vars[k] },
	}
}

func TestHealthcheck(t *testing.T) {
	serve := func(t *testing.T, status int) *httptest.Server {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/readyz" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	hostPort := func(srv *httptest.Server) string {
		return strings.TrimPrefix(srv.URL, "http://")
	}

	t.Run("ready", func(t *testing.T) {
		srv := serve(t, http.StatusOK)
		code, out, errOut := run([]string{"healthcheck"},
			healthcheckEnv(t, map[string]string{"LISTEN_ADDR": hostPort(srv)}))
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr %q)", code, errOut)
		}
		if out != "ready\n" {
			t.Errorf("stdout = %q, want %q", out, "ready\n")
		}
		if errOut != "" {
			t.Errorf("stderr = %q, want empty", errOut)
		}
	})

	t.Run("not ready", func(t *testing.T) {
		srv := serve(t, http.StatusServiceUnavailable)
		code, out, errOut := run([]string{"healthcheck"},
			healthcheckEnv(t, map[string]string{"LISTEN_ADDR": hostPort(srv)}))
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if want := "not ready: HTTP 503\n"; errOut != want {
			t.Errorf("stderr = %q, want %q", errOut, want)
		}
	})

	t.Run("not listening", func(t *testing.T) {
		srv := serve(t, http.StatusOK)
		addr := hostPort(srv)
		srv.Close()
		code, out, errOut := run([]string{"healthcheck"},
			healthcheckEnv(t, map[string]string{"LISTEN_ADDR": addr}))
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if !strings.HasPrefix(errOut, "not ready: ") {
			t.Errorf("stderr = %q, want prefix %q", errOut, "not ready: ")
		}
	})

	t.Run("bad listen address", func(t *testing.T) {
		code, out, errOut := run([]string{"healthcheck"},
			healthcheckEnv(t, map[string]string{"LISTEN_ADDR": "8080"}))
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if want := "healthcheck: LISTEN_ADDR 8080: "; !strings.HasPrefix(errOut, want) {
			t.Errorf("stderr = %q, want prefix %q", errOut, want)
		}
	})

	t.Run("empty port", func(t *testing.T) {
		code, out, errOut := run([]string{"healthcheck"},
			healthcheckEnv(t, map[string]string{"LISTEN_ADDR": "localhost:"}))
		if code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
		if out != "" {
			t.Errorf("stdout = %q, want empty", out)
		}
		if want := "healthcheck: LISTEN_ADDR localhost:: "; !strings.HasPrefix(errOut, want) {
			t.Errorf("stderr = %q, want prefix %q", errOut, want)
		}
	})

	t.Run("environment proxy ignored", func(t *testing.T) {
		srv := serve(t, http.StatusOK)
		const proxy = "http://127.0.0.1:1"
		t.Setenv("HTTP_PROXY", proxy)
		t.Setenv("http_proxy", proxy)
		code, _, errOut := run([]string{"healthcheck"},
			healthcheckEnv(t, map[string]string{
				"LISTEN_ADDR": hostPort(srv),
				"HTTP_PROXY":  proxy,
			}))
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr %q)", code, errOut)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		srv := serve(t, http.StatusOK)
		code, _, errOut := run([]string{"healthcheck"},
			Env{Getenv: func(k string) string {
				if k == "LISTEN_ADDR" {
					return hostPort(srv)
				}
				return ""
			}})
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr %q)", code, errOut)
		}
	})
}

// httptest servers listen on loopback, which Go's environment proxy never
// proxies, so the proxy case above cannot fail by itself; pin the client
// configuration directly.
func TestHealthcheckClient(t *testing.T) {
	c := healthcheckClient()
	if c.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v, want 3s", c.Timeout)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", c.Transport)
	}
	if tr.Proxy != nil {
		t.Error("Transport.Proxy is set, want nil (environment proxy must be ignored)")
	}
}

func TestUsageListsHealthcheck(t *testing.T) {
	want := "  healthcheck     Exit 0 if the server answers /readyz with 200\n"
	if !strings.Contains(usage, want) {
		t.Errorf("usage does not contain %q", want)
	}
	_, out, _ := run([]string{"help"}, Env{})
	if !strings.Contains(out, want) {
		t.Errorf("help output does not contain %q", want)
	}
}
