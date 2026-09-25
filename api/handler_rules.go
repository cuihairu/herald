package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cuihairu/herald/core/rules"
)

// HandleRules handles rule list requests (GET /api/v1/rules).
func (h *Handler) HandleRules(w http.ResponseWriter, r *http.Request) {
	if h.rulesEngine == nil {
		h.respondError(w, http.StatusServiceUnavailable, "rules engine is not configured")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Method == http.MethodPost {
		h.createRule(w, r)
		return
	}

	// List serves the live table (the evaluation truth), so it cannot
	// fail; the error result stays in the signature for API symmetry.
	list, _ := h.rulesEngine.List(r.Context())
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]interface{}{"rules": list, "count": len(list)},
	})
}

// HandleRuleByID handles rule get/update/delete requests
// (GET/PUT/DELETE /api/v1/rules/{id}).
func (h *Handler) HandleRuleByID(w http.ResponseWriter, r *http.Request) {
	if h.rulesEngine == nil {
		h.respondError(w, http.StatusServiceUnavailable, "rules engine is not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "rule id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getRule(w, r, id)
	case http.MethodPut:
		h.updateRule(w, r, id)
	case http.MethodDelete:
		h.deleteRule(w, r, id)
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// createRule validates and stores a new rule; expressions are compiled
// here, so invalid ones are rejected before ever reaching evaluation.
func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	var rule rules.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if _, err := h.rulesEngine.Get(r.Context(), rule.ID); err == nil {
		h.respondError(w, http.StatusConflict, "rule already exists: "+rule.ID)
		return
	} else if !errors.Is(err, rules.ErrNotFound) {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.rulesEngine.Put(r.Context(), &rule); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rule})
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, err := h.rulesEngine.Get(r.Context(), id)
	if errors.Is(err, rules.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "rule not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rule})
}

// updateRule replaces the rule at id (the URL wins over the body id).
func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request, id string) {
	var rule rules.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	rule.ID = id
	if err := h.rulesEngine.Put(r.Context(), &rule); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rule})
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request, id string) {
	err := h.rulesEngine.Delete(r.Context(), id)
	if errors.Is(err, rules.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "rule not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok"})
}
