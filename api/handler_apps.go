package api

import (
	"net/http"
	"strings"

	"github.com/cuihairu/herald/core/apps"
)

// The §13.1 集成者接入面. App routes authenticate with the namespace's
// own tokens (分级权限 config/trigger/query), NOT with the operator API
// key — an integration app never holds operator power. A nil registry
// keeps the whole face 404, the same "not configured = closed" shape as
// the source entries.

// withApp gates one app-scoped handler behind namespace auth: the app
// name comes from the path, the credential from the bearer/X-API-Key
// header (never the query string — URLs outlive the request in logs).
// Unknown app and wrong secret answer one uniform 401.
func (s *Server) withApp(scope apps.Scope, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apps == nil {
			http.NotFound(w, r)
			return
		}
		app := r.PathValue("app")
		secret := appSecret(r)
		scopes, ok := s.apps.Authenticate(app, secret)
		if !ok {
			s.handler.respondError(w, http.StatusUnauthorized, "invalid app credentials")
			return
		}
		if !scopes.Allows(scope) {
			s.handler.respondError(w, http.StatusForbidden, "token lacks "+string(scope)+" scope")
			return
		}
		fn(w, r)
	}
}

// appSecret extracts the bearer credential without the query-string
// fallback the operator API key allows.
func appSecret(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		parts := strings.SplitN(h, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
	}
	return r.Header.Get("X-API-Key")
}

// handleAppShow is the namespace introspection read: confirms the
// credential works and reports the token's granted scopes.
func (s *Server) handleAppShow(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	scopes, _ := s.apps.Authenticate(app, appSecret(r))
	s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{
		"name":   app,
		"scopes": scopes.Strings(),
	}})
}
