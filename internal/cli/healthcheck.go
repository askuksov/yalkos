package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const defaultListenAddr = ":8080"

// healthcheck asks the running server for /readyz so a container runtime can
// use the binary itself as the probe (no shell or curl in the image). It reads
// only LISTEN_ADDR, so it works with any environment.
func healthcheck(env Env) int {
	listen := env.Getenv("LISTEN_ADDR")
	if listen == "" {
		listen = defaultListenAddr
	}
	target, err := healthcheckTarget(listen)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "healthcheck: LISTEN_ADDR %s: %v\n", listen, err)
		return 2
	}

	req, err := http.NewRequestWithContext(ctxOf(env), http.MethodGet, target, nil)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "not ready: %v\n", err)
		return 1
	}
	resp, err := healthcheckClient().Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(env.Stderr, "not ready: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = fmt.Fprintf(env.Stderr, "not ready: HTTP %d\n", resp.StatusCode)
		return 1
	}
	_, _ = fmt.Fprintln(env.Stdout, "ready")
	return 0
}

// healthcheckClient never uses proxy settings from the environment: the probe
// must reach the local server directly.
func healthcheckClient() *http.Client {
	return &http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
}

// healthcheckTarget turns a listen address into the /readyz URL to probe. A
// wildcard or empty host means "this machine", so it becomes 127.0.0.1.
func healthcheckTarget(listenAddr string) (string, error) {
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", err
	}
	if port == "" {
		return "", errors.New("missing port")
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}
