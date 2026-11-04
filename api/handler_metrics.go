package api

import (
	"net/http"
	"time"
)

// HandleMetricsHistory serves the process metrics ring for the dashboard
// trend charts (GET /api/v1/metrics/history?window=1h|6h|24h).
func (h *Handler) HandleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	window, err := time.ParseDuration(r.URL.Query().Get("window"))
	if err != nil || window <= 0 {
		h.respondError(w, http.StatusBadRequest, "invalid window")
		return
	}

	samples := []map[string]any{}
	if h.metrics != nil {
		since := time.Now().Add(-window)
		for _, sample := range h.metrics.History(since) {
			samples = append(samples, map[string]any{
				"time":        sample.Time,
				"cpu_percent": sample.CPUPercent,
				"rss_bytes":   sample.RSSBytes,
				"goroutines":  sample.Goroutines,
				"workers":     sample.Workers,
			})
		}
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"window":           window.String(),
			"interval_seconds": h.metricsIntervalSeconds(),
			"samples":          samples,
		},
	})
}

// metricsIntervalSeconds reports the sampler cadence; 0 when metrics are
// not wired so the client knows there is no live series behind the data.
func (h *Handler) metricsIntervalSeconds() int {
	if h.metrics == nil {
		return 0
	}
	return int(h.metrics.Interval().Seconds())
}
