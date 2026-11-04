package api

import (
	"encoding/json"
	"net/http"

	"github.com/cuihairu/herald/internal/store"
)

// SetKeyStore wires the credential store behind the dashboard key
// management endpoints. Nil (the zero state) keeps the face closed.
func (h *Handler) SetKeyStore(db *store.DB) {
	h.keysDB = db
}

// HandleKeys handles key list/create requests (GET/POST /api/v1/keys).
func (h *Handler) HandleKeys(w http.ResponseWriter, r *http.Request) {
	if h.keysDB == nil {
		h.respondError(w, http.StatusServiceUnavailable, "key store is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		keys, err := h.keysDB.ListAPIKeys()
		if err != nil {
			h.respondError(w, http.StatusInternalServerError, "failed to list keys")
			return
		}
		h.respondJSON(w, &Response{
			Code:    0,
			Message: "ok",
			Data:    map[string]interface{}{"keys": keys, "count": len(keys)},
		})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			h.respondError(w, http.StatusBadRequest, "name is required")
			return
		}
		created, err := h.keysDB.CreateAPIKey(req.Name)
		if err != nil {
			h.respondError(w, http.StatusInternalServerError, "failed to create key")
			return
		}
		h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: created})
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// HandleKeyByID handles key revocation (DELETE /api/v1/keys/{id}).
func (h *Handler) HandleKeyByID(w http.ResponseWriter, r *http.Request) {
	if h.keysDB == nil {
		h.respondError(w, http.StatusServiceUnavailable, "key store is not configured")
		return
	}
	if r.Method != http.MethodDelete {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "key id is required")
		return
	}
	if err := h.keysDB.RevokeAPIKey(id); err != nil {
		if err == store.ErrNotFound {
			h.respondError(w, http.StatusNotFound, "key not found: "+id)
			return
		}
		h.respondError(w, http.StatusInternalServerError, "failed to revoke key")
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok"})
}
