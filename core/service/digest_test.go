package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/audience"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/digest"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/template"
)

// newDigestService builds a service with a digest aggregator whose clock
// is injected, plus a preference registry the test seeds directly.
func newDigestService(t *testing.T, routes *route.Config) (*NotificationService, *digest.Aggregator, *audience.PreferenceRegistry, *mockQueue) {
	t.Helper()
	loc := time.UTC
	daily, err := digest.ParseDaily("09:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	weekly, err := digest.ParseWeekly("Mon 09:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	agg := digest.NewAggregator(daily, weekly)
	prefs := audience.NewPreferenceRegistry()

	templates := template.NewManager()
	router := route.NewRouter(routes)
	runtime := newMockProviderRuntime()
	provider := &mockProvider{
		providerType: "email",
		capability: core.ProviderCapability{
			PayloadKinds:   []core.PayloadKind{core.PayloadContent},
			ContentFormats: []string{"plain"},
		},
	}
	runtime.RegisterProvider("email", provider, true)
	queue := newMockQueue()

	svc := NewNotificationService(templates, router, runtime, dedup.NewDedup(&dedup.Config{}), queue)
	svc.SetDigest(agg, prefs)
	return svc, agg, prefs, queue
}

func setFoldPref(t *testing.T, prefs *audience.PreferenceRegistry, audienceID, category, channel string, freq audience.Frequency) {
	t.Helper()
	if err := prefs.Set(audience.Preference{AudienceID: audienceID, Category: category, Channel: channel, Frequency: freq}); err != nil {
		t.Fatal(err)
	}
}

// TestDigestCollectsAndSummarizes: a folding preference collects events
// (no direct tasks); once the window flips, one summary rides the normal
// pipeline in the「今日 N 条」shape with the events' titles and channels.
func TestDigestCollectsAndSummarizes(t *testing.T) {
	svc, agg, prefs, queue := newDigestService(t, &route.Config{})
	setFoldPref(t, prefs, "alice", "bills", "email", audience.FreqDaily)

	opened := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	agg.SetClock(func() time.Time { return opened })

	for i, title := range []string{"October bill", "Tax bill"} {
		n := &core.Notification{
			ID:           "n" + string(rune('0'+i)),
			Type:         "bills",
			AudienceID:   "alice",
			Channels:     []string{"email"},
			Content:      &core.DirectContent{Title: title, Body: "b"},
			CreatedAt:    opened.Add(time.Duration(i) * time.Minute),
			RelationType: "subscription",
			Source:       "bot",
		}
		res, err := svc.Process(context.Background(), n)
		if err != nil {
			t.Fatalf("Process(%q): %v", title, err)
		}
		if len(res.TaskIDs) != 0 {
			t.Fatalf("Process(%q) enqueued %d tasks, want 0 (folded)", title, len(res.TaskIDs))
		}
	}
	if len(queue.tasks) != 0 {
		t.Fatalf("queue holds %d tasks before the flip, want 0", len(queue.tasks))
	}

	// Flip the window (past Oct 8 09:01, the window's next flip) and flush.
	agg.SetClock(func() time.Time { return time.Date(2026, time.October, 8, 9, 2, 0, 0, time.UTC) })
	made, err := svc.FlushDigest()
	if err != nil {
		t.Fatalf("FlushDigest: %v", err)
	}
	if made != 1 || len(queue.tasks) != 1 {
		t.Fatalf("FlushDigest made=%d queue=%d, want 1 summary task", made, len(queue.tasks))
	}
	task := queue.tasks[0]
	if task.Payload.Kind != core.PayloadContent || task.Payload.Content == nil {
		t.Fatalf("payload = %+v, want content payload", task.Payload)
	}
	if got := task.Payload.Content.Title; got != "今日 2 条bills" {
		t.Errorf("summary title = %q, want %q", got, "今日 2 条bills")
	}
	for _, want := range []string{"October bill", "Tax bill"} {
		if !strings.Contains(task.Payload.Content.Body, want) {
			t.Errorf("summary body %q misses %q", task.Payload.Content.Body, want)
		}
	}

	// The flipped window restarted: a second flush has nothing due.
	made, err = svc.FlushDigest()
	if err != nil || made != 0 {
		t.Errorf("second FlushDigest = (%d,%v), want (0,nil)", made, err)
	}
}

// TestDigestRealtimeDeliversDirect: realtime-preferring channels bypass
// the window entirely, and an event without an audience reference is
// never collected.
func TestDigestRealtimeDeliversDirect(t *testing.T) {
	svc, agg, prefs, queue := newDigestService(t, &route.Config{})
	setFoldPref(t, prefs, "alice", "bills", "email", audience.FreqRealtime)

	n := &core.Notification{
		Type:       "bills",
		AudienceID: "alice",
		Channels:   []string{"email"},
		Content:    &core.DirectContent{Title: "live", Body: "b"},
		CreatedAt:  time.Now(),
	}
	res, err := svc.Process(context.Background(), n)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TaskIDs) != 1 {
		t.Fatalf("realtime event tasks = %d, want 1 (direct)", len(res.TaskIDs))
	}

	// Anonymous events have no preference to fold by: direct too.
	anon := &core.Notification{
		Type:      "marketing",
		Channels:  []string{"email"},
		Content:   &core.DirectContent{Title: "anon", Body: "b"},
		CreatedAt: time.Now(),
	}
	if res, err = svc.Process(context.Background(), anon); err != nil {
		t.Fatal(err)
	}
	if len(res.TaskIDs) != 1 {
		t.Fatalf("anonymous event tasks = %d, want 1", len(res.TaskIDs))
	}

	made, err := svc.FlushDigest()
	if err != nil || made != 0 || len(queue.tasks) != 2 {
		t.Fatalf("FlushDigest = (%d,%v) queue=%d, want (0,nil) queue=2", made, err, len(queue.tasks))
	}
	if agg == nil {
		t.Fatal("aggregator nil")
	}
}

