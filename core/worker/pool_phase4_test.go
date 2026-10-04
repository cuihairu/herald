package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/runtime"
)

// settleTasks drives the pool until every pushed task has been settled
// (acked or nacked) and returns the counts. The queue's mutex is the
// happens-before edge: the worker writes the task's final state BEFORE
// calling Ack/Nack, so reading the task after observing the count is
// race-free.
func settleTasks(t *testing.T, q *memQueue, ids []string, wantAcked, wantNacked int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		acked, nacked, popped := q.counts()
		if popped >= len(ids) && len(acked) == wantAcked && len(nacked) == wantNacked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tasks not settled: popped=%d acked=%v nacked=%v", popped, acked, nacked)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// runPool starts a one-worker pool over the queue and registers cleanup.
func runPool(t *testing.T, q *memQueue, m *runtime.Manager) {
	t.Helper()
	p := NewPool(q, m, NewRegistry(), 1)
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(runCtx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("pool did not stop within the deadline")
		}
	})
}

// TestPoolQueueWorkerRetrySemantics runs the whole §16/§17 loop —
// enqueue (Push) → dequeue (Pop) → Provider → Ack/Nack → settled task
// state — and asserts the delivery bookkeeping the worker settles onto
// the task: the attempt budget, the §14 last_error, the retry count.
func TestPoolQueueWorkerRetrySemantics(t *testing.T) {
	q := newMemQueue(false)
	m := runtime.NewManager(10)
	if err := m.RegisterProvider("ok", &stubProvider{}, true); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterProvider("bad", &stubProvider{err: errors.New("boom")}, true); err != nil {
		t.Fatal(err)
	}
	runPool(t, q, m)

	delivered := &core.DeliveryTask{ID: "p4-ok", Provider: "ok"}
	failed := &core.DeliveryTask{ID: "p4-bad", Provider: "bad"}
	ctx := context.Background()
	for _, task := range []*core.DeliveryTask{delivered, failed} {
		if err := q.Push(ctx, task); err != nil {
			t.Fatalf("Push(%s): %v", task.ID, err)
		}
	}

	settleTasks(t, q, []string{"p4-ok", "p4-bad"}, 1, 1)

	// Success: delivered, no error residue, single-attempt budget (no
	// retryer configured on this manager).
	if delivered.Status != core.StatusDelivered {
		t.Errorf("delivered.Status = %q, want %q", delivered.Status, core.StatusDelivered)
	}
	if delivered.MaxAttempts != 1 {
		t.Errorf("delivered.MaxAttempts = %d, want 1", delivered.MaxAttempts)
	}
	if delivered.LastError != "" {
		t.Errorf("delivered.LastError = %q, want empty", delivered.LastError)
	}

	// Failure: failed, the provider error lands on the task (§14
	// last_error), budget and retry count untouched.
	if failed.Status != core.StatusFailed {
		t.Errorf("failed.Status = %q, want %q", failed.Status, core.StatusFailed)
	}
	if failed.MaxAttempts != 1 {
		t.Errorf("failed.MaxAttempts = %d, want 1", failed.MaxAttempts)
	}
	if failed.LastError != "boom" {
		t.Errorf("failed.LastError = %q, want boom", failed.LastError)
	}
	if failed.RetryCount != 0 {
		t.Errorf("failed.RetryCount = %d, want 0", failed.RetryCount)
	}
}

// TestPoolDeadViaRetryableFailure settles the retry side of §17 with a
// retryable provider error: the worker's one Pop covers the whole retry
// cycle (synchronous by design), exhausting into dead — distinct from a
// plain failed, and carrying the budget and the settled error.
func TestPoolDeadViaRetryableFailure(t *testing.T) {
	q := newMemQueue(false)
	m := runtime.NewManager(10, &retry.Config{
		Max:          1,
		Backoff:      "fixed",
		InitialDelay: time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
	})
	if err := m.RegisterProvider("flaky", &stubProvider{err: retry.NewRetryableError(errors.New("flaky-boom"))}, true); err != nil {
		t.Fatal(err)
	}
	runPool(t, q, m)

	task := &core.DeliveryTask{ID: "p4-dead", Provider: "flaky"}
	if err := q.Push(context.Background(), task); err != nil {
		t.Fatalf("Push: %v", err)
	}

	settleTasks(t, q, []string{"p4-dead"}, 0, 1)

	if task.Status != core.StatusDead {
		t.Errorf("Status = %q, want %q", task.Status, core.StatusDead)
	}
	if task.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1 retry performed", task.RetryCount)
	}
	if task.MaxAttempts != 2 {
		t.Errorf("MaxAttempts = %d, want 2 (1 retry + initial try)", task.MaxAttempts)
	}
	if !strings.Contains(task.LastError, "max retries exceeded") {
		t.Errorf("LastError = %q, want the max-retries residue", task.LastError)
	}
}