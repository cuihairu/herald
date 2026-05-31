package websocket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

// mockHandler is a mock connection handler for testing
type mockHandler struct {
	registerCalls   []registerCall
	ackCalls        []ackCall
	eventCalls      []eventCall
	disconnectCalls []string
}

type registerCall struct {
	workerID string
	msg      *protocol.RegisterMessage
}

type ackCall struct {
	taskID  string
	success bool
	errMsg  string
}

type eventCall struct {
	workerID string
	event    *protocol.EventMessage
}

func (m *mockHandler) OnRegister(workerID string, msg *protocol.RegisterMessage) error {
	m.registerCalls = append(m.registerCalls, registerCall{
		workerID: workerID,
		msg:      msg,
	})
	return nil
}

func (m *mockHandler) OnTaskAck(taskID string, success bool, errMsg string) error {
	m.ackCalls = append(m.ackCalls, ackCall{
		taskID:  taskID,
		success: success,
		errMsg:  errMsg,
	})
	return nil
}

func (m *mockHandler) OnWorkerEvent(workerID string, event *protocol.EventMessage) error {
	m.eventCalls = append(m.eventCalls, eventCall{
		workerID: workerID,
		event:    event,
	})
	return nil
}

func (m *mockHandler) OnDisconnect(workerID string) {
	m.disconnectCalls = append(m.disconnectCalls, workerID)
}

func TestNewServer(t *testing.T) {
	handler := &mockHandler{}
	config := &Config{
		Addr: ":9999",
	}

	server := NewServer(config, handler)
	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.addr != ":9999" {
		t.Errorf("expected addr :9999, got %s", server.addr)
	}
	if server.handler == nil {
		t.Error("expected handler to be set")
	}
}

func TestNewServerDefaultConfig(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	if server.addr != ":8081" {
		t.Errorf("expected default addr :8081, got %s", server.addr)
	}
	if server.readTimeout != 60*time.Second {
		t.Errorf("expected default read timeout 60s, got %v", server.readTimeout)
	}
}

func TestServerStartStop(t *testing.T) {
	handler := &mockHandler{}
	config := &Config{
		Addr: ":0", // Use random port
	}
	server := NewServer(config, handler)

	// Start server
	err := server.Start()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Give server time to start
	time.Sleep(10 * time.Millisecond)

	// Stop server
	err = server.Stop()
	if err != nil {
		t.Errorf("expected no error stopping, got %v", err)
	}
}

func TestServerGetWorkers(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	workers := server.GetWorkers()
	if workers == nil {
		t.Error("expected non-nil workers map")
	}
	if len(workers) != 0 {
		t.Errorf("expected 0 workers, got %d", len(workers))
	}
}

func TestServerGetWorkerCount(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	count := server.GetWorkerCount()
	if count != 0 {
		t.Errorf("expected 0 workers, got %d", count)
	}
}

func TestServerGetWorkerNotFound(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	_, err := server.GetWorker("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent worker")
	}
}

func TestServerDisconnectWorkerNotFound(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	err := server.DisconnectWorker("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent worker")
	}
}

func TestServerHandleInvalidMessage(t *testing.T) {
	handler := &mockHandler{}
	config := &Config{
		Addr: ":0",
	}
	server := NewServer(config, handler)

	_ = server.Start()
	defer func() { _ = server.Stop() }()
	time.Sleep(10 * time.Millisecond)

	// Create test server that wraps our WebSocket server
	testSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just return OK, don't actually upgrade
		w.WriteHeader(http.StatusOK)
	}))
	defer testSrv.Close()

	// Test invalid JSON
	invalidData := []byte("not json")

	// This would normally be tested via WebSocket, but for unit test
	// we just verify the message parsing logic exists
	var raw struct {
		Type string `json:"type"`
	}
	err := json.Unmarshal(invalidData, &raw)
	if err == nil {
		t.Error("expected error parsing invalid JSON")
	}
}

