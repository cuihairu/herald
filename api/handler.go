package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/websocket"
	"github.com/cuihairu/herald/internal/logger"
	"github.com/google/uuid"
)

// Handler handles HTTP requests
type Handler struct {
	router   *route.Router
	queue    core.Queue
	runtime  *runtime.Manager
	dedup    *dedup.Dedup
_wsServer *websocket.Server
}

// NewHandler creates a new handler
func NewHandler(router *route.Router, queue core.Queue, runtime *runtime.Manager, dedup *dedup.Dedup) *Handler {
	return &Handler{
		router:  router,
		queue:   queue,
		runtime: runtime,
		dedup:   dedup,
	}
}

// SetWebSocketServer sets the WebSocket server
func (h *Handler) SetWebSocketServer(wsServer *websocket.Server) {
	h._wsServer = wsServer
}

// NotifyRequest is a notify request
type NotifyRequest struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Level    string   `json:"level,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// EventRequest is an event request
type EventRequest struct {
	Type   string                 `json:"type"`
	Labels map[string]string      `json:"labels"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

// Response is a response
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// HandleNotify handles notify requests
func (h *Handler) HandleNotify(w http.ResponseWriter, r *http.Request) {
	var req NotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Determine providers
	providers := req.Channels
	if len(providers) == 0 && req.Level != "" {
		routed, err := h.router.RouteByLevel(req.Level)
		if err != nil {
			logger.Warn("no route for level", "level", req.Level)
		} else {
			providers = routed
		}
	}

	if len(providers) == 0 {
		h.respondError(w, http.StatusBadRequest, "no providers specified")
		return
	}

	// Create tasks
	for _, provider := range providers {
		task := &core.Task{
			ID:        uuid.New().String(),
			Provider:  provider,
			Title:     req.Title,
			Body:      req.Body,
			Level:     req.Level,
			CreatedAt: time.Now(),
		}

		// Check dedup
		if h.dedup != nil && h.dedup.CheckTask(task) {
			logger.Info("task deduplicated", "task_id", task.ID)
			continue
		}

		// Push to queue
		if err := h.queue.PushTask(r.Context(), task); err != nil {
			logger.Error("failed to push task", "error", err)
			h.respondError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
	})
}

// HandleEvent handles event requests
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	var req EventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	event := &core.Event{
		ID:        uuid.New().String(),
		Type:      req.Type,
		Labels:    req.Labels,
		Data:      req.Data,
		Timestamp: time.Now(),
	}

	// Check dedup
	if h.dedup != nil && h.dedup.Check(event) {
		logger.Info("event deduplicated", "event_id", event.ID)
		h.respondJSON(w, &Response{
			Code:    0,
			Message: "ok",
			Data: map[string]string{
				"event_id": event.ID,
				"status":   "deduplicated",
			},
		})
		return
	}

	// Push to queue
	if err := h.queue.Push(r.Context(), event); err != nil {
		logger.Error("failed to push event", "error", err)
		h.respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]string{
			"event_id": event.ID,
		},
	})
}

// HandleStatus handles status requests
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	statuses := h.runtime.GetProviderStatus()

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"status":    "running",
			"providers": statuses,
		},
	})
}

// HandleProviders handles providers requests
func (h *Handler) HandleProviders(w http.ResponseWriter, r *http.Request) {
	statuses := h.runtime.GetProviderStatus()

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"providers": statuses,
		},
	})
}

// HandleWorkers handles workers status requests
func (h *Handler) HandleWorkers(w http.ResponseWriter, r *http.Request) {
	if h._wsServer == nil {
		h.respondJSON(w, &Response{
			Code:    0,
			Message: "ok",
			Data: map[string]interface{}{
				"workers": []interface{}{},
			},
		})
		return
	}

	workers := h._wsServer.GetWorkers()
	workerList := make([]map[string]interface{}, 0, len(workers))

	for _, state := range workers {
		workerInfo := map[string]interface{}{
			"worker_id":    state.WorkerID,
			"platform":     state.Platform,
			"version":      state.Version,
			"capabilities": state.Capabilities,
			"connected_at": state.ConnectedAt,
			"last_heartbeat": state.LastHeartbeat,
			"status":       state.Status,
		}
		workerList = append(workerList, workerInfo)
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"count":   len(workerList),
			"workers": workerList,
		},
	})
}

// HandleQueue handles queue status requests
func (h *Handler) HandleQueue(w http.ResponseWriter, r *http.Request) {
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"size": h.queue.Size(),
		},
	})
}

// HandleEnableProvider enables a provider
func (h *Handler) HandleEnableProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	if err := h.runtime.Enable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "provider enabled",
	})
}

// HandleDisableProvider disables a provider
func (h *Handler) HandleDisableProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	if err := h.runtime.Disable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "provider disabled",
	})
}

// HandleEnableProviderWithName enables a provider by name
func (h *Handler) HandleEnableProviderWithName(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.runtime.Enable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "provider enabled",
	})
}

// HandleDisableProviderWithName disables a provider by name
func (h *Handler) HandleDisableProviderWithName(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.runtime.Disable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "provider disabled",
	})
}

func (h *Handler) respondJSON(w http.ResponseWriter, data *Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(&Response{
		Code:    status,
		Message: message,
	})
}
