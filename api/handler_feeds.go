package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/feeds"
)

// feedCategoryPattern bounds the category names that have a feed URL.
// Store categories are free-form strings, but URLs stay slug-shaped:
// a category outside this charset still receives projections, it just
// has no public feed address.
var feedCategoryPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// setFeeds wires the §9 pull half (server-side): the store to render,
// the channel metadata for every feed, the token resolver for private
// feeds and the live relation table private pulls consult. Called once
// from NewServer when the store is configured.
func (h *Handler) SetFeeds(store *feeds.Store, meta feeds.ChannelMeta, surfaces *audience.SurfaceRegistry, relations *audience.Registry) {
	h.feedStore = store
	h.feedMeta = meta
	h.feedSurfaces = surfaces
	h.feedRelations = relations
}

// HandleFeed renders one public category feed: GET /feeds/{name} with
// name = "<category>.xml". It carries only 公开内容 — items projected
// without an audience reference. An unknown category renders as an empty
// channel: the honest answer of a pull source nobody has written to.
func (h *Handler) HandleFeed(w http.ResponseWriter, r *http.Request) {
	category, ok := feedSlug(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.serveFeed(w, r, h.feedStore.Public(category))
}

// HandlePrivateFeed renders one audience's personal feed: GET
// /feeds/private/{token}.xml (§9 私密 feed). The token is the
// credential — it resolves to exactly one audience, and an unknown one
// answers not-found instead of guessing. Visibility is decided at read
// time: a personal item surfaces only while the audience's live
// subscription relation on that category admits the rss channel, so
// 取关即从下一次拉取起消失 (§9).
func (h *Handler) HandlePrivateFeed(w http.ResponseWriter, r *http.Request) {
	token, ok := feedSlug(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	audienceID, found := h.feedSurfaces.RSSAudience(token)
	if !found {
		http.NotFound(w, r)
		return
	}
	h.serveFeed(w, r, h.feedStore.Private(audienceID, h.feedAllows(audienceID)))
}

// feedAllows is the read-time admission callback handed to the store:
// one category at a time, checked against the audience's live
// subscription relation on audience×category×rss and the §5 channel
// matrix (the same gate the push half runs at delivery, re-run at pull).
func (h *Handler) feedAllows(audienceID string) func(category string) bool {
	return func(category string) bool {
		if h.feedRelations == nil {
			return false
		}
		rel, ok := h.feedRelations.Lookup(audienceID, category, "rss")
		return ok && rel.AllowsOn(audience.ChannelRSS)
	}
}

// serveFeed renders the items as RSS 2.0. The content type names the
// charset because titles and bodies carry user text end to end.
func (h *Handler) serveFeed(w http.ResponseWriter, r *http.Request, items []feeds.Item) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out, err := feeds.RenderRSS(h.feedMeta, items, time.Now())
	if err != nil {
		http.Error(w, "feed unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write([]byte(out))
}

// feedSlug strips the ".xml" suffix a reader expects and validates the
// remainder as a feed address segment.
func feedSlug(name string) (string, bool) {
	slug, found := strings.CutSuffix(name, ".xml")
	if !found || !feedCategoryPattern.MatchString(slug) {
		return "", false
	}
	return slug, true
}
