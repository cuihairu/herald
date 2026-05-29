package api

import (
	"encoding/json"
	"net/http"

	"github.com/cuihairu/herald/core/template"
)

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
