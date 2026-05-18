package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/cuihaitao/herald/core"
	"github.com/cuihaitao/herald/core/dedup"
	"github.com/cuihaitao/herald/core/route"
	"github.com/cuihaitao/herald/core/runtime"
	"github.com/cuihaitao/herald/internal/logger"
	"github.com/google/uuid"
)

// Handler handles HTTP requests
type Handler struct {
	router  *route.Router
	queue   core.Queue
	runtime *runtime.Manager
	dedup   *dedup.Dedup
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

func (h *Handler) respondJSON(w http.ResponseWriter, data *Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handler) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(&Response{
		Code:    status,
		Message: message,
	})
}
