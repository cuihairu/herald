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

	"github.com/alicebob/miniredis/v2"

	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/groups"
	"github.com/cuihairu/herald/core/incident"
	"github.com/cuihairu/herald/core/limiter"
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

func (p *recordingProvider) lastTask() *core.DeliveryTask {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tasks) == 0 {
		return nil
	}
	return p.tasks[len(p.tasks)-1]
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

// TestAwaitingQueueWaitTimeoutWhileBlocked covers the select's ctx.Done arm:
// the context dies while wait is blocked on its listener — not before it
// registers — so the timeout must release the waiter from the select itself.
func TestAwaitingQueueWaitTimeoutWhileBlocked(t *testing.T) {
	backend, err := queue.NewMemoryQueue(&queue.QueueConfig{Type: "memory", Size: 10})
	if err != nil {
		t.Fatalf("NewMemoryQueue() error = %v", err)
	}
	aq := newAwaitingQueue(backend)
	defer func() { _ = backend.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := aq.wait(ctx, "never-delivered"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("wait() that outlives its ctx = %v, want context.DeadlineExceeded", err)
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

func TestNewRejectsCorruptEscalationStore(t *testing.T) {
	// A corrupt escalation_store is a construction failure for the library
	// caller (the daemon logs and continues): a silent restore failure
	// would drop armed upgrades without anyone knowing.
	path := filepath.Join(t.TempDir(), "pendings.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := config.Default()
	cfg.EscalationStore = path
	app, err := New(cfg)
	if err == nil {
		_ = app.Close()
		t.Fatal("New() with a corrupt escalation store should fail")
	}
	if !strings.Contains(err.Error(), "restore escalations") {
		t.Fatalf("unexpected error: %v", err)
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

func TestNewWithGroups(t *testing.T) {
	t.Run("seeds groups and routes through them", func(t *testing.T) {
		cfg := config.Default()
		cfg.Groups = []groups.Group{{
			ID:      "ops",
			Members: []groups.Member{{Channel: "rec"}},
		}}
		app, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = app.Close() }()

		members, ok := app.Groups().Resolver().ExpandGroup("ops")
		if !ok || len(members) != 1 || members[0].Channel != "rec" {
			t.Fatalf("seeded group must resolve, got %v (%v)", members, ok)
		}

		prov := &recordingProvider{}
		if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
			t.Fatalf("RegisterProvider: %v", err)
		}
		res, err := app.DispatchSync(context.Background(), &core.Notification{
			Type:     "deploy",
			Channels: []string{groups.Ref("ops")},
			Content:  &core.DirectContent{Title: "v1"},
		})
		if err != nil {
			t.Fatalf("DispatchSync: %v", err)
		}
		if len(res.Accepted) != 1 || res.Accepted[0] != "rec" {
			t.Fatalf("group reference must reach the member channel, got %v", res.Accepted)
		}
		if got := prov.count(); got != 1 {
			t.Fatalf("provider got %d tasks, want 1", got)
		}
	})

	t.Run("seeds persist through the file store", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "groups.json")
		cfg := config.Default()
		cfg.GroupsStore = path
		cfg.Groups = []groups.Group{{
			ID:      "ops",
			Members: []groups.Member{{Channel: "rec"}},
		}}
		app, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = app.Close() }()

		reopened, err := groups.NewFileStore(path)
		if err != nil {
			t.Fatalf("reopen store: %v", err)
		}
		got, err := reopened.List(context.Background())
		if err != nil {
			t.Fatalf("list persisted groups: %v", err)
		}
		if len(got) != 1 || got[0].ID != "ops" {
			t.Fatalf("expected seeded group persisted, got %v", got)
		}
	})

	t.Run("store holding an invalid group aborts construction", func(t *testing.T) {
		// The JSON parses, so the store opens; the invalid member list
		// then fails the reload validation.
		invalid := filepath.Join(t.TempDir(), "invalid-groups.json")
		if err := os.WriteFile(invalid, []byte(`{"version":1,"groups":[{"id":"bad","members":[]}]}`), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg := config.Default()
		cfg.GroupsStore = invalid
		if _, err := New(cfg); err == nil {
			t.Error("New() with a store holding an invalid group should fail")
		}
	})

	t.Run("invalid seed aborts construction", func(t *testing.T) {
		cfg := config.Default()
		cfg.Groups = []groups.Group{{ID: "broken", Members: nil}}
		if _, err := New(cfg); err == nil {
			t.Error("New() with an invalid group should fail")
		}
	})

	t.Run("malformed store aborts construction", func(t *testing.T) {
		malformed := filepath.Join(t.TempDir(), "bad-groups.json")
		if err := os.WriteFile(malformed, []byte("{not json"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		cfg := config.Default()
		cfg.GroupsStore = malformed
		if _, err := New(cfg); err == nil {
			t.Error("New() with a malformed groups store should fail")
		}
	})
}

func TestNewWithRulesDefaultPolicy(t *testing.T) {
	t.Run("empty policy stays allow", func(t *testing.T) {
		cfg := config.Default()
		app, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = app.Close() }()
		d, err := app.Rules().Evaluate(context.Background(), rules.NewEnv("deploy", "error", "", "", nil))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d != nil {
			t.Fatalf("allow default must keep the nil decision, got %+v", d)
		}
	})

	t.Run("deny withholds unmatched traffic", func(t *testing.T) {
		cfg := config.Default()
		cfg.RulesDefaultPolicy = "deny"
		app, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer func() { _ = app.Close() }()
		d, err := app.Rules().Evaluate(context.Background(), rules.NewEnv("deploy", "error", "", "", nil))
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if d == nil || !d.Defaulted || d.Action != rules.ActionSuppress {
			t.Fatalf("expected defaulted suppression, got %+v", d)
		}
	})

	t.Run("unknown policy aborts construction", func(t *testing.T) {
		cfg := config.Default()
		cfg.RulesDefaultPolicy = "quarantine"
		if _, err := New(cfg); err == nil {
			t.Error("New() with unknown rules_default_policy should fail")
		}
	})
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

func TestNewWithRulesState(t *testing.T) {
	t.Run("unknown state type aborts construction", func(t *testing.T) {
		cfg := config.Default()
		cfg.RulesState = &config.RulesStateConfig{Type: "etcd"}
		if _, err := New(cfg); err == nil {
			t.Error("New() with unknown rules_state type should fail")
		}
	})

	t.Run("unreachable redis aborts construction", func(t *testing.T) {
		cfg := config.Default()
		cfg.RulesState = &config.RulesStateConfig{Type: "redis", Addr: "127.0.0.1:1"}
		if _, err := New(cfg); err == nil {
			t.Error("New() with unreachable redis state store should fail")
		}
	})
}

func TestDispatchWithForRuleSuppressesFirstHit(t *testing.T) {
	cfg := config.Default()
	forDur := "1h"
	cfg.Rules = []rules.Rule{{
		ID:    "alert-for",
		Match: `type == "alert"`,
		Mode:  rules.ModeActive,
		For:   &forDur,
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

	// First hit: the window is pending, so nothing is delivered and no
	// task is queued — the event is swallowed like a dedup hit.
	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:    "alert",
		Level:   "error",
		Content: &core.DirectContent{Title: "t"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 0 {
		t.Fatalf("pending window must suppress delivery, accepted = %v", res.Accepted)
	}
	if got := prov.count(); got != 0 {
		t.Fatalf("expected no deliveries, got %d", got)
	}

	// The suppression is observable in the delivery log.
	logs := app.Runtime().GetLogs(0, 10, &logstore.Filter{Status: "pending"})
	if len(logs) != 1 || logs[0].RuleID != "alert-for" {
		t.Fatalf("expected one pending log for alert-for, got %+v", logs)
	}
}

func TestDispatchWithGroupByRuleFoldsAndSummarizes(t *testing.T) {
	cfg := config.Default()
	interval := "100ms" // tiny window so the test can outlive it with real time
	cfg.Rules = []rules.Rule{{
		ID:            "alert-grouped",
		Match:         `type == "alert"`,
		Mode:          rules.ModeActive,
		Route:         []rules.RouteStep{{Channels: []string{"rec"}}},
		GroupBy:       []string{"env"},
		GroupInterval: &interval,
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

	env := func(body string) *core.Notification {
		return &core.Notification{
			Type:    "alert",
			Level:   "error",
			Params:  map[string]any{"env": "prod"},
			Content: &core.DirectContent{Title: "t", Body: body},
		}
	}

	// First event of the env=prod group: delivered.
	res, err := app.DispatchSync(context.Background(), env("first"))
	if err != nil {
		t.Fatalf("DispatchSync(first) error = %v", err)
	}
	if len(res.Accepted) != 1 {
		t.Fatalf("first event must be delivered, accepted = %v", res.Accepted)
	}

	// Second event, same group (body differs so dedup stays out of the
	// way): folded — counted, not delivered.
	res, err = app.DispatchSync(context.Background(), env("second"))
	if err != nil {
		t.Fatalf("DispatchSync(second) error = %v", err)
	}
	if len(res.Accepted) != 0 {
		t.Fatalf("folded event must suppress delivery, accepted = %v", res.Accepted)
	}
	logs := app.Runtime().GetLogs(0, 10, &logstore.Filter{Status: "folded"})
	if len(logs) != 1 || logs[0].RuleID != "alert-grouped" {
		t.Fatalf("expected one folded log for alert-grouped, got %+v", logs)
	}

	// Quiet past the interval: the next event is delivered and carries the
	// finished round's summary, so this Process call accepts two deliveries
	// (the event and the summary) and the provider sees three in total.
	time.Sleep(150 * time.Millisecond)
	res, err = app.DispatchSync(context.Background(), env("third"))
	if err != nil {
		t.Fatalf("DispatchSync(third) error = %v", err)
	}
	if len(res.Accepted) != 2 {
		t.Fatalf("event after quiet must be delivered together with the summary, accepted = %v", res.Accepted)
	}
	if got := prov.count(); got != 3 {
		t.Fatalf("expected 3 deliveries (event, summary, event), got %d", got)
	}
}

func TestDispatchWithInhibitRuleSuppressesLeafAlerts(t *testing.T) {
	cfg := config.Default()
	cfg.Rules = []rules.Rule{
		{
			ID:    "root-down",
			Match: `level == "critical"`,
			Mode:  rules.ModeActive,
			Route: []rules.RouteStep{{Channels: []string{"rec"}}},
		},
		{
			ID:    "leaf-error",
			Match: `level == "error"`,
			Mode:  rules.ModeActive,
			Route: []rules.RouteStep{{Channels: []string{"rec"}}},
			Inhibit: &rules.InhibitSpec{
				Source: "root-down",
				Equal:  []string{"env"},
				// Tiny TTL so the test can outlive it with real time.
				TTL: strPtr("100ms"),
			},
		},
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prov := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	notif := func(level, env, body string) *core.Notification {
		return &core.Notification{
			Type:    "alert",
			Level:   level,
			Params:  map[string]any{"env": env},
			Content: &core.DirectContent{Title: "t", Body: body},
		}
	}

	// Root cause fires for env=prod: its delivery marks presence.
	if _, err := app.DispatchSync(context.Background(), notif("critical", "prod", "root")); err != nil {
		t.Fatalf("DispatchSync(root) error = %v", err)
	}
	if got := prov.count(); got != 1 {
		t.Fatalf("expected root delivery, got %d", got)
	}

	// The leaf alert for the same env is withheld. Bodies differ so dedup
	// stays out of the way — inhibition is what suppresses this one.
	res, err := app.DispatchSync(context.Background(), notif("error", "prod", "leaf during outage"))
	if err != nil {
		t.Fatalf("DispatchSync(leaf) error = %v", err)
	}
	if len(res.Accepted) != 0 {
		t.Fatalf("inhibited leaf must suppress delivery, accepted = %v", res.Accepted)
	}
	logs := app.Runtime().GetLogs(0, 10, &logstore.Filter{Status: "inhibited"})
	if len(logs) != 1 || logs[0].RuleID != "leaf-error" {
		t.Fatalf("expected one inhibited log for leaf-error, got %+v", logs)
	}

	// After the TTL the same leaf alert goes out again.
	time.Sleep(150 * time.Millisecond)
	if _, err := app.DispatchSync(context.Background(), notif("error", "prod", "leaf after recovery")); err != nil {
		t.Fatalf("DispatchSync(leaf after ttl) error = %v", err)
	}
	if got := prov.count(); got != 2 {
		t.Fatalf("expected leaf delivery after presence expiry, got %d", got)
	}
}

func strPtr(s string) *string { return &s }

// escalationApp builds an app with an active rule whose escalation plan
// re-delivers to the "phone" provider after the given timeout.
func escalationApp(t *testing.T, ackTimeout string) (*App, *recordingProvider, *recordingProvider) {
	t.Helper()
	cfg := config.Default()
	cfg.Rules = []rules.Rule{{
		ID:    "disk-down",
		Match: `type == "alert"`,
		Mode:  rules.ModeActive,
		Route: []rules.RouteStep{{Channels: []string{"rec"}}},
		Escalation: &rules.EscalationSpec{
			AckTimeout: ackTimeout,
			To:         []string{"phone"},
		},
	}}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.Escalation() == nil || app.Acks() == nil {
		t.Fatal("New() must expose the escalation manager and the ack store")
	}
	t.Cleanup(func() { _ = app.Close() })
	rec := &recordingProvider{}
	phone := &recordingProvider{}
	for name, prov := range map[string]*recordingProvider{"rec": rec, "phone": phone} {
		if err := app.Runtime().RegisterProvider(name, prov, true); err != nil {
			t.Fatalf("RegisterProvider(%s) error = %v", name, err)
		}
	}
	return app, rec, phone
}

// TestDispatchWithEscalationFiresUpgradeWithoutAck delivers a rule-routed
// alert and verifies the upgrade reaches the escalation channel after the
// (tiny) ack_timeout.
func TestDispatchWithEscalationFiresUpgradeWithoutAck(t *testing.T) {
	app, rec, phone := escalationApp(t, "30ms")

	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:    "alert",
		Level:   "critical",
		Params:  map[string]any{"alert_id": "inc-1"},
		Content: &core.DirectContent{Title: "disk full", Body: "b"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 1 {
		t.Fatalf("expected the original delivery, accepted = %v", res.Accepted)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if phone.count() == 1 {
			task := phone.lastTask()
			if task == nil || task.Payload.Content == nil ||
				!strings.Contains(task.Payload.Content.Title, "[Escalation]") ||
				!strings.Contains(task.Payload.Content.Title, "disk full") {
				t.Fatalf("unexpected escalation payload: %+v", task)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("upgrade never fired; rec=%d phone=%d", rec.count(), phone.count())
}

// TestDispatchWithEscalationAckCancelsUpgrade acknowledges the alert right
// after delivery and verifies the upgrade never fires.
func TestDispatchWithEscalationAckCancelsUpgrade(t *testing.T) {
	app, rec, phone := escalationApp(t, "30ms")

	_, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:    "alert",
		Level:   "critical",
		Params:  map[string]any{"alert_id": "inc-1"},
		Content: &core.DirectContent{Title: "disk full", Body: "b"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if _, err := app.Acks().Ack(context.Background(), "inc-1", "alice", "test"); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}

	time.Sleep(80 * time.Millisecond)
	if phone.count() != 0 {
		t.Fatalf("acknowledged alert must not escalate")
	}
	if rec.count() != 1 {
		t.Fatalf("expected exactly the original delivery, got %d", rec.count())
	}
}

// TestDispatchWithEscalationRepeatsReArm verifies that a repeated delivery
// of the same alert id resets the escalation window.
func TestDispatchWithEscalationRepeatsReArm(t *testing.T) {
	app, _, phone := escalationApp(t, "60ms")

	notif := &core.Notification{
		Type:    "alert",
		Level:   "critical",
		Params:  map[string]any{"alert_id": "inc-1"},
		Content: &core.DirectContent{Title: "disk full", Body: "b"},
	}
	if _, err := app.DispatchSync(context.Background(), notif); err != nil {
		t.Fatalf("DispatchSync(1) error = %v", err)
	}
	time.Sleep(35 * time.Millisecond)
	notif2 := &core.Notification{
		Type:    "alert",
		Level:   "critical",
		Params:  map[string]any{"alert_id": "inc-1"},
		Content: &core.DirectContent{Title: "disk full", Body: "still full"},
	}
	if _, err := app.DispatchSync(context.Background(), notif2); err != nil {
		t.Fatalf("DispatchSync(2) error = %v", err)
	}
	// 35ms after the second delivery the re-armed window (60ms) is still
	// open; the first window would have expired by now.
	time.Sleep(35 * time.Millisecond)
	if phone.count() != 0 {
		t.Fatalf("upgrade fired before the re-armed window closed")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if phone.count() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("re-armed upgrade never fired")
}

// TestDispatchWithSilenceRuleWithholdsInsideWindow builds a rule whose
// silence window is centered on the current wall clock (now-2h to now+2h
// covers the current minute at every time of day, including across
// midnight) and verifies the event is withheld and observed.
func TestDispatchWithSilenceRuleWithholdsInsideWindow(t *testing.T) {
	now := time.Now()
	window := func(offset time.Duration) string {
		return now.Add(offset).Format("15:04")
	}
	cfg := config.Default()
	cfg.Rules = []rules.Rule{
		{
			ID:    "quiet-hours",
			Match: `type == "alert"`,
			Mode:  rules.ModeActive,
			Route: []rules.RouteStep{{Channels: []string{"rec"}}},
			Silence: &rules.SilenceSpec{
				Start: window(-2 * time.Hour),
				End:   window(2 * time.Hour),
			},
		},
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prov := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:    "alert",
		Level:   "error",
		Content: &core.DirectContent{Title: "t", Body: "inside the window"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 0 {
		t.Fatalf("event inside the silence window must be withheld, accepted = %v", res.Accepted)
	}
	if got := prov.count(); got != 0 {
		t.Fatalf("expected no deliveries inside the window, got %d", got)
	}
	logs := app.Runtime().GetLogs(0, 10, &logstore.Filter{Status: "silenced"})
	if len(logs) != 1 || logs[0].RuleID != "quiet-hours" {
		t.Fatalf("expected one silenced log for quiet-hours, got %+v", logs)
	}
}

// TestDispatchWithSilenceRuleDeliversOutsideWindow places the window two
// hours ahead of the current wall clock (now+2h to now+3h never covers the
// current minute) and verifies delivery is unaffected.
func TestDispatchWithSilenceRuleDeliversOutsideWindow(t *testing.T) {
	now := time.Now()
	window := func(offset time.Duration) string {
		return now.Add(offset).Format("15:04")
	}
	cfg := config.Default()
	cfg.Rules = []rules.Rule{
		{
			ID:    "future-quiet",
			Match: `type == "alert"`,
			Mode:  rules.ModeActive,
			Route: []rules.RouteStep{{Channels: []string{"rec"}}},
			Silence: &rules.SilenceSpec{
				Start: window(2 * time.Hour),
				End:   window(3 * time.Hour),
			},
		},
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	prov := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", prov, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	res, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:    "alert",
		Level:   "error",
		Content: &core.DirectContent{Title: "t", Body: "outside the window"},
	})
	if err != nil {
		t.Fatalf("DispatchSync() error = %v", err)
	}
	if len(res.Accepted) != 1 {
		t.Fatalf("event outside the window must be delivered, accepted = %v", res.Accepted)
	}
	if got := prov.count(); got != 1 {
		t.Fatalf("expected one delivery outside the window, got %d", got)
	}
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

func TestNewWiresProviderRateLimit(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = map[string]config.ProviderConfig{
		"rec": {
			Type:      "log",
			Enabled:   nil,
			RateLimit: &limiter.Config{Type: "token_bucket", Rate: 100, Burst: 2},
		},
	}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = app.Close() }()

	// The limiter must be reachable through the runtime manager, so
	// deliveries through "rec" are throttled from the first tick.
	lm, ok := app.Runtime().LimiterFor("rec")
	if !ok {
		t.Fatal("expected a limiter registered for provider rec")
	}
	if lm == nil {
		t.Fatal("expected a non-nil limiter")
	}
}

// forApp builds an app with a for-window rule grouped by "env": the
// recovery (the same env reporting at a level that no longer matches) is
// event-driven and delivers the resolved summary. "alert" also has a
// static route: a recovered event no longer matches the rule and falls
// back to plain routing like any notification.
func forApp(t *testing.T, forDur string) (*App, *recordingProvider) {
	t.Helper()
	cfg := config.Default()
	cfg.Routes = map[string][]string{"alert": {"rec"}}
	cfg.Rules = []rules.Rule{{
		ID:      "disk-down",
		Match:   `type == "alert" && level == "error"`,
		Mode:    rules.ModeActive,
		Route:   []rules.RouteStep{{Channels: []string{"rec"}}},
		For:     &forDur,
		GroupBy: []string{"env"},
	}}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	rec := &recordingProvider{}
	if err := app.Runtime().RegisterProvider("rec", rec, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	return app, rec
}

// TestDispatchForEpisodeLifecycle walks the full incident lifecycle: a
// for-window alert fires after the duration (episode opens), the same env
// reporting at info resolves it (episode closes, recovery summary goes
// out on the same channel).
func TestDispatchForEpisodeLifecycle(t *testing.T) {
	app, rec := forApp(t, "30ms")

	alert := func(level string) *core.Notification {
		return &core.Notification{
			Type:    "alert",
			Level:   level,
			Content: &core.DirectContent{Title: "disk full", Body: "b"},
			Params:  map[string]any{"env": "prod"},
		}
	}

	// While the window runs the event is withheld.
	if _, err := app.DispatchSync(context.Background(), alert("error")); err != nil {
		t.Fatalf("DispatchSync(1) error = %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	// Window elapsed: the alert fires and the episode opens.
	if _, err := app.DispatchSync(context.Background(), alert("error")); err != nil {
		t.Fatalf("DispatchSync(2) error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := app.Incidents().List(nil); len(got) == 1 && got[0].Status() == incident.StatusOpen {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	// No alert_id in params: the ledger falls back to the content identity.
	open := app.Incidents().List(nil)
	if len(open) != 1 || open[0].AlertID == "" {
		t.Fatalf("expected an open incident with a fallback identity, got %+v", open)
	}

	// prod recovers: info is still reported for the same env, so the
	// engine sees the match stop holding for the fired group.
	if _, err := app.DispatchSync(context.Background(), alert("info")); err != nil {
		t.Fatalf("DispatchSync(resolve) error = %v", err)
	}

	// The recovery summary lands on the episode's channel, next to the
	// recovered event's own plain delivery.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= 3 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if rec.count() != 3 {
		t.Fatalf("expected alert + recovery summary + plain recovery delivery, got %d", rec.count())
	}
	if !rec.hasTitle("[Resolved]") {
		t.Fatalf("expected a resolved summary among %+v", rec.count())
	}

	// The ledger shows the closed episode.
	resolved := app.Incidents().List(&incident.Filter{Status: incident.StatusResolved})
	if len(resolved) != 1 {
		t.Fatalf("expected one resolved episode, got %+v", app.Incidents().List(nil))
	}
}

// TestDispatchForAckThenRecover pins the mid-life of an episode: an ack
// between fire and recovery leaves an acked-then-resolved ledger trail.
func TestDispatchForAckThenRecover(t *testing.T) {
	app, rec := forApp(t, "30ms")

	alert := &core.Notification{
		Type:    "alert",
		Level:   "error",
		Content: &core.DirectContent{Title: "disk full", Body: "b"},
		Params:  map[string]any{"env": "prod", "alert_id": "inc-7"},
	}
	if _, err := app.DispatchSync(context.Background(), alert); err != nil {
		t.Fatalf("DispatchSync(1) error = %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := app.DispatchSync(context.Background(), alert); err != nil {
		t.Fatalf("DispatchSync(2) error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := app.Incidents().List(&incident.Filter{AlertID: "inc-7"}); len(got) == 1 && got[0].Status() == incident.StatusOpen {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if _, err := app.AckAlert(context.Background(), "inc-7", "alice", "test"); err != nil {
		t.Fatalf("AckAlert() error = %v", err)
	}
	if _, err := app.DispatchSync(context.Background(), &core.Notification{
		Type:    "alert",
		Level:   "info",
		Content: &core.DirectContent{Title: "disk full", Body: "b"},
		Params:  map[string]any{"env": "prod", "alert_id": "inc-7"},
	}); err != nil {
		t.Fatalf("DispatchSync(resolve) error = %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := app.Incidents().List(&incident.Filter{AlertID: "inc-7"}); len(got) == 1 && got[0].Status() == incident.StatusResolved {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	inc := app.Incidents().List(&incident.Filter{AlertID: "inc-7"})
	if len(inc) != 1 || inc[0].Status() != incident.StatusResolved || inc[0].AckedBy != "alice" {
		t.Fatalf("expected an acked-then-resolved episode, got %+v", inc)
	}
	// The recovery summary plus the recovered event's own plain delivery.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= 3 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if rec.count() != 3 {
		t.Fatalf("expected alert + recovery summary + plain recovery delivery, got %d", rec.count())
	}
	if !rec.hasTitle("[Resolved]") {
		t.Fatalf("expected a resolved summary among the deliveries")
	}
}

// TestAckAlertGuardsEmptyID pins the library-level ack pairing: an ack
// without an identity is rejected by the ack store and must not touch the
// ledger.
func TestAckAlertGuardsEmptyID(t *testing.T) {
	app, _ := forApp(t, "1h")
	app.Incidents().Open(incident.Opening{RuleID: "disk-down", GroupKey: "prod", AlertID: "a1", Title: "t"})
	if _, err := app.AckAlert(context.Background(), "", "alice", "test"); err == nil {
		t.Fatal("an empty alert id must be rejected")
	}
	if got := app.Incidents().List(&incident.Filter{AlertID: "a1"})[0]; got.Status() != incident.StatusOpen {
		t.Fatalf("the ledger must stay open, got %+v", got)
	}
}

func (p *recordingProvider) hasTitle(substr string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.tasks {
		if t.Payload.Content != nil && strings.Contains(t.Payload.Content.Title, substr) {
			return true
		}
	}
	return false
}

func TestNewWiresRedisRulesState(t *testing.T) {
	mr := miniredis.RunT(t)
	cfg := config.Default()
	cfg.RulesState = &config.RulesStateConfig{Type: "redis", Addr: mr.Addr()}
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with a live redis state store: %v", err)
	}
	defer func() { _ = app.Close() }()
}

func TestNewRejectsUnwritableRulesStore(t *testing.T) {
	// The rule must pass validation; the failure comes from persisting it
	// into a directory that does not exist.
	cfg := config.Default()
	cfg.Rules = []rules.Rule{{
		ID:    "ok",
		Match: `level == "error"`,
		Mode:  rules.ModeActive,
		Route: []rules.RouteStep{{Channels: []string{"rec"}}},
	}}
	cfg.RulesStore = filepath.Join(t.TempDir(), "missing-dir", "rules.json")
	if _, err := New(cfg); err == nil {
		t.Fatal("New() with an unwritable rules store must fail")
	}
}
