package herald

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/config"
	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/queue"
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

func (p *recordingProvider) Name() string                  { return "rec" }
func (p *recordingProvider) Type() string                  { return "recording" }
func (p *recordingProvider) Status() *core.ProviderStatus  { return &core.ProviderStatus{Name: "rec", Type: "recording", Status: "ok"} }

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
