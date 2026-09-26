package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

type fakeQueue struct {
	tasks    chan *core.DeliveryTask
	acks     chan string
	nacks    chan string
	failNext atomic.Bool
	ackErr   error
	nackErr  error
}

func newFakeQueue() *fakeQueue {
	return &fakeQueue{
		tasks: make(chan *core.DeliveryTask, 16),
		acks:  make(chan string, 16),
		nacks: make(chan string, 16),
	}
}

func (q *fakeQueue) Push(ctx context.Context, task *core.DeliveryTask) error {
	select {
	case q.tasks <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *fakeQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	if q.failNext.CompareAndSwap(true, false) {
		return nil, errors.New("temporary pop failure")
	}
	select {
	case task := <-q.tasks:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *fakeQueue) Ack(_ context.Context, taskID string) error {
	q.acks <- taskID
	return q.ackErr
}

func (q *fakeQueue) Nack(_ context.Context, taskID string, _ error) error {
	q.nacks <- taskID
	return q.nackErr
}

func (q *fakeQueue) Size() int { return len(q.tasks) }

func (q *fakeQueue) Close() error { return nil }

func (q *fakeQueue) push(t *testing.T, task *core.DeliveryTask) {
	t.Helper()
	select {
	case q.tasks <- task:
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout pushing task %s", task.ID)
	}
}

func waitForID(t *testing.T, ch chan string, want string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Errorf("expected %s, got %s", want, got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for %s", want)
	}
}

func testConfig(id string) *protocol.WorkerConfig {
	// CoreURL empty = embedded mode (no control plane): Run registers
	// nothing and goes straight to queue consumption. Control-plane
	// behavior is covered by the tests that dial a real WebSocket server.
	return &protocol.WorkerConfig{
		WorkerID:          id,
		ReconnectDelay:    100 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
		Capabilities:      []string{"email", "webhook"},
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(nil, nil)
	if c.Config() == nil {
		t.Fatal("expected non-nil default config")
	}
	if c.Config().ReconnectDelay != 5*time.Second {
		t.Errorf("expected reconnect delay 5s, got %v", c.Config().ReconnectDelay)
	}
	if c.Config().HeartbeatInterval != 30*time.Second {
		t.Errorf("expected heartbeat interval 30s, got %v", c.Config().HeartbeatInterval)
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

func TestNewClientWithConfig(t *testing.T) {
	cfg := testConfig("worker-1")
	q := newFakeQueue()
	c := NewClient(cfg, q)
	if c.Config() != cfg {
		t.Error("expected config to be returned as-is")
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

func TestSetStateCallbacks(t *testing.T) {
	c := NewClient(testConfig("worker-s"), nil)

	var calls []protocol.ConnectionState
	done := make(chan struct{})
	c.OnStateChange(func(s protocol.ConnectionState) {
		calls = append(calls, s)
		if s == protocol.StateReady {
			close(done)
		}
	})

	c.setState(protocol.StateDisconnected)
	if len(calls) != 0 {
		t.Errorf("expected no callback for same state, got %v", calls)
	}

	c.setState(protocol.StateReady)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for state change callback")
	}
	if c.State() != protocol.StateReady {
		t.Errorf("expected ready state, got %s", c.State())
	}
}

func TestCallbackSetters(t *testing.T) {
	c := NewClient(testConfig("worker-cb"), nil)

	taskHandled := make(chan *core.DeliveryTask, 1)
	c.OnTask(func(task *core.DeliveryTask) error {
		taskHandled <- task
		return nil
	})
	c.OnEvent(func(event *protocol.EventMessage) {})
	c.OnConnect(func() {})
	c.OnDisconnect(func(err error) {})
	c.OnStateChange(func(state protocol.ConnectionState) {})

	if c.handler == nil || c.eventHandler == nil || c.onConnect == nil || c.onDisconnect == nil || c.onStateChange == nil {
		t.Error("expected all callbacks to be set")
	}

	task := &core.DeliveryTask{ID: "t-cb"}
	if err := c.handler(task); err != nil {
		t.Errorf("expected no error from handler, got %v", err)
	}
	select {
	case got := <-taskHandled:
		if got.ID != "t-cb" {
			t.Errorf("expected task t-cb, got %s", got.ID)
		}
	default:
		t.Error("expected handler to be invoked")
	}
}

func TestRunTaskProcessing(t *testing.T) {
	q := newFakeQueue()
	cfg := testConfig("worker-run")
	c := NewClient(cfg, q)

	states := make(chan protocol.ConnectionState, 16)
	c.OnStateChange(func(s protocol.ConnectionState) { states <- s })
	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })
	c.OnEvent(func(event *protocol.EventMessage) {})
	c.OnTask(func(task *core.DeliveryTask) error {
		if task.ID == "task-fail" {
			return errors.New("handler error")
		}
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}

	if c.State() != protocol.StateReady {
		t.Errorf("expected ready state, got %s", c.State())
	}

	var seenConnecting, seenReady bool
drain:
	for {
		select {
		case s := <-states:
			if s == protocol.StateConnecting {
				seenConnecting = true
			}
			if s == protocol.StateReady {
				seenReady = true
			}
		default:
			break drain
		}
	}
	if !seenConnecting || !seenReady {
		t.Errorf("expected connecting and ready states, connecting=%v ready=%v", seenConnecting, seenReady)
	}

	time.Sleep(50 * time.Millisecond)

	q.push(t, &core.DeliveryTask{ID: "task-ok"})
	waitForID(t, q.acks, "task-ok")

	q.push(t, &core.DeliveryTask{ID: "task-fail"})
	waitForID(t, q.nacks, "task-fail")

	q.failNext.Store(true)
	q.push(t, &core.DeliveryTask{ID: "task-ok2"})
	waitForID(t, q.acks, "task-ok2")

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}

	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

// TestRunPopFailureBackoff pins the pop-failure backoff branch: failNext
// is set before Run starts, so the worker's very first Pop fails no matter
// how the goroutine is scheduled (nothing can race the flag away or
// pre-deliver a task). The loop must back off, retry, and then consume
// the task delivered afterwards.
func TestRunPopFailureBackoff(t *testing.T) {
	q := newFakeQueue()
	q.failNext.Store(true)
	c := NewClient(testConfig("worker-popfail"), q)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	// The first Pop failed and the retry is now parked on the empty
	// queue; delivering a task proves the loop recovered from the error.
	q.push(t, &core.DeliveryTask{ID: "task-after-fail"})
	waitForID(t, q.acks, "task-after-fail")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
}

func TestRunAckNackErrors(t *testing.T) {
	q := newFakeQueue()
	q.ackErr = errors.New("ack error")
	q.nackErr = errors.New("nack error")
	c := NewClient(testConfig("worker-errs"), q)

	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })
	c.OnTask(func(task *core.DeliveryTask) error {
		return errors.New("always fails")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}

	q.push(t, &core.DeliveryTask{ID: "task-nack-err"})
	waitForID(t, q.nacks, "task-nack-err")

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
}

func TestRunWithoutHandler(t *testing.T) {
	q := newFakeQueue()
	c := NewClient(testConfig("worker-nohandler"), q)

	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}

	q.push(t, &core.DeliveryTask{ID: "task-auto"})
	waitForID(t, q.acks, "task-auto")

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
}

func TestRunLegacyNoQueue(t *testing.T) {
	c := NewClient(testConfig("worker-legacy"), nil)

	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}

	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

func TestDisconnectWhileRunning(t *testing.T) {
	q := newFakeQueue()
	c := NewClient(testConfig("worker-disc"), q)

	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })
	c.OnDisconnect(func(err error) {})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}

	c.setState(protocol.StateConnecting)
	time.Sleep(50 * time.Millisecond)

	if err := c.Disconnect(); err != nil {
		t.Errorf("expected no error from Disconnect, got %v", err)
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
}

func TestDisconnectWhenNotRunning(t *testing.T) {
	c := NewClient(testConfig("worker-idle"), nil)
	if err := c.Disconnect(); err != nil {
		t.Errorf("expected no error from Disconnect, got %v", err)
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

func TestClose(t *testing.T) {
	q := newFakeQueue()
	c := NewClient(testConfig("worker-close"), q)

	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}

	c.setState(protocol.StateConnecting)
	time.Sleep(50 * time.Millisecond)

	if err := c.Close(); err != nil {
		t.Errorf("expected no error from Close, got %v", err)
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}

	if err := c.Close(); err != nil {
		t.Errorf("expected no error from repeated Close, got %v", err)
	}
}

func TestDetectPlatform(t *testing.T) {
	if detectPlatform() != runtime.GOOS {
		t.Errorf("expected %s, got %s", runtime.GOOS, detectPlatform())
	}
}

func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func newMockHeraldServer(registered chan *protocol.RegisterMessage, beats chan *protocol.HeartbeatMessage) *httptest.Server {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var envelope struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				errMsg, _ := protocol.MarshalMessage(&protocol.ErrorMessage{Code: "bad_request", Message: "invalid json"})
				_ = conn.WriteMessage(websocket.TextMessage, errMsg)
				continue
			}
			switch envelope.Type {
			case protocol.MessageTypeRegister:
				var msg protocol.RegisterMessage
				if err := json.Unmarshal(data, &msg); err != nil {
					return
				}
				ack, _ := protocol.MarshalMessage(&protocol.RegisterAckMessage{
					WorkerID:  msg.WorkerID,
					Success:   true,
					ServerID:  "herald-test",
					Timestamp: time.Now().Unix(),
				})
				_ = conn.WriteMessage(websocket.TextMessage, ack)
				registered <- &msg
			case protocol.MessageTypeHeartbeat:
				var msg protocol.HeartbeatMessage
				if err := json.Unmarshal(data, &msg); err != nil {
					return
				}
				beats <- &msg
			default:
				errMsg, _ := protocol.MarshalMessage(&protocol.ErrorMessage{Code: "unknown_type", Message: "unknown message type"})
				_ = conn.WriteMessage(websocket.TextMessage, errMsg)
			}
		}
	}))
}

