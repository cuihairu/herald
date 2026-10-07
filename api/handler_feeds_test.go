package api

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/feeds"
)

// feedTestEnv builds a server with the §9 pull half wired: a store, the
// channel metadata, a surface registry (private token resolution) and a
// relation registry (read-time admission).
func feedTestEnv(t *testing.T, mutate func(store *feeds.Store, surfaces *audience.SurfaceRegistry, relations *audience.Registry)) *testEnv {
	t.Helper()
	store := feeds.NewStore(0)
	surfaces := audience.NewSurfaceRegistry()
	relations := audience.NewRegistry()
	env := newTestEnv(t, func(c *Config) {
		c.Feeds = store
		c.FeedMeta = feeds.ChannelMeta{Title: "Herald 测试", Link: "https://herald.example", Description: "测试 feed"}
		c.FeedSurfaces = surfaces
		c.FeedRelations = relations
	})
	if mutate != nil {
		mutate(store, surfaces, relations)
	}
	return env
}

func getBody(t *testing.T, url string) (int, string, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

func TestPublicFeedCarriesAnonymousItemsOnly(t *testing.T) {
	base := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	env := feedTestEnv(t, func(store *feeds.Store, _ *audience.SurfaceRegistry, _ *audience.Registry) {
		_ = store.Add(feeds.Item{ID: "pub-1", Category: "alerts", Title: "公开告警", Body: "全员注意", PublishedAt: base})
		_ = store.Add(feeds.Item{ID: "priv-1", Category: "alerts", Title: "私人账单", AudienceID: "alice", PublishedAt: base})
	})

	code, ctype, body := getBody(t, env.ts.URL+"/feeds/alerts.xml")
	if code != http.StatusOK {
		t.Fatalf("GET public feed = %d, want 200\n%s", code, body)
	}
	if !strings.HasPrefix(ctype, "application/rss+xml") {
		t.Errorf("content type = %q, want application/rss+xml", ctype)
	}
	if !strings.Contains(body, "<title>公开告警</title>") {
		t.Errorf("public feed misses the anonymous item:\n%s", body)
	}
	if strings.Contains(body, "私人账单") {
		t.Errorf("public feed leaked a personal item:\n%s", body)
	}
	var doc struct {
		Channel struct {
			Items []struct{ GUID string } `xml:"item>guid"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("feed does not parse: %v\n%s", err, body)
	}
	if len(doc.Channel.Items) != 1 {
		t.Fatalf("parsed %d items, want 1", len(doc.Channel.Items))
	}
}

func TestPublicFeedUnknownCategoryIsEmptyChannel(t *testing.T) {
	env := feedTestEnv(t, nil)
	code, _, body := getBody(t, env.ts.URL+"/feeds/nosuch.xml")
	if code != http.StatusOK {
		t.Fatalf("GET unknown category = %d, want 200 (empty channel)", code)
	}
	if strings.Contains(body, "<item>") {
		t.Errorf("unknown category rendered items:\n%s", body)
	}
}

func TestFeedRoutesRejectBadSlugsAndMethods(t *testing.T) {
	env := feedTestEnv(t, nil)
	for _, path := range []string{"/feeds/alerts", "/feeds/.xml", "/feeds/" + strings.Repeat("x", 70) + ".xml"} {
		code, _, _ := getBody(t, env.ts.URL+path)
		if code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
	resp, err := http.Post(env.ts.URL+"/feeds/alerts.xml", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST feed = %d, want 405", resp.StatusCode)
	}
}

func TestPrivateFeedResolvesTokenAndAppliesReadTimeAdmission(t *testing.T) {
	base := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	var token string
	env := feedTestEnv(t, func(store *feeds.Store, surfaces *audience.SurfaceRegistry, relations *audience.Registry) {
		tok, err := surfaces.RSSToken("alice")
		if err != nil {
			t.Fatal(err)
		}
		token = tok
		// alice subscribes to bills on rss only.
		if err := relations.Subscribe(audience.Relation{AudienceID: "alice", Category: "bills", Channel: "rss", Source: "test", Type: audience.RelationSubscription}); err != nil {
			t.Fatal(err)
		}
		_ = store.Add(feeds.Item{ID: "b-1", Category: "bills", Title: "十月账单", AudienceID: "alice", PublishedAt: base})
		_ = store.Add(feeds.Item{ID: "a-1", Category: "alerts", Title: "未订阅的告警", AudienceID: "alice", PublishedAt: base})
		_ = store.Add(feeds.Item{ID: "other-1", Category: "bills", Title: "别人的账单", AudienceID: "bob", PublishedAt: base})
	})

	code, _, body := getBody(t, env.ts.URL+"/feeds/private/"+token+".xml")
	if code != http.StatusOK {
		t.Fatalf("GET private feed = %d\n%s", code, body)
	}
	if !strings.Contains(body, "<title>十月账单</title>") {
		t.Errorf("private feed misses the subscribed item:\n%s", body)
	}
	if strings.Contains(body, "未订阅的告警") || strings.Contains(body, "别人的账单") {
		t.Errorf("private feed leaked non-admitted items:\n%s", body)
	}

	// 取关即从下一次拉取起消失 (§9): terminating the relation hides the
	// item on the next pull — no projection-side bookkeeping involved.
	if _, err := env.server.handler.feedRelations.Terminate("alice", "bills", "rss"); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	_, _, body = getBody(t, env.ts.URL+"/feeds/private/"+token+".xml")
	if strings.Contains(body, "十月账单") {
		t.Errorf("terminated subscription still rendered:\n%s", body)
	}
}

func TestPrivateFeedUnknownTokenIsNotFound(t *testing.T) {
	env := feedTestEnv(t, nil)
	code, _, _ := getBody(t, env.ts.URL+"/feeds/private/deadbeefdeadbeefdeadbeefdeadbeef.xml")
	if code != http.StatusNotFound {
		t.Fatalf("GET private feed with unknown token = %d, want 404", code)
	}
}

// TestPrivateFeedBadSlugIsNotFound: the private route takes the same
// "<segment>.xml" shape as the public one — anything else is not-found.
func TestPrivateFeedBadSlugIsNotFound(t *testing.T) {
	env := feedTestEnv(t, nil)
	code, _, _ := getBody(t, env.ts.URL+"/feeds/private/tok")
	if code != http.StatusNotFound {
		t.Fatalf("GET private feed without .xml = %d, want 404", code)
	}
}

// TestPrivateFeedWithoutRelationsAdmitsNothing: a deployment wired with
// tokens but no relation registry has no entitlement source — every
// category filters out and the feed renders empty rather than leaking.
func TestPrivateFeedWithoutRelationsAdmitsNothing(t *testing.T) {
	store := feeds.NewStore(0)
	surfaces := audience.NewSurfaceRegistry()
	token, err := surfaces.RSSToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	_ = store.Add(feeds.Item{ID: "i-1", Category: "bills", Title: "账单", AudienceID: "alice", PublishedAt: base})

	env := newTestEnv(t, func(c *Config) {
		c.Feeds = store
		c.FeedMeta = feeds.ChannelMeta{Title: "Herald"}
		c.FeedSurfaces = surfaces
		// FeedRelations deliberately nil.
	})
	code, _, body := getBody(t, env.ts.URL+"/feeds/private/"+token+".xml")
	if code != http.StatusOK {
		t.Fatalf("GET private feed = %d\n%s", code, body)
	}
	if strings.Contains(body, "<item>") {
		t.Errorf("nil relation registry leaked items:\n%s", body)
	}
}

// TestFeedRenderFailureIs500: a feed whose channel metadata cannot
// render (empty title — a configuration error the renderer refuses)
// reports 500 instead of emitting a broken document.
func TestFeedRenderFailureIs500(t *testing.T) {
	env := newTestEnv(t, func(c *Config) {
		c.Feeds = feeds.NewStore(0)
		c.FeedMeta = feeds.ChannelMeta{} // no title — renderer refuses
	})
	code, _, _ := getBody(t, env.ts.URL+"/feeds/alerts.xml")
	if code != http.StatusInternalServerError {
		t.Fatalf("GET feed with empty title = %d, want 500", code)
	}
}