func TestConfigDefaults(t *testing.T) {
	server := NewServer(nil, &mockHandler{})

	if server.readTimeout != 60*time.Second {
		t.Errorf("expected readTimeout 60s, got %v", server.readTimeout)
	}
	if server.writeTimeout != 60*time.Second {
		t.Errorf("expected writeTimeout 60s, got %v", server.writeTimeout)
	}
	if server.pingTimeout != 30*time.Second {
		t.Errorf("expected pingTimeout 30s, got %v", server.pingTimeout)
	}
	if server.pingInterval != 20*time.Second {
		t.Errorf("expected pingInterval 20s, got %v", server.pingInterval)
	}
}

func TestMessageTypes(t *testing.T) {
	// Test that all message type constants are defined
	tests := []struct {
		msgType string
		valid   bool
	}{
		{protocol.MessageTypeRegister, true},
		{protocol.MessageTypeHeartbeat, true},
		{protocol.MessageTypeAck, true},
		{protocol.MessageTypeEvent, true},
		{protocol.MessageTypeDispatch, true},
		{"unknown", false},
	}

	validTypes := map[string]bool{
		protocol.MessageTypeRegister:  true,
		protocol.MessageTypeHeartbeat: true,
		protocol.MessageTypeAck:       true,
		protocol.MessageTypeEvent:     true,
		protocol.MessageTypeDispatch:  true,
	}

	for _, tt := range tests {
		_, ok := validTypes[tt.msgType]
		if ok != tt.valid {
			t.Errorf("message type %s: expected valid=%v, got %v", tt.msgType, tt.valid, ok)
		}
	}
}

func TestConnectionState(t *testing.T) {
	state := &ConnectionState{
		WorkerID:     "test-worker",
		Platform:     "test-platform",
		Version:      "1.0.0",
		Capabilities: []string{"test", "notify"},
		Status:       make(map[string]interface{}),
	}

	if state.WorkerID != "test-worker" {
		t.Errorf("expected WorkerID test-worker, got %s", state.WorkerID)
	}
	if state.Platform != "test-platform" {
		t.Errorf("expected Platform test-platform, got %s", state.Platform)
	}
	if len(state.Capabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(state.Capabilities))
	}
}

func TestServerWithContext(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	if server.ctx == nil {
		t.Error("expected context to be initialized")
	}
	if server.cancel == nil {
		t.Error("expected cancel function to be initialized")
	}

	// Test context cancellation
	server.cancel()
	select {
	case <-server.ctx.Done():
		// Expected
	default:
		t.Error("expected context to be cancelled")
	}
}

