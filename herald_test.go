package herald

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/logstore"
	"github.com/cuihairu/herald/core/queue"
	"github.com/cuihairu/herald/core/rules"
	"github.com/cuihairu/herald/core/template"
)

// recordingProvider records delivered tasks, or always fails when err is set.
type recordingProvider struct {
	mu    sync.Mutex
	tasks []*core.DeliveryTask
	err   error
}

func (p *recordingProvider) Deliver(_ context.Context, task *core.DeliveryTask) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.tasks = append(p.tasks, task)
	return nil
}

func (p *recordingProvider) Name() string { return "rec" }
func (p *recordingProvider) Type() string { return "recording" }
func (p *recordingProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: "rec", Type: "recording", Status: "ok"}
}

func (p *recordingProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.tasks)
}

func newTestApp(t *testing.T) (*App, *recordingProvider) {
	t.Helper()
	app, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prov := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return app, prov
}

func TestNewDefaults(t *testing.T) {
	app, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) error = %v", err)
	}
	defer func() { _ = app.Close() }()

	if app.cfg.Queue.Type != "memory" {
		t.Errorf("default queue type = %q, want memory", app.cfg.Queue.Type)
	}
	if app.cfg.Queue.Workers <= 0 {
		t.Errorf("default workers = %d, want > 0", app.cfg.Queue.Workers)
	}
	if !app.cfg.Dedup.Enabled {
		t.Error("default dedup should be enabled")
	}
	if app.manager == nil || app.svc == nil {
		t.Error("New(nil) should wire manager and notification service")
	}
}

func TestNewAppliesDefaultsToPartialConfig(t *testing.T) {
	cfg := &config.Config{} // all zero values
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New(empty) error = %v", err)
	}
	defer func() { _ = app.Close() }()

	if app.cfg.Queue.Type != "memory" {
		t.Errorf("queue type = %q, want memory default", app.cfg.Queue.Type)
	}
	if app.cfg.Retry.Max != 3 {
		t.Errorf("retry max = %d, want 3 default", app.cfg.Retry.Max)
	}
}

func TestNewUnknownProviderType(t *testing.T) {
	cfg := config.Default()
	cfg.Providers["bad"] = config.ProviderConfig{Type: "no-such"}
	if _, err := New(cfg); err == nil {
		t.Error("New() with unknown provider type should fail")
	}
}

func TestDispatchSyncDelivers(t *testing.T) {
	app, prov := newTestApp(t)

	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:     "deploy",
		Channels: []string{"rec"},
		Content:  &core.DirectContent{Title: "v1"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 1 || res.Accepted[0] != "rec" {
		t.Errorf("Accepted = %v, want [rec]", res.Accepted)
	}
	if got := prov.count(); got != 1 {
		t.Errorf("provider got %d tasks, want 1", got)
	}
}

func TestDispatchAsyncEventuallyDelivers(t *testing.T) {
	app, prov := newTestApp(t)

	if _, err := app.Dispatch(context.Background(), &core.Notification{
		Type:     "deploy",
		Channels: []string{"rec"},
	}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for prov.count() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("task was never delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDispatchSyncReturnsDeliveryError(t *testing.T) {
	app, _ := newTestApp(t)
	boom := errors.New("boom")
	if err := app.Runtime().RegisterProvider("bad", &recordingProvider{err: boom}, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}

	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:     "alert",
		Channels: []string{"bad"},
	})
	if !errors.Is(err, boom) {
		t.Errorf("DispatchSync() error = %v, want boom", err)
	}
	// The channel was accepted onto the queue, so the failure is a delivery
	// outcome, not a per-channel acceptance error.
	if res == nil || len(res.TaskIDs) != 1 || len(res.Failed) != 0 {
		t.Errorf("res = %+v, want one task and no acceptance failures", res)
	}
}

func TestDispatchSyncPartialFailure(t *testing.T) {
	app, prov := newTestApp(t)
	if err := app.Runtime().RegisterProvider("off", prov, false); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}

	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:     "alert",
		Channels: []string{"rec", "off"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v, want nil for partial failure", err)
	}
	if len(res.Accepted) != 1 || res.Accepted[0] != "rec" {
		t.Errorf("Accepted = %v, want [rec]", res.Accepted)
	}
	if len(res.Failed) != 1 || res.Failed[0].Channel != "off" {
		t.Errorf("Failed = %v, want [off]", res.Failed)
	}
}

