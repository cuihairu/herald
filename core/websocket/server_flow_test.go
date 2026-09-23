package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

type channelHandler struct {
	mockHandler
	heartbeatCh  chan string
	disconnectCh chan string
}

func newChannelHandler() *channelHandler {
	return &channelHandler{
		heartbeatCh:  make(chan string, 8),
		disconnectCh: make(chan string, 8),
	}
}

func (h *channelHandler) OnHeartbeat(workerID string) error {
	select {
	case h.heartbeatCh <- workerID:
	default:
	}
	return nil
}

func (h *channelHandler) OnDisconnect(workerID string) {
	select {
	case h.disconnectCh <- workerID:
	default:
	}
}

func newFlowTestServer(t *testing.T, handler ConnHandler, cfg *Config) (*Server, *httptest.Server) {
	t.Helper()
	if cfg == nil {
		cfg = &Config{
			Addr:           "127.0.0.1:0",
			ReadTimeout:    5 * time.Second,
			WriteTimeout:   5 * time.Second,
			PingTimeout:    time.Second,
			PingInterval:   time.Second,
			AllowedOrigins: []string{"*"},
		}
	}
	server := NewServer(cfg, handler)
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	t.Cleanup(ts.Close)
	return server, ts
}

func dialFlowWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + ts.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func registerFlowWorker(t *testing.T, conn *websocket.Conn, workerID string) {
	t.Helper()
	msg := &protocol.RegisterMessage{
		WorkerID:     workerID,
		Platform:     "linux",
		Version:      "1.0.0",
		Capabilities: []string{"telegram"},
	}
	payload, err := protocol.MarshalMessage(msg)
	if err != nil {
		t.Fatalf("failed to marshal register message: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("failed to write register message: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register ack: %v", err)
	}
	var ack protocol.RegisterAckMessage
	if err := json.Unmarshal(data, &ack); err != nil {
		t.Fatalf("failed to decode register ack: %v", err)
	}
	if ack.WorkerID != workerID || !ack.Success {
		t.Fatalf("unexpected register ack: %#v", ack)
	}
}

func waitChannelMessage(t *testing.T, ch <-chan string, timeout time.Duration) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(timeout):
		t.Fatal("timed out waiting for handler callback")
		return ""
	}
}

func assertNoChannelMessage(t *testing.T, ch <-chan string, timeout time.Duration) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected handler callback: %s", v)
	case <-time.After(timeout):
	}
}

func TestHub_OnHeartbeat(t *testing.T) {
	t.Run("updates heartbeat for registered worker", func(t *testing.T) {
		registry := worker.NewRegistry()
		hub := NewHub(NewServer(nil, nil), registry)

		if err := registry.Register(&worker.Info{ID: "worker-1", Mode: worker.Remote}); err != nil {
			t.Fatalf("failed to register worker: %v", err)
		}
		before, err := registry.Get("worker-1")
		if err != nil {
			t.Fatalf("expected worker to exist: %v", err)
		}

		time.Sleep(10 * time.Millisecond)
		if err := hub.OnHeartbeat("worker-1"); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		after, err := registry.Get("worker-1")
		if err != nil {
			t.Fatalf("expected worker to exist after heartbeat: %v", err)
		}
		if !after.LastHeartbeat.After(before.LastHeartbeat) {
			t.Error("expected LastHeartbeat to be updated")
		}
	})

	t.Run("unknown worker returns error", func(t *testing.T) {
		hub := NewHub(NewServer(nil, nil), worker.NewRegistry())

		if err := hub.OnHeartbeat("nonexistent"); err == nil {
			t.Error("expected error for unknown worker")
		}
	})
}

func TestHandleWebSocketUpgradeFailure(t *testing.T) {
	server := NewServer(nil, &mockHandler{})
	ts := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("failed to send plain HTTP request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for non-websocket request, got %d", resp.StatusCode)
	}
	if server.GetWorkerCount() != 0 {
		t.Errorf("expected 0 workers, got %d", server.GetWorkerCount())
	}
}