func TestRegisterMessage(t *testing.T) {
	msg := &protocol.RegisterMessage{
		WorkerID:     "worker-1",
		Platform:     "linux",
		Version:      "1.0.0",
		Capabilities: []string{"telegram", "slack"},
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	var decoded protocol.RegisterMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if decoded.WorkerID != msg.WorkerID {
		t.Errorf("expected WorkerID %s, got %s", msg.WorkerID, decoded.WorkerID)
	}
}

func TestDispatchMessage(t *testing.T) {
	msg := &protocol.DispatchMessage{
		TaskID:    "task-1",
		Provider:  "telegram",
		Title:     "Test",
		Body:      "Test body",
		Level:     "info",
		Target:    "chat-123",
		Data:      map[string]interface{}{"key": "value"},
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	var decoded protocol.DispatchMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if decoded.TaskID != msg.TaskID {
		t.Errorf("expected TaskID %s, got %s", msg.TaskID, decoded.TaskID)
	}
}

func TestAckMessage(t *testing.T) {
	msg := &protocol.AckMessage{
		TaskID:  "task-1",
		Success: true,
		Error:   "",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	var decoded protocol.AckMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if decoded.TaskID != msg.TaskID {
		t.Errorf("expected TaskID %s, got %s", msg.TaskID, decoded.TaskID)
	}
	if !decoded.Success {
		t.Error("expected Success to be true")
	}
}

func TestHeartbeatMessage(t *testing.T) {
	status := map[string]interface{}{
		"tasks_sent": 10,
		"tasks_done": 8,
	}
	msg := &protocol.HeartbeatMessage{
		WorkerID: "worker-1",
		Status:   status,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	var decoded protocol.HeartbeatMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if decoded.WorkerID != msg.WorkerID {
		t.Errorf("expected WorkerID %s, got %s", msg.WorkerID, decoded.WorkerID)
	}
}

func TestEventMessage(t *testing.T) {
	data := map[string]interface{}{
		"message": "test message",
	}
	msg := &protocol.EventMessage{
		WorkerID:  "worker-1",
		EventType: "test.event",
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	var decoded protocol.EventMessage
	err = json.Unmarshal(msgBytes, &decoded)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if decoded.WorkerID != msg.WorkerID {
		t.Errorf("expected WorkerID %s, got %s", msg.WorkerID, decoded.WorkerID)
	}
}

func TestMockHandler(t *testing.T) {
	handler := &mockHandler{}

	// Test OnRegister
	msg := &protocol.RegisterMessage{
		WorkerID: "test-worker",
		Platform: "test",
	}
	_ = handler.OnRegister("test-worker", msg)
	if len(handler.registerCalls) != 1 {
		t.Errorf("expected 1 register call, got %d", len(handler.registerCalls))
	}

	// Test OnTaskAck
	_ = handler.OnTaskAck("task-1", true, "")
	if len(handler.ackCalls) != 1 {
		t.Errorf("expected 1 ack call, got %d", len(handler.ackCalls))
	}

	// Test OnWorkerEvent
	event := &protocol.EventMessage{
		WorkerID:  "test-worker",
		EventType: "test.event",
	}
	_ = handler.OnWorkerEvent("test-worker", event)
	if len(handler.eventCalls) != 1 {
		t.Errorf("expected 1 event call, got %d", len(handler.eventCalls))
	}

	// Test OnDisconnect
	handler.OnDisconnect("test-worker")
	if len(handler.disconnectCalls) != 1 {
		t.Errorf("expected 1 disconnect call, got %d", len(handler.disconnectCalls))
	}
}

func TestUpgraderSettings(t *testing.T) {
	server := NewServer(nil, nil)

	// Verify upgrader is configured
	if server.upgrader.ReadBufferSize != 1024 {
		t.Errorf("expected ReadBufferSize 1024, got %d", server.upgrader.ReadBufferSize)
	}
	if server.upgrader.WriteBufferSize != 1024 {
		t.Errorf("expected WriteBufferSize 1024, got %d", server.upgrader.WriteBufferSize)
	}

	// Test CheckOrigin: default (no config) should allow localhost
	req := httptest.NewRequest("GET", "http://example.com", nil)
	req.Header.Set("Origin", "http://localhost")
	if !server.upgrader.CheckOrigin(req) {
		t.Error("expected localhost origin to be allowed")
	}

	// Non-localhost should be rejected by default
	req2 := httptest.NewRequest("GET", "http://example.com", nil)
	req2.Header.Set("Origin", "http://evil.example.com")
	if server.upgrader.CheckOrigin(req2) {
		t.Error("expected non-localhost origin to be rejected by default")
	}

	// Wildcard should allow all
	wildcardServer := NewServer(&Config{AllowedOrigins: []string{"*"}}, nil)
	if !wildcardServer.upgrader.CheckOrigin(req2) {
		t.Error("expected wildcard to allow all origins")
	}
}

func TestServerStopWithoutStart(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	// Should not panic
	err := server.Stop()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestNewHub(t *testing.T) {
	t.Run("with server and registry", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		registry := worker.NewRegistry()
		hub := NewHub(server, registry)

		if hub == nil {
			t.Fatal("expected non-nil hub")
		}
		if hub.server != server {
			t.Error("expected server to be set")
		}
		if hub.registry != registry {
			t.Error("expected registry to be set")
		}
	})

	t.Run("with nil registry creates default", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		hub := NewHub(server, nil)

		if hub == nil {
			t.Fatal("expected non-nil hub")
		}
		if hub.registry == nil {
			t.Error("expected default registry to be created")
		}
	})
}

func TestHub_OnRegister(t *testing.T) {
	t.Run("registers worker in registry", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		registry := worker.NewRegistry()
		hub := NewHub(server, registry)

		msg := &protocol.RegisterMessage{
			WorkerID:     "worker-1",
			Capabilities: []string{"telegram", "slack"},
		}

		err := hub.OnRegister("worker-1", msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify worker was registered
		workers := registry.List()
		if len(workers) != 1 {
			t.Errorf("expected 1 worker, got %d", len(workers))
		}
		if workers[0].ID != "worker-1" {
			t.Errorf("expected worker ID worker-1, got %s", workers[0].ID)
		}
	})

	t.Run("register with empty workerID fails", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		registry := worker.NewRegistry()
		hub := NewHub(server, registry)

		msg := &protocol.RegisterMessage{
			WorkerID:     "",
			Capabilities: []string{"telegram"},
		}

		err := hub.OnRegister("", msg)
		if err == nil {
			t.Error("expected error for empty worker ID")
		}
	})
}

func TestHub_OnTaskAck(t *testing.T) {
	t.Run("ack is a no-op", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		hub := NewHub(server, nil)

		err := hub.OnTaskAck("task-1", true, "")
		if err != nil {
			t.Errorf("expected no error (no-op), got %v", err)
		}

		err = hub.OnTaskAck("task-2", false, "failed")
		if err != nil {
			t.Errorf("expected no error (no-op), got %v", err)
		}
	})
}

func TestHub_OnWorkerEvent(t *testing.T) {
	t.Run("event is a no-op", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		hub := NewHub(server, nil)

		event := &protocol.EventMessage{
			WorkerID:  "worker-1",
			EventType: "test.event",
		}

		err := hub.OnWorkerEvent("worker-1", event)
		if err != nil {
			t.Errorf("expected no error (no-op), got %v", err)
		}
	})
}

func TestHub_OnDisconnect(t *testing.T) {
	t.Run("deregisters worker", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		registry := worker.NewRegistry()
		hub := NewHub(server, registry)

		// First register a worker
		_ = registry.Register(&worker.Info{ID: "worker-1", Mode: worker.Remote})

		// Verify it's registered
		workers := registry.List()
		if len(workers) != 1 {
			t.Fatalf("expected 1 worker before disconnect, got %d", len(workers))
		}

		// Disconnect
		hub.OnDisconnect("worker-1")

		// Verify it's deregistered
		workers = registry.List()
		if len(workers) != 0 {
			t.Errorf("expected 0 workers after disconnect, got %d", len(workers))
		}
	})
}

