package api

import (
	"encoding/json"
	"net/http"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/service"
)

// NotifyRequest is the notification request
type NotifyRequest struct {
	Type       string              `json:"type"`
	Level      string              `json:"level,omitempty"`
	Channels   []string            `json:"channels"`
	Recipients map[string][]string `json:"recipients,omitempty"`
	Template   string              `json:"template,omitempty"`
	Params     map[string]any      `json:"params,omitempty"`

	// Channel is the singular spelling of Channels: both shapes extend
	// each other, and a request may name receivers in either or both.
	Channel string `json:"channel,omitempty"`
	// Audience names audience references ("group:", "user:", or a bare
	// channel name) to deliver to; each entry expands exactly like a
	// Channels element during routing.
	Audience []string `json:"audience,omitempty"`
	// Data supplies template data; merged into Params, which wins on a
	// clash (docs/design-audience-model.md §21).
	Data map[string]any `json:"data,omitempty"`
	// IDempotencyKey makes repeated requests carrying the same key
	// within the process lifetime return the recorded first result
	// without delivering again (docs/design-audience-model.md §24).
	IDempotencyKey string `json:"idempotency_key,omitempty"`

	// Direct content (when no template)
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// notifyChannels resolves the receivers named across the request's
// channel spellings: Channels, singular Channel, then Audience (each
// audience reference expands like a channel during routing).
func notifyChannels(req NotifyRequest) []string {
	channels := req.Channels
	if req.Channel != "" {
		channels = append(channels, req.Channel)
	}
	return append(channels, req.Audience...)
}

// notifyParams merges the request's template data: Params is the
// established field, Data the plan's spelling; on a clash Params wins.
func notifyParams(req NotifyRequest) map[string]any {
	if len(req.Data) == 0 {
		return req.Params
	}
	params := req.Params
	if params == nil {
		params = make(map[string]any, len(req.Data))
	}
	for k, v := range req.Data {
		if _, exists := params[k]; !exists {
			params[k] = v
		}
	}
	return params
}

// HandleNotify handles notify requests
func (h *Handler) HandleNotify(w http.ResponseWriter, r *http.Request) {
	var req NotifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Idempotent replay: a recorded key returns the original result
	// without touching the delivery pipeline.
	if req.IDempotencyKey != "" {
		if rec, ok := h.idempotency.get(req.IDempotencyKey); ok {
			h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: notifyData(rec)})
			return
		}
	}

	// Build Notification from request
	notification := &core.Notification{
		Type:        req.Type,
		Level:       req.Level,
		Channels:    notifyChannels(req),
		Recipients:  req.Recipients,
		TemplateRef: req.Template,
		Params:      notifyParams(req),
	}

	// Attach direct content if present
	if req.Title != "" || req.Body != "" {
		notification.Content = &core.DirectContent{
			Title: req.Title,
			Body:  req.Body,
		}
	}

	// Process notification
	result, err := h.notificationSvc.Process(r.Context(), notification)
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

	// A successfully processed request becomes the replay value for its
	// key; failed requests are never recorded and stay retryable.
	if req.IDempotencyKey != "" {
		h.idempotency.put(req.IDempotencyKey, *result)
	}

	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: notifyData(*result)})
}

// notifyData assembles the data payload of a successful notify response.
func notifyData(res service.ProcessResult) map[string]interface{} {
	data := map[string]interface{}{
		"notification_id": res.NotificationID,
		"task_ids":        res.TaskIDs,
		"accepted":        res.Accepted,
	}
	if len(res.Failed) > 0 {
		data["failed"] = res.Failed
	}
	return data
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
