package api

import (
	"net/http"

	"github.com/cuihairu/herald/core/feeds"
)

// feedPreviewCount bounds the recent-entries preview the dashboard feed
// index carries per feed — a scan read, not the whole log.
const feedPreviewCount = 5

// HandleFeedsIndex handles GET /api/v1/feeds: every feed address the
// dashboard can copy — one public feed per known category plus one
// private feed per minted token — each with a recent-entries preview.
// An unconfigured store keeps the face 503 (not configured = closed).
func (h *Handler) HandleFeedsIndex(w http.ResponseWriter, r *http.Request) {
	if h.feedStore == nil {
		h.respondError(w, http.StatusServiceUnavailable, "feeds store is not configured")
		return
	}
	if r.Method != http.MethodGet {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	type feedEntry struct {
		Name     string       `json:"name,omitempty"`
		Audience string       `json:"audience,omitempty"`
		URL      string       `json:"url"`
		Total    int          `json:"total"`
		Recent   []feeds.Item `json:"recent"`
	}

	preview := func(items []feeds.Item) []feeds.Item {
		if len(items) > feedPreviewCount {
			return items[:feedPreviewCount]
		}
		return items
	}

	public := []feedEntry{}
	for _, cat := range h.feedStore.Categories() {
		items := h.feedStore.Public(cat)
		public = append(public, feedEntry{
			Name:   cat,
			URL:    "/feeds/" + cat + ".xml",
			Total:  len(items),
			Recent: preview(items),
		})
	}

	private := []feedEntry{}
	if h.feedSurfaces != nil {
		for audienceID, token := range h.feedSurfaces.RSSTokens() {
			items := h.feedStore.Private(audienceID, h.feedAllows(audienceID))
			private = append(private, feedEntry{
				Audience: audienceID,
				URL:      "/feeds/private/" + token + ".xml",
				Total:    len(items),
				Recent:   preview(items),
			})
		}
	}

	h.respondJSON(w, &Response{
		Code:    0,
		Message: "ok",
		Data:    map[string]interface{}{"public": public, "private": private},
	})
}
