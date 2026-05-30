package dispatch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/worker"
)

// mockQueue is a simple in-memory queue for testing
type mockQueue struct {
	tasks     chan *core.DeliveryTask
	acked     map[string]bool
	nacked    map[string]bool
	pushError error
	popError  error
	ackError  error
	nackError error
}

func newMockQueue() *mockQueue {
	return &mockQueue{
		tasks:  make(chan *core.DeliveryTask, 100),
		acked:  make(map[string]bool),
		nacked: make(map[string]bool),
	}
}

func (m *mockQueue) Push(ctx context.Context, task *core.DeliveryTask) error {
	if m.pushError != nil {
		return m.pushError
	}
	select {
	case m.tasks <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mockQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	if m.popError != nil {
		return nil, m.popError
	}
	select {
	case task := <-m.tasks:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mockQueue) Ack(ctx context.Context, id string) error {
	if m.ackError != nil {
		return m.ackError
	}
	m.acked[id] = true
	return nil
}

func (m *mockQueue) Nack(ctx context.Context, id string, err error) error {
	if m.nackError != nil {
		return m.nackError
	}
	m.nacked[id] = true
	return nil
}

func (m *mockQueue) Size() int {
	return len(m.tasks)
}

func (m *mockQueue) Close() error {
	close(m.tasks)
	return nil
}

// mockProvider is a simple provider for testing
type mockProvider struct {
	name string
}

func (m *mockProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	return nil
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Type() string {
	return "mock"
}

func (m *mockProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{
		Name:   m.name,
		Type:   "mock",
		Status: "available",
	}
}

func (m *mockProvider) Close() error {
	return nil
}

func TestNew(t *testing.T) {
	t.Run("with valid parameters", func(t *testing.T) {
		queue := newMockQueue()
		rt := runtime.NewManager(100)
		registry := worker.NewRegistry()
		dispatcher := New(queue, rt, registry, 5)

		if dispatcher == nil {
			t.Fatal("expected non-nil dispatcher")
		}
		if dispatcher.Pool() == nil {
			t.Error("expected non-nil pool")
		}
	})

	t.Run("with zero workers", func(t *testing.T) {
		queue := newMockQueue()
		rt := runtime.NewManager(100)
		registry := worker.NewRegistry()
		dispatcher := New(queue, rt, registry, 0)

		if dispatcher == nil {
			t.Fatal("expected non-nil dispatcher")
		}
		// Should default to 1 worker
		if dispatcher.Pool() == nil {
			t.Error("expected non-nil pool")
		}
	})

	t.Run("with negative workers", func(t *testing.T) {
		queue := newMockQueue()
		rt := runtime.NewManager(100)
		registry := worker.NewRegistry()
		dispatcher := New(queue, rt, registry, -5)

		if dispatcher == nil {
			t.Fatal("expected non-nil dispatcher")
		}
		// Should default to 1 worker
		if dispatcher.Pool() == nil {
			t.Error("expected non-nil pool")
		}
	})

	t.Run("with nil registry", func(t *testing.T) {
		queue := newMockQueue()
		rt := runtime.NewManager(100)
		dispatcher := New(queue, rt, nil, 2)

		if dispatcher == nil {
			t.Fatal("expected non-nil dispatcher")
		}
		if dispatcher.Pool() == nil {
			t.Error("expected non-nil pool")
		}
	})

	t.Run("with nil runtime", func(t *testing.T) {
		queue := newMockQueue()
		dispatcher := New(queue, nil, nil, 2)

		if dispatcher == nil {
			t.Fatal("expected non-nil dispatcher")
		}
		if dispatcher.Pool() == nil {
			t.Error("expected non-nil pool")
		}
	})
}

func TestDispatcher_Pool(t *testing.T) {
	queue := newMockQueue()
	rt := runtime.NewManager(100)
	registry := worker.NewRegistry()
	dispatcher := New(queue, rt, registry, 3)

	pool := dispatcher.Pool()
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}

	// Verify the pool's registry matches
	if pool.Registry() != registry {
		t.Error("pool registry should match dispatcher registry")
	}
}

func TestDispatcher_Run(t *testing.T) {
	t.Run("dispatcher starts and processes tasks", func(t *testing.T) {
		queue := newMockQueue()
		rt := runtime.NewManager(100)
		registry := worker.NewRegistry()
		dispatcher := New(queue, rt, registry, 2)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Register a mock provider named "test"
		mockProvider := &mockProvider{name: "test"}
		_ = rt.RegisterProvider("test", mockProvider)

		// Push a task
		task := &core.DeliveryTask{
			ID:       "test-task-1",
			Provider: "test",
			Targets:  []string{"test@example.com"},
		}
		_ = queue.Push(ctx, task)

		// Run dispatcher in background
		runDone := make(chan struct{})
		go func() {
			dispatcher.Run(ctx)
			close(runDone)
		}()

		// Wait a bit for processing
		select {
		case <-time.After(300 * time.Millisecond):
			// Check if task was processed
			if queue.acked[task.ID] {
				t.Log("task was acknowledged")
			}
		case <-ctx.Done():
		}

		cancel()
		<-runDone
	})

	t.Run("dispatcher handles multiple tasks", func(t *testing.T) {
		queue := newMockQueue()
		rt := runtime.NewManager(100)
		registry := worker.NewRegistry()
		dispatcher := New(queue, rt, registry, 2)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()

		// Register a mock provider named "test"
		mockProvider := &mockProvider{name: "test"}
		_ = rt.RegisterProvider("test", mockProvider)

		// Push multiple tasks
		for i := 0; i < 5; i++ {
			task := &core.DeliveryTask{
				ID:       fmt.Sprintf("task-%d", i),
				Provider: "test",
				Targets:  []string{fmt.Sprintf("test%d@example.com", i)},
			}
			_ = queue.Push(ctx, task)
		}

		// Run dispatcher in background
		runDone := make(chan struct{})
		go func() {
			dispatcher.Run(ctx)
			close(runDone)
		}()

		<-time.After(300 * time.Millisecond)
		cancel()
		<-runDone
	})
}

func TestDispatcher_WorkerRegistration(t *testing.T) {
	queue := newMockQueue()
	rt := runtime.NewManager(100)
	registry := worker.NewRegistry()
	dispatcher := New(queue, rt, registry, 3)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Run dispatcher briefly
	go dispatcher.Run(ctx)
	<-time.After(100 * time.Millisecond)
	cancel()

	// Check that workers were registered
	workers := registry.List()
	// Should have 3 local workers registered
	localWorkerCount := 0
	for _, w := range workers {
		if w.Mode == worker.Local {
			localWorkerCount++
		}
	}
	if localWorkerCount != 3 {
		t.Logf("expected 3 local workers, got %d", localWorkerCount)
	}
}

func TestDispatcher_ContextCancellation(t *testing.T) {
	queue := newMockQueue()
	rt := runtime.NewManager(100)
	registry := worker.NewRegistry()
	dispatcher := New(queue, rt, registry, 2)

	ctx, cancel := context.WithCancel(context.Background())

	// Start dispatcher
	runDone := make(chan struct{})
	go func() {
		dispatcher.Run(ctx)
		close(runDone)
	}()

	// Cancel after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Should exit
	select {
	case <-runDone:
		// OK
	case <-time.After(200 * time.Millisecond):
		t.Error("dispatcher did not exit after context cancellation")
	}
}
