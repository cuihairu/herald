package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/runtime"
)

var errQueueEmpty = errors.New("queue empty")

type memQueue struct {
	mu        sync.Mutex
	tasks     []*core.DeliveryTask
	byID      map[string]*core.DeliveryTask
	redeliver bool
	popped    []string
	acked     []string
	nacked    []string
}

func newMemQueue(redeliver bool) *memQueue {
	return &memQueue{byID: make(map[string]*core.DeliveryTask), redeliver: redeliver}
}

func (q *memQueue) Push(ctx context.Context, task *core.DeliveryTask) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
	q.byID[task.ID] = task
	return nil
}

func (q *memQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) == 0 {
		return nil, errQueueEmpty
	}
	t := q.tasks[0]
	q.tasks = q.tasks[1:]
	q.popped = append(q.popped, t.ID)
	return t, nil
}

func (q *memQueue) Ack(ctx context.Context, taskID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acked = append(q.acked, taskID)
	return nil
}

func (q *memQueue) Nack(ctx context.Context, taskID string, reason error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nacked = append(q.nacked, taskID)
	if q.redeliver {
		if task, ok := q.byID[taskID]; ok {
			q.tasks = append(q.tasks, task)
		}
	}
	return nil
}

func (q *memQueue) Size() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

func (q *memQueue) Close() error {
	return nil
}

func (q *memQueue) counts() (acked map[string]int, nacked map[string]int, popped int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	acked = make(map[string]int)
	nacked = make(map[string]int)
	for _, id := range q.acked {
		acked[id]++
	}
	for _, id := range q.nacked {
		nacked[id]++
	}
	return acked, nacked, len(q.popped)
}

type stubProvider struct {
	err error
}

func (p *stubProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	return p.err
}

func (p *stubProvider) Name() string { return "stub" }

func (p *stubProvider) Type() string { return "stub" }

func (p *stubProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{}
}

func TestNewPoolDefaultsWorkers(t *testing.T) {
	t.Run("zero workers", func(t *testing.T) {
		p := NewPool(nil, nil, NewRegistry(), 0)
		if p.workers != 1 {
			t.Errorf("workers = %d, want 1", p.workers)
		}
	})
	t.Run("negative workers", func(t *testing.T) {
		p := NewPool(nil, nil, NewRegistry(), -3)
		if p.workers != 1 {
			t.Errorf("workers = %d, want 1", p.workers)
		}
	})
	t.Run("explicit workers", func(t *testing.T) {
		p := NewPool(nil, nil, NewRegistry(), 4)
		if p.workers != 4 {
			t.Errorf("workers = %d, want 4", p.workers)
		}
	})
}

func TestPoolRegistry(t *testing.T) {
	reg := NewRegistry()
	p := NewPool(nil, nil, reg, 1)
	if p.Registry() != reg {
		t.Error("Registry() should return the registry passed to NewPool")
	}
}

func TestPoolRunLifecycle(t *testing.T) {
	reg := NewRegistry()
	q := newMemQueue(false)
	m := runtime.NewManager(10)
	p := NewPool(q, m, reg, 2)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}

	if reg.Count() != 0 {
		t.Errorf("registry Count() = %d, want 0 after shutdown", reg.Count())
	}
}

func TestPoolRunProcessesTasks(t *testing.T) {
	reg := NewRegistry()
	q := newMemQueue(false)
	m := runtime.NewManager(10)
	if err := m.RegisterProvider("ok", &stubProvider{}, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}
	if err := m.RegisterProvider("bad", &stubProvider{err: errors.New("boom")}, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}

	ctx := context.Background()
	for _, task := range []*core.DeliveryTask{
		{ID: "t-ok", Provider: "ok"},
		{ID: "t-bad", Provider: "bad"},
		{ID: "t-unknown", Provider: "unknown"},
	} {
		if err := q.Push(ctx, task); err != nil {
			t.Fatalf("Push() error = %v", err)
		}
	}

	p := NewPool(q, m, reg, 2)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		p.Run(runCtx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		acked, nacked, popped := q.counts()
		if popped == 3 && len(acked) == 1 && len(nacked) == 2 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("tasks not settled: popped=%d acked=%v nacked=%v", popped, acked, nacked)
		}
		time.Sleep(10 * time.Millisecond)
	}

	acked, nacked, _ := q.counts()
	if acked["t-ok"] != 1 {
		t.Errorf("acked = %v, want t-ok acked once", acked)
	}
	if nacked["t-bad"] != 1 {
		t.Errorf("nacked = %v, want t-bad nacked once", nacked)
	}
	if nacked["t-unknown"] != 1 {
		t.Errorf("nacked = %v, want t-unknown nacked once", nacked)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
	if reg.Count() != 0 {
		t.Errorf("registry Count() = %d, want 0 after shutdown", reg.Count())
	}
}

func TestPoolRunRegistersLocalWorkers(t *testing.T) {
	reg := NewRegistry()
	q := newMemQueue(false)
	m := runtime.NewManager(10)
	p := NewPool(q, m, reg, 3)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for reg.Count() < 3 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("registry Count() = %d, want 3", reg.Count())
		}
		time.Sleep(5 * time.Millisecond)
	}

	locals := reg.ListByMode(Local)
	if len(locals) != 3 {
		t.Errorf("ListByMode(Local) length = %d, want 3", len(locals))
	}
	for _, w := range locals {
		if w.Status != "online" {
			t.Errorf("worker %s status = %q, want online", w.ID, w.Status)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}

func TestPoolWorkerLoopDeliveryFailureRetriesTask(t *testing.T) {
	reg := NewRegistry()
	q := newMemQueue(true)
	m := runtime.NewManager(10)
	if err := m.RegisterProvider("flaky", &stubProvider{err: errors.New("always fails")}, true); err != nil {
		t.Fatalf("RegisterProvider() error = %v", err)
	}

	task := &core.DeliveryTask{ID: fmt.Sprintf("t-flaky-%d", time.Now().UnixNano()), Provider: "flaky"}
	if err := q.Push(context.Background(), task); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	p := NewPool(q, m, reg, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		_, nacked, _ := q.counts()
		if nacked[task.ID] >= 2 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("task not nacked twice, nacked count = %d", nacked[task.ID])
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
