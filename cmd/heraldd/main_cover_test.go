package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	gws "github.com/gorilla/websocket"
)

// TestServeCmdStartupFailures drives each startup-time rejection with a
// minimal config: every case must exit with code 1 before binding anything.
func TestServeCmdStartupFailures(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		// A rules store pointing at a directory cannot be opened.
		"rules store is a dir": fmt.Sprintf("rules_store: %s\n", dir),
		// Only memory and redis are valid rule-state backends.
		"unknown rules state": "rules_state:\n  type: etcd\n",
		// An unreachable redis fails the state store at startup.
		"dead redis state": "rules_state:\n  type: redis\n  addr: 127.0.0.1:1\n",
		// A rule that does not compile is rejected at load time.
		"invalid rule": "rules:\n  - id: bad\n    match: \"&&&\"\n    route:\n      - channels: [log]\n",
	}
	for name, cfgYAML := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTestConfig(t, cfgYAML)
			if code := serveCmd([]string{"--config", path}); code != 1 {
				t.Fatalf("serveCmd(%s) = %d, want 1", name, code)
			}
		})
	}
}

// TestServeCmdFullFeaturedLifecycle starts the server with every optional
// subsystem enabled — persistent rules, redis rule state, card-callback
// encryption, a rate limit, and a broken escalation store (which must only
// be logged) — and shuts it down via SIGTERM.
func TestServeCmdFullFeaturedLifecycle(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)
	mr := miniredis.RunT(t)
	rulesPath := filepath.Join(t.TempDir(), "rules.json")
	// A directory as escalation store makes Restore fail; the server must
	// come up anyway.
	escStore := t.TempDir()

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
rules_store: %s
rules_state:
  type: redis
  addr: %s
escalation_store: %s
card_callback:
  encrypt_key: test-encrypt-key
providers:
  hook:
    type: webhook
    config:
      url: http://127.0.0.1:1/hook
    rate_limit:
      type: token_bucket
      rate: 10
      burst: 10
rules:
  - id: r-hook
    match: "type == 'quickstart'"
    route:
      - channels: [hook]
`, httpPort, wsPort, rulesPath, mr.Addr(), escStore)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serveCmd = %d, want 0", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}

	// The persistent rules store must hold the configured rule.
	data, err := os.ReadFile(rulesPath)
	if err != nil || !strings.Contains(string(data), "r-hook") {
		t.Fatalf("rules store not persisted: %q / %v", data, err)
	}
}

// TestServeCmdWebSocketPortTaken covers the websocket server failing to
// bind: the error cancels the run context, and the process still exits
// through its normal signal path.
func TestServeCmdWebSocketPortTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	wsPort := ln.Addr().(*net.TCPAddr).Port
	httpPort := freePort(t)

	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
`, httpPort, wsPort)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	// Give the websocket server time to hit the taken port, then tear the
	// process down the way the signal handler expects.
	time.Sleep(300 * time.Millisecond)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serveCmd = %d, want 0 after websocket bind failure", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not exit after the websocket bind failure")
	}
}

// TestReadRemoteWorkerMessagesCancel covers the context-cancelled exit of
// the reader loop while the connection is still alive.
func TestReadRemoteWorkerMessagesCancel(t *testing.T) {
	up := gws.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Hold the connection open until the test tears down.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := gws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		readRemoteWorkerMessages(ctx, conn)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("readRemoteWorkerMessages did not return after cancel")
	}
}
