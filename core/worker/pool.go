package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/internal/logger"
)

// Pool manages a set of workers that consume tasks from a queue
type Pool struct {
	queue    core.Queue
	runtime  *runtime.Manager
	registry *Registry
	workers  int
	wg       sync.WaitGroup
	// scheduler is non-nil when the queue can hold tasks aside for a
	// future time: retries then wait in the queue (async re-enqueue,
	// NextRetryAt stamped on the task) instead of sleeping inside a
	// worker. Queues without the capability keep the synchronous in-worker
	// backoff.
	scheduler core.Scheduler
}

// NewPool creates a new worker pool
func NewPool(queue core.Queue, runtime *runtime.Manager, registry *Registry, workers int) *Pool {
	if workers <= 0 {
		workers = 1
	}
	var scheduler core.Scheduler
	if s, ok := queue.(core.Scheduler); ok {
		scheduler = s
	}
	return &Pool{
		queue:     queue,
		runtime:   runtime,
		registry:  registry,
		workers:   workers,
		scheduler: scheduler,
	}
}

// Run starts the worker pool and blocks until context is cancelled
func (p *Pool) Run(ctx context.Context) {
	// Start stale worker checker
	p.wg.Add(1)
	go p.checkStaleWorkers(ctx)

	// Start local worker goroutines
	for i := 0; i < p.workers; i++ {
		workerID := fmt.Sprintf("local-%d", i)
		_ = p.registry.Register(&Info{
			ID:           workerID,
			Mode:         Local,
			Capabilities: []string{"*"},
			Status:       "online",
		})
		p.wg.Add(1)
		go p.workerLoop(ctx, workerID)
	}

	logger.Info("worker pool started", "local_workers", p.workers)

	// Wait for cancellation
	<-ctx.Done()
	p.wg.Wait()
	logger.Info("worker pool stopped")
}

func (p *Pool) workerLoop(ctx context.Context, workerID string) {
	defer p.wg.Done()
	// Deregistration is deferred so every exit path leaves the registry
	// clean, and it runs before wg.Done() (LIFO): Run() returns the moment
	// wg.Wait() unblocks, so a worker returning without deregistering would
	// leak its entry past shutdown. The Pop error branch below is where that
	// happened — a cancellation error returned early with the entry still
	// registered, which flaked the post-shutdown Count() assertion.
	defer p.registry.Deregister(workerID)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, err := p.queue.Pop(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if task == nil {
			// A closed queue pops its zero value (memory queue semantics):
			// no task will ever come again, so this worker retires the
			// same way it would on context cancellation.
			return
		}

		if err := p.deliver(ctx, task); err != nil {
			var deferred *retry.Deferred
			if errors.As(err, &deferred) {
				// Retry owed, not failed: hand the task back to the queue
				// with its stamped NextRetryAt and pick up the next one.
				if schedErr := p.scheduler.Schedule(ctx, task, deferred.Delay); schedErr != nil {
					logger.Error("schedule failed", "task_id", task.ID, "error", schedErr)
					// The queue refused the re-enqueue: Nack settles the
					// waiters rather than leaving the task in limbo.
					if nackErr := p.queue.Nack(ctx, task.ID, err); nackErr != nil {
						logger.Error("nack failed", "task_id", task.ID, "error", nackErr)
					}
				}
				continue
			}
			logger.Error("task failed", "task_id", task.ID, "worker", workerID, "error", err)
			if nackErr := p.queue.Nack(ctx, task.ID, err); nackErr != nil {
				logger.Error("nack failed", "task_id", task.ID, "error", nackErr)
			}
			continue
		}

		if ackErr := p.queue.Ack(ctx, task.ID); ackErr != nil {
			logger.Error("ack failed", "task_id", task.ID, "error", ackErr)
		}
	}
}

// deliver runs one delivery according to the pool's mode: with a scheduler
// queue every attempt is single-shot and a retry owed comes back as a
// *retry.Deferred for the queue to hold; otherwise Deliver waits out the
// whole backoff inside this call (sync fallback, historical behavior).
func (p *Pool) deliver(ctx context.Context, task *core.DeliveryTask) error {
	if p.scheduler != nil {
		return p.runtime.DeliverOnce(ctx, task)
	}
	return p.runtime.Deliver(ctx, task)
}

// staleCheckInterval is the period between stale-worker sweeps; a package
// variable so tests can shorten it.
var staleCheckInterval = 30 * time.Second

func (p *Pool) checkStaleWorkers(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(staleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.registry.RemoveStale(2 * time.Minute)
		}
	}
}

// Registry returns the worker registry
func (p *Pool) Registry() *Registry {
	return p.registry
}
