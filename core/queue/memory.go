package queue

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
)

type memoryQueue struct {
	tasks  chan *core.DeliveryTask
	mu     sync.RWMutex
	closed bool
	// delayed holds tasks scheduled for the future (Schedule), earliest
	// deadline first; Pop only returns one once its deadline has passed.
	// It has its own mutex on purpose: Push holds mu.RLock while blocked
	// on a full channel, so Pop's hot path must never need mu.Lock — a
	// write lock here would deadlock a full queue against its own pusher.
	delayed   delayedHeap
	delayedMu sync.Mutex
}

// Now and the backoff-wait fire are package variables so tests can pin the
// clock and observe a scheduled wait without sleeping it (same seam pattern
// as core/retry's after).
var (
	now   = time.Now
	after = time.After
)

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

// Schedule holds the task aside until the delay has passed; a non-positive
// delay goes straight onto the ready queue. Pop (below) is what releases
// due entries — no background timer runs between calls. Closing the queue
// abandons still-delayed tasks the way it stops serving the ready channel.
func (q *memoryQueue) Schedule(ctx context.Context, task *core.DeliveryTask, delay time.Duration) error {
	q.mu.RLock()
	closed := q.closed
	q.mu.RUnlock()
	if closed {
		return ErrQueueClosed
	}

	if delay <= 0 {
		return q.Push(ctx, task)
	}

	q.delayedMu.Lock()
	heap.Push(&q.delayed, &delayedEntry{due: now().Add(delay), task: task})
	q.delayedMu.Unlock()
	return nil
}

func (q *memoryQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	for {
		// Release everything already due before touching the ready
		// channel, so a timed-out scheduled task is not starved behind a
		// steady stream of fresh pushes.
		q.delayedMu.Lock()
		if len(q.delayed) > 0 && !q.delayed[0].due.After(now()) {
			task := heap.Pop(&q.delayed).(*delayedEntry).task
			q.delayedMu.Unlock()
			return task, nil
		}
		q.delayedMu.Unlock()

		// Nothing due: wait for a fresh push, the next scheduled deadline,
		// or the caller giving up.
		q.delayedMu.Lock()
		var fire <-chan time.Time
		if len(q.delayed) > 0 {
			// due.Sub(now()) rather than time.Until: the wait must be
			// measured on the same clock that stamped the deadline, or a
			// pinned test clock drifts against the real one and the
			// deadline never arrives. In production now == time.Now, so
			// this is exactly time.Until.
			fire = after(q.delayed[0].due.Sub(now()))
		}
		q.delayedMu.Unlock()

		select {
		case task := <-q.tasks:
			return task, nil
		case <-fire:
			// Loop: the earliest entry is due now.
		case <-ctx.Done():
			return nil, ctx.Err()
		}
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

// delayedEntry is one scheduled task; delayedHeap orders entries by due
// time (min-heap).
type delayedEntry struct {
	due  time.Time
	task *core.DeliveryTask
}

type delayedHeap []*delayedEntry

func (h delayedHeap) Len() int           { return len(h) }
func (h delayedHeap) Less(i, j int) bool { return h[i].due.Before(h[j].due) }
func (h delayedHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *delayedHeap) Push(x any)        { *h = append(*h, x.(*delayedEntry)) }
func (h *delayedHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return item
}
