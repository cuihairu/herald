package queue

import (
	"context"
	"sync"

	"github.com/cuihairu/herald/core"
)

type memoryQueue struct {
	tasks  chan *core.DeliveryTask
	mu     sync.RWMutex
	closed bool
}

// NewMemoryQueue creates a new in-memory queue
func NewMemoryQueue(config *QueueConfig) (core.Queue, error) {
	size := config.Size
	if size <= 0 {
		size = 10000
	}

	return &memoryQueue{
		tasks: make(chan *core.DeliveryTask, size),
	}, nil
}

func (q *memoryQueue) Push(ctx context.Context, task *core.DeliveryTask) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return ErrQueueClosed
	}

	select {
	case q.tasks <- task:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *memoryQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	select {
	case task := <-q.tasks:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *memoryQueue) Size() int {
	return len(q.tasks)
}

func (q *memoryQueue) Ack(_ context.Context, _ string) error {
	return nil
}

func (q *memoryQueue) Nack(_ context.Context, _ string, _ error) error {
	return nil
}

func (q *memoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true
	close(q.tasks)
	return nil
}

var (
	ErrQueueClosed = &QueueError{Message: "queue is closed"}
)

type QueueError struct {
	Message string
}

func (e *QueueError) Error() string {
	return e.Message
}
