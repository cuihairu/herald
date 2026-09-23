package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

func TestDemoProviderDeliver(t *testing.T) {
	p := &DemoProvider{}
	task := &core.DeliveryTask{
		ID:       "task-demo",
		Provider: "demo",
		Targets:  []string{"user-1"},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("Deliver() error = %v, want nil", err)
	}
}

func TestHandleDemoTask(t *testing.T) {
	// With content.
	task := &core.DeliveryTask{
		ID:       "task-1",
		Provider: "demo",
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "hi", Body: "there"},
		},
	}
	if err := handleDemoTask(task); err != nil {
		t.Errorf("handleDemoTask(content) error = %v, want nil", err)
	}

	// Without content.
	if err := handleDemoTask(&core.DeliveryTask{ID: "task-2"}); err != nil {
		t.Errorf("handleDemoTask(no content) error = %v, want nil", err)
	}
}

func TestDemoCallbacks(t *testing.T) {
	onDemoEvent(&protocol.EventMessage{EventType: "task.created"})
	onDemoConnect()
	onDemoDisconnect(errors.New("conn lost"))
	onDemoStateChange(protocol.StateReady)
}

// newAckServer upgrades the first connection and answers exactly one
// register handshake with a success ack; later messages (heartbeats) are
// read and dropped until the client goes away.
func newAckServer(registered chan *protocol.RegisterMessage) *httptest.Server {
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
			if err := json.Unmarshal(data, &envelope); err != nil || envelope.Type != protocol.MessageTypeRegister {
				continue
			}
			var msg protocol.RegisterMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			ack, _ := protocol.MarshalMessage(&protocol.RegisterAckMessage{
				WorkerID: msg.WorkerID,
				Success:  true,
				ServerID: "example-test",
			})
			if err := conn.WriteMessage(websocket.TextMessage, ack); err != nil {
				return
			}
			if registered != nil {
				registered <- &msg
			}
		}
	}))
}

func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func TestRunRegistersAndStopsOnCtxCancel(t *testing.T) {
	registered := make(chan *protocol.RegisterMessage, 1)
	server := newAckServer(registered)
	defer server.Close()

	cfg := demoConfig()
	cfg.CoreURL = wsURL(server)
	cfg.HeartbeatInterval = 10 * time.Millisecond

	// run must register with the server, then block until ctx is canceled
	// and return nil.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if err := run(ctx, cfg); err != nil {
		t.Errorf("run() error = %v, want nil after ctx cancel", err)
	}

	select {
	case msg := <-registered:
		if msg.WorkerID != cfg.WorkerID {
			t.Errorf("registered worker = %s, want %s", msg.WorkerID, cfg.WorkerID)
		}
		if msg.Mode != "remote" || msg.Platform == "" {
			t.Errorf("unexpected register message: %+v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for register message on server")
	}
}

func TestRunFailsFastWhenCoreUnreachable(t *testing.T) {
	// Registration is a real WebSocket handshake: an unreachable CoreURL
	// makes run fail with the dial error instead of blocking forever.
	cfg := demoConfig()
	cfg.CoreURL = "ws://127.0.0.1:1/worker"

	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), cfg)
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("run() error = nil, want dial failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after dial failure")
	}
}