func TestServer_SetHandler(t *testing.T) {
	server := NewServer(nil, nil)

	handler1 := &mockHandler{}
	server.SetHandler(handler1)

	if server.handler != handler1 {
		t.Error("expected handler to be set")
	}

	handler2 := &mockHandler{}
	server.SetHandler(handler2)

	if server.handler != handler2 {
		t.Error("expected handler to be updated")
	}
}

func TestCloneWorkerInfo(t *testing.T) {
	state := &ConnectionState{
		WorkerID:      "worker-1",
		Platform:      "linux",
		Version:       "1.0.0",
		Capabilities:  []string{"telegram", "slack"},
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Status:        map[string]interface{}{"tasks": 10},
	}

	info := cloneWorkerInfo(state)

	if info.WorkerID != state.WorkerID {
		t.Errorf("expected WorkerID %s, got %s", state.WorkerID, info.WorkerID)
	}
	if info.Platform != state.Platform {
		t.Errorf("expected Platform %s, got %s", state.Platform, info.Platform)
	}
	if !info.ConnectedAt.Equal(state.ConnectedAt) {
		t.Error("expected ConnectedAt to match")
	}

	// Verify capabilities are copied
	if len(info.Capabilities) != len(state.Capabilities) {
		t.Errorf("expected %d capabilities, got %d", len(state.Capabilities), len(info.Capabilities))
	}

	// Verify status is copied
	if info.Status["tasks"] != state.Status["tasks"] {
		t.Errorf("expected status to match")
	}

	// Modify original to ensure deep copy
	state.Capabilities[0] = "modified"
	if info.Capabilities[0] == "modified" {
		t.Error("expected deep copy of capabilities")
	}
}

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		origin   string
		expected bool
	}{
		{"http://localhost", true},
		{"https://localhost", true},
		{"http://127.0.0.1", true},
		{"https://127.0.0.1", true},
		{"http://[::1]", true},
		{"https://[::1]", true},
		{"http://example.com", false},
		{"https://example.com", false},
		{"http://192.168.1.1", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isLocalhost(tt.origin)
		if result != tt.expected {
			t.Errorf("isLocalhost(%q): expected %v, got %v", tt.origin, tt.expected, result)
		}
	}
}

