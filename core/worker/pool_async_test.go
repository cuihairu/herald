package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/errclass"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/runtime"
)

// schedQueue is a fake queue that reports the Scheduler capability and
// records what the pool asked it to hold. Scheduled tasks stay held until
// release() requeues them, standing in for the queue honoring deadlines.
type schedQueue struct {
	mu       sync.Mutex
	tasks    []*core.DeliveryTask
	held     []*core.DeliveryTask
	heldFor  []time.Duration
	schedule error // returned by Schedule once armed
	nackErr  error // returned by Nack once armed
	acked    []string
	nacked   []string
}

func (q *schedQueue) Push(_ context.Context, task *core.DeliveryTask) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
	return nil
}

func (q *schedQueue) Pop(_ context.Context) (*core.DeliveryTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) == 0 {
		return nil, errQueueEmpty
	}
	t := q.tasks[0]
	q.tasks = q.tasks[1:]
	return t, nil
}

func (q *schedQueue) Schedule(_ context.Context, task *core.DeliveryTask, delay time.Duration) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.schedule != nil {
		return q.schedule
	}
	q.held = append(q.held, task)
	q.heldFor = append(q.heldFor, delay)
	return nil
}

func (q *schedQueue) release() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, q.held...)
	q.held = nil
}

func (q *schedQueue) Ack(_ context.Context, taskID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acked = append(q.acked, taskID)
	return nil
}

func (q *schedQueue) Nack(_ context.Context, taskID string, _ error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nacked = append(q.nacked, taskID)
	return q.nackErr
}

func (q *schedQueue) Size() int { return 0 }

func (q *schedQueue) Close() error { return nil }

func (q *schedQueue) snapshot() (held int, heldFor []time.Duration, acked, nacked []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.held), append([]time.Duration(nil), q.heldFor...), q.acked, q.nacked
}

// flipProvider fails its first N deliveries with a retryable error, then
// succeeds — the async shape of "fails once, recovers".
type flipProvider struct {
	mu       sync.Mutex
	name     string
	remain   int
	taskMemo []*core.DeliveryTask
}

func (p *flipProvider) Deliver(_ context.Context, task *core.DeliveryTask) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.taskMemo = append(p.taskMemo, task)
	if p.remain > 0 {
		p.remain--
		return errclass.New(errclass.RateLimited, errors.New("429"))
	}
	return nil
}

func (p *flipProvider) Name() string { return p.name }
func (p *flipProvider) Type() string { return "flip" }
func (p *flipProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: p.name, Type: "flip", Status: "available"}
}

func (p *flipProvider) attempts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.taskMemo)
}

