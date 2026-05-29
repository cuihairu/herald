package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
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
}

// NewPool creates a new worker pool
func NewPool(queue core.Queue, runtime *runtime.Manager, registry *Registry, workers int) *Pool {
	if workers <= 0 {
		workers = 1
	}
	return &Pool{
		queue:    queue,
		runtime:  runtime,
		registry: registry,
		workers:  workers,
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

	for {
		select {
		case <-ctx.Done():
			p.registry.Deregister(workerID)
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

		if err := p.runtime.Deliver(ctx, task); err != nil {
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

func (p *Pool) checkStaleWorkers(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
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
