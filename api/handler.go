package api

import (
	"encoding/json"
	"net/http"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/service"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/websocket"
)

// Handler handles HTTP requests
type Handler struct {
	notificationSvc *service.NotificationService
	runtime         *runtime.Manager
	templateManager *template.Manager
	_wsServer       *websocket.Server
	queue           core.Queue
}

// NewHandler creates a new handler
func NewHandler(notificationSvc *service.NotificationService, rt *runtime.Manager, templateMgr *template.Manager) *Handler {
	if templateMgr == nil {
		templateMgr = template.NewManager()
	}
	return &Handler{
		notificationSvc: notificationSvc,
		runtime:         rt,
		templateManager: templateMgr,
	}
}

// SetWebSocketServer sets the WebSocket server
func (h *Handler) SetWebSocketServer(wsServer *websocket.Server) {
	h._wsServer = wsServer
}

// GetTemplateManager returns the template manager
func (h *Handler) GetTemplateManager() *template.Manager {
	return h.templateManager
}

// getQueue returns the queue
func (h *Handler) getQueue() core.Queue {
	return h.queue
}

// SetQueue sets the queue reference
func (h *Handler) SetQueue(q core.Queue) {
	h.queue = q
}

// Response is a response
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (h *Handler) respondJSON(w http.ResponseWriter, data *Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(&Response{Code: status, Message: message})
}
