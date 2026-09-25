package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cuihairu/herald/core/groups"
)

// HandleGroups handles group list/create requests (GET/POST /api/v1/groups).
func (h *Handler) HandleGroups(w http.ResponseWriter, r *http.Request) {
	if h.groupsManager == nil {
		h.respondError(w, http.StatusServiceUnavailable, "groups manager is not configured")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Method == http.MethodPost {
		h.createGroup(w, r)
		return
	}

	// List serves the live table, so it cannot fail; the error result
	// stays in the signature for symmetry with the store-backed CRUD ops.
	list, _ := h.groupsManager.List(r.Context())
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]interface{}{"groups": list, "count": len(list)},
	})
}

// HandleGroupByID handles group get/update/delete requests
// (GET/PUT/DELETE /api/v1/groups/{id}).
func (h *Handler) HandleGroupByID(w http.ResponseWriter, r *http.Request) {
	if h.groupsManager == nil {
		h.respondError(w, http.StatusServiceUnavailable, "groups manager is not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "group id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getGroup(w, r, id)
	case http.MethodPut:
		h.updateGroup(w, r, id)
	case http.MethodDelete:
		h.deleteGroup(w, r, id)
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// createGroup validates and stores a new group.
func (h *Handler) createGroup(w http.ResponseWriter, r *http.Request) {
	var group groups.Group
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if _, err := h.groupsManager.Get(r.Context(), group.ID); err == nil {
		h.respondError(w, http.StatusConflict, "group already exists: "+group.ID)
		return
	} else if !errors.Is(err, groups.ErrNotFound) {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.groupsManager.Put(r.Context(), &group); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: group})
}

func (h *Handler) getGroup(w http.ResponseWriter, r *http.Request, id string) {
	group, err := h.groupsManager.Get(r.Context(), id)
	if errors.Is(err, groups.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "group not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: group})
}

// updateGroup replaces the group at id (the URL wins over the body id).
func (h *Handler) updateGroup(w http.ResponseWriter, r *http.Request, id string) {
	var group groups.Group
	if err := json.NewDecoder(r.Body).Decode(&group); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	group.ID = id
	if err := h.groupsManager.Put(r.Context(), &group); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: group})
}

func (h *Handler) deleteGroup(w http.ResponseWriter, r *http.Request, id string) {
	err := h.groupsManager.Delete(r.Context(), id)
	if errors.Is(err, groups.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "group not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok"})
}
