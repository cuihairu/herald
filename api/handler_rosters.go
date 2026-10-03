package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cuihairu/herald/core/roster"
)

// rostersGet is a package-level seam (same pattern as groupsGet): a real
// Manager.Get only ever returns nil or ErrNotFound, so the internal-error
// arms in createRoster/getRoster below could not be executed without a
// substitutable lookup. They exist so that a widened Get contract fails
// loudly as 500 instead of being read as "not found".
var rostersGet = func(ctx context.Context, m *roster.Manager, id string) (roster.Roster, error) {
	return m.Get(ctx, id)
}

// HandleRosters handles duty roster list/create requests
// (GET/POST /api/v1/rosters).
func (h *Handler) HandleRosters(w http.ResponseWriter, r *http.Request) {
	if h.rostersManager == nil {
		h.respondError(w, http.StatusServiceUnavailable, "rosters manager is not configured")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Method == http.MethodPost {
		h.createRoster(w, r)
		return
	}

	// List serves the live table, so it cannot fail; the error result
	// stays in the signature for symmetry with the store-backed CRUD ops.
	list, _ := h.rostersManager.List(r.Context())
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]interface{}{"rosters": list, "count": len(list)},
	})
}

// HandleRosterByID handles duty roster get/update/delete requests
// (GET/PUT/DELETE /api/v1/rosters/{id}).
func (h *Handler) HandleRosterByID(w http.ResponseWriter, r *http.Request) {
	if h.rostersManager == nil {
		h.respondError(w, http.StatusServiceUnavailable, "rosters manager is not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "roster id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getRoster(w, r, id)
	case http.MethodPut:
		h.updateRoster(w, r, id)
	case http.MethodDelete:
		h.deleteRoster(w, r, id)
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// createRoster validates and stores a new roster.
func (h *Handler) createRoster(w http.ResponseWriter, r *http.Request) {
	var rt roster.Roster
	if err := json.NewDecoder(r.Body).Decode(&rt); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if _, err := rostersGet(r.Context(), h.rostersManager, rt.ID); err == nil {
		h.respondError(w, http.StatusConflict, "roster already exists: "+rt.ID)
		return
	} else if !errors.Is(err, roster.ErrNotFound) {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.rostersManager.Put(r.Context(), &rt); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rt})
}

func (h *Handler) getRoster(w http.ResponseWriter, r *http.Request, id string) {
	rt, err := rostersGet(r.Context(), h.rostersManager, id)
	if errors.Is(err, roster.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "roster not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rt})
}

// updateRoster replaces the roster at id (the URL wins over the body id).
func (h *Handler) updateRoster(w http.ResponseWriter, r *http.Request, id string) {
	var rt roster.Roster
	if err := json.NewDecoder(r.Body).Decode(&rt); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	rt.ID = id
	if err := h.rostersManager.Put(r.Context(), &rt); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rt})
}

func (h *Handler) deleteRoster(w http.ResponseWriter, r *http.Request, id string) {
	err := h.rostersManager.Delete(r.Context(), id)
	if errors.Is(err, roster.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "roster not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok"})
}
