package api

import (
	"math"
	"net/http"
	"time"

	"github.com/cuihairu/herald/core/logstore"
)

// overviewNow is the clock seam for the 24h delivery window; tests move it
// forward to prove old entries fall out of the window.
var overviewNow = time.Now

// overviewWindow is how far back the delivery success rate looks.
const overviewWindow = 24 * time.Hour

// HandleOverview returns the dashboard landing metrics in one read:
// provider enablement, queue depth, the 24h delivery success rate and
// the online worker count.
func (h *Handler) HandleOverview(w http.ResponseWriter, r *http.Request) {
	enabled, total := 0, 0
	for _, status := range h.runtime.GetProviderStatus() {
		total++
		if status.Enabled {
			enabled++
		}
	}

	depth := 0
	if q := h.getQueue(); q != nil {
		depth = q.Size()
	}

	since := overviewNow().Add(-overviewWindow)
	success := h.countLogs(since, "success")
	failed := h.countLogs(since, "failed")
	deliveryTotal := success + failed
	var rate float64
	if deliveryTotal > 0 {
		rate = math.Round(float64(success)/float64(deliveryTotal)*1000) / 10
	}
	// The bounded in-memory ring may not reach back a full 24h after a
	// restart or under high volume; report the window it actually covers
	// so the dashboard never presents a truncated sample as complete.
	coverage := overviewWindow
	if oldest, ok := h.runtime.OldestLogCreatedAt(); ok {
		covered := overviewNow().Sub(oldest)
		if covered > 0 && covered < coverage {
			coverage = covered
		}
	}

	online, workerTotal := 0, 0
	if h.workerRegistry != nil {
		for _, info := range h.workerRegistry.List() {
			workerTotal++
			if info.Status == "online" {
				online++
			}
		}
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]any{
			"providers": map[string]any{"enabled": enabled, "total": total},
			"queue":     map[string]any{"depth": depth},
			"delivery": map[string]any{
				"window":       "24h",
				"coverage":     coverage.String(),
				"success":      success,
				"failed":       failed,
				"total":        deliveryTotal,
				"success_rate": rate,
			},
			"workers": map[string]any{"online": online, "total": workerTotal},
		},
	})
}

// countLogs counts log entries with the given attempt status inside the
// 24h window ending at the overview clock.
func (h *Handler) countLogs(since time.Time, status string) int {
	return h.runtime.GetLogsCount(&logstore.Filter{Since: since, Status: status})
}
