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
