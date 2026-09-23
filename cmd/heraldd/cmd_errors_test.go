package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/internal/config"
	"github.com/cuihairu/herald/protocol"
	gws "github.com/gorilla/websocket"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

func TestRunUsageAndUnknownCommand(t *testing.T) {
	if code := run([]string{"heraldd"}); code != 1 {
		t.Errorf("run(no args) = %d, want 1", code)
	}
	if code := run([]string{"heraldd", "frobnicate"}); code != 1 {
		t.Errorf("run(unknown cmd) = %d, want 1", code)
	}
}

func TestServeCmdConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  string
	}{
		{"bad yaml", "server: [unclosed"},
		{"unknown queue type", "server:\n  addr: 127.0.0.1:0\nqueue:\n  type: banana\n"},
		{"redis unreachable", "server:\n  addr: 127.0.0.1:0\nqueue:\n  type: redis\n  redis:\n    addr: 127.0.0.1:1\n"},
		{"unknown provider type", "server:\n  addr: 127.0.0.1:0\nproviders:\n  bad:\n    type: no-such\n"},
		{"bad template", "server:\n  addr: 127.0.0.1:0\ntemplates:\n  ops:\n    title: \"t\"\n"},
		{"auth without api_keys", "server:\n  addr: 127.0.0.1:0\nauth:\n  enabled: true\n"},
		{"provider without type", "server:\n  addr: 127.0.0.1:0\nproviders:\n  bad: {}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestConfig(t, tc.cfg)
			if code := serveCmd([]string{"--config", path}); code != 1 {
				t.Errorf("serveCmd(%s) = %d, want 1", tc.name, code)
			}
		})
	}

	// A config path that does not exist must fail config loading.
	if code := serveCmd([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}); code != 1 {
		t.Errorf("serveCmd(missing file) = %d, want 1", code)
	}
}

func TestRunDispatch(t *testing.T) {
	// run() must dispatch to serveCmd/workerCmd; a bad config keeps the run
	// short and both commands must report failure through the exit code.
	badPath := writeTestConfig(t, "queue:\n  type: banana\n")
	if code := run([]string{"heraldd", "serve", "--config", badPath}); code != 1 {
		t.Errorf("run(serve, bad config) = %d, want 1", code)
	}
	if code := run([]string{"heraldd", "worker", "--config", badPath}); code != 1 {
		t.Errorf("run(worker, bad config) = %d, want 1", code)
	}
}

func TestWorkerCmdConfigErrors(t *testing.T) {
	redisOK := newFakeRedis(t)
	cases := []struct {
		name string
		cfg  string
	}{
		{"memory rejected", "server:\n  addr: 127.0.0.1:0\nqueue:\n  type: memory\n"},
		{"unknown queue type", "server:\n  addr: 127.0.0.1:0\nqueue:\n  type: banana\n"},
		{"redis unreachable", "server:\n  addr: 127.0.0.1:0\nqueue:\n  type: redis\n  redis:\n    addr: 127.0.0.1:1\n"},
		{
			"unknown provider type",
			fmt.Sprintf("server:\n  addr: 127.0.0.1:0\nqueue:\n  type: redis\n  redis:\n    addr: %s\n  workers: 1\nproviders:\n  bad:\n    type: no-such\n", redisOK.Addr()),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestConfig(t, tc.cfg)
			if code := workerCmd([]string{"--config", path}); code != 1 {
				t.Errorf("workerCmd(%s) = %d, want 1", tc.name, code)
			}
		})
	}

	// A config path that does not exist must fail config loading.
	if code := workerCmd([]string{"--config", filepath.Join(t.TempDir(), "missing.yaml")}); code != 1 {
		t.Errorf("workerCmd(missing file) = %d, want 1", code)
	}
}