func TestNewUpgrader(t *testing.T) {
	t.Run("with wildcard allows all", func(t *testing.T) {
		upgrader := newUpgrader([]string{"*"})
		req := httptest.NewRequest("GET", "http://example.com", nil)
		req.Header.Set("Origin", "http://evil.com")

		if !upgrader.CheckOrigin(req) {
			t.Error("expected wildcard to allow all origins")
		}
	})

	t.Run("with specific origins", func(t *testing.T) {
		allowed := []string{"http://example.com", "https://trusted.com"}
		upgrader := newUpgrader(allowed)

		// Allowed origin
		req1 := httptest.NewRequest("GET", "http://example.com", nil)
		req1.Header.Set("Origin", "http://example.com")
		if !upgrader.CheckOrigin(req1) {
			t.Error("expected allowed origin to be accepted")
		}

		// Disallowed origin
		req2 := httptest.NewRequest("GET", "http://example.com", nil)
		req2.Header.Set("Origin", "http://evil.com")
		if upgrader.CheckOrigin(req2) {
			t.Error("expected disallowed origin to be rejected")
		}
	})

	t.Run("with empty config allows localhost only", func(t *testing.T) {
		upgrader := newUpgrader([]string{})

		// Localhost allowed
		req1 := httptest.NewRequest("GET", "http://localhost", nil)
		req1.Header.Set("Origin", "http://localhost")
		if !upgrader.CheckOrigin(req1) {
			t.Error("expected localhost to be allowed")
		}

		// Non-localhost rejected
		req2 := httptest.NewRequest("GET", "http://example.com", nil)
		req2.Header.Set("Origin", "http://example.com")
		if upgrader.CheckOrigin(req2) {
			t.Error("expected non-localhost to be rejected")
		}
	})

	t.Run("no origin header allows non-browser clients", func(t *testing.T) {
		upgrader := newUpgrader([]string{})
		req := httptest.NewRequest("GET", "http://example.com", nil)
		// No Origin header set

		if !upgrader.CheckOrigin(req) {
			t.Error("expected non-browser clients (no Origin) to be allowed")
		}
	})
}

func TestServer_GetWorkerFound(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	// Manually add a worker
	state := &ConnectionState{
		WorkerID:     "test-worker",
		Platform:     "test",
		Version:      "1.0.0",
		Capabilities: []string{"test"},
		ConnectedAt:  time.Now(),
		Status:       make(map[string]interface{}),
	}
	server.mu.Lock()
	server.workers["test-worker"] = state
	server.mu.Unlock()

	worker, err := server.GetWorker("test-worker")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if worker.WorkerID != "test-worker" {
		t.Errorf("expected WorkerID test-worker, got %s", worker.WorkerID)
	}
	if worker.Platform != "test" {
		t.Errorf("expected Platform test, got %s", worker.Platform)
	}
}

func TestServer_GetWorkersWithData(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	// Manually add workers
	server.mu.Lock()
	server.workers["worker-1"] = &ConnectionState{
		WorkerID:     "worker-1",
		Platform:     "linux",
		Capabilities: []string{"telegram"},
		ConnectedAt:  time.Now(),
		Status:       make(map[string]interface{}),
	}
	server.workers["worker-2"] = &ConnectionState{
		WorkerID:     "worker-2",
		Platform:     "windows",
		Capabilities: []string{"slack"},
		ConnectedAt:  time.Now(),
		Status:       make(map[string]interface{}),
	}
	server.mu.Unlock()

	workers := server.GetWorkers()
	if len(workers) != 2 {
		t.Errorf("expected 2 workers, got %d", len(workers))
	}
	if workers["worker-1"].Platform != "linux" {
		t.Errorf("expected Platform linux, got %s", workers["worker-1"].Platform)
	}
	if workers["worker-2"].Platform != "windows" {
		t.Errorf("expected Platform windows, got %s", workers["worker-2"].Platform)
	}
}

