package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/retry"
)

// TestDeliverOnceRetryKeepsLogOpenAndStateInStore is the integration
// guard for the Logs/Delivery split: during a retry the log row is a
// factual open event ("pending") and the delivery store owns the
// lifecycle ("retrying"). Neither writes state into the other — the log
// never learns the retrying/dead split, and the store's retrying state
// never reaches the log.
func TestDeliverOnceRetryKeepsLogOpenAndStateInStore(t *testing.T) {
	m := newRetryTestManager() // Max 2 -> three attempts, then spent
	flaky := &failingProvider{name: "flaky", err: retry.NewRetryableError(errors.New("transient"))}
	if err := m.RegisterProvider("flaky", flaky); err != nil {
		t.Fatal(err)
	}

	newTask := func(id string) *core.DeliveryTask {
		return &core.DeliveryTask{
			ID:        id,
			Provider:  "flaky",
			Status:    core.StatusQueued,
			Payload:   core.DeliveryPayload{Kind: core.PayloadContent},
			CreatedAt: time.Now(),
		}
	}

	// Attempt 1: retryable failure defers the task. The delivery store
	// holds the retrying lifecycle; the log row stays a factual open
	// event.
	task := newTask("d-retry-1")
	err := m.DeliverOnce(context.Background(), task)
	var deferred *retry.Deferred
	if !errors.As(err, &deferred) {
		t.Fatalf("attempt 1: expected a *retry.Deferred, got %v", err)
	}
	if task.Status != core.StatusRetrying {
		t.Errorf("attempt 1: task Status = %q, want retrying", task.Status)
	}
	if task.RetryCount != 1 {
		t.Errorf("attempt 1: RetryCount = %d, want 1", task.RetryCount)
	}
	d, ok := m.GetDeliveryByID("d-retry-1")
	if !ok {
		t.Fatal("attempt 1: delivery store must hold the task")
	}
	if d.Status != string(core.StatusRetrying) {
		t.Errorf("attempt 1: store Status = %q, want retrying", d.Status)
	}
	if d.RetryCount != 1 {
		t.Errorf("attempt 1: store RetryCount = %d, want 1", d.RetryCount)
	}
	if d.NextRetryAt == nil {
		t.Error("attempt 1: store must stamp NextRetryAt while retrying")
	}
	log := m.GetLogByID("d-retry-1")
	if log == nil {
		t.Fatal("attempt 1: log row must exist")
	}
	if log.Status != "pending" {
		t.Errorf("attempt 1: log Status = %q, want pending (open event)", log.Status)
	}

	// Attempt 2: still retryable, still deferred. The log stays open;
	// the store keeps tracking the lifecycle.
	err = m.DeliverOnce(context.Background(), task)
	if !errors.As(err, &deferred) {
		t.Fatalf("attempt 2: expected a *retry.Deferred, got %v", err)
	}
	if task.Status != core.StatusRetrying || task.RetryCount != 2 {
		t.Errorf("attempt 2: task = %q/%d, want retrying/2", task.Status, task.RetryCount)
	}
	d, _ = m.GetDeliveryByID("d-retry-1")
	if d.Status != string(core.StatusRetrying) || d.RetryCount != 2 {
		t.Errorf("attempt 2: store = %q/%d, want retrying/2", d.Status, d.RetryCount)
	}
	log = m.GetLogByID("d-retry-1")
	if log.Status != "pending" {
		t.Errorf("attempt 2: log Status = %q, want pending (still open)", log.Status)
	}

	// Attempt 3: budget spent, the task settles dead. The store records
	// the terminal lifecycle; the log records its own factual outcome.
	err = m.DeliverOnce(context.Background(), task)
	if !errors.Is(err, retry.ErrMaxRetries) {
		t.Fatalf("attempt 3: expected ErrMaxRetries, got %v", err)
	}
	if task.Status != core.StatusDead {
		t.Errorf("attempt 3: task Status = %q, want dead", task.Status)
	}
	d, _ = m.GetDeliveryByID("d-retry-1")
	if d.Status != string(core.StatusDead) {
		t.Errorf("attempt 3: store Status = %q, want dead", d.Status)
	}
	log = m.GetLogByID("d-retry-1")
	if log == nil || log.Status != "failed" {
		t.Errorf("attempt 3: log = %+v, want failed (factual outcome)", log)
	}
	// The dead/failed split lives only in the store: the log says
	// "failed", the store says "dead".
	if d.Status == log.Status {
		t.Errorf("store and log must not share a status vocabulary, both say %q", d.Status)
	}
}

