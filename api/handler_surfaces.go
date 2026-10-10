package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/cuihairu/herald/core/audience"
)

// surfaceBindBody is the 联系面绑定 request: one channel target plus the
// optional default categories to subscribe alongside — the same Follow
// semantics the platform entries (bot, 公众号) apply on a follow event.
type surfaceBindBody struct {
	Channel    string   `json:"channel"`
	Target     string   `json:"target"`
	Categories []string `json:"categories"`
}

// HandleAudienceSurfaces is the operator-side 联系面绑定 face: an
// integrator whose users' contact info lives in its own database needs a
// machine entry to lay that binding down — the bot and 公众号 entries
// only cover platform-vouched follows, and the preference centre can
// only toggle categories on an existing surface. The acting source
// records as admin (代绑定); the §12 audit answers 谁在何时通过哪条入口
// 改了什么 as for every other entry. GET answers the audience's bound
// surfaces plus its private RSS feed token (§9: the address's secret
// part, minted on first read, stable for the audience's lifetime) —
// the read arm runs off the registry alone, the write arms stay
// source-gated.
func (h *Handler) HandleAudienceSurfaces(w http.ResponseWriter, r *http.Request) {
	audienceID := r.PathValue("id")
	if r.Method == http.MethodGet {
		if h.sourceSurfaces == nil {
			h.respondError(w, http.StatusNotFound, "not found")
			return
		}
		if !audience.ValidID(audienceID) {
			h.respondError(w, http.StatusUnprocessableEntity, "invalid audience id")
			return
		}
		// The unknown-audience 404 runs BEFORE RSSToken: the token mints
		// on first use, so a read must not create registry entries for
		// arbitrary ids.
		surfaces := h.sourceSurfaces.Surfaces(audienceID)
		if len(surfaces) == 0 {
			h.respondError(w, http.StatusNotFound, "audience not found")
			return
		}
		token, err := h.sourceSurfaces.RSSToken(audienceID)
		if err != nil {
			// ValidID above shares RSSToken's id pattern, so the format
			// refusal is unreachable here; the entropy source is the only
			// failure left, injected through the audience.RandRead seam
			// in TestAudienceSurfacesReadTokenFailure.
			h.respondError(w, http.StatusInternalServerError, "rss token generation failed")
			return
		}
		// Rendered field-by-field: ContactSurface carries no json tags
		// (the source entries return it verbatim in its own wire
		// vocabulary), same contract as the bind arm and relations read.
		rows := make([]map[string]any, 0, len(surfaces))
		for _, surface := range surfaces {
			rows = append(rows, map[string]any{
				"audience_id": surface.AudienceID,
				"channel":     surface.Channel,
				"target":      surface.Target,
				"status":      surface.Status,
			})
		}
		h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{
			"surfaces":  rows,
			"rss_token": token,
		}})
		return
	}
	if h.sourceAdapter == nil {
		h.respondError(w, http.StatusNotFound, "not found")
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req surfaceBindBody
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			h.respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if !audience.ValidID(audienceID) {
			h.respondError(w, http.StatusUnprocessableEntity, "invalid audience id")
			return
		}
		if len(req.Channel) == 0 || len(req.Channel) > 64 {
			h.respondError(w, http.StatusUnprocessableEntity, "invalid channel")
			return
		}
		if strings.TrimSpace(req.Target) == "" {
			h.respondError(w, http.StatusUnprocessableEntity, "invalid target")
			return
		}
		res, err := h.sourceAdapter.Follow(audienceID, req.Channel, req.Target, audience.SourceAdmin, req.Categories)
		if err != nil {
			h.respondError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		// Rendered field-by-field: FollowResult embeds ContactSurface and
		// Relation, which carry no json tags (the source entries return
		// them verbatim in their own wire vocabulary).
		h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{
			"surface": map[string]any{
				"audience_id": res.Surface.AudienceID,
				"channel":     res.Surface.Channel,
				"target":      res.Surface.Target,
				"status":      res.Surface.Status,
			},
			"bind_changed": res.BindChanged,
			"subscribed":   len(res.Subscribed),
		}})
	case http.MethodDelete:
		channel := r.URL.Query().Get("channel")
		if !audience.ValidID(audienceID) {
			h.respondError(w, http.StatusUnprocessableEntity, "invalid audience id")
			return
		}
		if len(channel) == 0 || len(channel) > 64 {
			h.respondError(w, http.StatusUnprocessableEntity, "invalid channel")
			return
		}
		res, err := h.sourceAdapter.Unfollow(audienceID, channel, audience.SourceAdmin)
		if err != nil {
			h.respondError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{
			"surface_invalidated": res.SurfaceInvalidated,
			"terminated":          len(res.Terminated),
		}})
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