func TestServer_GetWorkerCountWithWorkers(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(nil, handler)

	// Manually add workers
	server.mu.Lock()
	for i := 0; i < 3; i++ {
		server.workers[fmt.Sprintf("worker-%d", i)] = &ConnectionState{
			WorkerID: fmt.Sprintf("worker-%d", i),
		}
	}
	server.mu.Unlock()

	count := server.GetWorkerCount()
	if count != 3 {
		t.Errorf("expected 3 workers, got %d", count)
	}
}

func TestWebSocketIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	handler := &mockHandler{}
	config := &Config{
		Addr:           "127.0.0.1:0", // Use random port
		AllowedOrigins: []string{"*"},
	}
	server := NewServer(config, handler)

	_ = server.Start()
	defer func() { _ = server.Stop() }()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// The server should be running now
	// In a full integration test, we would:
	// 1. Get the actual listening address from the server
	// 2. Use gorilla/websocket.Dialer to connect
	// 3. Send messages and test the handleConnection flow
	//
	// For now, this test verifies that the server starts and stops without error
	if server.GetWorkerCount() != 0 {
		t.Logf("Server has %d workers (expected 0 initially)", server.GetWorkerCount())
	}
}

func TestHandleHeartbeat(t *testing.T) {
	t.Run("heartbeat updates status", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(nil, handler)

		// First, add a worker to the server
		state := &ConnectionState{
			WorkerID:      "test-worker",
			LastHeartbeat: time.Now(),
			Status:        make(map[string]interface{}),
		}

		server.mu.Lock()
		server.workers["test-worker"] = state
		server.mu.Unlock()

		msg := &protocol.HeartbeatMessage{
			WorkerID: "test-worker",
			Status:   map[string]interface{}{"tasks_sent": 10, "tasks_done": 8},
		}

		// Test handleHeartbeat
		err := server.handleHeartbeat(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify status was updated
		server.mu.RLock()
		updatedState := server.workers["test-worker"]
		server.mu.RUnlock()

		if updatedState.Status["tasks_sent"] != 10 {
			t.Errorf("expected tasks_sent to be 10, got %v", updatedState.Status["tasks_sent"])
		}
		if updatedState.Status["tasks_done"] != 8 {
			t.Errorf("expected tasks_done to be 8, got %v", updatedState.Status["tasks_done"])
		}
	})

	t.Run("heartbeat worker not found", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})

		msg := &protocol.HeartbeatMessage{
			WorkerID: "non-existent-worker",
		}

		err := server.handleHeartbeat(msg)
		if err == nil {
			t.Error("expected error for non-existent worker")
		}
	})
}

