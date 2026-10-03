package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cuihairu/herald/core/rules"
)

// HandleRules handles rule list requests (GET /api/v1/rules).
func (h *Handler) HandleRules(w http.ResponseWriter, r *http.Request) {
	if h.rulesEngine == nil {
		h.respondError(w, http.StatusServiceUnavailable, "rules engine is not configured")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.Method == http.MethodPost {
		h.createRule(w, r)
		return
	}

	// List serves the live table (the evaluation truth), so it cannot
	// fail; the error result stays in the signature for API symmetry.
	list, _ := h.rulesEngine.List(r.Context())
	views := make([]ruleView, 0, len(list))
	for i := range list {
		views = append(views, ruleView{Rule: list[i], ShadowHits: h.shadowHits(list[i].ID)})
	}
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]interface{}{"rules": views, "count": len(views)},
	})
}

// ruleView is the read projection of a stored rule: the rule document
// itself plus the live shadow statistic the console shows next to it. The
// counter is process-local observation state (it resets on restart and is
// not part of the rule), so it is served beside the rule rather than
// stored with it. The detail read additionally carries the full shadow
// stats (windows + samples); the list stays lean with the counter only.
type ruleView struct {
	rules.Rule
	ShadowHits  uint64             `json:"shadow_hits"`
	ShadowStats *rules.ShadowStats `json:"shadow_stats,omitempty"`
}

// shadowHits reads the live shadow-hit counter. A handler built without a
// runtime manager (the rules engine is optional wiring) reports zero
// rather than panicking: the rule table itself is still the truth.
func (h *Handler) shadowHits(ruleID string) uint64 {
	if h.runtime == nil {
		return 0
	}
	return h.runtime.ShadowRuleCount(ruleID)
}

// shadowStats reads the full shadow observation state for the rule detail
// view (trailing windows + latest samples). Nil runtime omits the field,
// mirroring shadowHits' zero fallback.
func (h *Handler) shadowStats(ruleID string) *rules.ShadowStats {
	if h.runtime == nil {
		return nil
	}
	stats := h.runtime.ShadowStats(ruleID)
	return &stats
}

// HandleRuleByID handles rule get/update/delete requests
// (GET/PUT/DELETE /api/v1/rules/{id}).
func (h *Handler) HandleRuleByID(w http.ResponseWriter, r *http.Request) {
	if h.rulesEngine == nil {
		h.respondError(w, http.StatusServiceUnavailable, "rules engine is not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		h.respondError(w, http.StatusBadRequest, "rule id is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getRule(w, r, id)
	case http.MethodPut:
		h.updateRule(w, r, id)
	case http.MethodDelete:
		h.deleteRule(w, r, id)
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// createRule validates and stores a new rule; expressions are compiled
// here, so invalid ones are rejected before ever reaching evaluation.
func (h *Handler) createRule(w http.ResponseWriter, r *http.Request) {
	var rule rules.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if _, err := h.rulesEngine.Get(r.Context(), rule.ID); err == nil {
		h.respondError(w, http.StatusConflict, "rule already exists: "+rule.ID)
		return
	} else if !errors.Is(err, rules.ErrNotFound) {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.rulesEngine.Put(r.Context(), &rule); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rule})
}

func (h *Handler) getRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, err := h.rulesEngine.Get(r.Context(), id)
	if errors.Is(err, rules.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "rule not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data: ruleView{
			Rule:        rule,
			ShadowHits:  h.shadowHits(rule.ID),
			ShadowStats: h.shadowStats(rule.ID),
		},
	})
}

// updateRule replaces the rule at id (the URL wins over the body id).
func (h *Handler) updateRule(w http.ResponseWriter, r *http.Request, id string) {
	var rule rules.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	rule.ID = id
	if err := h.rulesEngine.Put(r.Context(), &rule); err != nil {
		h.respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rule})
}

func (h *Handler) deleteRule(w http.ResponseWriter, r *http.Request, id string) {
	err := h.rulesEngine.Delete(r.Context(), id)
	if errors.Is(err, rules.ErrNotFound) {
		h.respondError(w, http.StatusNotFound, "rule not found: "+id)
		return
	}
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok"})
}
