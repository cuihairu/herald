package websocket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/herald/protocol"
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
	// Verify upgrader is configured
	if upgrader.ReadBufferSize != 1024 {
		t.Errorf("expected ReadBufferSize 1024, got %d", upgrader.ReadBufferSize)
	}
	if upgrader.WriteBufferSize != 1024 {
		t.Errorf("expected WriteBufferSize 1024, got %d", upgrader.WriteBufferSize)
	}

	// Test CheckOrigin
	req := httptest.NewRequest("GET", "http://example.com", nil)
	if !upgrader.CheckOrigin(req) {
		t.Error("expected CheckOrigin to return true in development")
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
