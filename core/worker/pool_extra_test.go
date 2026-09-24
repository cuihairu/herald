package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/runtime"
)

// poolStubProvider delivers successfully and reports itself available.
type poolStubProvider struct{}

func (poolStubProvider) Deliver(_ context.Context, _ *core.DeliveryTask) error { return nil }
func (poolStubProvider) Name() string                                          { return "stub" }
func (poolStubProvider) Type() string                                          { return "stub" }
func (poolStubProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: "stub", Type: "stub", Status: "available"}
}

type popStep struct {
	task *core.DeliveryTask
	err  error
}

// fakePoolQueue replays a scripted sequence of Pop results, records
// Ack/Nack activity as events, and blocks on ctx once the script runs out.
type fakePoolQueue struct {
	mu     sync.Mutex
	script []popStep
	nacks  []string
	ackErr error
	events chan string
}

func (q *fakePoolQueue) Push(_ context.Context, _ *core.DeliveryTask) error { return nil }

func (q *fakePoolQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	q.mu.Lock()
	if len(q.script) > 0 {
		step := q.script[0]
		q.script = q.script[1:]
		q.mu.Unlock()
		return step.task, step.err
	}
	q.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (q *fakePoolQueue) Ack(_ context.Context, taskID string) error {
	q.events <- "ack:" + taskID
	return q.ackErr
}

func (q *fakePoolQueue) Nack(_ context.Context, taskID string, _ error) error {
	q.mu.Lock()
	q.nacks = append(q.nacks, taskID)
	q.mu.Unlock()
	q.events <- "nack:" + taskID
	// The second scripted ghost task exercises the nack-failure logging path.
	if taskID == "t-ghost2" {
		return errors.New("nack boom")
	}
	return nil
}

func (q *fakePoolQueue) Size() int    { return 0 }
func (q *fakePoolQueue) Close() error { return nil }

func TestPoolWorkerLoopPaths(t *testing.T) {
	mgr := runtime.NewManager(10)
	if err := mgr.RegisterProvider("stub", poolStubProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	q := &fakePoolQueue{events: make(chan string, 16)}
	q.script = []popStep{
		{nil, errors.New("pop boom")},                                  // transient Pop failure → retry
		{&core.DeliveryTask{ID: "t-ghost", Provider: "no-such"}, nil},  // Deliver fails → Nack
		{&core.DeliveryTask{ID: "t-ghost2", Provider: "no-such"}, nil}, // Deliver fails → Nack error path
		{&core.DeliveryTask{ID: "t-ok", Provider: "stub"}, nil},        // Deliver ok → Ack error path
	}

	pool := NewPool(q, mgr, NewRegistry(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pool.Run(ctx); close(done) }()

	want := []string{"nack:t-ghost", "nack:t-ghost2", "ack:t-ok"}
	for _, w := range want {
		select {
		case got := <-q.events:
			if got != w {
				t.Errorf("event = %q, want %q", got, w)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for %q", w)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not stop after cancel")
	}
}

func TestPoolAckError(t *testing.T) {
	mgr := runtime.NewManager(10)
	if err := mgr.RegisterProvider("stub", poolStubProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	// A task that delivers fine but whose Ack fails exercises the
	// ack-failure logging path in workerLoop.
	q := &fakePoolQueue{events: make(chan string, 16), ackErr: errors.New("ack boom")}
	q.script = []popStep{
		{&core.DeliveryTask{ID: "t-ack", Provider: "stub"}, nil},
	}

	pool := NewPool(q, mgr, NewRegistry(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pool.Run(ctx); close(done) }()

	select {
	case got := <-q.events:
		if got != "ack:t-ack" {
			t.Errorf("event = %q, want %q", got, "ack:t-ack")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for %q", "ack:t-ack")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not stop after cancel")
	}
}

func TestPoolStaleWorkerSweep(t *testing.T) {
	orig := staleCheckInterval
	staleCheckInterval = 5 * time.Millisecond
	defer func() { staleCheckInterval = orig }()

	mgr := runtime.NewManager(10)
	if err := mgr.RegisterProvider("stub", poolStubProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	registry := NewRegistry()
	stale := &Info{ID: "remote-stale", Mode: Remote}
	if err := registry.Register(stale); err != nil {
		t.Fatalf("register stale worker: %v", err)
	}
	// Register stamps a fresh heartbeat, so backdate the entry afterwards.
	// The registry stores this same pointer, so the map entry ages with it.
	stale.LastHeartbeat = time.Now().Add(-5 * time.Minute)

	fresh := &Info{ID: "remote-fresh", Mode: Remote}
	if err := registry.Register(fresh); err != nil {
		t.Fatalf("register fresh worker: %v", err)
	}

	q := &fakePoolQueue{events: make(chan string, 8)}
	pool := NewPool(q, mgr, registry, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pool.Run(ctx); close(done) }()

	// The 5ms ticker must sweep the aged remote worker away.
	swept := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := registry.Get("remote-stale"); err != nil {
			swept = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !swept {
		t.Error("expected stale remote worker to be swept")
	}
	if _, err := registry.Get("remote-fresh"); err != nil {
		t.Error("expected fresh remote worker to survive the sweep")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not stop after cancel")
	}
}

// retireQueue hands the worker exactly one nil task — a closed queue's
// zero value — and signals when that Pop happened, so the test can assert
// on the worker's retirement without racing its microsecond-long lifecycle.
type retireQueue struct {
	once   sync.Once
	popped chan struct{}
}

func (q *retireQueue) Push(_ context.Context, _ *core.DeliveryTask) error { return nil }

func (q *retireQueue) Pop(context.Context) (*core.DeliveryTask, error) {
	q.once.Do(func() { close(q.popped) })
	return nil, nil
}

func (q *retireQueue) Ack(_ context.Context, _ string) error           { return nil }
func (q *retireQueue) Nack(_ context.Context, _ string, _ error) error { return nil }
func (q *retireQueue) Size() int                                       { return 0 }
func (q *retireQueue) Close() error                                    { return nil }

// TestPoolWorkerRetiresOnClosedQueue pins the shutdown semantics of a
// closed queue: memory queues pop their zero value once closed, and a
// worker must retire quietly instead of dereferencing the nil task.
func TestPoolWorkerRetiresOnClosedQueue(t *testing.T) {
	mgr := runtime.NewManager(10)
	if err := mgr.RegisterProvider("stub", poolStubProvider{}); err != nil {
		t.Fatalf("register provider: %v", err)
	}

	registry := NewRegistry()
	q := &retireQueue{popped: make(chan struct{})}

	pool := NewPool(q, mgr, registry, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { pool.Run(ctx); close(done) }()

	// Wait until the worker actually popped the nil task.
	select {
	case <-q.popped:
	case <-time.After(3 * time.Second):
		t.Fatal("worker never popped the closed queue's zero value")
	}

	// The context is still live here, so the only way the worker can leave
	// the registry is the nil-task retirement itself.
	retired := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := registry.Get("local-0"); err != nil {
			retired = true
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !retired {
		t.Fatal("worker must retire after the queue closed")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pool did not stop after cancel")
	}
}
