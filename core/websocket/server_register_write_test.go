package websocket

import (
	"strings"
	"testing"

	"github.com/cuihairu/herald/protocol"
	"github.com/gorilla/websocket"
)

func TestHandleRegisterOnClosedServerConn(t *testing.T) {
	server, ts := newFlowTestServer(t, newChannelHandler(), nil)
	url := "ws" + ts.URL[len("http"):]

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	registerFlowWorker(t, conn, "w-regfail")

	server.mu.Lock()
	state := server.workers["w-regfail"]
	server.mu.Unlock()
	if state == nil {
		t.Fatal("worker w-regfail not registered")
	}

	// Closing the server-side conn makes the ack write fail. Gorilla's
	// SetWriteDeadline still succeeds on a closed conn (the deadline is
	// decoupled from the write path), so the failure surfaces at
	// WriteMessage as a wrapped "failed to send ack" error.
	_ = state.conn.Close()
	err = server.handleRegister(state, &protocol.RegisterMessage{WorkerID: "w-regfail", Platform: "linux"})
	if err == nil {
		t.Fatal("handleRegister on closed conn should fail")
	}
	if !strings.Contains(err.Error(), "failed to send ack") {
		t.Errorf("error = %v, want wrapped ack write failure", err)
	}
}
