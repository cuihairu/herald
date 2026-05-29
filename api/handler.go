package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/logstore"
	"github.com/cuihairu/herald/core/service"
	"github.com/cuihairu/herald/core/template"
	"github.com/cuihairu/herald/core/websocket"
)

// Handler handles HTTP requests
type Handler struct {
	notificationSvc *service.NotificationService
	templateManager *template.Manager
	_wsServer       *websocket.Server
	queue           core.Queue
}

// NewHandler creates a new handler
func NewHandler(notificationSvc *service.NotificationService, templateMgr *template.Manager) *Handler {
	if templateMgr == nil {
		templateMgr = template.NewManager()
	}
	return &Handler{
		notificationSvc: notificationSvc,
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

// NotifyRequest is the notification request
type NotifyRequest struct {
	Type       string              `json:"type"`
	Level      string              `json:"level,omitempty"`
	Channels   []string            `json:"channels"`
	Recipients map[string][]string `json:"recipients,omitempty"`
	Template   string              `json:"template,omitempty"`
	Params     map[string]any      `json:"params,omitempty"`

	// Direct content (when no template)
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
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

	// Build Notification from request
	notification := &core.Notification{
		Type:        req.Type,
		Level:       req.Level,
		Channels:    req.Channels,
		Recipients:  req.Recipients,
		TemplateRef: req.Template,
		Params:      req.Params,
	}

	// Attach direct content if present
	if req.Title != "" || req.Body != "" {
		notification.Content = &core.DirectContent{
			Title: req.Title,
			Body:  req.Body,
		}
	}

	// Process notification
	result, err := h.notificationSvc.Process(r.Context(), notification, h.getQueue())
	if err != nil {
		data := map[string]interface{}{
			"accepted": []string{},
			"failed":   []string{},
		}
		if result != nil {
			data["notification_id"] = result.NotificationID
			data["accepted"] = result.Accepted
			data["failed"] = result.Failed
		}
		h.respondJSON(w, &Response{
			Code:    422,
			Message: err.Error(),
			Data:    data,
		})
		return
	}

	data := map[string]interface{}{
		"notification_id": result.NotificationID,
		"task_ids":        result.TaskIDs,
		"accepted":        result.Accepted,
	}

	if len(result.Failed) > 0 {
		data["failed"] = result.Failed
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    data,
	})
}

// HandleStatus handles status requests
func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	statuses := h.notificationSvc.GetRuntime().GetProviderStatus()
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
	statuses := h.notificationSvc.GetRuntime().GetProviderStatus()
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
			Data:    map[string]interface{}{"workers": []interface{}{}},
		})
		return
	}

	workers := h._wsServer.GetWorkers()
	workerList := make([]map[string]interface{}, 0, len(workers))
	for _, state := range workers {
		workerList = append(workerList, map[string]interface{}{
			"worker_id":      state.WorkerID,
			"platform":       state.Platform,
			"version":        state.Version,
			"capabilities":   state.Capabilities,
			"connected_at":   state.ConnectedAt,
			"last_heartbeat": state.LastHeartbeat,
			"status":         state.Status,
		})
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
		Data:    map[string]interface{}{"size": h.getQueue().Size()},
	})
}

// HandleEnableProvider enables a provider
func (h *Handler) HandleEnableProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}
	if err := h.notificationSvc.GetRuntime().Enable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "provider enabled"})
}

// HandleDisableProvider disables a provider
func (h *Handler) HandleDisableProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}
	if err := h.notificationSvc.GetRuntime().Disable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "provider disabled"})
}

// HandleEnableProviderWithName enables a provider by name
func (h *Handler) HandleEnableProviderWithName(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.notificationSvc.GetRuntime().Enable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "provider enabled"})
}

// HandleDisableProviderWithName disables a provider by name
func (h *Handler) HandleDisableProviderWithName(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.notificationSvc.GetRuntime().Disable(name); err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "provider disabled"})
}

// getQueue returns the queue
func (h *Handler) getQueue() core.Queue {
	return h.queue
}

// SetQueue sets the queue reference
func (h *Handler) SetQueue(q core.Queue) {
	h.queue = q
}

// HandleLogs handles logs requests
func (h *Handler) HandleLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	filter := &logstore.Filter{
		Status:   query.Get("status"),
		Provider: query.Get("provider"),
		Level:    query.Get("level"),
	}

	if since := query.Get("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = t
		}
	}
	if until := query.Get("until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = t
		}
	}

	rt := h.notificationSvc.GetRuntime()
	logs := rt.GetLogs(offset, limit, filter)
	total := rt.GetLogsCount(filter)

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"total":  total,
			"offset": offset,
			"limit":  limit,
			"logs":   logs,
		},
	})
}

// HandleLogsStats handles logs statistics requests
func (h *Handler) HandleLogsStats(w http.ResponseWriter, r *http.Request) {
	stats := h.notificationSvc.GetRuntime().GetLogsStats()
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: stats})
}

// HandleLogByID handles a single log request by ID
func (h *Handler) HandleLogByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "log id is required")
		return
	}
	log := h.notificationSvc.GetRuntime().GetLogByID(id)
	if log == nil {
		h.respondError(w, http.StatusNotFound, "log not found")
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: log})
}

// ProviderConfigResponse is a provider config response
type ProviderConfigResponse struct {
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Enabled bool                   `json:"enabled"`
	Config  map[string]interface{} `json:"config"`
	Schema  map[string]string      `json:"schema,omitempty"`
}

