package websocket

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

// syncHandler is a race-safe ConnHandler that records callbacks so the
// integration test can assert on them after the connection goroutines ran.
type syncHandler struct {
	mu           sync.Mutex
	registered   []string
	disconnected []string
	events       []string
	acks         []string
	heartbeats   []string
}

func (h *syncHandler) OnRegister(workerID string, msg *protocol.RegisterMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.registered = append(h.registered, workerID)
	return nil
}

func (h *syncHandler) OnHeartbeat(workerID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.heartbeats = append(h.heartbeats, workerID)
	return nil
}

func (h *syncHandler) OnTaskAck(taskID string, success bool, errMsg string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.acks = append(h.acks, taskID)
	return nil
}

func (h *syncHandler) OnWorkerEvent(workerID string, event *protocol.EventMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event.EventType)
	return nil
}

func (h *syncHandler) OnDisconnect(workerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.disconnected = append(h.disconnected, workerID)
}

func (h *syncHandler) calls() (registered, disconnected, events, acks, heartbeats []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.registered, h.disconnected, h.events, h.acks, h.heartbeats
}

// startWSTestServer serves a real upgrade endpoint on an httptest server and
// returns the ws:// URL to dial.
func startWSTestServer(t *testing.T) (*Server, *syncHandler, string) {
	t.Helper()
	// A non-nil Config is taken verbatim, so the timeouts must be set.
	handler := &syncHandler{}
	server := NewServer(&Config{
		Addr:           "127.0.0.1:0",
		AllowedOrigins: []string{"*"},
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		PingTimeout:    30 * time.Second,
		PingInterval:   20 * time.Second,
	}, handler)
	testSrv := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	t.Cleanup(testSrv.Close)
	url := "ws" + strings.TrimPrefix(testSrv.URL, "http") + "/worker"
	return server, handler, url
}

// TestWebSocketConnectionLifecycle drives a real worker connection through
// registration, heartbeat, ack, event, junk frames and disconnect, asserting
// every callback fires and the worker table tracks the connection.
func TestWebSocketConnectionLifecycle(t *testing.T) {
	server, handler, url := startWSTestServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Registration: the server answers with a success ack.
	reg := `{"type":"register","worker_id":"w1","mode":"remote","platform":"linux","version":"v1","capabilities":["push"]}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(reg)); err != nil {
		t.Fatalf("send register: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var ack protocol.RegisterAckMessage
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if !ack.Success || ack.WorkerID != "w1" || ack.ServerID == "" {
		t.Fatalf("unexpected ack: %+v", ack)
	}
	if got := server.GetWorkerCount(); got != 1 {
		t.Fatalf("expected 1 registered worker, got %d", got)
	}

	// Heartbeat updates the tracked state without a reply.
	hb := `{"type":"heartbeat","worker_id":"w1","status":{"load":2}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(hb)); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	// An ack from the worker reaches the handler.
	ackMsg := `{"type":"ack","task_id":"t1","success":true}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ackMsg)); err != nil {
		t.Fatalf("send ack: %v", err)
	}

	// A worker event reaches the handler.
	ev := `{"type":"event","worker_id":"w1","event_type":"online"}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ev)); err != nil {
		t.Fatalf("send event: %v", err)
	}

	// Unknown types are logged and ignored, binary frames are skipped;
	// neither may tear down the connection.
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"mystery"}`)); err != nil {
		t.Fatalf("send unknown type: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0xde, 0xad}); err != nil {
		t.Fatalf("send binary frame: %v", err)
	}

	// Wait until the queued frames were all processed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, _, events, acks, heartbeats := handler.calls()
		if len(heartbeats) == 1 && len(acks) == 1 && len(events) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("handler callbacks incomplete: heartbeats=%v acks=%v events=%v", heartbeats, acks, events)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The heartbeat refreshed the stored state.
	server.mu.RLock()
	state := server.workers["w1"]
	server.mu.RUnlock()
	if state == nil || state.Status["load"] != float64(2) {
		t.Fatalf("heartbeat status not stored: %+v", state)
	}

	// Disconnect cleans up the worker table and fires the callback.
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for server.GetWorkerCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("worker was not removed after disconnect")
		}
		time.Sleep(10 * time.Millisecond)
	}
	registered, disconnected, _, _, _ := handler.calls()
	if len(registered) != 1 || registered[0] != "w1" || len(disconnected) != 1 || disconnected[0] != "w1" {
		t.Fatalf("unexpected lifecycle callbacks: registered=%v disconnected=%v", registered, disconnected)
	}
}

// TestWebSocketRegisterMarshalFailure injects an encoder failure and drives
// handleRegister directly: the registration must fail with the marshal error
// and the stale ack must never be written.
func TestWebSocketRegisterMarshalFailure(t *testing.T) {
	server, _, url := startWSTestServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("encode boom") }
	defer func() { jsonMarshal = orig }()

	state := &ConnectionState{conn: conn, Status: make(map[string]interface{})}
	msg := &protocol.RegisterMessage{WorkerID: "w-broken", Mode: "remote"}
	err = server.handleRegister(state, msg)
	if err == nil || !strings.Contains(err.Error(), "failed to marshal ack") {
		t.Fatalf("expected the marshal error, got %v", err)
	}

	// A closed connection fails the write deadline on the ack path.
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	err = server.handleRegister(state, &protocol.RegisterMessage{WorkerID: "w-closed", Mode: "remote"})
	if err == nil {
		t.Fatal("registering over a closed connection must fail")
	}
}

// TestWebSocketUpgradeRejectsPlainGET confirms the upgrade error branch: a
// plain HTTP request is refused before any connection state is created.
func TestWebSocketUpgradeRejectsPlainGET(t *testing.T) {
	_, _, wsURL := startWSTestServer(t)
	httpURL := "http" + strings.TrimPrefix(wsURL, "ws")

	resp, err := http.Get(httpURL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 400 {
		t.Fatalf("a plain GET must be refused with 4xx, got %d", resp.StatusCode)
	}
}
