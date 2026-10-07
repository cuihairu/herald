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
// 改了什么 as for every other entry.
func (h *Handler) HandleAudienceSurfaces(w http.ResponseWriter, r *http.Request) {
	if h.sourceAdapter == nil {
		h.respondError(w, http.StatusNotFound, "not found")
		return
	}
	audienceID := r.PathValue("id")
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
