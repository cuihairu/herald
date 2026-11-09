package websocket

import (
	"encoding/json"
	"errors"
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

func TestCheckStaleWorkersTickerPrunes(t *testing.T) {
	orig := staleCheckInterval
	staleCheckInterval = 5 * time.Millisecond
	defer func() { staleCheckInterval = orig }()

	handler := newChannelHandler()
	server := NewServer(&Config{Addr: "127.0.0.1:0"}, handler)

	// Seed a worker whose heartbeat is far in the past so the periodic
	// sweep has something to prune.
	server.mu.Lock()
	server.workers["w-ticker"] = &ConnectionState{
		WorkerID:      "w-ticker",
		LastHeartbeat: time.Now().Add(-5 * time.Minute),
		Status:        make(map[string]interface{}),
	}
	server.mu.Unlock()

	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// The 5ms ticker must prune the stale worker without any traffic.
	pruned := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if server.GetWorkerCount() == 0 {
			pruned = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !pruned {
		t.Error("expected stale worker to be pruned by the ticker")
	}
	if got := waitChannelMessage(t, handler.disconnectCh, 3*time.Second); got != "w-ticker" {
		t.Errorf("disconnect callback = %q, want w-ticker", got)
	}

	if err := server.Stop(); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestHandleRegisterAckMarshalError(t *testing.T) {
	server, ts := newFlowTestServer(t, newChannelHandler(), nil)
	url := "ws" + ts.URL[len("http"):]

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	registerFlowWorker(t, conn, "w-marshal")

	server.mu.Lock()
	state := server.workers["w-marshal"]
	server.mu.Unlock()
	if state == nil {
		t.Fatal("worker w-marshal not registered")
	}

	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { jsonMarshal = orig }()

	// Nothing is written on the wire during the override window: the client
	// sits idle and the register is driven synchronously from this goroutine.
	err = server.handleRegister(state, &protocol.RegisterMessage{WorkerID: "w-marshal", Platform: "linux"})
	if err == nil {
		t.Fatal("handleRegister with failing marshal should fail")
	}
	if !strings.Contains(err.Error(), "failed to marshal ack") {
		t.Errorf("error = %v, want wrapped ack marshal failure", err)
	}
}

// TestHandleRegisterReleasesHubLock: by the time a register's ack reaches the
// wire, the hub lock must be free — a leaked s.mu would deadlock every later
// register/heartbeat/ack, and the ack write itself runs outside the lock. Both
// paths are driven through the real read loop (so the test does not race it)
// and the lock is polled for release.
func TestHandleRegisterReleasesHubLock(t *testing.T) {
	t.Run("success path", func(t *testing.T) {
		server, ts := newFlowTestServer(t, newChannelHandler(), nil)
		url := "ws" + ts.URL[len("http"):]

		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		registerFlowWorker(t, conn, "w-lock") // returns once the ack is read

		assertHubLockFree(t, server)
	})

	t.Run("rejection path", func(t *testing.T) {
		server, ts := newFlowTestServer(t, newChannelHandler(), &Config{
			Addr:           "127.0.0.1:0",
			ReadTimeout:    time.Second,
			WriteTimeout:   time.Second,
			PingTimeout:    time.Second,
			PingInterval:   time.Second,
			AllowedOrigins: []string{"*"},
			AuthToken:      "secret",
		})
		url := "ws" + ts.URL[len("http"):]

		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()

		// A wrong-token register takes the rejection path: the refusal ack is
		// written and the connection closed, and the lock is released.
		register := &protocol.RegisterMessage{WorkerID: "w-lock", Platform: "linux", AuthToken: "wrong"}
		payload, _ := protocol.MarshalMessage(register)
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Fatalf("write register: %v", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read register ack: %v", err)
		}
		var ack protocol.RegisterAckMessage
		_ = json.Unmarshal(data, &ack)
		if ack.Success {
			t.Fatal("wrong-token register: success = true, want refusal")
		}

		assertHubLockFree(t, server)
	})
}

// assertHubLockFree polls for the hub lock to be acquirable: a register path
// that leaked s.mu would fail here after the timeout.
func assertHubLockFree(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !server.mu.TryLock() {
		if time.Now().After(deadline) {
			t.Fatal("hub lock still held after register path returned")
		}
		time.Sleep(5 * time.Millisecond)
	}
	server.mu.Unlock()
}

// panicHandler panics inside OnRegister to prove the server recovers
// without leaking the hub lock.
type panicHandler struct {
	mockHandler
}

func (h *panicHandler) OnRegister(workerID string, msg *protocol.RegisterMessage) error {
	panic("register handler panic")
}

// TestHandleRegisterPanicDoesNotLeakHubLock: if the OnRegister callback
// panics, the per-connection goroutine's recovery must not leave s.mu
// held — a leaked lock would deadlock every later register/heartbeat/ack.
func TestHandleRegisterPanicDoesNotLeakHubLock(t *testing.T) {
	server, ts := newFlowTestServer(t, &panicHandler{}, nil)
	url := "ws" + ts.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// The register is read by the real read loop, which recovers the
	// panic in the handler goroutine; the ack still reaches the client.
	msg := &protocol.RegisterMessage{WorkerID: "w-panic", Platform: "linux"}
	payload, _ := protocol.MarshalMessage(msg)
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write register: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = conn.ReadMessage()
	if err != nil {
		t.Fatalf("read register ack: %v", err)
	}
	// The handler panicked; the lock must still be free for the next
	// register to proceed.
	assertHubLockFree(t, server)
}

// TestHandleRegisterCriticalPanicRemovesWorker: a panic while s.mu is held
// must not leak that lock into the connection recovery path. Recovery calls
// handleDisconnect, so a leaked critical section would deadlock the hub.
func TestHandleRegisterCriticalPanicRemovesWorker(t *testing.T) {
	original := jsonMarshal
	handler := &mockHandler{}
	server, ts := newFlowTestServer(t, handler, nil)
	defer ts.Close()
	defer func() { jsonMarshal = original }()
	jsonMarshal = func(v any) ([]byte, error) { panic("registration critical panic") }
	url := "ws" + ts.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	msg := &protocol.RegisterMessage{WorkerID: "w-critical-panic", Platform: "linux"}
	payload, _ := protocol.MarshalMessage(msg)
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write register: %v", err)
	}

	assertHubLockFree(t, server)
	deadline := time.Now().Add(2 * time.Second)
	for server.GetWorkerCount() != 0 || handler.disconnectCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("worker count = %d, disconnect callbacks = %d; want 0, 1", server.GetWorkerCount(), handler.disconnectCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = conn.Close()
}

// ackGateHandler records whether OnRegister ran and signals the caller.
type ackGateHandler struct {
	mockHandler
	registerRan chan string
}

func (h *ackGateHandler) OnRegister(workerID string, msg *protocol.RegisterMessage) error {
	h.registerRan <- workerID
	return nil
}

// TestHandleRegisterCallbackAfterAck: OnRegister must run only after the
// ack reached the wire. If the ack write fails, the handler must not be
// told the worker registered — the worker never learned of the success.
func TestHandleRegisterCallbackAfterAck(t *testing.T) {
	h := &ackGateHandler{registerRan: make(chan string, 1)}
	_, ts := newFlowTestServer(t, h, nil)
	url := "ws" + ts.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	msg := &protocol.RegisterMessage{WorkerID: "w-ackgate", Platform: "linux"}
	payload, _ := protocol.MarshalMessage(msg)
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write register: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read register ack: %v", err)
	}
	var ack protocol.RegisterAckMessage
	_ = json.Unmarshal(data, &ack)
	if !ack.Success {
		t.Fatal("register ack = false, want true")
	}
	select {
	case got := <-h.registerRan:
		if got != "w-ackgate" {
			t.Fatalf("OnRegister worker = %q, want w-ackgate", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnRegister was not called after a successful ack")
	}
}

// TestHandleRegisterNoCallbackOnAckFailure: when the ack cannot reach the
// wire, the handler must not be notified — a worker that never got its
// confirmation is not "registered" to the observer. The marshal step is
// injected to fail deterministically before any bytes hit the wire.
func TestHandleRegisterNoCallbackOnAckFailure(t *testing.T) {
	h := &ackGateHandler{registerRan: make(chan string, 1)}
	server, ts := newFlowTestServer(t, h, nil)
	url := "ws" + ts.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Register once to get a live state, then inject an ack marshal
	// failure so the register path returns before any callback runs.
	registerFlowWorker(t, conn, "w-noack")
	// The first register's callback is the success proof; drain it so the
	// failure assertion below only sees the second (failed) attempt. The
	// callback runs after the ack hits the wire, so the client's read does
	// not order it — wait instead of assuming it already ran.
	select {
	case <-h.registerRan:
	case <-time.After(2 * time.Second):
		t.Fatal("first register did not run its callback")
	}
	server.mu.Lock()
	state := server.workers["w-noack"]
	server.mu.Unlock()
	if state == nil {
		t.Fatal("worker w-noack not registered")
	}

	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("ack marshal boom") }
	defer func() { jsonMarshal = orig }()

	err = server.handleRegister(state, &protocol.RegisterMessage{WorkerID: "w-noack-2", Platform: "linux"})
	if err == nil || !strings.Contains(err.Error(), "failed to marshal ack") {
		t.Fatalf("handleRegister = %v, want ack marshal failure", err)
	}
	select {
	case <-h.registerRan:
		t.Fatal("OnRegister ran despite a failed ack")
	case <-time.After(500 * time.Millisecond):
		// expected: no callback
	}
}

// slowAckHandler blocks inside OnTaskAck so the test can close the server-side
// conn while handleConnection is parked in the handler.
type slowAckHandler struct {
	mockHandler
	started chan struct{}
	release chan struct{}
}

func (h *slowAckHandler) OnTaskAck(taskID string, success bool, errMsg string) error {
	select {
	case h.started <- struct{}{}:
	default:
	}
	<-h.release
	return nil
}

// wsTextFrame encodes payload as a masked client-to-server websocket text
// frame. A zero mask key is a valid key whose XOR leaves the payload bytes
// unchanged.
func wsTextFrame(payload []byte) []byte {
	if len(payload) >= 126 {
		panic("helper only supports short payloads")
	}
	out := make([]byte, 0, len(payload)+6)
	out = append(out, 0x81)                    // FIN + text opcode
	out = append(out, 0x80|byte(len(payload))) // MASK bit + payload length
	out = append(out, 0x00, 0x00, 0x00, 0x00)  // zero mask key
	return append(out, payload...)
}

func TestHandleConnectionResetReadDeadlineFailure(t *testing.T) {
	h := &slowAckHandler{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	// newFlowTestServer never calls Start(), so the server's wait group
	// tracks exactly the one handleConnection goroutine.
	server, ts := newFlowTestServer(t, h, nil)
	conn := dialFlowWS(t, ts)
	registerFlowWorker(t, conn, "w-deadline")

	// Push two ack frames in ONE underlying write. The server's read loop
	// drains both frames into gorilla's bufio reader with a single network
	// read (the blob is fully queued before the reader can wake), hands the
	// first to handleMessage, and keeps the second buffered in userspace.
	ackPayload, err := protocol.MarshalMessage(&protocol.AckMessage{TaskID: "t-1", Success: true})
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}
	blob := append(wsTextFrame(ackPayload), wsTextFrame(ackPayload)...)
	if _, err := conn.UnderlyingConn().Write(blob); err != nil {
		t.Fatalf("write raw frames: %v", err)
	}

	// Wait until the read loop is parked in OnTaskAck holding ack #2 in its
	// bufio buffer.
	select {
	case <-h.started:
	case <-time.After(3 * time.Second):
		t.Fatal("OnTaskAck never invoked")
	}

	// Close the server-side conn now. gorilla's SetReadDeadline delegates to
	// the (now closed) net.Conn while the buffered frame keeps the next
	// ReadMessage successful, so the loop lands on the deadline-reset error
	// branch instead of the ReadMessage error branch.
	server.mu.RLock()
	state := server.workers["w-deadline"]
	server.mu.RUnlock()
	if state == nil {
		t.Fatal("worker w-deadline not registered")
	}
	_ = state.conn.Close()

	close(h.release)

	// Returns once the read loop ran the failing deadline reset and exited
	// (this path returns without a disconnect callback).
	server.wg.Wait()
}

// TestHandleConnectionSkipsClosingGate verifies that a connection that
// reaches handleConnection after Stop set the closing gate is closed and
// does not join the wait group (which would race an already-started Wait).
func TestHandleConnectionSkipsClosingGate(t *testing.T) {
	server, ts := newFlowTestServer(t, newChannelHandler(), nil)
	conn := dialFlowWS(t, ts)

	// Mark the server as closing, then run handleConnection exactly as a
	// late upgrade would. The gate must close the conn and return without
	// adding to the wait group.
	server.closingMu.Lock()
	server.closing = true
	server.closingMu.Unlock()

	server.handleConnection(&ConnectionState{conn: conn})

	// The server-side conn was closed by the gate. A client read must now
	// fail; Wait() returns promptly because no goroutine joined it.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("connection was not closed by the closing gate")
	}

	done := make(chan struct{})
	go func() { server.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wait group never released: the closing path joined it")
	}
	_ = ts
}

func TestHandleConnectionAddInsideClosingGate(t *testing.T) {
	server, ts := newFlowTestServer(t, newChannelHandler(), nil)
	conn := dialFlowWS(t, ts)

	// Hold the closing read lock exactly as a connection does inside the
	// gate critical section (after reading closing==false, before wg.Add).
	server.closingMu.RLock()

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		_ = server.Stop()
	}()

	// Stop must block on the matching write lock while the connection holds
	// the read lock: it cannot set closing nor reach wg.Wait before the
	// connection's wg.Add has completed. If Add ever moved outside the
	// critical section, this ordering would be lost and the Add/Wait race
	// the finding describes would be back.
	server.wg.Add(1)
	server.wg.Done()

	server.closingMu.RUnlock()
	// Release the dialed connection so its read goroutine exits and Stop's
	// wg.Wait completes once the gate is closed.
	_ = conn.Close()
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked after the gate critical section was released")
	}
	_ = ts
}
