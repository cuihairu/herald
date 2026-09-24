package api

import (
	"encoding/json"
	"net/http"
)

// ackSource marks acknowledgements arriving through the HTTP API; card
// callbacks will use their own source label.
const ackSource = "api"

// HandleAlertByID handles alert acknowledgement status requests
// (GET /api/v1/alerts/{id}).
func (h *Handler) HandleAlertByID(w http.ResponseWriter, r *http.Request) {
	if h.ackStore == nil {
		h.respondError(w, http.StatusServiceUnavailable, "ack store is not configured")
		return
	}
	if r.Method != http.MethodGet {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "alert id is required")
		return
	}
	rec, err := h.ackStore.Get(r.Context(), id)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	data := map[string]interface{}{"alert_id": id, "acknowledged": rec != nil}
	if rec != nil {
		data["ack"] = rec
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: data})
}

// HandleAlertAck records an acknowledgement (POST /api/v1/alerts/{id}/ack).
// Acknowledging is idempotent: the first record for an id wins.
func (h *Handler) HandleAlertAck(w http.ResponseWriter, r *http.Request) {
	if h.ackStore == nil {
		h.respondError(w, http.StatusServiceUnavailable, "ack store is not configured")
		return
	}
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "alert id is required")
		return
	}
	var body struct {
		AckedBy string `json:"acked_by"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			h.respondError(w, http.StatusBadRequest, "invalid request")
			return
		}
	}
	rec, err := h.ackStore.Ack(r.Context(), id, body.AckedBy, ackSource)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rec})
}
