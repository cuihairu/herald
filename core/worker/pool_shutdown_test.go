package worker

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/runtime"
)

// TestPoolRunDeregistersWorkersOnShutdown pins the shutdown contract that
// TestPoolRunLifecycle / TestPoolRunProcessesTasks only check by luck: Run()
// must not return while a worker is still registered.
//
// The pool parks every worker inside fakePoolQueue.Pop (which blocks until
// ctx fires, then reports ctx.Err()), so cancellation lands precisely in
// workerLoop's Pop error branch. That branch used to `return` without
// deregistering — Run() then returned with the entry still in the registry
// and the post-shutdown Count() assertion flaked at roughly the rate the
// window was hit. workerLoop now deregisters via defer on every exit path.
func TestPoolRunDeregistersWorkersOnShutdown(t *testing.T) {
	const workers = 3

	reg := NewRegistry()
	q := &fakePoolQueue{events: make(chan string, 4)}
	pool := NewPool(q, runtime.NewManager(10), reg, workers)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		pool.Run(ctx)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for reg.Count() < workers {
		if time.Now().After(deadline) {
			t.Fatalf("registry Count() = %d, want %d after startup", reg.Count(), workers)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
	if got := reg.Count(); got != 0 {
		t.Errorf("registry Count() = %d, want 0 after shutdown", got)
	}
}
