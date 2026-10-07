package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cuihairu/herald/core/apps"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/logstore"
	"github.com/cuihairu/herald/core/service"
	"github.com/cuihairu/herald/core/template"
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

// handleAppTemplates is the §13.2 模板注册 face. POST upserts (config
// power) with the manager's validation refusing bad templates; GET
// lists the namespace's templates (query power). Rendering goes through
// the namespace's own manager — global templates are invisible here.
func (s *Server) handleAppTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if !s.withAppScope(w, r, apps.ScopeConfig) {
			return
		}
		var tmpl template.Template
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&tmpl); err != nil {
			s.handler.respondError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		// withAppScope already proved the namespace exists (no API
		// removes one), so the manager lookup cannot refuse.
		mgr, _ := s.apps.Templates(r.PathValue("app"))
		if err := mgr.Register(&tmpl); err != nil {
			s.handler.respondError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: tmpl})
	case http.MethodGet:
		if !s.withAppScope(w, r, apps.ScopeQuery) {
			return
		}
		// Auth proved the namespace exists; the manager's List always
		// answers a non-nil slice, so a fresh namespace marshals [] not null.
		list, _ := s.apps.ListTemplates(r.PathValue("app"))
		s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: list})
	default:
		s.handler.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAppTemplateByID reads or removes one namespace template.
func (s *Server) handleAppTemplateByID(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	id := r.PathValue("id")
	switch r.Method {
	case http.MethodGet:
		if !s.withAppScope(w, r, apps.ScopeQuery) {
			return
		}
		tmpl, err := s.apps.GetTemplate(app, id)
		if err != nil {
			s.handler.respondError(w, http.StatusNotFound, err.Error())
			return
		}
		s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: tmpl})
	case http.MethodDelete:
		if !s.withAppScope(w, r, apps.ScopeConfig) {
			return
		}
		if err := s.apps.DeleteTemplate(app, id); err != nil {
			s.handler.respondError(w, http.StatusNotFound, err.Error())
			return
		}
		s.handler.respondJSON(w, &Response{Code: 0, Message: "deleted"})
	default:
		s.handler.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// dispatchBody is the §13.3 request shape. urgency is optional — 缺省取
