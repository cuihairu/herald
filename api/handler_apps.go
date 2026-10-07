package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/dedup"
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

// policiesJSON renders the namespace's override set in canonical
// vocabulary (intensities L0..L5, modes fixed/escalation/parallel,
// durations Go-style) so read-back is stable across writes.
func policiesJSON(p apps.AppPolicies) map[string]any {
	intensity := make(map[string]string, len(p.ChannelIntensity))
	for ch, i := range p.ChannelIntensity {
		intensity[ch] = i.String()
	}
	modes := make(map[string]string, len(p.ModeByCategory))
	for cat, m := range p.ModeByCategory {
		modes[cat] = m.String()
	}
	tiers := make(map[string]string, len(p.DedupTiers))
	for cat, tier := range p.DedupTiers {
		tiers[cat] = string(tier)
	}
	windows := make(map[string]string, len(p.DedupWindows))
	for cat, d := range p.DedupWindows {
		windows[cat] = d.String()
	}
	data := map[string]any{}
	if len(intensity) > 0 {
		data["channel_intensity"] = intensity
	}
	if len(modes) > 0 {
		data["mode_by_category"] = modes
	}
	if p.AckTimeout > 0 {
		data["ack_timeout"] = p.AckTimeout.String()
	}
	if len(tiers) > 0 {
		data["dedup_tiers"] = tiers
	}
	if len(windows) > 0 {
		data["dedup_windows"] = windows
	}
	return data
}

// handleAppPoliciesRead is the aggregate read-back (query power).
func (s *Server) handleAppPoliciesRead(w http.ResponseWriter, r *http.Request) {
	if !s.withAppScope(w, r, apps.ScopeQuery) {
		return
	}
	// Auth proved the namespace exists; an unset override set reads
	// back as an empty object.
	p, _ := s.apps.Policies(r.PathValue("app"))
	s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: policiesJSON(p)})
}

// putAppPolicies runs the shared PUT shape: the family parser turns the
// body into a typed setter (refusing any bad value with 422 naming the
// key) and the setter applies under the registry lock; the answer is
// the full read-back.
func (s *Server) putAppPolicies(w http.ResponseWriter, r *http.Request, parse func(body map[string]any) (func(*apps.AppPolicies), error)) {
	if !s.withAppScope(w, r, apps.ScopeConfig) {
		return
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		s.handler.respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	set, err := parse(body)
	if err != nil {
		s.handler.respondError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// withAppScope already proved the namespace exists (no API removes
	// one) and the family was validated above, so the write cannot
	// refuse — the ignored error is the same contract impossibility as
	// the nil second return of the reads above.
	app := r.PathValue("app")
	_ = s.apps.UpdatePolicies(app, set)
	updated, _ := s.apps.Policies(app)
	s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: policiesJSON(updated)})
}

func intensityFamily(body map[string]any) (func(*apps.AppPolicies), error) {
	table := make(map[string]audience.Intensity, len(body))
	for key, raw := range body {
		name, _ := raw.(string)
		i, err := audience.ParseIntensity(name)
		if err != nil {
			return nil, fmt.Errorf("channel_intensity[%s]: %w", key, err)
		}
		table[key] = i
	}
	return func(p *apps.AppPolicies) { p.ChannelIntensity = table }, nil
}

func modeFamily(body map[string]any) (func(*apps.AppPolicies), error) {
	table := make(map[string]audience.Mode, len(body))
	for key, raw := range body {
		name, _ := raw.(string)
		m, err := audience.ParseMode(name)
		if err != nil {
			return nil, fmt.Errorf("mode_by_category[%s]: %w", key, err)
		}
		table[key] = m
	}
	return func(p *apps.AppPolicies) { p.ModeByCategory = table }, nil
}

func escalationFamily(body map[string]any) (func(*apps.AppPolicies), error) {
	raw, _ := body["ack_timeout"].(string)
	if raw == "" {
		return nil, errors.New("ack_timeout is required (e.g. \"15m\"; \"0s\" clears the override)")
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return nil, fmt.Errorf("ack_timeout %q: not a non-negative duration", raw)
	}
	return func(p *apps.AppPolicies) { p.AckTimeout = d }, nil
}

func dedupFamily(body map[string]any) (func(*apps.AppPolicies), error) {
	tiers := make(map[string]dedup.Tier)
	if rawTiers, ok := body["tiers"].(map[string]any); ok {
		for cat, raw := range rawTiers {
			name, _ := raw.(string)
			tier, err := dedup.ParseTier(name)
			if err != nil {
				return nil, fmt.Errorf("tiers[%s]: %w", cat, err)
			}
			tiers[cat] = tier
		}
	}
	windows := make(map[string]time.Duration)
	if rawWins, ok := body["windows"].(map[string]any); ok {
		for cat, raw := range rawWins {
			name, _ := raw.(string)
			d, err := time.ParseDuration(name)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("windows[%s]: %q is not a positive duration", cat, name)
			}
			windows[cat] = d
		}
	}
	return func(p *apps.AppPolicies) { p.DedupTiers = tiers; p.DedupWindows = windows }, nil
}

// The four PUT families each own their body shape; the aggregate GET
// (handleAppPoliciesRead) reads the whole set back.
func (s *Server) handleAppPoliciesIntensity(w http.ResponseWriter, r *http.Request) {
	s.putAppPolicies(w, r, intensityFamily)
}

func (s *Server) handleAppPoliciesMode(w http.ResponseWriter, r *http.Request) {
	s.putAppPolicies(w, r, modeFamily)
}

func (s *Server) handleAppPoliciesEscalation(w http.ResponseWriter, r *http.Request) {
	s.putAppPolicies(w, r, escalationFamily)
}

func (s *Server) handleAppPoliciesDedup(w http.ResponseWriter, r *http.Request) {
	s.putAppPolicies(w, r, dedupFamily)
}
