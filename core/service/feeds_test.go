package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/feeds"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/template"
)

// newFeedService builds a service whose router maps categories to the
// rss channel (plus any extras), with a feed store wired for the pull
// half (§9).
func newFeedService(t *testing.T, routes map[string][]string, withFeeds bool) (*NotificationService, *feeds.Store, *mockQueue) {
	t.Helper()
	svc, _, _, queue := newDigestService(t, &route.Config{Routes: routes})
	var store *feeds.Store
	if withFeeds {
		store = feeds.NewStore(0)
		svc.SetFeeds(store)
	}
	return svc, store, queue
}

// TestRSSChannelProjectsInsteadOfEnqueueing: a routed rss channel never
// becomes a provider task — it files one feed item, whose title and body
// come from the direct content.
func TestRSSChannelProjectsInsteadOfEnqueueing(t *testing.T) {
	svc, store, queue := newFeedService(t, map[string][]string{"alerts": {"rss"}}, true)

	res, err := svc.Process(context.Background(), &core.Notification{
		Type:         "alerts",
		Channels:     []string{"rss"},
		Content:      &core.DirectContent{Title: "CPU 高", Body: "node-17 离线"},
		RelationType: "subscription",
		Source:       "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("projection failed: %+v", res.Failed)
	}
	if len(queue.tasks) != 0 {
		t.Fatalf("rss channel produced %d provider tasks, want 0", len(queue.tasks))
	}
	got := store.Public("alerts")
	if len(got) != 1 || got[0].Title != "CPU 高" || got[0].Body != "node-17 离线" {
		t.Fatalf("public feed = %+v, want one projected item", got)
	}
}

// TestMixedFanOutProjectsAndDelivers: rss+email fan-out both files the
// item and enqueues the push half — the pull channel is an additional
// surface, not a replacement.
func TestMixedFanOutProjectsAndDelivers(t *testing.T) {
	svc, store, queue := newFeedService(t, map[string][]string{"notices": {"rss", "email"}}, true)

	res, err := svc.Process(context.Background(), &core.Notification{
		Type:         "notices",
		Channels:     []string{"rss", "email"},
		Content:      &core.DirectContent{Title: "周报", Body: "b"},
		RelationType: "subscription",
		Source:       "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("mixed fan-out failed: %+v", res.Failed)
	}
	if len(queue.tasks) != 1 {
		t.Fatalf("email half produced %d tasks, want 1", len(queue.tasks))
	}
	if got := store.Public("notices"); len(got) != 1 {
		t.Fatalf("feed has %d items, want 1", len(got))
	}
}

// TestProjectionScopesByAudience: a broadcast lands public; an
// audience-scoped notification lands only in that audience's private
// feed — 公开内容 and personal items never mix.
func TestProjectionScopesByAudience(t *testing.T) {
	svc, store, _ := newFeedService(t, map[string][]string{"notices": {"rss"}}, true)

	mk := func(id, aud, title string) *core.Notification {
		return &core.Notification{
			ID: id, Type: "notices", AudienceID: aud, Channels: []string{"rss"},
			Content:      &core.DirectContent{Title: title, Body: "b"},
			RelationType: "subscription", Source: "bot",
		}
	}
	if _, err := svc.Process(context.Background(), mk("pub", "", "广播")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Process(context.Background(), mk("mine", "alice", "私事")); err != nil {
		t.Fatal(err)
	}

	if got := store.Public("notices"); len(got) != 1 || got[0].Title != "广播" {
		t.Fatalf("public feed = %+v, want only the broadcast", got)
	}
	if got := store.Private("alice", func(string) bool { return true }); len(got) != 1 || got[0].Title != "私事" {
		t.Fatalf("alice's private feed = %+v, want only her item", got)
	}
	if got := store.Private("bob", func(string) bool { return true }); len(got) != 0 {
		t.Fatalf("bob's private feed = %+v, want empty", got)
	}
}

// TestDigestSummaryRidesRSSChannel: a folded window's summary delivered
// over the rss channel lands as one personal feed item, not a provider
// task (§9: the pull half is a channel like any other at the summary
// gate).
func TestDigestSummaryRidesRSSChannel(t *testing.T) {
	svc, agg, prefs, queue := newDigestService(t, &route.Config{})
	store := feeds.NewStore(0)
	svc.SetFeeds(store)
	setFoldPref(t, prefs, "alice", "bills", "rss", audience.FreqDaily)

	opened := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	agg.SetClock(func() time.Time { return opened })
	for i, title := range []string{"October bill", "Tax bill"} {
		_, err := svc.Process(context.Background(), &core.Notification{
			ID:           "n" + string(rune('0'+i)),
			Type:         "bills",
			AudienceID:   "alice",
			Channels:     []string{"rss"},
			Content:      &core.DirectContent{Title: title, Body: "b"},
			CreatedAt:    opened.Add(time.Duration(i) * time.Minute),
			RelationType: "subscription",
			Source:       "bot",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	flipAt := opened.Add(25 * time.Hour)
	agg.SetClock(func() time.Time { return flipAt })
	batch := agg.Due()
	if len(batch) != 1 {
		t.Fatalf("due batches = %d, want 1 summary window", len(batch))
	}
	if err := svc.FlushDigestBatch(batch[0]); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if len(queue.tasks) != 0 {
		t.Fatalf("digest summary over rss produced %d tasks, want 0", len(queue.tasks))
	}
	got := store.Private("alice", func(string) bool { return true })
	if len(got) != 1 {
		t.Fatalf("alice's feed has %d items, want 1 summary", len(got))
	}
	if !strings.Contains(got[0].Title, "2") {
		t.Errorf("summary title %q does not carry the fold count", got[0].Title)
	}
}

// TestNilFeedsKeepsUnknownProviderFailure: without a feed store the rss
// channel behaves as any unknown provider — the channel fails visibly
// instead of silently delivering nowhere.
func TestNilFeedsKeepsUnknownProviderFailure(t *testing.T) {
	svc, _, queue := newFeedService(t, map[string][]string{"alerts": {"rss"}}, false)

	_, err := svc.Process(context.Background(), &core.Notification{
		Type:         "alerts",
		Channels:     []string{"rss"},
		Content:      &core.DirectContent{Title: "t", Body: "b"},
		RelationType: "subscription",
		Source:       "bot",
	})
	// A lone channel that fails fails the whole Process — the visible
	// "provider not found" is the contract the pull half removes.
	if err == nil || !strings.Contains(err.Error(), "provider not found: rss") {
		t.Fatalf("rss without feeds = %v, want provider-not-found error", err)
	}
	if len(queue.tasks) != 0 {
		t.Fatalf("unknown provider enqueued %d tasks, want 0", len(queue.tasks))
	}
}

// TestTemplateRenderedTitleRidesProjection: a template-rendered title
// (not the direct one) is what the feed item carries — the projection
// reads the same RenderedData the push half would send.
func TestTemplateRenderedTitleRidesProjection(t *testing.T) {
	templates := template.NewManager()
	if err := templates.Register(&template.Template{
		ID:    "rss-tpl",
		Name:  "RSS Template",
		Title: "渲染标题 {{.count}}",
		Fields: []template.Field{
			{Label: "body", Value: "{{.count}} 条待办"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	router := route.NewRouter(&route.Config{Routes: map[string][]string{"alerts": {"rss"}}})
	queue := newMockQueue()
	svc := NewNotificationService(templates, router, newMockProviderRuntime(), dedup.NewDedup(&dedup.Config{}), queue)
	store := feeds.NewStore(0)
	svc.SetFeeds(store)

	res, err := svc.Process(context.Background(), &core.Notification{
		Type:         "alerts",
		Channels:     []string{"rss"},
		TemplateRef:  "rss-tpl",
		Params:       map[string]any{"count": 3},
		RelationType: "subscription",
		Source:       "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("projection failed: %+v", res.Failed)
	}
	got := store.Public("alerts")
	if len(got) != 1 || got[0].Title != "渲染标题 3" {
		t.Fatalf("feed item = %+v, want the rendered title", got)
	}
}

// TestProjectionRejectsMissingTitle: a notification with neither direct
// content nor a template has no title to project — the store refuses
// the item and the failure lands in result.Failed like any other channel
// error, while the push half of the fan-out still goes out.
func TestProjectionRejectsMissingTitle(t *testing.T) {
	svc, store, queue := newFeedService(t, map[string][]string{"alerts": {"rss", "email"}}, true)

	res, err := svc.Process(context.Background(), &core.Notification{
		Type:         "alerts",
		Channels:     []string{"rss", "email"},
		RelationType: "subscription",
		Source:       "bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Channel != "rss" {
		t.Fatalf("failures = %+v, want one rss projection error", res.Failed)
	}
	if len(queue.tasks) != 1 {
		t.Fatalf("email half produced %d tasks, want 1", len(queue.tasks))
	}
	if got := store.Public("alerts"); len(got) != 0 {
		t.Fatalf("refused item still landed: %+v", got)
	}
}