func TestDedupSuppressesRepeat(t *testing.T) {
	app, prov := newTestApp(t)

	notify := &core.Notification{Type: "dup", Channels: []string{"rec"}}
	first, err := app.DispatchSync(context.Background(), notify)
	if err != nil {
		t.Fatalf("first DispatchSync() error = %v", err)
	}
	second, err := app.DispatchSync(context.Background(), notify)
	if err != nil {
		t.Fatalf("second DispatchSync() error = %v", err)
	}
	if len(first.TaskIDs) != 1 {
		t.Errorf("first TaskIDs = %v, want one", first.TaskIDs)
	}
	if len(second.TaskIDs) != 0 {
		t.Errorf("second TaskIDs = %v, want none (deduped)", second.TaskIDs)
	}
	if got := prov.count(); got != 1 {
		t.Errorf("provider got %d tasks, want 1", got)
	}
}

func TestDispatchNoRoute(t *testing.T) {
	app, _ := newTestApp(t)

	if _, err := app.Dispatch(context.Background(), &core.Notification{Type: "nothing"}); err == nil {
		t.Error("Dispatch() without channels or routes should fail")
	} else if !strings.Contains(err.Error(), "no route") {
		t.Errorf("error = %v, want no-route failure", err)
	}
}

func TestDispatchAfterClose(t *testing.T) {
	app, _ := newTestApp(t)
	if err := app.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err := app.Dispatch(context.Background(), &core.Notification{
		Type:     "late",
		Channels: []string{"rec"},
	})
	if err == nil {
		t.Error("Dispatch() after Close should fail")
	}
}

func TestRouteFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Routes["deploy"] = []string{"rec"}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prov := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	defer func() { _ = app.Close() }()

	// No explicit channels: the static route resolves the provider.
	res, err := app.DispatchSync(context.Background(), &core.Notification{Type: "deploy"})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 1 || res.Accepted[0] != "rec" {
		t.Errorf("Accepted = %v, want [rec]", res.Accepted)
	}
}

func TestAwaitingQueueWaiters(t *testing.T) {
	backend, err := queue.NewMemoryQueue(&queue.QueueConfig{Type: "memory", Size: 10})
	if err != nil {
		t.Fatalf("NewMemoryQueue() error = %v", err)
	}
	aq := newAwaitingQueue(backend)
	ctx := context.Background()

	// Outcome resolved before wait: served from the result cache.
	task := &core.DeliveryTask{ID: "t1"}
	if err := aq.Push(ctx, task); err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if err := aq.Ack(ctx, "t1"); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if err := aq.wait(ctx, "t1"); err != nil {
		t.Errorf("wait(t1) after Ack = %v, want nil", err)
	}

	task2 := &core.DeliveryTask{ID: "t2"}
	if err := aq.Push(ctx, task2); err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	reason := errors.New("delivery failed")
	if err := aq.Nack(ctx, "t2", reason); err != nil {
		t.Fatalf("Nack() error = %v", err)
	}
	if err := aq.wait(ctx, "t2"); !errors.Is(err, reason) {
		t.Errorf("wait(t2) after Nack = %v, want reason", err)
	}

	// A wait for a task with no outcome blocks; a canceled ctx releases it.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := aq.wait(cctx, "unknown"); !errors.Is(err, context.Canceled) {
		t.Errorf("wait(unknown) with canceled ctx = %v, want context.Canceled", err)
	}

	// Push on a closed backend still fails and stays untracked.
	_ = backend.Close()
	if err := aq.Push(ctx, &core.DeliveryTask{ID: "t3"}); err == nil {
		t.Error("Push() on closed queue should fail")
	}
}

func TestAwaitingQueueListenerPath(t *testing.T) {
	backend, err := queue.NewMemoryQueue(&queue.QueueConfig{Type: "memory", Size: 10})
	if err != nil {
		t.Fatalf("NewMemoryQueue() error = %v", err)
	}
	aq := newAwaitingQueue(backend)
	defer func() { _ = backend.Close() }()

	// Resolve lands after wait has registered its listener.
	go func() {
		time.Sleep(10 * time.Millisecond)
		aq.resolve("t1", nil)
	}()
	if err := aq.wait(context.Background(), "t1"); err != nil {
		t.Errorf("wait(t1) = %v, want nil", err)
	}
}

