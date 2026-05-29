package api

import (
	"net/http"
)

// HandleWorkers handles workers status requests
func (h *Handler) HandleWorkers(w http.ResponseWriter, r *http.Request) {
	if h._wsServer == nil {
		h.respondJSON(w, &Response{
			Code:    0,
			Message: "ok",
			Data:    map[string]interface{}{"workers": []interface{}{}},
		})
		return
	}

	workers := h._wsServer.GetWorkers()
	workerList := make([]map[string]interface{}, 0, len(workers))
	for _, state := range workers {
		workerList = append(workerList, map[string]interface{}{
			"worker_id":      state.WorkerID,
			"platform":       state.Platform,
			"version":        state.Version,
			"capabilities":   state.Capabilities,
			"connected_at":   state.ConnectedAt,
			"last_heartbeat": state.LastHeartbeat,
			"status":         state.Status,
		})
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: map[string]interface{}{
			"count":   len(workerList),
			"workers": workerList,
		},
	})
}

// HandleQueue handles queue status requests
func (h *Handler) HandleQueue(w http.ResponseWriter, r *http.Request) {
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]interface{}{"size": h.getQueue().Size()},
	})
}
