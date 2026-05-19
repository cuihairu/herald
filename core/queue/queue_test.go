package queue

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihaitao/herald/core"
)

func TestNewMemoryQueue(t *testing.T) {
	config := &QueueConfig{
		Size: 100,
	}

	q, err := NewMemoryQueue(config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if q == nil {
		t.Error("expected non-nil queue")
	}

	// Check size
	if q.Size() != 0 {
		t.Errorf("expected size 0, got %d", q.Size())
	}

	// Close queue
	if err := q.Close(); err != nil {
		t.Errorf("expected no error on close, got %v", err)
	}
}

func TestMemoryQueuePushPop(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
	defer q.Close()

	event := &core.Event{
		ID:      "test-1",
		Type:    "test.event",
		Labels:  map[string]string{"level": "info"},
	}

	ctx := context.Background()

	// Push event
	err := q.Push(ctx, event)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Pop event
	popped, err := q.Pop(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if popped == nil {
		t.Error("expected non-nil event")
		return
	}
	if popped.ID != event.ID {
		t.Errorf("expected ID %s, got %s", event.ID, popped.ID)
	}
}

func TestMemoryQueuePushTask(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
	defer q.Close()

	task := &core.Task{
		ID:       "task-1",
		Provider: "test",
		Title:    "Test Task",
		Body:     "Test Body",
	}

	ctx := context.Background()

	// Push task
	err := q.PushTask(ctx, task)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Pop task
	popped, err := q.PopTask(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if popped == nil {
		t.Error("expected non-nil task")
		return
	}
	if popped.ID != task.ID {
		t.Errorf("expected ID %s, got %s", task.ID, popped.ID)
	}
}

func TestMemoryQueueContextCancel(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
	defer q.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context before pop
	cancel()

	_, err := q.Pop(ctx)
	if err != context.Canceled {
		t.Errorf("expected context canceled error, got %v", err)
	}
}

func TestMemoryQueueClosed(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 100})

	// Close queue
	q.Close()

	event := &core.Event{
		ID:   "test",
		Type: "test",
	}

	ctx := context.Background()

	// Push to closed queue should error
	err := q.Push(ctx, event)
	if err != ErrQueueClosed {
		t.Errorf("expected ErrQueueClosed, got %v", err)
	}
}

func TestMemoryQueueSize(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 100})
	defer q.Close()

	ctx := context.Background()

	// Add multiple events
	for i := 0; i < 5; i++ {
		event := &core.Event{
			ID:   fmt.Sprintf("event-%d", i),
			Type: "test",
		}
		_ = q.Push(ctx, event)
	}

	// Check size
	size := q.Size()
	if size != 5 {
		t.Errorf("expected size 5, got %d", size)
	}
}

func TestMemoryQueueConcurrent(t *testing.T) {
	q, _ := NewMemoryQueue(&QueueConfig{Size: 1000})
	defer q.Close()

	ctx := context.Background()
	done := make(chan bool)

	// Producer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			event := &core.Event{
				ID:   fmt.Sprintf("event-%d", i),
				Type: "test",
			}
			_ = q.Push(ctx, event)
		}
		done <- true
	}()

	// Consumer goroutine
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

	// Wait for producer
	<-done

	// Wait a bit for consumer
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt64(&consumed) < 100 {
		t.Errorf("expected at least 100 consumed, got %d", consumed)
	}
}

func TestNewQueue(t *testing.T) {
	tests := []struct {
		name       string
		config     *core.QueueConfig
		expectError bool
	}{
		{
			name: "memory queue",
			config: &core.QueueConfig{
				Type: "memory",
				Size: 100,
			},
			expectError: false,
		},
		{
			name: "default config",
			config: &core.QueueConfig{},
			expectError: false,
		},
		{
			name: "unknown type",
			config: &core.QueueConfig{
				Type: "unknown",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := NewQueue(tt.config)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
				if q == nil {
					t.Error("expected non-nil queue")
				}
				if q != nil {
					q.Close()
				}
			}
		})
	}
}
