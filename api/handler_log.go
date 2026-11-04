package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/cuihairu/herald/core/logstore"
)

// HandleLogs handles logs requests
func (h *Handler) HandleLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	filter := &logstore.Filter{
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

	logs := h.runtime.GetLogs(offset, limit, filter)
	total := h.runtime.GetLogsCount(filter)

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"total":  total,
			"offset": offset,
			"limit":  limit,
			"logs":   logs,
		},
	})
}

// HandleLogsStats handles logs statistics requests
func (h *Handler) HandleLogsStats(w http.ResponseWriter, r *http.Request) {
	stats := h.runtime.GetLogsStats()
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: stats})
}

// HandleLogsTimeseries serves the send-volume series
// (GET /api/v1/logs/timeseries?window=1h|6h|24h&bucket=...). The bucket
// defaults by window: 1h→1m, 6h→15m, otherwise 1h.
func (h *Handler) HandleLogsTimeseries(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	window, err := time.ParseDuration(query.Get("window"))
	if err != nil || window <= 0 {
		h.respondError(w, http.StatusBadRequest, "invalid window")
		return
	}

	bucketRaw := query.Get("bucket")
	var bucket time.Duration
	if bucketRaw == "" {
		switch {
		case window <= time.Hour:
			bucket = time.Minute
		case window <= 6*time.Hour:
			bucket = 15 * time.Minute
		default:
			bucket = time.Hour
		}
	} else {
		bucket, err = time.ParseDuration(bucketRaw)
		if err != nil {
			h.respondError(w, http.StatusBadRequest, "invalid bucket")
			return
		}
	}
	if bucket <= 0 || bucket > window {
		h.respondError(w, http.StatusBadRequest, "bucket must be positive and within the window")
		return
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"window": window.String(),
			"bucket": bucket.String(),
			"points": h.runtime.GetLogsTimeseries(window, bucket),
		},
	})
}

// HandleLogByID handles a single log request by ID
func (h *Handler) HandleLogByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "log id is required")
		return
	}
	log := h.runtime.GetLogByID(id)
	if log == nil {
		h.respondError(w, http.StatusNotFound, "log not found")
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: log})
}
