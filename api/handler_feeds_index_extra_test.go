package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/feeds"
)

// TestHandleFeedsIndexRejectsNonGET keeps the face read-only: any write
// verb is a 405, never a silent pass-through.
func TestHandleFeedsIndexRejectsNonGET(t *testing.T) {
	env := feedTestEnv(t, nil)

	code, _ := env.do(t, http.MethodPost, "/api/v1/feeds", "", nil)
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", code)
	}
}

// TestHandleFeedsIndexTruncatesPreview bounds the recent-entries preview:
// a category with more entries than feedPreviewCount shows only the
// newest feedPreviewCount of them, with the total still reporting all.
func TestHandleFeedsIndexTruncatesPreview(t *testing.T) {
	base := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	env := feedTestEnv(t, func(store *feeds.Store, _ *audience.SurfaceRegistry, _ *audience.Registry) {
		for i := 0; i < feedPreviewCount+3; i++ {
			_ = store.Add(feeds.Item{
				ID:          "pub-" + string(rune('a'+i)),
				Category:    "alerts",
				Title:       "条目",
				PublishedAt: base.Add(time.Duration(i) * time.Minute),
			})
		}
	})

	code, resp := env.do(t, http.MethodGet, "/api/v1/feeds", "", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/feeds = %d\n%s", code, resp)
	}
	body := dataOf(t, resp)
	public := body["public"].([]any)
	var alerts map[string]any
	for _, f := range public {
		feed := f.(map[string]any)
		if feed["name"] == "alerts" {
			alerts = feed
		}
	}
	if alerts == nil {
		t.Fatalf("expected an alerts feed: %v", body)
	}
	if alerts["total"] != float64(feedPreviewCount+3) {
		t.Fatalf("expected total %d, got %v", feedPreviewCount+3, alerts["total"])
	}
	recent := alerts["recent"].([]any)
	if len(recent) != feedPreviewCount {
		t.Fatalf("expected the preview capped at %d entries, got %d", feedPreviewCount, len(recent))
	}
}