// TestDigestDefaultTableDecides: with no registry entries the category
// default table decides — marketing folds weekly, unknown categories
// stay realtime.
func TestDigestDefaultTableDecides(t *testing.T) {
	svc, agg, prefs, _ := newDigestService(t, &route.Config{})
	_ = prefs

	marketing := &core.Notification{
		Type:       "marketing",
		AudienceID: "alice",
		Channels:   []string{"email"},
		Content:    &core.DirectContent{Title: "sale", Body: "b"},
		CreatedAt:  time.Now(),
	}
	res, err := svc.Process(context.Background(), marketing)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TaskIDs) != 0 {
		t.Fatalf("marketing tasks = %d, want 0 (default weekly fold)", len(res.TaskIDs))
	}

	custom := &core.Notification{
		Type:       "custom.thing",
		AudienceID: "alice",
		Channels:   []string{"email"},
		Content:    &core.DirectContent{Title: "unknown", Body: "b"},
		CreatedAt:  time.Now(),
	}
	if res, err = svc.Process(context.Background(), custom); err != nil {
		t.Fatal(err)
	}
	if len(res.TaskIDs) != 1 {
		t.Fatalf("unknown category tasks = %d, want 1 (default realtime)", len(res.TaskIDs))
	}

	agg.SetClock(func() time.Time { return time.Date(2026, time.October, 12, 9, 2, 0, 0, time.UTC) })
	made, err := svc.FlushDigest()
	if err != nil || made != 1 {
		t.Fatalf("FlushDigest = (%d,%v), want the marketing window", made, err)
	}
}

