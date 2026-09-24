package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/cuihairu/herald/core/incident"
)

// HandleIncidents lists the incident ledger
// (GET /api/v1/incidents?status=open&rule_id=..&alert_id=..&limit=..),
// newest first.
func (h *Handler) HandleIncidents(w http.ResponseWriter, r *http.Request) {
	if h.incidents == nil {
		h.respondError(w, http.StatusServiceUnavailable, "incident store is not configured")
		return
	}
	if r.Method != http.MethodGet {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	filter := &incident.Filter{}
	switch status := strings.TrimSpace(q.Get("status")); status {
	case "", "all":
	case string(incident.StatusOpen), string(incident.StatusAcked), string(incident.StatusResolved):
		filter.Status = incident.Status(status)
	default:
		h.respondError(w, http.StatusBadRequest, "invalid status filter (open, acked, resolved)")
		return
	}
	filter.RuleID = strings.TrimSpace(q.Get("rule_id"))
	filter.AlertID = strings.TrimSpace(q.Get("alert_id"))

	limit := 100
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 1000 {
			h.respondError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}

	incidents := h.incidents.List(filter)
	if len(incidents) > limit {
		incidents = incidents[:limit]
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{
		"incidents": incidents,
		"count":     len(incidents),
	}})
}

// HandleIncidentByID returns one incident with its full timeline
// (GET /api/v1/incidents/{id}).
func (h *Handler) HandleIncidentByID(w http.ResponseWriter, r *http.Request) {
	if h.incidents == nil {
		h.respondError(w, http.StatusServiceUnavailable, "incident store is not configured")
		return
	}
	if r.Method != http.MethodGet {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.PathValue("id")
	inc := h.incidents.Get(id)
	if inc == nil {
		h.respondError(w, http.StatusNotFound, "incident not found")
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: inc})
}
