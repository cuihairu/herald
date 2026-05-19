package queue

import (
	"context"
	"sync"

	"github.com/cuihairu/herald/core"
)

// memoryQueue is an in-memory queue implementation
type memoryQueue struct {
	events chan *core.Event
	tasks  chan *core.Task
	mu     sync.RWMutex
	closed bool
}

// NewMemoryQueue creates a new in-memory queue
func NewMemoryQueue(config *QueueConfig) (Queue, error) {
	size := config.Size
	if size <= 0 {
		size = 10000
	}

	return &memoryQueue{
		events: make(chan *core.Event, size),
		tasks:  make(chan *core.Task, size),
	}, nil
}

// Push pushes an event to the queue
func (q *memoryQueue) Push(ctx context.Context, event *core.Event) error {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.closed {
		return ErrQueueClosed
	}

	select {
	case q.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Pop pops an event from the queue
func (q *memoryQueue) Pop(ctx context.Context) (*core.Event, error) {
	select {
	case event := <-q.events:
		return event, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// PushTask pushes a task to the queue
func (q *memoryQueue) PushTask(ctx context.Context, task *core.Task) error {
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

// PopTask pops a task from the queue
func (q *memoryQueue) PopTask(ctx context.Context) (*core.Task, error) {
	select {
	case task := <-q.tasks:
		return task, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Size returns the current queue size
func (q *memoryQueue) Size() int {
	return len(q.events) + len(q.tasks)
}

// Close closes the queue
func (q *memoryQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil
	}

	q.closed = true
	close(q.events)
	close(q.tasks)

	return nil
}

// Errors
var (
	ErrQueueClosed = &QueueError{Message: "queue is closed"}
)

// QueueError is a queue error
type QueueError struct {
	Message string
}

func (e *QueueError) Error() string {
	return e.Message
}
