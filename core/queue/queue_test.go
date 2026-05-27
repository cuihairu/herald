package queue

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNewMemoryQueue(t *testing.T) {
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
}

func TestMemoryQueuePushPop(t *testing.T) {
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
}

func TestMemoryQueueContextCancel(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
	defer func() { _ = q.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := q.Pop(ctx)
	if err != context.Canceled {
		t.Errorf("expected context canceled error, got %v", err)
	}
}

func TestMemoryQueueClosed(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
	_ = q.Close()

	err := q.Push(context.Background(), &core.DeliveryTask{ID: "test", Provider: "test"})
	if err != ErrQueueClosed {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
}

func TestMemoryQueueSize(t *testing.T) {
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
}

func TestMemoryQueueConcurrent(t *testing.T) {
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

	var consumed int64
	go func() {
		for {
			_, err := q.Pop(ctx)
			if err != nil {
				return
			}
			atomic.AddInt64(&consumed, 1)
		}
	}()

	<-done
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&consumed) < 100 {
		t.Errorf("expected at least 100 consumed, got %d", consumed)
	}
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