func TestAwaitingQueueWaitContextCanceled(t *testing.T) {
	backend, err := queue.NewMemoryQueue(&queue.QueueConfig{Type: "memory", Size: 10})
	if err != nil {
		t.Fatalf("NewMemoryQueue() error = %v", err)
	}
	aq := newAwaitingQueue(backend)
	defer func() { _ = backend.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	if err := aq.Push(ctx, &core.DeliveryTask{ID: "t1"}); err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	cancel()
	if err := aq.wait(ctx, "t1"); !errors.Is(err, context.Canceled) {
		t.Errorf("wait() with canceled ctx = %v, want context.Canceled", err)
	}
}

func TestNewUnknownQueueType(t *testing.T) {
	cfg := config.Default()
	cfg.Queue.Type = "bogus"
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "create queue") {
		t.Errorf("New() with unknown queue type = %v, want create-queue failure", err)
	}
}

func TestNewInvalidTemplate(t *testing.T) {
	cfg := config.Default()
	cfg.Templates = map[string]template.TemplateConfig{"bad": {Name: "no title"}}
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "load templates") {
		t.Errorf("New() with invalid template = %v, want load-templates failure", err)
	}
}

func TestAppQueueAccessor(t *testing.T) {
	app, _ := newTestApp(t)
	if app.Queue() == nil {
		t.Error("Queue() = nil, want the backend queue")
	}
}

func TestDispatchSyncProcessError(t *testing.T) {
	app, _ := newTestApp(t)

	// No channels and no route: Process fails before anything is queued, so
	// DispatchSync surfaces the same error as Dispatch.
	res, err := app.DispatchSync(context.Background(), &core.Notification{Type: "nothing"})
	if err == nil || !strings.Contains(err.Error(), "no route") {
		t.Errorf("DispatchSync() error = %v, want no-route failure", err)
	}
	// Process failed before anything was queued, so there is no result object.
	if res != nil {
		t.Errorf("DispatchSync() res = %+v, want nil alongside the error", res)
	}
}

func TestResolvedEviction(t *testing.T) {
	backend, err := queue.NewMemoryQueue(&queue.QueueConfig{Type: "memory", Size: 10})
	if err != nil {
		t.Fatalf("NewMemoryQueue() error = %v", err)
	}
	aq := newAwaitingQueue(backend)
	defer func() { _ = backend.Close() }()

	prev := resolvedResultsLimit
	resolvedResultsLimit = 2
	t.Cleanup(func() { resolvedResultsLimit = prev })

	ctx := context.Background()
	for _, id := range []string{"t1", "t2", "t3"} {
		if err := aq.Push(ctx, &core.DeliveryTask{ID: id}); err != nil {
			t.Fatalf("Push(%s) error = %v", id, err)
		}
		if err := aq.Ack(ctx, id); err != nil {
			t.Fatalf("Ack(%s) error = %v", id, err)
		}
	}

	// t1 is the oldest outcome and was evicted; its wait can only end via ctx.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := aq.wait(cctx, "t1"); !errors.Is(err, context.Canceled) {
		t.Errorf("wait(evicted t1) = %v, want context.Canceled", err)
	}
	// t3 survived the eviction and is served from the cache.
	if err := aq.wait(ctx, "t3"); err != nil {
		t.Errorf("wait(cached t3) = %v, want nil", err)
	}
}

func TestAppRulesAccessor(t *testing.T) {
	app, _ := newTestApp(t)
	if app.Rules() == nil {
		t.Fatal("Rules() = nil, want the rule engine")
	}
	if _, err := app.Rules().List(context.Background()); err != nil {
		t.Fatalf("Rules().List() error = %v", err)
	}
}

func TestNewSeedsRulesFromConfig(t *testing.T) {
	app, prov := newTestApp(t)

	if _, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:     "alert",
		Channels: []string{"rec"},
		Content:  &core.DirectContent{Title: "t"},
	}); err != nil {
		t.Fatalf("baseline dispatch (no rules) error = %v", err)
	}
	if prov.count() != 1 {
		t.Fatalf("baseline delivery count = %d, want 1", prov.count())
	}
}