// HandleProviderConfig handles provider config requests
func (h *Handler) HandleProviderConfig(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	provider, err := h.notificationSvc.GetRuntime().GetProvider(name)
	if err != nil {
		h.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Read actual config from provider if it exposes one
	config := make(map[string]interface{})
	if cg, ok := provider.(interface{ GetConfig() map[string]interface{} }); ok {
		config = core.MaskConfig(cg.GetConfig())
	}

	status := provider.Status()
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: &ProviderConfigResponse{
			Name:    name,
			Type:    status.Type,
			Enabled: h.notificationSvc.GetRuntime().IsEnabled(name),
			Config:  config,
			Schema:  getProviderSchema(name),
		},
	})
}

// UpdateProviderConfigRequest is a request to update provider config
type UpdateProviderConfigRequest struct {
	Config map[string]interface{} `json:"config"`
	Merge  bool                   `json:"merge,omitempty"`
}

// HandleUpdateProviderConfig handles provider config update requests
func (h *Handler) HandleUpdateProviderConfig(w http.ResponseWriter, r *http.Request, name string) {
	if name == "" {
		h.respondError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	var req UpdateProviderConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	config := req.Config

	// Merge with existing config if requested
	if req.Merge {
		if existing, err := h.notificationSvc.GetRuntime().GetProvider(name); err == nil {
			if cg, ok := existing.(interface{ GetConfig() map[string]interface{} }); ok {
				merged := cg.GetConfig()
				for k, v := range req.Config {
					// Don't overwrite with masked values
					if s, ok := v.(string); !ok || s != "******" {
						merged[k] = v
					}
				}
				config = merged
			}
		}
	}

	rt := h.notificationSvc.GetRuntime()
	provider, err := rt.CreateProvider(name, config)
	if err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	wasEnabled := rt.IsEnabled(name)
	if err := rt.ReplaceProvider(name, provider); err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if wasEnabled {
		_ = rt.Enable(name)
	}

	h.respondJSON(w, &Response{Code: 0, Message: "provider config updated"})
}

// getProviderSchema returns the config schema for a provider
func getProviderSchema(name string) map[string]string {
	schemas := map[string]map[string]string{
		"telegram":   {"token": "string", "chat_id": "string"},
		"feishu":     {"webhook_url": "string"},
		"wecom":      {"webhook_url": "string"},
		"dingtalk":   {"access_token": "string", "secret": "string"},
		"slack":      {"webhook_url": "string"},
		"discord":    {"webhook_url": "string"},
		"email":      {"host": "string", "port": "number", "username": "string", "password": "string", "from": "string"},
		"webhook":    {"url": "string"},
		"wechat":     {"service": "string", "send_key": "string", "token": "string", "app_token": "string", "uid": "string"},
		"wechatmp":   {"app_id": "string", "app_secret": "string", "template_id": "string", "default_url": "string"},
		"aliyunsms":  {"access_key_id": "string", "access_key_secret": "string", "sign_name": "string"},
		"tencentsms": {"secret_id": "string", "secret_key": "string", "app_id": "string", "sign_name": "string"},
		"neteasesms": {"app_key": "string", "app_secret": "string"},
	}
	if schema, ok := schemas[name]; ok {
		return schema
	}
	return map[string]string{"config": "object"}
}

// TemplateRequest is a template create/update request
type TemplateRequest struct {
	ID       string                      `json:"id"`
	Name     string                      `json:"name"`
	Title    string                      `json:"title"`
	Level    string                      `json:"level"`
	Fields   []template.Field            `json:"fields"`
	Bindings map[string]template.Binding `json:"bindings,omitempty"`
}

// HandleTemplates handles template list requests
func (h *Handler) HandleTemplates(w http.ResponseWriter, r *http.Request) {
	templates := h.templateManager.List()
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]interface{}{"templates": templates, "count": len(templates)},
	})
}

// HandleTemplateByID handles template get/update/delete requests
func (h *Handler) HandleTemplateByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "template id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getTemplate(w, id)
	case http.MethodPut, http.MethodPost:
		h.updateTemplate(w, r, id)
	case http.MethodDelete:
		h.deleteTemplate(w, id)
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// HandleCreateTemplate handles template creation requests
func (h *Handler) HandleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req TemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	tmpl := &template.Template{
		ID:       req.ID,
		Name:     req.Name,
		Title:    req.Title,
		Level:    req.Level,
		Fields:   req.Fields,
		Bindings: req.Bindings,
	}

	if err := h.templateManager.Register(tmpl); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, &Response{Code: 0, Message: "template created", Data: map[string]interface{}{"id": tmpl.ID}})
}

func (h *Handler) getTemplate(w http.ResponseWriter, id string) {
	tmpl, err := h.templateManager.Get(id)
	if err != nil {
		h.respondError(w, http.StatusNotFound, "template not found")
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: tmpl})
}

func (h *Handler) updateTemplate(w http.ResponseWriter, r *http.Request, id string) {
	var req TemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Preserve existing bindings if client didn't send any
	bindings := req.Bindings
	if bindings == nil {
		if existing, err := h.templateManager.Get(id); err == nil && existing.Bindings != nil {
			bindings = existing.Bindings
		}
	}

	tmpl := &template.Template{
		ID:       id,
		Name:     req.Name,
		Title:    req.Title,
		Level:    req.Level,
		Fields:   req.Fields,
		Bindings: bindings,
	}

	if err := h.templateManager.Register(tmpl); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.respondJSON(w, &Response{Code: 0, Message: "template updated"})
}

func (h *Handler) deleteTemplate(w http.ResponseWriter, id string) {
	if err := h.templateManager.Delete(id); err != nil {
		h.respondError(w, http.StatusNotFound, "template not found")
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "template deleted"})
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