// TestDeliverOnceSuccessSettlesStoreAndLog asserts the happy async path:
// the store reaches delivered and the log records the success outcome,
// each in its own vocabulary.
func TestDeliverOnceSuccessSettlesStoreAndLog(t *testing.T) {
	m := newRetryTestManager()
	ok := &mockProvider{name: "ok", status: &core.ProviderStatus{Name: "ok"}}
	if err := m.RegisterProvider("ok", ok); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{
		ID:        "d-ok",
		Provider:  "ok",
		Status:    core.StatusQueued,
		Payload:   core.DeliveryPayload{Kind: core.PayloadContent},
		CreatedAt: time.Now(),
	}
	if err := m.DeliverOnce(context.Background(), task); err != nil {
		t.Fatalf("DeliverOnce: %v", err)
	}
	if task.Status != core.StatusDelivered {
		t.Errorf("task Status = %q, want delivered", task.Status)
	}
	d, ok2 := m.GetDeliveryByID("d-ok")
	if !ok2 || d.Status != string(core.StatusDelivered) {
		t.Fatalf("store = %+v, want delivered", d)
	}
	log := m.GetLogByID("d-ok")
	if log == nil || log.Status != "success" {
		t.Errorf("log = %+v, want success", log)
	}
}

// TestDeliverSyncPathWritesStoreNotLogLifecycle asserts the sync path
// keeps the split too: the store owns the lifecycle, the log records the
// factual outcome, and the two never share a write.
func TestDeliverSyncPathWritesStoreNotLogLifecycle(t *testing.T) {
	m := newRetryTestManager()
	if err := m.RegisterProvider("flaky", &failingProvider{name: "flaky", err: retry.NewRetryableError(errors.New("transient"))}); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{
		ID:        "d-sync",
		Provider:  "flaky",
		Status:    core.StatusQueued,
		Payload:   core.DeliveryPayload{Kind: core.PayloadContent},
		CreatedAt: time.Now(),
	}
	err := m.Deliver(context.Background(), task)
	if !errors.Is(err, retry.ErrMaxRetries) {
		t.Fatalf("Deliver: %v", err)
	}
	if task.Status != core.StatusDead {
		t.Errorf("task Status = %q, want dead", task.Status)
	}
	d, ok := m.GetDeliveryByID("d-sync")
	if !ok || d.Status != string(core.StatusDead) {
		t.Fatalf("store = %+v, want dead", d)
	}
	log := m.GetLogByID("d-sync")
	if log == nil || log.Status != "failed" {
		t.Errorf("log = %+v, want failed", log)
	}
}

// TestGetDeliveriesStatsAndClear covers the stats read face and the clear
// path of the delivery store, mirroring the logs face's ClearLogs unit.
func TestGetDeliveriesStatsAndClear(t *testing.T) {
	m := newRetryTestManager()
	if err := m.RegisterProvider("ok", &mockProvider{name: "ok", status: &core.ProviderStatus{Name: "ok"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterProvider("bad", &failingProvider{name: "bad", err: errors.New("boom")}); err != nil {
		t.Fatal(err)
	}

	deliver := func(id, provider string) {
		t.Helper()
		task := &core.DeliveryTask{
			ID:        id,
			Provider:  provider,
			Status:    core.StatusQueued,
			Payload:   core.DeliveryPayload{Kind: core.PayloadContent},
			CreatedAt: time.Now(),
		}
		_ = m.Deliver(context.Background(), task)
	}
	deliver("d-s1", "ok")
	deliver("d-s2", "ok")
	deliver("d-s3", "bad")

	stats := m.GetDeliveriesStats()
	if stats.Total != 3 {
		t.Fatalf("expected 3 deliveries, got %d", stats.Total)
	}
	if stats.ByStatus[string(core.StatusDelivered)] != 2 {
		t.Errorf("expected 2 delivered, got %d", stats.ByStatus[string(core.StatusDelivered)])
	}
	if stats.ByProvider["ok"] != 2 {
		t.Errorf("expected 2 for ok, got %d", stats.ByProvider["ok"])
	}

	m.ClearDeliveries()
	if got := m.GetDeliveriesCount(nil); got != 0 {
		t.Errorf("expected 0 deliveries after clear, got %d", got)
	}
	stats = m.GetDeliveriesStats()
	if stats.Total != 0 {
		t.Errorf("expected 0 total after clear, got %d", stats.Total)
	}
}
