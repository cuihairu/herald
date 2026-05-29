package dispatch

import (
	"context"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/worker"
	"github.com/cuihairu/herald/internal/logger"
)

// Dispatcher is a thin wrapper around worker.Pool for backward compatibility.
// New code should use worker.Pool directly.
type Dispatcher struct {
	pool *worker.Pool
}

// New creates a dispatcher backed by a worker pool.
func New(queue core.Queue, rt *runtime.Manager, registry *worker.Registry, workers int) *Dispatcher {
	if workers <= 0 {
		workers = 1
	}
	if registry == nil {
		registry = worker.NewRegistry()
	}
	return &Dispatcher{
		pool: worker.NewPool(queue, rt, registry, workers),
	}
}

// Run starts the worker pool and blocks until context is cancelled.
func (d *Dispatcher) Run(ctx context.Context) {
	logger.Info("dispatcher starting (backed by worker pool)")
	d.pool.Run(ctx)
}

// Pool returns the underlying worker pool.
func (d *Dispatcher) Pool() *worker.Pool {
	return d.pool
}