// TestWorkerCmdLifecycle boots a full remote worker against the RESP stub:
// the queue connects and pool workers block on XREADGROUP, then SIGTERM must
// trigger the graceful shutdown path and workerCmd must return 0.
func TestWorkerCmdLifecycle(t *testing.T) {
	fr := newFakeRedis(t)
	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:0
queue:
  type: redis
  redis:
    addr: %s
  workers: 1
providers:
  hook:
    type: webhook
    config:
      url: http://127.0.0.1:1/hook
worker:
  id: w-lifecycle
`, fr.Addr())
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- workerCmd([]string{"--config", path}) }()

	// Give the worker time to connect, create the consumer group, and park a
	// pool worker on the blocked XREADGROUP.
	time.Sleep(500 * time.Millisecond)

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("workerCmd() = %d, want 0", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("workerCmd did not return after SIGTERM")
	}
}

// TestServeCmdStartFailure occupies both listen addresses so the API and
// WebSocket servers fail to start; the error goroutines must cancel the
// context and SIGTERM must still shut serveCmd down cleanly.
func TestServeCmdStartFailure(t *testing.T) {
	l1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l1.Close() }()
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l2.Close() }()

	cfgYAML := fmt.Sprintf(`
server:
  addr: %s
websocket:
  addr: %s
queue:
  type: memory
  workers: 1
`, l1.Addr().String(), l2.Addr().String())
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	time.Sleep(500 * time.Millisecond) // let both Start calls fail and cancel
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}
}

// TestServeCmdFeaturesLifecycle boots the scheduler with dedup enabled and a
// disabled provider so the optional-config branches run, then shuts down on
// SIGTERM.
func TestServeCmdFeaturesLifecycle(t *testing.T) {
	httpPort := freePort(t)
	wsPort := freePort(t)
	cfgYAML := fmt.Sprintf(`
server:
  addr: 127.0.0.1:%d
websocket:
  addr: 127.0.0.1:%d
queue:
  type: memory
  workers: 1
dedup:
  enabled: true
  window: 1s
providers:
  hook:
    type: webhook
    enabled: false
    config:
      url: http://127.0.0.1:1/hook
`, httpPort, wsPort)
	path := writeTestConfig(t, cfgYAML)

	done := make(chan int, 1)
	go func() { done <- serveCmd([]string{"--config", path}) }()

	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/", httpPort), 10*time.Second)
	time.Sleep(200 * time.Millisecond) // let signal.Notify register first

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("serveCmd() = %d, want 0", code)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("serveCmd did not return after SIGTERM")
	}
}

// TestRunControlPlaneDefaults uses an unreachable server URL with an empty
// worker config so the control plane applies its own defaults (generated
// worker ID, default reconnect delay and heartbeat interval) while dialing.
func TestRunControlPlaneDefaults(t *testing.T) {
	cfg := &config.Config{
		Worker: config.WorkerConfig{ServerURL: "ws://127.0.0.1:1/ws"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(ctx, cfg, worker.NewRegistry())
		close(done)
	}()

	time.Sleep(80 * time.Millisecond) // at least one failed dial + retry
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control plane did not stop after ctx cancel")
	}
}

// TestRunControlPlaneRegisterRejected rejects the registration once, then
// parks the control plane in its (long) reconnect sleep so canceling the
// context deterministically ends it on the post-registration-failure path.
func TestRunControlPlaneRegisterRejected(t *testing.T) {
	stub := newWSStub(t, func(conn *gws.Conn) {
		ack, _ := json.Marshal(&protocol.RegisterAckMessage{WorkerID: "w-rej", Success: false, Error: "denied"})
		_ = conn.WriteMessage(gws.TextMessage, ack)
	})

	cfg := &config.Config{
		Worker: config.WorkerConfig{
			ServerURL:      stub.url(),
			ID:             "w-rej",
			ReconnectDelay: 10 * time.Second,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(ctx, cfg, worker.NewRegistry())
		close(done)
	}()

	// Wait until the stub saw the registration attempt before canceling.
	stub.nextConn(3 * time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control plane did not stop after ctx cancel")
	}
}

// TestRunControlPlaneRegisterRejectedRetry keeps rejecting with a short
// reconnect delay so the loop actually retries registration several times.
func TestRunControlPlaneRegisterRejectedRetry(t *testing.T) {
	stub := newWSStub(t, func(conn *gws.Conn) {
		ack, _ := json.Marshal(&protocol.RegisterAckMessage{WorkerID: "w-retry", Success: false, Error: "denied"})
		_ = conn.WriteMessage(gws.TextMessage, ack)
	})

	cfg := &config.Config{
		Worker: config.WorkerConfig{
			ServerURL:      stub.url(),
			ID:             "w-retry",
			ReconnectDelay: 20 * time.Millisecond,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(ctx, cfg, worker.NewRegistry())
		close(done)
	}()

	time.Sleep(120 * time.Millisecond) // several reject → sleep → retry cycles
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control plane did not stop after ctx cancel")
	}
}

// TestRunControlPlaneHeartbeatDrop accepts the registration and immediately
// drops the conn; with a fast heartbeat the first tick's write must fail and
// park the control plane in its reconnect sleep until ctx cancel.
func TestRunControlPlaneHeartbeatDrop(t *testing.T) {
	stub := newWSStub(t, func(conn *gws.Conn) {
		ack, _ := json.Marshal(&protocol.RegisterAckMessage{WorkerID: "w-drop", Success: true, ServerID: "herald-core"})
		_ = conn.WriteMessage(gws.TextMessage, ack)
		_ = conn.Close() // first heartbeat write must fail
	})

	cfg := &config.Config{
		Worker: config.WorkerConfig{
			ServerURL:         stub.url(),
			ID:                "w-drop",
			ReconnectDelay:    10 * time.Second,
			HeartbeatInterval: 10 * time.Millisecond,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(ctx, cfg, worker.NewRegistry())
		close(done)
	}()

	time.Sleep(100 * time.Millisecond) // register, then fail the first heartbeat
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("control plane did not stop after ctx cancel")
	}
}

// TestRunControlPlaneCanceledBeforeStart feeds an already-canceled ctx so the
// loop-top ctx check ends the control plane before any dial.
func TestRunControlPlaneCanceledBeforeStart(t *testing.T) {
	cfg := &config.Config{Worker: config.WorkerConfig{ServerURL: "ws://127.0.0.1:1/ws"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		runRemoteWorkerControlPlane(ctx, cfg, worker.NewRegistry())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("control plane did not return on a pre-canceled ctx")
	}
}

// TestRegisterRemoteWorkerWriteError closes the client conn first: gorilla's
// SetWriteDeadline still succeeds on a closed conn, so the failure must
// surface at WriteMessage.
func TestRegisterRemoteWorkerWriteError(t *testing.T) {
	stub := newWSStub(t, nil)

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	if err := registerRemoteWorker(conn, "w-wrerr", nil); err == nil {
		t.Error("registerRemoteWorker() on closed conn = nil, want write error")
	}
}

// TestRegisterRemoteWorkerReadError drops the conn before any ack, so the
// client's ack read must fail.
func TestRegisterRemoteWorkerReadError(t *testing.T) {
	stub := newWSStub(t, nil)

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Close the stub-side conn as soon as the register arrives.
	serverConn := stub.nextConn(3 * time.Second)
	_ = serverConn.Close()

	if err := registerRemoteWorker(conn, "w-readerr", nil); err == nil {
		t.Error("registerRemoteWorker() without ack should fail on read")
	}
	_ = conn.Close()
}

// TestHeartbeatRemoteWorkerWriteError closes the conn while the heartbeat
// loop is ticking; the next heartbeat write must fail and end the loop.
func TestHeartbeatRemoteWorkerWriteError(t *testing.T) {
	stub := newWSStub(t, nil)

	conn, _, err := gws.DefaultDialer.Dial(stub.url(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	// Keep the stub's handler from consuming the write side; we drive the
	// client side entirely here. Close the client conn after the loop starts.
	registry := worker.NewRegistry()
	_ = registry.Register(&worker.Info{ID: "w-hberr", Mode: worker.Remote})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- heartbeatRemoteWorker(ctx, conn, "w-hberr", 10*time.Millisecond, registry)
	}()

	time.Sleep(40 * time.Millisecond) // at least one successful tick
	_ = conn.Close()                  // next heartbeat write must fail

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("heartbeatRemoteWorker() = nil, want write error after conn close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for heartbeat loop to fail")
	}
}
