package sdk

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

// silentWSServer upgrades and then drops the connection with a TCP RST, so
// the next client write or read fails immediately.
func silentWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// SO_LINGER 0 turns Close into an RST, failing the client
		// immediately instead of after a buffered retry.
		if tcp, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
	}))
}

// echoAckServer replies to the register message and then closes, letting
// the handshake complete while later reads fail.
func echoAckServer(t *testing.T, ack []byte) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, ack)
	}))
}

func newControlClient(t *testing.T, coreURL string) *Client {
	t.Helper()
	cfg := protocol.DefaultWorkerConfig()
	cfg.CoreURL = coreURL
	cfg.WorkerID = "w-test"
	return NewClient(cfg, nil)
}

func TestRegisterDialFailure(t *testing.T) {
	c := newControlClient(t, "ws://127.0.0.1:1/worker")
	if err := c.register(context.Background()); err == nil || !strings.Contains(err.Error(), "dial") {
		t.Fatalf("expected dial error, got %v", err)
	}
}

func TestRegisterWriteFailsOnRST(t *testing.T) {
	srv := silentWSServer(t)
	defer srv.Close()
	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err == nil {
		t.Fatal("a RST connection must fail the register write")
	}
}

func TestRegisterAckReadFailureDeterministic(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}))
	defer srv.Close()

	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err == nil || !strings.Contains(err.Error(), "read register ack") {
		t.Fatalf("expected the ack read to fail, got %v", err)
	}
}

