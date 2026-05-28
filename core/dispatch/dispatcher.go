package dispatch

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/runtime"
	"github.com/cuihairu/herald/core/websocket"
	"github.com/cuihairu/herald/internal/logger"
	"github.com/cuihairu/herald/protocol"
)

// Dispatcher consumes queued tasks and delivers them through runtime.
type Dispatcher struct {
	queue   core.Queue
	runtime *runtime.Manager
	hub     *websocket.Hub
}

// New creates a dispatcher.
func New(queue core.Queue, runtime *runtime.Manager, hub *websocket.Hub) *Dispatcher {
	return &Dispatcher{queue: queue, runtime: runtime, hub: hub}
}

// Run starts the consume loop.
func (d *Dispatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			task, err := d.queue.Pop(ctx)
			if err != nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			d.process(ctx, task)
		}
	}
}

func (d *Dispatcher) process(ctx context.Context, task *core.DeliveryTask) {
	if d.hub != nil {
		provider, err := d.runtime.GetProvider(task.Provider)
		if err == nil && provider.Type() == "worker" {
			if err := d.dispatchToWorker(task); err != nil {
				logger.Error("failed to dispatch worker task", "task_id", task.ID, "provider", task.Provider, "error", err)
				return
			}
			logger.Info("worker task dispatched", "task_id", task.ID, "provider", task.Provider)
			return
		}
	}

	if err := d.runtime.Deliver(ctx, task); err != nil {
		logger.Error("failed to deliver task", "task_id", task.ID, "provider", task.Provider, "error", err)
		return
	}
	logger.Info("task delivered", "task_id", task.ID, "provider", task.Provider)
}

func (d *Dispatcher) dispatchToWorker(task *core.DeliveryTask) error {
	if task.Payload.Content == nil {
		return fmt.Errorf("worker dispatch requires content payload")
	}

	provider, err := d.runtime.GetProvider(task.Provider)
	if err != nil {
		return err
	}

	msg := &protocol.DispatchMessage{
		TaskID:    task.ID,
		Provider:  task.Provider,
		Title:     task.Payload.Content.Title,
		Body:      task.Payload.Content.Body,
		Level:     task.Level,
		Timestamp: time.Now().Unix(),
	}
	if len(task.Targets) > 0 {
		msg.Target = task.Targets[0]
	}

	if task.Payload.Raw != nil {
		msg.Data = task.Payload.Raw
	}

	if targeter, ok := provider.(interface{ Target() string }); ok {
		target := targeter.Target()
		if target != "" {
			return d.hub.DispatchToWorker(target, msg)
		}
	}

	return d.hub.Dispatch(msg)
}
