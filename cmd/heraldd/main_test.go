package main

import (
	"context"
	"encoding/json"
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

	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/internal/config"
	"github.com/cuihairu/herald/protocol"
	gws "github.com/gorilla/websocket"
)

// freePort reserves a loopback port and immediately releases it, so a
// background server started by the process under test can bind it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return port
}

func waitHTTPReady(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never became ready", url)
}

func TestServeCmdLifecycle(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)

	// A fully valid config: memory queue (no external deps), a template to
	// exercise the template-loading branch, and one webhook provider so the
	// provider-registration path runs too.
	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 2
providers:
  hook:
    type: webhook
    config:
      url: http://127.0.0.1:1/hook
templates:
  ops:
    name: ops
    title: "Ops: {{.Title}}"
`, httpPort, wsPort)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	done := make(chan struct{})
	go func() {
		serveCmd([]string{"--config", cfgPath})
		close(done)
	}()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	// serveCmd registers its signal handler right after both servers start;
	// give the main goroutine a moment to get past that line before we
	// deliver SIGTERM (otherwise the default disposition would kill the
	// whole test binary).
	time.Sleep(200 * time.Millisecond)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}
}

func TestParseFlags(t *testing.T) {
	if got := parseFlags("serve", nil); got != "config.yaml" {
		t.Errorf("parseFlags(serve, nil) = %q, want default config.yaml", got)
	}
	if got := parseFlags("unknown", nil); got != "config.yaml" {
		t.Errorf("parseFlags(unknown, nil) = %q, want default config.yaml", got)
	}
	if got := parseFlags("worker", []string{"--config", "/tmp/other.yaml"}); got != "/tmp/other.yaml" {
		t.Errorf("parseFlags(--config) = %q, want /tmp/other.yaml", got)
	}
	if got := parseFlags("worker", []string{"-c", "/tmp/short.yaml"}); got != "/tmp/short.yaml" {
		t.Errorf("parseFlags(-c) = %q, want /tmp/short.yaml", got)
	}
	// A trailing flag without a value keeps the default.
	if got := parseFlags("serve", []string{"--config"}); got != "config.yaml" {
		t.Errorf("parseFlags(trailing --config) = %q, want default", got)
	}
	if got := parseFlags("serve", []string{"--config", "a.yaml", "--config", "b.yaml"}); got != "b.yaml" {
		t.Errorf("parseFlags(repeated --config) = %q, want last value b.yaml", got)
	}
}

func TestBuildWorkerWebSocketURL(t *testing.T) {
	if got := buildWorkerWebSocketURL(&config.Config{}); got != "" {
		t.Errorf("empty config = %q, want empty", got)
	}

	if got := buildWorkerWebSocketURL(&config.Config{
		Worker: config.WorkerConfig{ServerURL: "ws://hub.example.com/ws"},
	}); got != "ws://hub.example.com/ws" {
		t.Errorf("ServerURL = %q, want trimmed", got)
	}

	got := buildWorkerWebSocketURL(&config.Config{WebSocket: config.WebSocketConfig{Addr: ":9000"}})
	if got != "ws://127.0.0.1:9000/worker" {
		t.Errorf(":9000 addr = %q, want ws://127.0.0.1:9000/worker", got)
	}

	got = buildWorkerWebSocketURL(&config.Config{WebSocket: config.WebSocketConfig{Addr: "0.0.0.0:9000"}})
	if got != "ws://0.0.0.0:9000/worker" {
		t.Errorf("full addr = %q, want ws://0.0.0.0:9000/worker", got)
	}
}

func TestSleepOrDone(t *testing.T) {
	if !sleepOrDone(context.Background(), time.Millisecond) {
		t.Error("sleepOrDone() = false, want true after timer fired")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepOrDone(ctx, time.Hour) {
		t.Error("sleepOrDone() = true, want false when ctx already canceled")
	}
}

// wsStub is a WebSocket server stub for exercising the remote-worker
// control-plane helpers entirely in-process.
type wsStub struct {
	server *httptest.Server
	conns  chan *gws.Conn
	ack    func(conn *gws.Conn) // response behavior after receiving a register
	t      *testing.T
}

func newWSStub(t *testing.T, ack func(conn *gws.Conn)) *wsStub {
	s := &wsStub{conns: make(chan *gws.Conn, 8), ack: ack, t: t}
	up := gws.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		select {
		case s.conns <- conn:
		default:
		}

		// Read the register message, then let the test decide the reply.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		if s.ack != nil {
			s.ack(conn)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *wsStub) url() string {
	return "ws" + strings.TrimPrefix(s.server.URL, "http")
}

func (s *wsStub) nextConn(timeout time.Duration) *gws.Conn {
	select {
	case conn := <-s.conns:
		return conn
	case <-time.After(timeout):
		s.t.Fatal("timed out waiting for stub connection")
		return nil
	}
}

func TestRegisterRemoteWorkerSuccess(t *testing.T) {
	stub := newWSStub(t, func(conn *gws.Conn) {
		ack, _ := json.Marshal(&protocol.RegisterAckMessage{WorkerID: "w1", Success: true, ServerID: "herald-core"})
		_ = conn.WriteMessage(gws.TextMessage, ack)
	})

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := registerRemoteWorker(conn, "w1", []string{"telegram"}); err != nil {
		t.Fatalf("registerRemoteWorker() error = %v", err)
	}
}

func TestRegisterRemoteWorkerRejected(t *testing.T) {
	stub := newWSStub(t, func(conn *gws.Conn) {
		ack, _ := json.Marshal(&protocol.RegisterAckMessage{WorkerID: "w1", Success: false, Error: "not allowed"})
		_ = conn.WriteMessage(gws.TextMessage, ack)
	})

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	err = registerRemoteWorker(conn, "w1", nil)
	if err == nil || !strings.Contains(err.Error(), "register rejected: not allowed") {
		t.Errorf("registerRemoteWorker() error = %v, want rejection", err)
	}
}

func TestRegisterRemoteWorkerBadAck(t *testing.T) {
	stub := newWSStub(t, func(conn *gws.Conn) {
		_ = conn.WriteMessage(gws.TextMessage, []byte("<<not json>>"))
	})

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := registerRemoteWorker(conn, "w1", nil); err == nil {
		t.Error("registerRemoteWorker() with non-JSON ack should fail")
	}
}

func TestRegisterRemoteWorkerWriteFailure(t *testing.T) {
	stub := newWSStub(t, nil)

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Close the stub-side conn so the worker's write fails promptly.
	serverConn := stub.nextConn(3 * time.Second)
	_ = serverConn.Close()

	if err := registerRemoteWorker(conn, "w1", nil); err == nil {
		t.Error("registerRemoteWorker() on dead conn should fail")
	}
	_ = conn.Close()
}

func TestHeartbeatRemoteWorker(t *testing.T) {
	stub := newWSStub(t, nil)

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	registry := worker.NewRegistry()
	_ = registry.Register(&worker.Info{ID: "w-hb", Mode: worker.Remote})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- heartbeatRemoteWorker(ctx, conn, "w-hb", 10*time.Millisecond, registry) }()

	// Let a few ticks fire, then stop and expect a clean ctx error.
	time.Sleep(60 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("heartbeatRemoteWorker() = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for heartbeat loop to exit")
	}

	if err := registry.Heartbeat("w-hb"); err != nil {
		t.Errorf("worker heartbeat should have been recorded: %v", err)
	}
}

func TestReadRemoteWorkerMessages(t *testing.T) {
	// The stub pushes one message and then closes; the reader goroutine must
	// return on its own once the conn dies.
	up := gws.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.WriteMessage(gws.TextMessage, []byte(`{"type":"heartbeat"}`))
		time.Sleep(30 * time.Millisecond)
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := gws.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		readRemoteWorkerMessages(ctx, conn)
		close(done)
	}()

	// The stub closes the conn, so the reader must return without cancel.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("readRemoteWorkerMessages did not return after conn close")
	}
}

func TestRunControlPlaneDisabled(t *testing.T) {
	// No server URL and no websocket addr: control plane must exit at once.
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(context.Background(), &config.Config{}, worker.NewRegistry())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("control plane should return immediately when disabled")
	}
}

func TestRunControlPlaneDialRetryUntilCancel(t *testing.T) {
	// Nothing listens on this port: dial fails and the loop retries.
	cfg := &config.Config{
		Worker: config.WorkerConfig{
			ServerURL:      "ws://127.0.0.1:1/ws",
			ID:             "w-retry",
			ReconnectDelay: 10 * time.Millisecond,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(ctx, cfg, worker.NewRegistry())
		close(done)
	}()

	time.Sleep(80 * time.Millisecond) // allow at least one failed dial + retry
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control plane did not stop after ctx cancel")
	}
}

func TestRunControlPlaneFullCycle(t *testing.T) {
	stub := newWSStub(t, func(conn *gws.Conn) {
		ack, _ := json.Marshal(&protocol.RegisterAckMessage{WorkerID: "w-cycle", Success: true})
		_ = conn.WriteMessage(gws.TextMessage, ack)
		// Drop the conn shortly after so the worker reconnects.
		time.Sleep(40 * time.Millisecond)
		_ = conn.Close()
	})

	registry := worker.NewRegistry()
	cfg := &config.Config{
		Worker: config.WorkerConfig{
			ServerURL:         stub.url(),
			ID:                "w-cycle",
			ReconnectDelay:    10 * time.Millisecond,
			HeartbeatInterval: 15 * time.Millisecond,
			Capabilities:      []string{"slack"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(ctx, cfg, registry)
		close(done)
	}()

	// Wait until the worker shows up in the registry (first registration).
	deadline := time.Now().Add(5 * time.Second)
	registered := false
	for time.Now().Before(deadline) {
		if _, err := registry.Get("w-cycle"); err == nil {
			registered = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !registered {
		t.Fatal("worker was never registered with the registry")
	}

	// Let a reconnect cycle happen, then shut down.
	time.Sleep(120 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control plane did not stop after ctx cancel")
	}

	// Deregistration happens on disconnect; eventually the worker is gone.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := registry.Get("w-cycle"); err != nil {
			return // deregistered
		}
		time.Sleep(5 * time.Millisecond)
	}
}
