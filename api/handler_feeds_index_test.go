package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/feeds"
)

// TestHandleFeedsIndex lists every feed address the dashboard can copy:
// one public feed per known category plus one private feed per minted
// token, each carrying a recent-entries preview.
func TestHandleFeedsIndex(t *testing.T) {
	base := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	var aliceToken string
	env := feedTestEnv(t, func(store *feeds.Store, surfaces *audience.SurfaceRegistry, relations *audience.Registry) {
		_ = store.Add(feeds.Item{ID: "pub-1", Category: "alerts", Title: "公开告警", PublishedAt: base})
		_ = store.Add(feeds.Item{ID: "pub-2", Category: "alerts", Title: "第二条", PublishedAt: base})
		_ = store.Add(feeds.Item{ID: "priv-1", Category: "alerts", Title: "私人账单", AudienceID: "alice", PublishedAt: base})
		_ = store.Add(feeds.Item{ID: "dep-1", Category: "deploy", Title: "部署完成", PublishedAt: base})
		// alice 订阅 alerts×rss：私密 feed 的读时准入要求活跃关系。
		if err := relations.Subscribe(audience.Relation{AudienceID: "alice", Category: "alerts", Channel: "rss", Source: "test", Type: audience.RelationSubscription}); err != nil {
			t.Fatal(err)
		}
		tok, err := surfaces.RSSToken("alice")
		if err != nil {
			t.Fatal(err)
		}
		aliceToken = tok
	})

	code, resp := env.do(t, http.MethodGet, "/api/v1/feeds", "", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/feeds = %d\n%s", code, resp)
	}
	body := dataOf(t, resp)
	public := body["public"].([]any)
	private := body["private"].([]any)

	// 公开 feed：两个 category，alerts 带 2 条公开条目预览（最新在前）。
	if len(public) != 2 {
		t.Fatalf("expected 2 public feeds, got %d: %v", len(public), body)
	}
	var alerts map[string]any
	for _, f := range public {
		feed := f.(map[string]any)
		if feed["name"] == "alerts" {
			alerts = feed
		}
	}
	if alerts == nil || alerts["url"] != "/feeds/alerts.xml" || alerts["total"] != float64(2) {
		t.Fatalf("unexpected alerts feed: %v", alerts)
	}
	recent := alerts["recent"].([]any)
	if len(recent) != 2 || recent[0].(map[string]any)["title"] != "第二条" {
		t.Fatalf("expected newest first, got %v", recent)
	}
	if public[1].(map[string]any)["name"] != "deploy" || public[1].(map[string]any)["url"] != "/feeds/deploy.xml" {
		t.Fatalf("unexpected deploy feed: %v", public[1])
	}

	// 私密 feed：alice 的 token 地址与私人条目预览（公开条目不进私人 feed）。
	if len(private) != 1 {
		t.Fatalf("expected 1 private feed, got %d: %v", len(private), body)
	}
	priv := private[0].(map[string]any)
	if priv["audience"] != "alice" || priv["url"] != "/feeds/private/"+aliceToken+".xml" {
		t.Fatalf("unexpected private feed: %v", priv)
	}
	if priv["total"] != float64(1) {
		t.Fatalf("unexpected private total: %v", priv)
	}
	privRecent := priv["recent"].([]any)
	if len(privRecent) != 1 || privRecent[0].(map[string]any)["title"] != "私人账单" {
		t.Fatalf("unexpected private preview: %v", privRecent)
	}
}

func TestHandleFeedsIndexEmptyStoreListsNoFeeds(t *testing.T) {
	env := feedTestEnv(t, nil)
	code, resp := env.do(t, http.MethodGet, "/api/v1/feeds", "", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/feeds = %d\n%s", code, resp)
	}
	body := dataOf(t, resp)
	if len(body["public"].([]any)) != 0 || len(body["private"].([]any)) != 0 {
		t.Fatalf("expected empty feed lists, got %v", body)
	}
}

func TestHandleFeedsIndexWithoutStoreIs503(t *testing.T) {
	env := newTestEnv(t)
	code, body := env.do(t, http.MethodGet, "/api/v1/feeds", "", nil)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without the feeds store, got %d\n%s", code, body)
	}
}