// runAsyncPool starts a one-worker pool over a scheduler queue and
// registers cleanup.
func runAsyncPool(t *testing.T, q *schedQueue, m *runtime.Manager) {
	t.Helper()
	p := NewPool(q, m, NewRegistry(), 1)
	if p.scheduler == nil {
		t.Fatal("pool did not pick up the Scheduler capability")
	}
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

// waitFor polls until cond reports true, failing the test at the deadline.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestPoolAsyncReenqueuesRetry: a retryable failure goes back to the queue
// with its stamped advice — no Nack (nothing failed terminally), the worker
// moves on, and the released task is delivered on its next attempt.
func TestPoolAsyncReenqueuesRetry(t *testing.T) {
	q := &schedQueue{}
	p := &flipProvider{name: "flaky", remain: 1}
	m := runtime.NewManager(100, &retry.Config{Max: 2, Backoff: "fixed", InitialDelay: time.Hour})
	if err := m.RegisterProvider("flaky", p); err != nil {
		t.Fatal(err)
	}
	runAsyncPool(t, q, m)

	if err := q.Push(context.Background(), &core.DeliveryTask{ID: "t1", Provider: "flaky"}); err != nil {
		t.Fatal(err)
	}

	// First attempt fails: the task is held with the Retry-After/policy
	// delay, and nothing was nacked.
	waitFor(t, func() bool {
		held, _, _, nacked := q.snapshot()
		return held == 1 && len(nacked) == 0
	}, "task was not re-enqueued")
	_, heldFor, _, _ := q.snapshot()
	if len(heldFor) != 1 || heldFor[0] != time.Hour {
		t.Fatalf("held for %v, want the policy delay 1h", heldFor)
	}

	// The queue's deadline elapses: the task comes back and succeeds.
	q.release()
	waitFor(t, func() bool {
		_, _, acked, _ := q.snapshot()
		return len(acked) == 1
	}, "task was not delivered after re-enqueue")
	if got := p.attempts(); got != 2 {
		t.Errorf("provider attempts = %d, want 2", got)
	}
	if _, _, _, nacked := q.snapshot(); len(nacked) != 0 {
		t.Errorf("nacks = %v, want none (a deferred retry is not a failure)", nacked)
	}
}

// TestPoolAsyncScheduleFailureNacks: when the queue refuses the re-enqueue
// the task must not vanish into limbo — it is nacked so waiters settle.
func TestPoolAsyncScheduleFailureNacks(t *testing.T) {
	q := &schedQueue{}
	q.mu.Lock()
	q.schedule = errors.New("redis down")
	q.mu.Unlock()
	p := &flipProvider{name: "flaky", remain: 1}
	m := runtime.NewManager(100, &retry.Config{Max: 2, Backoff: "fixed", InitialDelay: time.Hour})
	if err := m.RegisterProvider("flaky", p); err != nil {
		t.Fatal(err)
	}
	runAsyncPool(t, q, m)

	if err := q.Push(context.Background(), &core.DeliveryTask{ID: "t1", Provider: "flaky"}); err != nil {
		t.Fatal(err)
	}

	waitFor(t, func() bool {
		_, _, _, nacked := q.snapshot()
		return len(nacked) == 1
	}, "task was not nacked after the schedule refusal")
}

// TestPoolAsyncNackFailureKeepsRunning: even when the queue also refuses
// the Nack, the worker logs and carries on — the next task is still
// delivered.
func TestPoolAsyncNackFailureKeepsRunning(t *testing.T) {
	q := &schedQueue{}
	q.mu.Lock()
	q.schedule = errors.New("redis down")
	q.nackErr = errors.New("redis still down")
	q.mu.Unlock()
	p := &flipProvider{name: "flaky", remain: 1}
	m := runtime.NewManager(100, &retry.Config{Max: 2, Backoff: "fixed", InitialDelay: time.Hour})
	if err := m.RegisterProvider("flaky", p); err != nil {
		t.Fatal(err)
	}
	runAsyncPool(t, q, m)

	if err := q.Push(context.Background(), &core.DeliveryTask{ID: "t1", Provider: "flaky"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, _, _, nacked := q.snapshot()
		return len(nacked) == 1
	}, "task was not nacked after the schedule refusal")

	// The worker survived the failed Nack: the next task goes through.
	if err := q.Push(context.Background(), &core.DeliveryTask{ID: "t2", Provider: "flaky"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, _, acked, _ := q.snapshot()
		return len(acked) == 1
	}, "worker did not deliver the next task after a failed Nack")
	if got := p.attempts(); got != 2 {
		t.Errorf("provider attempts = %d, want 2 (t1 failed, t2 delivered)", got)
	}
}

// TestPoolSyncFallbackWithoutScheduler: a queue without the capability
// keeps the historical synchronous backoff — the manager's Deliver runs
// the whole retry curve in one call and only terminal outcomes settle.
func TestPoolSyncFallbackWithoutScheduler(t *testing.T) {
	q := newMemQueue(false)
	p := &flipProvider{name: "flaky", remain: 1}
	m := runtime.NewManager(100, &retry.Config{Max: 2, Backoff: "fixed", InitialDelay: time.Millisecond})
	if err := m.RegisterProvider("flaky", p); err != nil {
		t.Fatal(err)
	}
	runPool(t, q, m)

	if err := q.Push(context.Background(), &core.DeliveryTask{ID: "t1", Provider: "flaky"}); err != nil {
		t.Fatal(err)
	}

	// One Deliver call carries both attempts, so the single task settles
	// as acked without ever being re-enqueued.
	waitFor(t, func() bool {
		acked, nacked, popped := q.counts()
		return popped >= 1 && len(acked) == 1 && len(nacked) == 0
	}, "task did not settle through the sync path")
	if got := p.attempts(); got != 2 {
		t.Errorf("provider attempts = %d, want both in one Deliver", got)
	}
}

// TestPoolSchedulerDetection: only queues that implement Scheduler switch
// the pool into the async mode.
func TestPoolSchedulerDetection(t *testing.T) {
	if p := NewPool(&schedQueue{}, nil, NewRegistry(), 1); p.scheduler == nil {
		t.Error("schedQueue should enable the async mode")
	}
	if p := NewPool(newMemQueue(false), nil, NewRegistry(), 1); p.scheduler != nil {
		t.Error("memQueue should keep the sync mode")
	}
}
