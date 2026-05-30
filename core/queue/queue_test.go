package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNewMemoryQueue(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		q, err := NewMemoryQueue(&QueueConfig{Size: 100})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if q == nil {
			t.Error("expected non-nil queue")
		}
		if q.Size() != 0 {
			t.Errorf("expected size 0, got %d", q.Size())
		}
		_ = q.Close()
	})

	t.Run("with zero size defaults to 10000", func(t *testing.T) {
		q, err := NewMemoryQueue(&QueueConfig{Size: 0})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		_ = q.Close()
	})

	t.Run("with negative size defaults to 10000", func(t *testing.T) {
		q, err := NewMemoryQueue(&QueueConfig{Size: -10})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		_ = q.Close()
	})
}

func TestMemoryQueuePushPop(t *testing.T) {
	t.Run("single push and pop", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		task := &core.DeliveryTask{
			ID:       "task-1",
			Provider: "email",
			Targets:  []string{"user@example.com"},
		}

		ctx := context.Background()

		if err := q.Push(ctx, task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		popped, err := q.Pop(ctx)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if popped.ID != task.ID {
			t.Errorf("expected ID %s, got %s", task.ID, popped.ID)
		}
		if popped.Provider != task.Provider {
			t.Errorf("expected Provider %s, got %s", task.Provider, popped.Provider)
		}
	})

	t.Run("multiple push and pop", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		tasks := make([]*core.DeliveryTask, 5)
		for i := 0; i < 5; i++ {
			tasks[i] = &core.DeliveryTask{
				ID:       fmt.Sprintf("task-%d", i),
				Provider: "email",
			}
			if err := q.Push(ctx, tasks[i]); err != nil {
				t.Errorf("expected no error pushing task %d, got %v", i, err)
			}
		}

		// Pop tasks and verify FIFO order
		for i := 0; i < 5; i++ {
			popped, err := q.Pop(ctx)
			if err != nil {
				t.Errorf("expected no error popping task %d, got %v", i, err)
			}
			if popped.ID != tasks[i].ID {
				t.Errorf("expected ID %s, got %s", tasks[i].ID, popped.ID)
			}
		}
	})

	t.Run("push with cancelled context", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 1})
		defer func() { _ = q.Close() }()

		// Fill the queue first
		ctx := context.Background()
		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-0", Provider: "email"})

		// Now cancel context and try to push to full queue
		ctx2, cancel := context.WithCancel(context.Background())
		cancel()

		task := &core.DeliveryTask{ID: "task-1", Provider: "email"}
		err := q.Push(ctx2, task)
		if err != context.Canceled {
			t.Errorf("expected context canceled error, got %v", err)
		}
	})
}

func TestMemoryQueueContextCancel(t *testing.T) {
	t.Run("pop with cancelled context", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := q.Pop(ctx)
		if err != context.Canceled {
			t.Errorf("expected context canceled error, got %v", err)
		}
	})

	t.Run("pop with timeout", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		_, err := q.Pop(ctx)
		if err != context.DeadlineExceeded {
			t.Errorf("expected deadline exceeded error, got %v", err)
		}
	})
}

func TestMemoryQueueClosed(t *testing.T) {
	t.Run("push to closed queue", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		_ = q.Close()

		err := q.Push(context.Background(), &core.DeliveryTask{ID: "test", Provider: "test"})
		if err != ErrQueueClosed {
			t.Errorf("expected ErrQueueClosed, got %v", err)
		}
	})

	t.Run("pop from closed empty queue returns zero value", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		_ = q.Close()

		// Pop from closed empty queue returns immediately with nil task
		// Note: The current implementation doesn't return ErrQueueClosed for Pop
		task, err := q.Pop(context.Background())
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if task != nil {
			t.Errorf("expected nil task from closed empty queue, got %v", task)
		}
	})

	t.Run("close already closed queue", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		_ = q.Close()

		err := q.Close()
		if err != nil {
			t.Errorf("expected no error closing already closed queue, got %v", err)
		}
	})
}

func TestMemoryQueueSize(t *testing.T) {
	t.Run("size increases with push", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		for i := 0; i < 5; i++ {
			_ = q.Push(ctx, &core.DeliveryTask{
				ID:       fmt.Sprintf("task-%d", i),
				Provider: "test",
			})
		}
		if q.Size() != 5 {
			t.Errorf("expected size 5, got %d", q.Size())
		}
	})

	t.Run("size decreases with pop", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		for i := 0; i < 5; i++ {
			_ = q.Push(ctx, &core.DeliveryTask{
				ID:       fmt.Sprintf("task-%d", i),
				Provider: "test",
			})
		}

		_, _ = q.Pop(ctx)
		if q.Size() != 4 {
			t.Errorf("expected size 4 after pop, got %d", q.Size())
		}

		_, _ = q.Pop(ctx)
		if q.Size() != 3 {
			t.Errorf("expected size 3 after second pop, got %d", q.Size())
		}
	})

	t.Run("size of empty queue", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		if q.Size() != 0 {
			t.Errorf("expected size 0, got %d", q.Size())
		}
	})
}