// TestDigestTemplateOverride: a registered digest:<category> template
// replaces the built-in rendering with count/items/audience params; an
// unrenderable template fails the flush visibly instead of losing the
// window silently.
func TestDigestTemplateOverride(t *testing.T) {
	svc, agg, prefs, queue := newDigestService(t, &route.Config{})
	setFoldPref(t, prefs, "alice", "bills", "email", audience.FreqDaily)
	if err := svc.templates.Register(&template.Template{
		ID:    "digest:bills",
		Name:  "bills digest",
		Title: "摘要：{{.count}} 条（{{.audience}}）",
		Level: "warning",
	}); err != nil {
		t.Fatal(err)
	}

	opened := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	agg.SetClock(func() time.Time { return opened })
	n := &core.Notification{
		Type:       "bills",
		AudienceID: "alice",
		Channels:   []string{"email"},
		Content:    &core.DirectContent{Title: "October bill", Body: "b"},
		CreatedAt:  opened,
	}
	if _, err := svc.Process(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	agg.SetClock(func() time.Time { return time.Date(2026, time.October, 8, 9, 2, 0, 0, time.UTC) })
	made, err := svc.FlushDigest()
	if err != nil || made != 1 {
		t.Fatalf("FlushDigest = (%d,%v), want 1", made, err)
	}
	got := queue.tasks[0].Payload.Content
	if got == nil || got.Title != "摘要：1 条（alice）" {
		t.Fatalf("template summary title = %+v, want the rendered digest template", got)
	}
	// The template's level rides the rendered data into the task.
	if task := queue.tasks[0]; task.Level != "warning" {
		t.Errorf("summary task level = %q, want the template's %q", task.Level, "warning")
	}
}

// TestDigestFlushReportsFailures: a template that cannot render and a
// window whose category has no route both surface as flush errors, so
// the flip loop logs them instead of dropping the window silently.
func TestDigestFlushReportsFailures(t *testing.T) {
	svc, agg, prefs, _ := newDigestService(t, &route.Config{})
	setFoldPref(t, prefs, "alice", "bills", "email", audience.FreqDaily)

	// A template whose title cannot render (bad function reference).
	if err := svc.templates.Register(&template.Template{
		ID:    "digest:bills",
		Name:  "bills digest broken",
		Title: "{{count | definitely_not_a_function}}",
	}); err != nil {
		t.Fatal(err)
	}
	opened := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	agg.SetClock(func() time.Time { return opened })
	if _, err := svc.Process(context.Background(), &core.Notification{
		Type: "bills", AudienceID: "alice", Channels: []string{"email"},
		Content: &core.DirectContent{Title: "e", Body: "b"}, CreatedAt: opened,
	}); err != nil {
		t.Fatal(err)
	}
	agg.SetClock(func() time.Time { return time.Date(2026, time.October, 8, 9, 2, 0, 0, time.UTC) })
	if made, err := svc.FlushDigest(); err == nil || made != 0 {
		t.Fatalf("FlushDigest = (%d,%v), want (0, render error)", made, err)
	}

	// Events naming no channels fall back to static routing; an empty
	// route table fails the flush.
	agg.SetClock(func() time.Time { return opened })
	if _, err := svc.Process(context.Background(), &core.Notification{
		Type: "bills", AudienceID: "alice",
		Content: &core.DirectContent{Title: "e2", Body: "b"}, CreatedAt: opened,
	}); err == nil {
		t.Fatal("Process without channels and without routes should fail")
	}
	// Feed the same shape straight into the window instead (routing can
	// succeed at event time and fail at flush time after config change);
	// bob's "notices" window is fresh, and with no digest:notices template
	// registered the built-in shape renders before routing fails.
	agg.Add(&core.Notification{
		Type: "notices", AudienceID: "bob",
		Content: &core.DirectContent{Title: "e2", Body: "b"}, CreatedAt: opened,
	}, digest.Daily)
	agg.SetClock(func() time.Time { return time.Date(2026, time.October, 8, 9, 2, 0, 0, time.UTC) })
	if made, err := svc.FlushDigest(); err == nil || made != 0 {
		t.Fatalf("FlushDigest = (%d,%v), want (0, no-route error)", made, err)
	}
}

// TestDigestSummaryWithoutChannelsUsesRouteTable: folded events that
// named no channels fall back to static routing at flush time, exactly
// as a direct notification would.
func TestDigestSummaryWithoutChannelsUsesRouteTable(t *testing.T) {
	svc, agg, _, queue := newDigestService(t, &route.Config{
		Routes: map[string][]string{"notices": {"email"}},
	})

	opened := time.Date(2026, time.October, 7, 10, 0, 0, 0, time.UTC)
	agg.SetClock(func() time.Time { return opened })
	agg.Add(&core.Notification{
		Type:       "notices",
		AudienceID: "bob",
		Content:    &core.DirectContent{Title: "maintenance", Body: "b"},
		CreatedAt:  opened,
	}, digest.Daily)
	agg.SetClock(func() time.Time { return time.Date(2026, time.October, 8, 9, 2, 0, 0, time.UTC) })

	made, err := svc.FlushDigest()
	if err != nil || made != 1 || len(queue.tasks) != 1 {
		t.Fatalf("FlushDigest = (%d,%v) queue=%d, want the routed summary", made, err, len(queue.tasks))
	}
	if task := queue.tasks[0]; task.Provider != "email" {
		t.Errorf("summary provider = %q, want the route table's email", task.Provider)
	}
}

// TestFlushDigestWithoutAggregator: SetDigest never wired → flush is a
// no-op, and an empty batch never panics.
func TestFlushDigestWithoutAggregator(t *testing.T) {
	templates := template.NewManager()
	router := route.NewRouter(&route.Config{})
	runtime := newMockProviderRuntime()
	queue := newMockQueue()
	svc := NewNotificationService(templates, router, runtime, nil, queue)
	made, err := svc.FlushDigest()
	if err != nil || made != 0 {
		t.Fatalf("FlushDigest = (%d,%v), want (0,nil)", made, err)
	}
	if err := svc.FlushDigestBatch(digest.Batch{}); err != nil {
		t.Fatalf("empty batch = %v, want nil", err)
	}
}
