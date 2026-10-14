package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/cuihairu/herald/core/deliverystore"
)

// HandleDeliveries handles delivery state-machine queries
// (GET /api/v1/deliveries). This face answers the lifecycle question the
// log face cannot: dead vs failed, retry progress and the next retry
// time.
func (h *Handler) HandleDeliveries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	filter := &deliverystore.Filter{
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

	deliveries := h.runtime.GetDeliveries(offset, limit, filter)
	total := h.runtime.GetDeliveriesCount(filter)

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"total":      total,
			"offset":     offset,
			"limit":      limit,
			"deliveries": deliveries,
		},
	})
}

// HandleDeliveriesStats handles delivery statistics requests
// (GET /api/v1/deliveries/stats), grouped by lifecycle status.
func (h *Handler) HandleDeliveriesStats(w http.ResponseWriter, r *http.Request) {
	stats := h.runtime.GetDeliveriesStats()
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: stats})
}

// HandleDeliveryByID handles a single delivery state-machine request by
// task ID (GET /api/v1/deliveries/{id}).
func (h *Handler) HandleDeliveryByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "delivery id is required")
		return
	}
	delivery, ok := h.runtime.GetDeliveryByID(id)
	if !ok {
		h.respondError(w, http.StatusNotFound, "delivery not found")
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: delivery})
}