func TestNewRejectsInvalidRule(t *testing.T) {
	cfg := config.Default()
	cfg.Rules = []rules.Rule{{
		ID:    "broken rule id!", // rejected by Validate (charset)
		Match: `level == "error"`,
		Route: []rules.RouteStep{{Channels: []string{"rec"}}},
	}, {
		ID:    "ok",
		Match: `level == "error"`,
		Mode:  rules.ModeActive,
		Route: []rules.RouteStep{{Channels: []string{"rec"}}},
	}}
	if _, err := New(cfg); err == nil {
		t.Error("New() with an invalid rule should fail")
	}
}

func TestNewBadRuleExpression(t *testing.T) {
	cfg := config.Default()
	cfg.Rules = []rules.Rule{{
		ID:    "range",
		Match: `1..99999999 != []`,
		Route: []rules.RouteStep{{Channels: []string{"rec"}}},
	}}
	if _, err := New(cfg); err == nil {
		t.Error("New() with a range expression rule should fail")
	}
}

func TestNewWithRulesStore(t *testing.T) {
	t.Run("seeds persist through the file store", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "rules.json")
		cfg := config.Default()
		cfg.RulesStore = path
		cfg.Rules = []rules.Rule{{
			ID:    "seeded",
			Match: `level == "error"`,
			Mode:  rules.ModeActive,
			Route: []rules.RouteStep{{Channels: []string{"rec"}}},
		}}
		app, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = app.Close() }()

		// A fresh store over the same file must see the seeded rule.
		reloaded, err := rules.NewFileStore(path)
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		got, err := reloaded.List(context.Background())
		if err != nil {
			t.Fatalf("list persisted rules: %v", err)
		}
		if len(got) != 1 || got[0].ID != "seeded" {
			t.Fatalf("expected seeded rule persisted, got %v", got)
		}
	})

	t.Run("unusable store path aborts construction", func(t *testing.T) {
		malformed := filepath.Join(t.TempDir(), "malformed.json")
		if err := os.WriteFile(malformed, []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg := config.Default()
		cfg.RulesStore = malformed
		if _, err := New(cfg); err == nil {
			t.Error("New() with a malformed rules store should fail")
		}
	})
}

func TestDispatchWithActiveRuleRoutes(t *testing.T) {
	cfg := config.Default()
	cfg.Rules = []rules.Rule{{
		ID:    "alert-to-rec",
		Match: `type == "alert" && level == "error"`,
		Mode:  rules.ModeActive,
		Route: []rules.RouteStep{{Channels: []string{"rec"}}},
	}}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prov := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	// Matching notification: routed to rec by the rule (no explicit channels).
	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:    "alert",
		Level:   "error",
		Content: &core.DirectContent{Title: "t"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 1 || res.Accepted[0] != "rec" {
		t.Fatalf("rule should route to rec, accepted = %v", res.Accepted)
	}

	// Non-matching notification with no channels and no static route:
	// static routing fails as it would without the engine.
	_, err = app.DispatchSync(context.Background(), &core.Notification{
		Type:    "other",
		Level:   "info",
		Content: &core.DirectContent{Title: "t"},
	})
	if err == nil || !strings.Contains(err.Error(), "no route") {
		t.Fatalf("expected static no-route error, got %v", err)
	}
}

func TestDispatchShadowRuleObserves(t *testing.T) {
	cfg := config.Default()
	cfg.Rules = []rules.Rule{{
		ID:    "shadow-all",
		Match: `type == "alert"`,
		Mode:  rules.ModeShadow,
		Route: []rules.RouteStep{{Channels: []string{"rec"}}},
	}}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prov := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	// Explicit channel wins over the (shadow) rule; observation still recorded.
	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:     "alert",
		Channels: []string{"rec"},
		Content:  &core.DirectContent{Title: "t"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 1 || res.Accepted[0] != "rec" {
		t.Fatalf("expected delivery to rec, accepted = %v", res.Accepted)
	}

	logs := app.manager.GetLogs(0, 10, &logstore.Filter{Status: "shadow"})
	if len(logs) != 1 || logs[0].RuleID != "shadow-all" || !logs[0].WouldFire {
		t.Fatalf("expected one shadow observation, got %+v", logs)
	}
}