// 品类默认; relation_type is optional — subscription by default, the
// 指派型 (enrollment) trigger must declare itself.
type dispatchBody struct {
	Category     string         `json:"category"`
	Urgency      string         `json:"urgency"`
	RelationType string         `json:"relation_type"`
	Audiences    []string       `json:"audiences"`
	DedupKey     string         `json:"dedup_key"`
	EventID      string         `json:"event_id"`
	State        string         `json:"state"`
	Template     string         `json:"template"`
	Params       map[string]any `json:"params"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
}

// handleAppDispatch is the §13.3 触发面: the namespaced dispatch with
// full policy semantics. The handler resolves the namespace half (the
// registered category's default urgency, the app's policy overrides
// over the global §6 tables, the namespace template manager) and the
// service owns the matching and delivery. /notify stays the
// anonymous-compatible face — this path is the one with categories.
func (s *Server) handleAppDispatch(w http.ResponseWriter, r *http.Request) {
	if !s.withAppScope(w, r, apps.ScopeTrigger) {
		return
	}
	var body dispatchBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		s.handler.respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Category == "" {
		s.handler.respondError(w, http.StatusUnprocessableEntity, "category is required")
		return
	}
	if len(body.Audiences) == 0 {
		s.handler.respondError(w, http.StatusUnprocessableEntity, "audiences must name at least one audience")
		return
	}
	if s.delivery == nil {
		// The §6 strategy件 is a startup fact; without it there is no
		// matching to speak of — the face stays closed, same shape as
		// every unconfigured face.
		s.handler.respondError(w, http.StatusNotFound, "delivery policy not configured")
		return
	}

	app := r.PathValue("app")
	// The namespace taxonomy is the point of the app face: an
	// unregistered category is 422, never a silent fallback to the
	// operator's global vocabulary.
	def, ok := s.apps.CategoryUrgency(app, body.Category)
	if !ok {
		s.handler.respondError(w, http.StatusUnprocessableEntity, "category "+body.Category+" is not registered in this namespace")
		return
	}
	var urgency audience.Urgency
	if body.Urgency != "" {
		u, err := audience.ParseUrgency(body.Urgency)
		if err != nil {
			s.handler.respondError(w, http.StatusUnprocessableEntity, "urgency "+body.Urgency+": "+err.Error())
			return
		}
		urgency = u
	} else {
		// Registry-stored urgencies are canonical vocabulary by
		// construction (RegisterCategory parsed and re-rendered them),
		// so this parse cannot refuse.
		urgency, _ = audience.ParseUrgency(def)
	}

	rel := audience.RelationSubscription
	switch body.RelationType {
	case "":
	case "subscription":
		rel = audience.RelationSubscription
	case "enrollment":
		rel = audience.RelationEnrollment
	default:
		s.handler.respondError(w, http.StatusUnprocessableEntity, "relation_type must be subscription or enrollment")
		return
	}

	// The namespace's overrides ride on top of the operator's global
	// policy; the phone gate is not overridable (§6.2 双重同意).
	appPolicies, _ := s.apps.Policies(app)
	effective := s.delivery.Overlay(audience.Overlay{
		ChannelIntensity: appPolicies.ChannelIntensity,
		ModeByCategory:   appPolicies.ModeByCategory,
	})

	// withAppScope already proved the namespace exists, so the manager
	// lookup cannot refuse; a template render miss fails the dispatch
	// with 422 inside Dispatch.
	templates, _ := s.apps.Templates(app)

	out, err := s.notificationSvc.Dispatch(r.Context(), service.DispatchRequest{
		Spec: service.DispatchSpec{
			App:          app,
			Category:     body.Category,
			Urgency:      urgency,
			RelationType: rel,
			Audiences:    body.Audiences,
			DedupKey:     body.DedupKey,
			EventID:      body.EventID,
			State:        body.State,
			Template:     body.Template,
			Params:       body.Params,
			Title:        body.Title,
			Body:         body.Body,
		},
		Policy:    effective,
		Filter:    s.filter,
		Templates: templates,
	})
	if err != nil {
		s.handler.respondError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: out})
}

// handleAppDeliveries is the §13.4 投递状态 query: the namespace's own
// delivery attempts, scoped by the "app:<name>" source the dispatch
// face stamps, narrowed by audience/category/status.
func (s *Server) handleAppDeliveries(w http.ResponseWriter, r *http.Request) {
	if !s.withAppScope(w, r, apps.ScopeQuery) {
		return
	}
	query := r.URL.Query()
	offset, _ := strconv.Atoi(query.Get("offset"))
	limit, _ := strconv.Atoi(query.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	// The namespace boundary rides the source dimension: an app reads
	// its own dispatches, never the operator's or a sibling's.
	filter := &logstore.Filter{
		Status:     query.Get("status"),
		Category:   query.Get("category"),
		AudienceID: query.Get("audience"),
		Source:     "app:" + r.PathValue("app"),
	}
	logs := s.handler.runtime.GetLogs(offset, limit, filter)
	total := s.handler.runtime.GetLogsCount(filter)
	s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{
		"total":  total,
		"offset": offset,
		"limit":  limit,
		"logs":   logs,
	}})
}

// handleAppAudit is the §13.4 审计流: the namespace's relational trail
// (dispatch folds, later relation changes) since an RFC3339 timestamp.
func (s *Server) handleAppAudit(w http.ResponseWriter, r *http.Request) {
	if s.audit == nil {
		// The audit trail is a process-wide face; unconfigured means
		// closed, same as every other face.
		s.handler.respondError(w, http.StatusNotFound, "audit trail not configured")
		return
	}
	if !s.withAppScope(w, r, apps.ScopeQuery) {
		return
	}
	var since time.Time
	if raw := r.URL.Query().Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			s.handler.respondError(w, http.StatusBadRequest, "since must be RFC3339")
			return
		}
		since = t
	}
	events := s.audit.ListBySource("app:"+r.PathValue("app"), since)
	s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{"events": events}})
}

// handleAudienceRelations is the §13.4 受众关系 read: one audience's
// standing relations (type/来源/策略位). Operator power — the audience
// registry is global, not namespaced, so this is not an app-token face.
func (s *Server) handleAudienceRelations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.handler.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.relations == nil {
		s.handler.respondError(w, http.StatusNotFound, "not found")
		return
	}
	relations := s.relations.Relations(r.PathValue("id"))
	rows := make([]map[string]any, 0, len(relations))
	// Rendered field-by-field in wire vocabulary: audience.Relation has
	// no json tags (the subscription faces return it verbatim), so this
	// read spells its own lowercase contract instead of leaking Go
	// field names onto the wire.
	for _, rel := range relations {
		rows = append(rows, map[string]any{
			"audience_id": rel.AudienceID,
			"category":    rel.Category,
			"channel":     rel.Channel,
			"type":        rel.Type,
			"source":      rel.Source,
			"policy": map[string]any{
				"allow_unsubscribe": rel.Policy.AllowUnsubscribe,
				"must_deliver":      rel.Policy.MustDeliver,
			},
		})
	}
	s.handler.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{"relations": rows}})
}