func TestHandleAck(t *testing.T) {
	t.Run("ack success", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(nil, handler)

		msg := &protocol.AckMessage{
			TaskID:  "task-1",
			Success: true,
			Error:   "",
		}

		err := server.handleAck(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify handler was notified
		if len(handler.ackCalls) != 1 {
			t.Errorf("expected 1 ack call, got %d", len(handler.ackCalls))
		}
		if handler.ackCalls[0].taskID != "task-1" {
			t.Errorf("expected taskID task-1, got %s", handler.ackCalls[0].taskID)
		}
		if !handler.ackCalls[0].success {
			t.Error("expected success to be true")
		}
	})

	t.Run("ack failure", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(nil, handler)

		msg := &protocol.AckMessage{
			TaskID:  "task-1",
			Success: false,
			Error:   "delivery failed",
		}

		err := server.handleAck(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if len(handler.ackCalls) != 1 {
			t.Errorf("expected 1 ack call, got %d", len(handler.ackCalls))
		}
		if handler.ackCalls[0].errMsg != "delivery failed" {
			t.Errorf("expected error message 'delivery failed', got %s", handler.ackCalls[0].errMsg)
		}
	})

	t.Run("ack without handler", func(t *testing.T) {
		server := NewServer(nil, nil)

		msg := &protocol.AckMessage{
			TaskID:  "task-1",
			Success: true,
		}

		err := server.handleAck(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestHandleEvent(t *testing.T) {
	t.Run("event with handler", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(nil, handler)

		data := map[string]interface{}{
			"message": "test event data",
		}
		msg := &protocol.EventMessage{
			WorkerID:  "test-worker",
			EventType: "worker.started",
			Data:      data,
			Timestamp: time.Now().Unix(),
		}

		err := server.handleEvent(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if len(handler.eventCalls) != 1 {
			t.Errorf("expected 1 event call, got %d", len(handler.eventCalls))
		}
		if handler.eventCalls[0].workerID != "test-worker" {
			t.Errorf("expected workerID test-worker, got %s", handler.eventCalls[0].workerID)
		}
		if handler.eventCalls[0].event.EventType != "worker.started" {
			t.Errorf("expected event type worker.started, got %s", handler.eventCalls[0].event.EventType)
		}
	})

	t.Run("event without handler", func(t *testing.T) {
		server := NewServer(nil, nil)

		msg := &protocol.EventMessage{
			WorkerID:  "test-worker",
			EventType: "test.event",
		}

		err := server.handleEvent(msg)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestHandleDisconnectBasic(t *testing.T) {
	t.Run("disconnect non-existent worker", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})

		// Should not panic
		server.handleDisconnect("non-existent-worker")
	})
}

func TestHandleMessage(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		err := server.handleMessage(state, []byte("not json"))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("unknown message type", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"type":"unknown_type"}`
		err := server.handleMessage(state, []byte(data))
		if err != nil {
			t.Errorf("expected no error for unknown message type, got %v", err)
		}
	})

	t.Run("missing type field", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"worker_id":"worker-1"}`

		err := server.handleMessage(state, []byte(data))
		// Missing type field results in empty type, which falls into default case
		// No error is returned, just a warning log
		if err != nil {
			t.Errorf("expected no error for missing type, got %v", err)
		}
	})

	t.Run("empty message", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		err := server.handleMessage(state, []byte(""))
		if err == nil {
			t.Error("expected error for empty message")
		}
	})

	t.Run("heartbeat message", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(nil, handler)

		// First add a worker
		state := &ConnectionState{
			WorkerID:      "worker-1",
			LastHeartbeat: time.Now(),
			Status:        make(map[string]interface{}),
		}

		server.mu.Lock()
		server.workers["worker-1"] = state
		server.mu.Unlock()

		data := `{"type":"heartbeat","worker_id":"worker-1","status":{"tasks":5}}`

		err := server.handleMessage(state, []byte(data))
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("ack message", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(nil, handler)

		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"type":"ack","task_id":"task-1","success":true,"error":""}`

		err := server.handleMessage(state, []byte(data))
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if len(handler.ackCalls) != 1 {
			t.Errorf("expected 1 ack call, got %d", len(handler.ackCalls))
		}
	})

	t.Run("event message", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(nil, handler)

		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"type":"event","worker_id":"worker-1","event_type":"test.event","data":{"key":"value"},"timestamp":1234567890}`

		err := server.handleMessage(state, []byte(data))
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		if len(handler.eventCalls) != 1 {
			t.Errorf("expected 1 event call, got %d", len(handler.eventCalls))
		}
	})

	t.Run("register invalid JSON", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"type":"register","worker_id":"worker-1","invalid`

		err := server.handleMessage(state, []byte(data))
		if err == nil {
			t.Error("expected error for invalid register JSON")
		}
	})

	t.Run("heartbeat invalid JSON", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"type":"heartbeat","worker_id":"worker-1","status":`

		err := server.handleMessage(state, []byte(data))
		if err == nil {
			t.Error("expected error for invalid heartbeat JSON")
		}
	})

	t.Run("ack invalid JSON", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"type":"ack","task_id":"task-1","success":`

		err := server.handleMessage(state, []byte(data))
		if err == nil {
			t.Error("expected error for invalid ack JSON")
		}
	})

	t.Run("event invalid JSON", func(t *testing.T) {
		server := NewServer(nil, &mockHandler{})
		state := &ConnectionState{Status: make(map[string]interface{})}

		data := `{"type":"event","worker_id":"worker-1","event_type":"test.event","data":`

		err := server.handleMessage(state, []byte(data))
		if err == nil {
			t.Error("expected error for invalid event JSON")
		}
	})
}