func TestHandleConnectionFlow(t *testing.T) {
	handler := newChannelHandler()
	server, ts := newFlowTestServer(t, handler, nil)
	conn := dialFlowWS(t, ts)

	registerFlowWorker(t, conn, "worker-1")

	if err := conn.WriteMessage(websocket.TextMessage, []byte("not json")); err != nil {
		t.Fatalf("failed to write invalid message: %v", err)
	}

	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0x00, 0x01}); err != nil {
		t.Fatalf("failed to write binary message: %v", err)
	}

	hb := &protocol.HeartbeatMessage{
		WorkerID: "worker-1",
		Status:   map[string]interface{}{"tasks_sent": 3},
	}
	payload, err := protocol.MarshalMessage(hb)
	if err != nil {
		t.Fatalf("failed to marshal heartbeat message: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("failed to write heartbeat message: %v", err)
	}

	if id := waitChannelMessage(t, handler.heartbeatCh, 3*time.Second); id != "worker-1" {
		t.Fatalf("expected heartbeat callback for worker-1, got %s", id)
	}

	server.mu.RLock()
	state := server.workers["worker-1"]
	server.mu.RUnlock()
	if state == nil {
		t.Fatal("expected worker-1 to stay registered")
	}
	if state.Status["tasks_sent"] != float64(3) {
		t.Errorf("expected status tasks_sent to be 3, got %v", state.Status["tasks_sent"])
	}

	pongCh := make(chan string, 1)
	conn.SetPongHandler(func(appData string) error {
		select {
		case pongCh <- appData:
		default:
		}
		return nil
	})
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	if err := conn.WriteControl(websocket.PingMessage, []byte("probe"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("failed to send ping: %v", err)
	}
	select {
	case data := <-pongCh:
		if data != "probe" {
			t.Errorf("expected pong payload probe, got %s", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pong")
	}
}

func TestHandleConnectionClientClose(t *testing.T) {
	handler := newChannelHandler()
	server, ts := newFlowTestServer(t, handler, nil)

	t.Run("registered client graceful close", func(t *testing.T) {
		conn := dialFlowWS(t, ts)
		registerFlowWorker(t, conn, "worker-1")

		if err := conn.Close(); err != nil {
			t.Fatalf("failed to close client: %v", err)
		}
		if id := waitChannelMessage(t, handler.disconnectCh, 3*time.Second); id != "worker-1" {
			t.Fatalf("expected disconnect callback for worker-1, got %s", id)
		}
		if server.GetWorkerCount() != 0 {
			t.Errorf("expected 0 workers after disconnect, got %d", server.GetWorkerCount())
		}
	})

	t.Run("registered client abnormal close", func(t *testing.T) {
		conn := dialFlowWS(t, ts)
		registerFlowWorker(t, conn, "worker-2")

		if err := conn.UnderlyingConn().Close(); err != nil {
			t.Fatalf("failed to close underlying connection: %v", err)
		}
		if id := waitChannelMessage(t, handler.disconnectCh, 3*time.Second); id != "worker-2" {
			t.Fatalf("expected disconnect callback for worker-2, got %s", id)
		}
		if _, err := server.GetWorker("worker-2"); err == nil {
			t.Error("expected worker-2 to be removed")
		}
	})

	t.Run("unregistered client close triggers no callback", func(t *testing.T) {
		conn := dialFlowWS(t, ts)
		if err := conn.Close(); err != nil {
			t.Fatalf("failed to close client: %v", err)
		}
		assertNoChannelMessage(t, handler.disconnectCh, 300*time.Millisecond)
	})
}

func TestHandleConnectionReadTimeout(t *testing.T) {
	handler := newChannelHandler()
	cfg := &Config{
		Addr:           "127.0.0.1:0",
		ReadTimeout:    250 * time.Millisecond,
		WriteTimeout:   5 * time.Second,
		PingTimeout:    time.Second,
		PingInterval:   time.Second,
		AllowedOrigins: []string{"*"},
	}
	server, ts := newFlowTestServer(t, handler, cfg)
	conn := dialFlowWS(t, ts)

	registerFlowWorker(t, conn, "worker-1")

	if id := waitChannelMessage(t, handler.disconnectCh, 3*time.Second); id != "worker-1" {
		t.Fatalf("expected disconnect callback for worker-1, got %s", id)
	}
	if _, err := server.GetWorker("worker-1"); err == nil {
		t.Error("expected worker-1 to be removed after read timeout")
	}
}

func TestDisconnectWorkerConnected(t *testing.T) {
	handler := newChannelHandler()
	server, ts := newFlowTestServer(t, handler, nil)
	conn := dialFlowWS(t, ts)
	registerFlowWorker(t, conn, "worker-1")

	if err := server.DisconnectWorker("worker-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id := waitChannelMessage(t, handler.disconnectCh, 3*time.Second); id != "worker-1" {
		t.Fatalf("expected disconnect callback for worker-1, got %s", id)
	}
	if _, err := server.GetWorker("worker-1"); err == nil {
		t.Error("expected worker-1 to be removed")
	}

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Error("expected read error after server-side disconnect")
	}
}

func TestDisconnectWorkerWithoutConn(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	server.mu.Lock()
	server.workers["ghost"] = &ConnectionState{WorkerID: "ghost"}
	server.mu.Unlock()

	if err := server.DisconnectWorker("ghost"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if server.GetWorkerCount() != 0 {
		t.Errorf("expected 0 workers, got %d", server.GetWorkerCount())
	}
	if len(handler.disconnectCalls) != 1 || handler.disconnectCalls[0] != "ghost" {
		t.Errorf("unexpected disconnect calls: %#v", handler.disconnectCalls)
	}
}
