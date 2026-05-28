package websocket

import (
	"fmt"
	"sync"

	"github.com/cuihairu/herald/protocol"
)

// Hub manages worker registration and task dispatch.
type Hub struct {
	server *Server
	mu     sync.RWMutex
	workerByCap map[string]string
}

// NewHub creates a hub for a websocket server.
func NewHub(server *Server) *Hub {
	return &Hub{
		server: server,
		workerByCap: make(map[string]string),
	}
}

// OnRegister records worker capabilities.
func (h *Hub) OnRegister(workerID string, msg *protocol.RegisterMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, cap := range msg.Capabilities {
		h.workerByCap[cap] = workerID
	}
	return nil
}

// OnTaskAck is currently a no-op.
func (h *Hub) OnTaskAck(taskID string, success bool, errMsg string) error {
	return nil
}

// OnWorkerEvent is currently a no-op.
func (h *Hub) OnWorkerEvent(workerID string, event *protocol.EventMessage) error {
	return nil
}

// OnDisconnect removes worker capability mappings.
func (h *Hub) OnDisconnect(workerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for cap, id := range h.workerByCap {
		if id == workerID {
			delete(h.workerByCap, cap)
		}
	}
}

// Dispatch dispatches a task to a worker selected by provider key.
func (h *Hub) Dispatch(task *protocol.DispatchMessage) error {
	h.mu.RLock()
	workerID, ok := h.workerByCap[task.Provider]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("worker not found for provider: %s", task.Provider)
	}
	return h.server.DispatchTask(workerID, task)
}

// DispatchToWorker dispatches a task to a specific worker ID.
func (h *Hub) DispatchToWorker(workerID string, task *protocol.DispatchMessage) error {
	if workerID == "" {
		return fmt.Errorf("worker id is empty")
	}
	return h.server.DispatchTask(workerID, task)
}
