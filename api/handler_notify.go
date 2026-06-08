package api

import (
	"encoding/json"
	"net/http"

	"github.com/cuihairu/herald/core"
)

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
