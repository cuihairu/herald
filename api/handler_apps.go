package api

import (
	"encoding/json"
	"errors"
	"io"
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
		if !s.withAppScope(w, r, scope) {
			return
		}
		fn(w, r)
	}
}

// withAppScope is the check half of withApp, for handlers that route
// several methods (and therefore scopes) through one endpoint.
func (s *Server) withAppScope(w http.ResponseWriter, r *http.Request, scope apps.Scope) bool {
	if s.apps == nil {
		http.NotFound(w, r)
		return false
	}
	scopes, ok := s.apps.Authenticate(r.PathValue("app"), appSecret(r))
	if !ok {
		s.handler.respondError(w, http.StatusUnauthorized, "invalid app credentials")
		return false
	}
	if !scopes.Allows(scope) {
		s.handler.respondError(w, http.StatusForbidden, "token lacks "+string(scope)+" scope")
		return false
	}
	return true
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

// handleAppCategories is the §13.2 品类注册 face: POST registers (or
// idempotently re-confirms) a namespace category with its default
// urgency, GET lists the namespace's categories. POST is config power,
// GET is query power.
func (s *Server) handleAppCategories(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	switch r.Method {
	case http.MethodPost:
		if !s.withAppScope(w, r, apps.ScopeConfig) {
			return
		}
		var body struct {
			Name           string `json:"name"`
			DefaultUrgency string `json:"default_urgency"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			s.handler.respondError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if body.Name == "" || body.DefaultUrgency == "" {
			s.handler.respondError(w, http.StatusUnprocessableEntity, "name and default_urgency are required")
			return
		}
		// withAppScope already proved the namespace exists (no API
		// removes one), so RegisterCategory can only refuse on
		// validation or conflict here.
		err := s.apps.RegisterCategory(app, body.Name, body.DefaultUrgency)
		switch {
		case err == nil:
			s.handler.respondJSON(w, &Response{Code: 0, Message: "created"})
		case errors.Is(err, apps.ErrCategoryConflict):
			s.handler.respondError(w, http.StatusConflict, "category already registered with a different default urgency")
		default:
			s.handler.respondError(w, http.StatusUnprocessableEntity, err.Error())
		}
	case http.MethodGet:
		if !s.withAppScope(w, r, apps.ScopeQuery) {
			return
		}
		// Auth proved the namespace exists; the list may be empty.
		categories, _ := s.apps.Categories(app)
		s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{"categories": categories}})
	default:
		s.handler.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