func TestRegisterHandshakeWithMockServer(t *testing.T) {
	registered := make(chan *protocol.RegisterMessage, 1)
	beats := make(chan *protocol.HeartbeatMessage, 1)
	server := newMockHeraldServer(registered, beats)
	defer server.Close()

	cfg := testConfig("worker-ws")
	c := NewClient(cfg, nil)
	if c.Config().WorkerID != "worker-ws" {
		t.Fatalf("expected worker-ws, got %s", c.Config().WorkerID)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}
	defer func() { _ = conn.Close() }()

	reg := &protocol.RegisterMessage{
		WorkerID:     cfg.WorkerID,
		Mode:         "remote",
		Platform:     detectPlatform(),
		Version:      "1.0.0",
		Capabilities: cfg.Capabilities,
	}
	data, err := protocol.MarshalMessage(reg)
	if err != nil {
		t.Fatalf("failed to marshal register message: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send register message: %v", err)
	}

	_, ackData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register ack: %v", err)
	}
	var ack struct {
		Type     string `json:"type"`
		WorkerID string `json:"worker_id"`
		Success  bool   `json:"success"`
		ServerID string `json:"server_id"`
	}
	if err := json.Unmarshal(ackData, &ack); err != nil {
		t.Fatalf("failed to parse register ack: %v", err)
	}
	if ack.Type != protocol.MessageTypeRegisterAck {
		t.Errorf("expected %s, got %s", protocol.MessageTypeRegisterAck, ack.Type)
	}
	if !ack.Success || ack.WorkerID != cfg.WorkerID {
		t.Errorf("unexpected ack: success=%v worker_id=%s", ack.Success, ack.WorkerID)
	}

	hb, _ := protocol.MarshalMessage(&protocol.HeartbeatMessage{WorkerID: cfg.WorkerID, Timestamp: time.Now().Unix()})
	if err := conn.WriteMessage(websocket.TextMessage, hb); err != nil {
		t.Fatalf("failed to send heartbeat: %v", err)
	}

	select {
	case msg := <-registered:
		if msg.WorkerID != cfg.WorkerID || msg.Mode != "remote" || msg.Platform != detectPlatform() || msg.Version != "1.0.0" {
			t.Errorf("unexpected register message: %+v", msg)
		}
		if len(msg.Capabilities) != 2 {
			t.Errorf("expected 2 capabilities, got %d", len(msg.Capabilities))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for register message on server")
	}

	select {
	case msg := <-beats:
		if msg.WorkerID != cfg.WorkerID {
			t.Errorf("expected heartbeat from %s, got %s", cfg.WorkerID, msg.WorkerID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for heartbeat message on server")
	}
}

func TestMockServerRejectsInvalidMessage(t *testing.T) {
	registered := make(chan *protocol.RegisterMessage, 1)
	beats := make(chan *protocol.HeartbeatMessage, 1)
	server := newMockHeraldServer(registered, beats)
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err != nil {
		t.Fatalf("failed to dial mock server: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatalf("failed to send invalid message: %v", err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read error response: %v", err)
	}
	var errMsg protocol.ErrorMessage
	if err := json.Unmarshal(data, &errMsg); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if errMsg.Code != "bad_request" {
		t.Errorf("expected bad_request, got %s", errMsg.Code)
	}

	dispatch, _ := protocol.MarshalMessage(&protocol.DispatchMessage{TaskID: "t1", Timestamp: time.Now().Unix()})
	if err := conn.WriteMessage(websocket.TextMessage, dispatch); err != nil {
		t.Fatalf("failed to send dispatch message: %v", err)
	}
	_, data, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read error response: %v", err)
	}
	if err := json.Unmarshal(data, &errMsg); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	if errMsg.Code != "unknown_type" {
		t.Errorf("expected unknown_type, got %s", errMsg.Code)
	}
}

func TestServerRejectsUpgrade(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server), nil)
	if err == nil {
		_ = conn.Close()
		t.Fatal("expected dial error when server rejects upgrade")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

// newRejectServer answers the first register message with a failed ack.
func newRejectServer(reason string) *httptest.Server {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != protocol.MessageTypeRegister {
			return
		}
		ack, _ := protocol.MarshalMessage(&protocol.RegisterAckMessage{
			Success: false,
			Error:   reason,
		})
		_ = conn.WriteMessage(websocket.TextMessage, ack)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
}

// newAckThenDropServer acks the register handshake, then immediately drops
// the connection to exercise the client's disconnect path.
func newAckThenDropServer() *httptest.Server {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != protocol.MessageTypeRegister {
			return
		}
		ack, _ := protocol.MarshalMessage(&protocol.RegisterAckMessage{
			WorkerID: "worker-drop",
			Success:  true,
			ServerID: "herald-test",
		})
		_ = conn.WriteMessage(websocket.TextMessage, ack)
	}))
}