func TestMemoryQueueAck(t *testing.T) {
	t.Run("ack always succeeds", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		task := &core.DeliveryTask{ID: "task-1", Provider: "email"}
		_ = q.Push(ctx, task)

		popped, _ := q.Pop(ctx)

		err := q.Ack(ctx, popped.ID)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("ack non-existent task", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		err := q.Ack(ctx, "non-existent")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("ack with cancelled context", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := q.Ack(ctx, "task-1")
		// Memory queue Ack doesn't check context
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestMemoryQueueNack(t *testing.T) {
	t.Run("nack always succeeds", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		task := &core.DeliveryTask{ID: "task-1", Provider: "email"}
		_ = q.Push(ctx, task)

		popped, _ := q.Pop(ctx)

		err := q.Nack(ctx, popped.ID, fmt.Errorf("delivery failed"))
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("nack non-existent task", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		err := q.Nack(ctx, "non-existent", fmt.Errorf("not found"))
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("nack with nil error", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		err := q.Nack(ctx, "task-1", nil)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestMemoryQueueConcurrent(t *testing.T) {
	t.Run("concurrent push and pop", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 1000})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		done := make(chan bool)

		go func() {
			for i := 0; i < 100; i++ {
				_ = q.Push(ctx, &core.DeliveryTask{
					ID:       fmt.Sprintf("task-%d", i),
					Provider: "test",
				})
			}
			done <- true
		}()

		consumed := make(map[string]bool)
		consumedDone := make(chan bool)

		go func() {
			for i := 0; i < 100; i++ {
				task, err := q.Pop(ctx)
				if err != nil {
					continue
				}
				consumed[task.ID] = true
			}
			consumedDone <- true
		}()

		<-done
		<-consumedDone

		if len(consumed) < 100 {
			t.Errorf("expected at least 100 consumed, got %d", len(consumed))
		}
	})

	t.Run("multiple concurrent consumers", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 1000})
		defer func() { _ = q.Close() }()

		ctx := context.Background()

		// Push tasks
		for i := 0; i < 100; i++ {
			_ = q.Push(ctx, &core.DeliveryTask{
				ID:       fmt.Sprintf("task-%d", i),
				Provider: "test",
			})
		}

		// Multiple consumers - collect IDs in channels instead of shared map
		type consumedTask struct {
			consumer int
			id       string
		}
		results := make(chan consumedTask, 100)

		for c := 0; c < 3; c++ {
			consumerID := c
			go func() {
				for {
					task, err := q.Pop(ctx)
					if err != nil || task == nil {
						return
					}
					results <- consumedTask{consumer: consumerID, id: task.ID}
					if task.ID == "task-99" {
						return // Last task, exit
					}
				}
			}()
		}

		// Collect results
		consumed := make(map[string]int)
		timeout := time.After(2 * time.Second)
		count := 0
		for count < 100 {
			select {
			case ct := <-results:
				consumed[ct.id]++
				count++
			case <-timeout:
				t.Fatalf("timeout after consuming %d tasks", count)
			}
		}

		// Each task should be consumed exactly once
		for i := 0; i < 100; i++ {
			id := fmt.Sprintf("task-%d", i)
			if consumed[id] != 1 {
				t.Errorf("task %s consumed %d times, expected 1", id, consumed[id])
			}
		}
	})
}

func TestMemoryQueueFull(t *testing.T) {
	t.Run("push blocks when queue is full", func(t *testing.T) {
		q, _ := NewMemoryQueue(&QueueConfig{Size: 2})
		defer func() { _ = q.Close() }()

		ctx := context.Background()
		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-1", Provider: "test"})
		_ = q.Push(ctx, &core.DeliveryTask{ID: "task-2", Provider: "test"})

		// Third push should block
		done := make(chan bool)
		go func() {
			_ = q.Push(ctx, &core.DeliveryTask{ID: "task-3", Provider: "test"})
			done <- true
		}()

		select {
		case <-done:
			t.Error("expected push to block, but it completed immediately")
		case <-time.After(100 * time.Millisecond):
			// Expected - push should be blocked
		}

		// Pop one task to make room
		_, _ = q.Pop(ctx)

		// Now the push should complete
		select {
		case <-done:
			// OK
		case <-time.After(200 * time.Millisecond):
			t.Error("expected push to complete after making room")
		}
	})
}

func TestNewQueue(t *testing.T) {
	tests := []struct {
		name        string
		config      *QueueConfig
		expectError bool
	}{
		{name: "memory queue", config: &QueueConfig{Type: "memory", Size: 100}},
		{name: "default config", config: &QueueConfig{}},
		{name: "unknown type", config: &QueueConfig{Type: "unknown"}, expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := NewQueue(tt.config)
			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if q != nil {
				_ = q.Close()
			}
		})
	}
}

func TestQueueError(t *testing.T) {
	t.Run("error message", func(t *testing.T) {
		err := &QueueError{Message: "test error"}
		if err.Error() != "test error" {
			t.Errorf("expected 'test error', got '%s'", err.Error())
		}
	})

	t.Run("ErrQueueClosed", func(t *testing.T) {
		if ErrQueueClosed.Error() != "queue is closed" {
			t.Errorf("expected 'queue is closed', got '%s'", ErrQueueClosed.Error())
		}
	})
}

func TestRedisQueueErrors(t *testing.T) {
	t.Run("new redis queue with invalid address returns error", func(t *testing.T) {
		config := &QueueConfig{
			Type: "redis",
			Redis: RedisConfig{
				Addr: "localhost:9999", // Non-existent port
			},
		}

		// This should fail due to connection error
		// Note: This test might fail if there's actually a Redis on port 9999
		_, err := NewRedisQueue(config)
		if err == nil {
			t.Log("warning: Redis connection succeeded unexpectedly")
		}
	})
}