func TestRegisterAckDecodeFailure(t *testing.T) {
	srv := echoAckServer(t, []byte("not-json"))
	defer srv.Close()
	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err == nil || !strings.Contains(err.Error(), "decode register ack") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestRegisterRejected(t *testing.T) {
	ack, err := protocol.MarshalMessage(&protocol.RegisterAckMessage{WorkerID: "w-test", Success: false, Error: "nope"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	srv := echoAckServer(t, ack)
	defer srv.Close()
	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err == nil || !strings.Contains(err.Error(), "register rejected") {
		t.Fatalf("expected rejection, got %v", err)
	}
}

func TestRegisterSuccessStoresConn(t *testing.T) {
	ack, err := protocol.MarshalMessage(&protocol.RegisterAckMessage{WorkerID: "w-test", Success: true, ServerID: "herald-test"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	srv := echoAckServer(t, ack)
	defer srv.Close()
	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() { _ = c.Disconnect() }()
	if c.controlConn() == nil || c.connID == "" {
		t.Fatal("register must store the live connection and id")
	}
}

func TestReadLoopExitsOnNilConn(t *testing.T) {
	c := newControlClient(t, "")
	done := make(chan struct{})
	c.wg.Add(1) // readLoop's Done is normally paired by Run
	go func() {
		c.readLoop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop must exit when no control connection exists")
	}
}

func TestReadLoopExitsOnCancelledCtx(t *testing.T) {
	c := newControlClient(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.wg.Add(1) // readLoop's Done is normally paired by Run
	done := make(chan struct{})
	go func() {
		c.readLoop(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop must exit on a cancelled context")
	}
}

// A dropped connection while the client never reached Ready exits quietly:
// the disconnect callback belongs to a registration that never completed.
func TestReadLoopQuietExitBeforeReady(t *testing.T) {
	c := newControlClient(t, "")
	c.mu.Lock()
	c.conn = newDeadWSConn(t)
	c.mu.Unlock()

	c.wg.Add(1) // readLoop's Done is normally paired by Run
	done := make(chan struct{})
	go func() {
		c.readLoop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop must exit quietly before the client is Ready")
	}
}

// pushEventServer completes the handshake and then pushes one event so the
// read loop has something to dispatch.
func pushEventServer(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		ack, _ := protocol.MarshalMessage(&protocol.RegisterAckMessage{WorkerID: "w-test", Success: true, ServerID: "herald-test"})
		if err := conn.WriteMessage(websocket.TextMessage, ack); err != nil {
			return
		}
		ev, _ := protocol.MarshalMessage(&protocol.EventMessage{WorkerID: "w-test", EventType: "online"})
		_ = conn.WriteMessage(websocket.TextMessage, ev)
		// Hold the connection open until the test disconnects.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
}

// A server that closes without answering the register makes the ack read
// fail deterministically.
func TestRegisterAckReadFailure(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}))
	defer srv.Close()

	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err == nil || !strings.Contains(err.Error(), "read register ack") {
		t.Fatalf("expected the ack read to fail, got %v", err)
	}
}

// The read loop must route a server-pushed event to the event handler.
func TestReadLoopDispatchesServerPushedEvent(t *testing.T) {
	srv := pushEventServer(t)
	defer srv.Close()
	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err != nil {
		t.Fatalf("register: %v", err)
	}

	var mu sync.Mutex
	var got *protocol.EventMessage
	c.eventHandler = func(ev *protocol.EventMessage) {
		mu.Lock()
		got = ev
		mu.Unlock()
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.setState(protocol.StateReady)
	defer func() { _ = c.Disconnect() }()

	c.wg.Add(1) // readLoop's Done is normally paired by Run
	go c.readLoop(c.ctx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		delivered := got
		mu.Unlock()
		if delivered != nil {
			if delivered.EventType != "online" {
				t.Fatalf("unexpected event: %+v", delivered)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("server-pushed event was never dispatched")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestReadLoopFiresDisconnectCallback(t *testing.T) {
	srv := echoAckServer(t, []byte(`{"type":"register_ack","worker_id":"w-test","success":true}`))
	defer srv.Close()
	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err != nil {
		t.Fatalf("register: %v", err)
	}

	var mu sync.Mutex
	called := false
	c.onDisconnect = func(err error) {
		mu.Lock()
		called = true
		mu.Unlock()
	}
	c.setState(protocol.StateReady)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.wg.Add(1) // readLoop's Done is normally paired by Run
	done := make(chan struct{})
	go func() {
		c.readLoop(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("readLoop must exit when the server drops the connection")
	}
	mu.Lock()
	calledNow := called
	mu.Unlock()
	if !calledNow {
		t.Fatal("the disconnect callback must fire on a dropped connection")
	}
	if c.State() != protocol.StateDisconnected {
		t.Fatalf("state must be Disconnected, got %v", c.State())
	}
}

func TestDispatchEventRouting(t *testing.T) {
	var mu sync.Mutex
	var got *protocol.EventMessage
	c := newControlClient(t, "")
	c.eventHandler = func(ev *protocol.EventMessage) {
		mu.Lock()
		got = ev
		mu.Unlock()
	}

	// Malformed JSON is dropped.
	c.dispatchEvent([]byte("not-json"))
	// Non-event types are dropped.
	c.dispatchEvent([]byte(`{"type":"heartbeat"}`))
	// Type-mismatched payloads fail the event decode and are dropped.
	c.dispatchEvent([]byte(`{"type":"event","worker_id":42}`))

	mu.Lock()
	if got != nil {
		mu.Unlock()
		t.Fatal("no event must be delivered before a valid one")
	}
	mu.Unlock()

	c.dispatchEvent([]byte(`{"type":"event","worker_id":"w1","event_type":"online"}`))
	mu.Lock()
	delivered := got
	mu.Unlock()
	if delivered == nil || delivered.EventType != "online" {
		t.Fatalf("expected the online event, got %+v", delivered)
	}
}

func TestDispatchEventWithoutHandler(t *testing.T) {
	c := newControlClient(t, "")
	// A nil handler must not panic on a valid event.
	c.dispatchEvent([]byte(`{"type":"event","event_type":"online"}`))
}

func TestWriteControlMarshalFailure(t *testing.T) {
	// A func field cannot be marshalled; writeControl must surface that
	// instead of sending garbage.
	err := writeControl(nil, &badMessage{})
	if err == nil {
		t.Fatal("an unmarshalable message must fail writeControl")
	}
}

type badMessage struct {
	F func()
}

func (m *badMessage) Type() string { return "event" }

func TestHeartbeatLoopStopsOnCancel(t *testing.T) {
	c := newControlClient(t, "")
	c.config.HeartbeatInterval = 5 * time.Millisecond
	c.wg.Add(1) // heartbeatLoop's Done is normally paired by Run
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.heartbeatLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatLoop must exit on cancel")
	}
}

func TestHeartbeatLoopStopsWhenNotReady(t *testing.T) {
	c := newControlClient(t, "")
	c.config.HeartbeatInterval = 5 * time.Millisecond
	// State stays Disconnected, so the first tick returns.
	c.wg.Add(1) // heartbeatLoop's Done is normally paired by Run
	done := make(chan struct{})
	go func() {
		c.heartbeatLoop(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatLoop must stop when the client is not Ready")
	}
}

func TestHeartbeatLoopSendsWhenReady(t *testing.T) {
	srv := echoAckServer(t, []byte(`{"type":"register_ack","worker_id":"w-test","success":true}`))
	defer srv.Close()
	c := newControlClient(t, wsURL(srv))
	if err := c.register(context.Background()); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Disconnect relies on Run's cancel; initialize it manually here.
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer func() { _ = c.Disconnect() }()
	c.setState(protocol.StateReady)
	c.config.HeartbeatInterval = 10 * time.Millisecond
	// Disconnect's wg.Wait runs before it flips the state, so the loop is
	// stopped through its own context instead of the state check.
	c.wg.Add(1) // heartbeatLoop's Done is normally paired by Run
	hbCtx, hbCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.heartbeatLoop(hbCtx)
		close(done)
	}()
	// Give the ticker a chance to fire at least one heartbeat.
	time.Sleep(50 * time.Millisecond)
	hbCancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatLoop must exit on cancel")
	}
	_ = c.Disconnect()
}

func TestSendHeartbeatWithoutConn(t *testing.T) {
	c := newControlClient(t, "")
	if err := c.sendHeartbeat(); err != nil {
		t.Fatalf("no conn is a silent no-op, got %v", err)
	}
}

func TestSendHeartbeatOnClosedConn(t *testing.T) {
	c := newControlClient(t, "")
	c.mu.Lock()
	c.conn = newDeadWSConn(t)
	c.mu.Unlock()
	if err := c.sendHeartbeat(); err == nil {
		t.Fatal("a dead connection must fail the heartbeat")
	}
}

// newDeadWSConn returns a closed websocket connection for write-failure
// tests.
func newDeadWSConn(t *testing.T) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)

	clientDone := make(chan *websocket.Conn, 1)
	go func() {
		dialer := websocket.Dialer{HandshakeTimeout: time.Second}
		ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			clientDone <- nil
			return
		}
		clientDone <- ws
	}()
	ws := <-clientDone
	if ws == nil {
		t.Fatal("dial dead conn helper failed")
	}
	_ = ws.Close()
	return ws
}