func TestClientRegisterWithMockServer(t *testing.T) {
	registered := make(chan *protocol.RegisterMessage, 1)
	beats := make(chan *protocol.HeartbeatMessage, 8)
	server := newMockHeraldServer(registered, beats)
	defer server.Close()

	cfg := testConfig("worker-e2e")
	cfg.CoreURL = wsURL(server)
	c := NewClient(cfg, newFakeQueue())

	connected := make(chan struct{}, 1)
	c.OnConnect(func() { connected <- struct{}{} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case msg := <-registered:
		if msg.WorkerID != cfg.WorkerID {
			t.Errorf("registered worker = %s, want %s", msg.WorkerID, cfg.WorkerID)
		}
		if msg.Mode != "remote" || msg.Platform != detectPlatform() || msg.Version != "1.0.0" {
			t.Errorf("unexpected register message: %+v", msg)
		}
		if len(msg.Capabilities) != 2 {
			t.Errorf("expected 2 capabilities, got %d", len(msg.Capabilities))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for register message on server")
	}

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connect callback")
	}
	if c.State() != protocol.StateReady {
		t.Errorf("expected ready state, got %s", c.State())
	}

	// The control-plane connection is live: heartbeats must reach the
	// server (HeartbeatInterval is 10ms in testConfig).
	select {
	case b := <-beats:
		if b.WorkerID != cfg.WorkerID {
			t.Errorf("expected heartbeat from %s, got %s", cfg.WorkerID, b.WorkerID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for heartbeat on server")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

func TestClientRegisterRejected(t *testing.T) {
	server := newRejectServer("unauthorized worker")
	defer server.Close()

	cfg := testConfig("worker-rej")
	cfg.CoreURL = wsURL(server)
	c := NewClient(cfg, nil)

	// Run fails synchronously: the register handshake is rejected before
	// queue consumption starts.
	err := c.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "register rejected: unauthorized worker") {
		t.Errorf("Run() error = %v, want register rejection", err)
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

func TestClientRegisterDialFailure(t *testing.T) {
	cfg := testConfig("worker-dialfail")
	cfg.CoreURL = "ws://127.0.0.1:1/worker"
	c := NewClient(cfg, nil)

	err := c.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to register") {
		t.Errorf("Run() error = %v, want dial failure", err)
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}
}

func TestClientDetectsServerClose(t *testing.T) {
	server := newAckThenDropServer()
	defer server.Close()

	cfg := testConfig("worker-drop")
	cfg.CoreURL = wsURL(server)
	c := NewClient(cfg, newFakeQueue())

	disconnected := make(chan error, 1)
	c.OnDisconnect(func(err error) { disconnected <- err })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	// The server drops the connection right after acking: the client must
	// notice, fire onDisconnect and mark itself Disconnected (queue
	// consumption keeps running).
	select {
	case err := <-disconnected:
		if err == nil {
			t.Error("expected non-nil error from disconnect callback")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for disconnect callback")
	}
	if c.State() != protocol.StateDisconnected {
		t.Errorf("expected disconnected state, got %s", c.State())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error from Run, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Run to return")
	}
}
