package websocket

import (
	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/protocol"
)

// Hub handles remote worker registration and lifecycle via WebSocket.
// Task dispatch is handled by the queue; Hub only manages registration/heartbeat.
type Hub struct {
	server   *Server
	registry *worker.Registry
}

// NewHub creates a hub for a websocket server backed by a worker registry.
func NewHub(server *Server, registry *worker.Registry) *Hub {
	if registry == nil {
		registry = worker.NewRegistry()
	}
	return &Hub{
		server:   server,
		registry: registry,
	}
}

// OnRegister records a remote worker in the registry.
func (h *Hub) OnRegister(workerID string, msg *protocol.RegisterMessage) error {
	return h.registry.Register(&worker.Info{
		ID:           workerID,
		Mode:         worker.Remote,
		Capabilities: msg.Capabilities,
	})
}

// OnTaskAck is currently a no-op (task results handled via queue Ack/Nack).
func (h *Hub) OnTaskAck(taskID string, success bool, errMsg string) error {
	return nil
}

// OnWorkerEvent is currently a no-op.
func (h *Hub) OnWorkerEvent(workerID string, event *protocol.EventMessage) error {
	return nil
}

// OnHeartbeat updates the liveness timestamp for a remote worker.
func (h *Hub) OnHeartbeat(workerID string) error {
	return h.registry.Heartbeat(workerID)
}

// OnDisconnect removes a remote worker from the registry.
func (h *Hub) OnDisconnect(workerID string) {
	h.registry.Deregister(workerID)
}