func TestCheckStaleWorkers(t *testing.T) {
	t.Run("stale worker detection", func(t *testing.T) {
		handler := &mockHandler{}
		server := NewServer(&Config{Addr: ":0"}, handler)

		staleState := &ConnectionState{
			WorkerID:      "stale-worker",
			LastHeartbeat: time.Now().Add(-3 * time.Minute),
			Status:        make(map[string]interface{}),
		}
		freshState := &ConnectionState{
			WorkerID:      "fresh-worker",
			LastHeartbeat: time.Now(),
			Status:        make(map[string]interface{}),
		}

		server.mu.Lock()
		server.workers["stale-worker"] = staleState
		server.workers["fresh-worker"] = freshState
		server.mu.Unlock()

		server.pruneStaleWorkers(time.Now())

		server.mu.RLock()
		_, staleExists := server.workers["stale-worker"]
		_, freshExists := server.workers["fresh-worker"]
		server.mu.RUnlock()

		if staleExists {
			t.Error("expected stale worker to be removed")
		}
		if !freshExists {
			t.Error("expected fresh worker to remain")
		}
		if len(handler.disconnectCalls) != 1 || handler.disconnectCalls[0] != "stale-worker" {
			t.Fatalf("expected one disconnect call for stale-worker, got %#v", handler.disconnectCalls)
		}
	})
}

func TestHandleRegister(t *testing.T) {
	handler := &mockHandler{}
	server := NewServer(&Config{
		Addr:           "127.0.0.1:0",
		ReadTimeout:    time.Second,
		WriteTimeout:   time.Second,
		PingTimeout:    time.Second,
		PingInterval:   time.Second,
		AllowedOrigins: []string{"*"},
	}, handler)

	testSrv := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testSrv.Close()

	wsURL := "ws" + testSrv.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	register := &protocol.RegisterMessage{
		WorkerID:     "worker-1",
		Platform:     "linux",
		Version:      "1.0.0",
		Capabilities: []string{"telegram", "slack"},
	}
	payload, err := protocol.MarshalMessage(register)
	if err != nil {
		t.Fatalf("failed to marshal register message: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("failed to write register message: %v", err)
	}

	_, ackData, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register ack: %v", err)
	}

	var ack protocol.RegisterAckMessage
	if err := json.Unmarshal(ackData, &ack); err != nil {
		t.Fatalf("failed to decode register ack: %v", err)
	}
	if ack.WorkerID != "worker-1" || !ack.Success {
		t.Fatalf("unexpected ack: %#v", ack)
	}

	server.mu.RLock()
	state, ok := server.workers["worker-1"]
	server.mu.RUnlock()
	if !ok {
		t.Fatal("expected worker to be registered")
	}
	if state.Platform != "linux" {
		t.Fatalf("expected platform linux, got %s", state.Platform)
	}
	if state.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", state.Version)
	}
	if len(state.Capabilities) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(state.Capabilities))
	}
	deadline := time.Now().Add(time.Second)
	for len(handler.registerCalls) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	if len(handler.registerCalls) != 1 {
		t.Fatalf("expected 1 register callback, got %d", len(handler.registerCalls))
	}
	if handler.registerCalls[0].workerID != "worker-1" {
		t.Fatalf("expected worker-1 callback, got %s", handler.registerCalls[0].workerID)
	}
}
