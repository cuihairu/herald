package api

import (
	"net/http"
)

// handleAppList handles GET /api/v1/apps: the sorted list of configured
// integration namespaces for the dashboard apps page. The registry face
// is closed (503) when no apps are configured; the route sits behind the
// operator auth, not the app-token gate.
func (s *Server) handleAppList(w http.ResponseWriter, r *http.Request) {
	if s.apps == nil {
		s.handler.respondError(w, http.StatusServiceUnavailable, "apps registry is not configured")
		return
	}
	if r.Method != http.MethodGet {
		s.handler.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	names := s.apps.Names()
	s.handler.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]any{"apps": names, "count": len(names)},
	})
}
