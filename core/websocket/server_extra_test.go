package websocket

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

// failingHandler reports an error from every callback so the server's
// handler-error logging paths are exercised.
type failingHandler struct {
	mockHandler
}

func (h *failingHandler) OnRegister(workerID string, msg *protocol.RegisterMessage) error {
	return &mockError{"register failed"}
}

func (h *failingHandler) OnHeartbeat(workerID string) error {
	return &mockError{"heartbeat failed"}
}

func (h *failingHandler) OnTaskAck(taskID string, success bool, errMsg string) error {
	return &mockError{"ack failed"}
}

func (h *failingHandler) OnWorkerEvent(workerID string, event *protocol.EventMessage) error {
	return &mockError{"event failed"}
}

type mockError struct{ msg string }

func (e *mockError) Error() string { return e.msg }

func TestServerStartListenError(t *testing.T) {
	// Occupy a port first so the server's listener fails immediately.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	addr := l.Addr().String()

	server := NewServer(&Config{Addr: addr}, &mockHandler{})
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give the serving goroutine time to attempt ListenAndServe and log
	// the bind failure, then shut the server down cleanly.
	time.Sleep(100 * time.Millisecond)
	if err := server.Stop(); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestServerStopClosesWorkerConns(t *testing.T) {
	server, ts := newFlowTestServer(t, newChannelHandler(), nil)

	client := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer client.Close()

	wsURL := "ws" + strings.TrimPrefix(client.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	reg, _ := protocol.MarshalMessage(&protocol.RegisterMessage{WorkerID: "w-stop", Platform: "linux"})
	if err := conn.WriteMessage(websocket.TextMessage, reg); err != nil {
		t.Fatalf("write register: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read ack: %v", err)
	}

	if server.GetWorkerCount() != 1 {
		t.Fatalf("workers = %d, want 1", server.GetWorkerCount())
	}

	if err := server.Stop(); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
	if server.GetWorkerCount() != 0 {
		t.Errorf("workers after Stop = %d, want 0", server.GetWorkerCount())
	}
	_ = ts
}

func TestHandleConnectionUnexpectedClose(t *testing.T) {
	handler := newChannelHandler()
	_, ts := newFlowTestServer(t, handler, nil)

	var wsURL string
	{
		u := strings.TrimPrefix(ts.URL, "http")
		wsURL = "ws" + u
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	registerFlowWorker(t, conn, "w-abrupt")

	// Close with a "normal closure" code, which the server treats as an
	// unexpected close (only GoingAway/AbnormalClosure are whitelisted).
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye"),
		time.Now().Add(time.Second))

	_ = conn.Close()
	if got := waitChannelMessage(t, handler.disconnectCh, 3*time.Second); got != "w-abrupt" {
		t.Errorf("disconnect callback = %q, want w-abrupt", got)
	}
}

func TestPingPong(t *testing.T) {
	_, ts := newFlowTestServer(t, newChannelHandler(), nil)
	url := "ws" + ts.URL[len("http"):]

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Gorilla delivers Pong control frames to the pong handler, not to
	// ReadMessage, so capture the payload there.
	pongCh := make(chan string, 1)
	conn.SetPongHandler(func(appData string) error {
		select {
		case pongCh <- appData:
		default:
		}
		return nil
	})

	if err := conn.WriteControl(websocket.PingMessage, []byte("ping-payload"), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	// Reading pumps the connection so the pong handler can fire; the read
	// itself times out since no data frame follows.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, _ = conn.ReadMessage()

	select {
	case got := <-pongCh:
		if got != "ping-payload" {
			t.Errorf("pong payload = %q, want ping-payload", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pong")
	}
}

func TestMalformedMessageBodies(t *testing.T) {
	server, ts := newFlowTestServer(t, newChannelHandler(), nil)
	url := "ws" + ts.URL[len("http"):]

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Valid outer type, but body fields with the wrong JSON types make the
	// per-message decodes fail.
	badBodies := []string{
		`{"type":"register","capabilities":"not-an-array"}`,
		`{"type":"heartbeat","status":"not-an-object"}`,
		`{"type":"ack","success":"not-a-bool"}`,
		`{"type":"event","event_type":123}`,
	}
	for _, body := range badBodies {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
			t.Fatalf("write %q: %v", body, err)
		}
	}

	// The server must survive the bad messages and still process a valid one.
	registerFlowWorker(t, conn, "w-recovered")
	if server.GetWorkerCount() != 1 {
		t.Errorf("workers = %d, want 1 after recovery", server.GetWorkerCount())
	}
}

func TestHandlerErrorPaths(t *testing.T) {
	handler := &failingHandler{}
	_, ts := newFlowTestServer(t, handler, nil)
	url := "ws" + ts.URL[len("http"):]

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	registerFlowWorker(t, conn, "w-err")

	// Heartbeat with failing handler.
	hb, _ := protocol.MarshalMessage(&protocol.HeartbeatMessage{WorkerID: "w-err", Timestamp: time.Now().Unix()})
	if err := conn.WriteMessage(websocket.TextMessage, hb); err != nil {
		t.Fatalf("write heartbeat: %v", err)
	}

	// Ack with failing handler.
	ack, _ := protocol.MarshalMessage(&protocol.AckMessage{TaskID: "t-1", Success: true})
	if err := conn.WriteMessage(websocket.TextMessage, ack); err != nil {
		t.Fatalf("write ack: %v", err)
	}

	// Event with failing handler.
	ev, _ := protocol.MarshalMessage(&protocol.EventMessage{WorkerID: "w-err", EventType: "online"})
	if err := conn.WriteMessage(websocket.TextMessage, ev); err != nil {
		t.Fatalf("write event: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // let the server process all three
}

func TestPruneStaleWithRealConn(t *testing.T) {
	handler := newChannelHandler()
	server, ts := newFlowTestServer(t, handler, nil)
	url := "ws" + ts.URL[len("http"):]

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	registerFlowWorker(t, conn, "w-stale")

	// Backdate the heartbeat so the worker looks stale.
	server.mu.Lock()
	state := server.workers["w-stale"]
	server.mu.Unlock()
	if state == nil {
		t.Fatal("worker w-stale not registered")
	}
	server.mu.Lock()
	state.LastHeartbeat = time.Now().Add(-5 * time.Minute)
	server.mu.Unlock()

	server.pruneStaleWorkers(time.Now())

	if server.GetWorkerCount() != 0 {
		t.Errorf("workers after prune = %d, want 0", server.GetWorkerCount())
	}
	if got := waitChannelMessage(t, handler.disconnectCh, 3*time.Second); got != "w-stale" {
		t.Errorf("disconnect callback = %q, want w-stale", got)
	}
}

func TestRegisterOnClosedConn(t *testing.T) {
	handler := newChannelHandler()
	server, ts := newFlowTestServer(t, handler, nil)
	url := "ws" + ts.URL[len("http"):]

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	registerFlowWorker(t, conn, "w-closed")

	// Close the server-side conn behind the client's back.
	server.mu.Lock()
	state := server.workers["w-closed"]
	server.mu.Unlock()
	if state == nil {
		t.Fatal("worker w-closed not registered")
	}
	_ = state.conn.Close()

	// A second register on the dead conn must fail the ack write cleanly.
	reg, _ := protocol.MarshalMessage(&protocol.RegisterMessage{WorkerID: "w-closed", Platform: "linux"})
	if err := conn.WriteMessage(websocket.TextMessage, reg); err != nil {
		t.Fatalf("write register: %v", err)
	}

	waitChannelMessage(t, handler.disconnectCh, 3*time.Second)
}

func TestHandleMessageUnknownAndInvalid(t *testing.T) {
	server := NewServer(nil, &mockHandler{})

	state := &ConnectionState{
		conn:        &websocket.Conn{},
		ConnectedAt: time.Now(),
		Status:      make(map[string]interface{}),
	}

	if err := server.handleMessage(state, []byte("not-json")); err == nil {
		t.Error("handleMessage(not-json) should fail")
	}
	if err := server.handleMessage(state, []byte(`{"type":"mystery"}`)); err != nil {
		t.Errorf("handleMessage(unknown type) error = %v, want nil", err)
	}
}

func TestStopDoubleAndWorkerInfoJSON(t *testing.T) {
	server, _ := newFlowTestServer(t, newChannelHandler(), nil)
	if err := server.Stop(); err != nil {
		t.Errorf("first Stop() error = %v", err)
	}
	if err := server.Stop(); err != nil {
		t.Errorf("second Stop() error = %v", err)
	}

	info := WorkerInfo{
		WorkerID:      "w-json",
		Platform:      "linux",
		Version:       "1.0",
		Capabilities:  []string{"slack"},
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Status:        map[string]interface{}{"cpu": 0.5},
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal WorkerInfo: %v", err)
	}
	var back WorkerInfo
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal WorkerInfo: %v", err)
	}
	if back.WorkerID != "w-json" || back.Platform != "linux" {
		t.Errorf("round-trip WorkerInfo = %+v", back)
	}
}
