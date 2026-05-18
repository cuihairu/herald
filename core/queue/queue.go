package queue

import (
	"context"
	"time"

	"github.com/cuihaitao/herald/core"
)

// Queue is the interface for event queuing
type Queue interface {
	// Push pushes an event to the queue
	Push(ctx context.Context, event *core.Event) error

	// Pop pops an event from the queue
	Pop(ctx context.Context) (*core.Event, error)

	// PushTask pushes a task to the queue
	PushTask(ctx context.Context, task *core.Task) error

	// PopTask pops a task from the queue
	PopTask(ctx context.Context) (*core.Task, error)

	// Size returns the current queue size
	Size() int

	// Close closes the queue
	Close() error
}

// QueueConfig is the configuration for a queue
type QueueConfig struct {
	Type     string        `yaml:"type"`     // memory, redis
	Size     int           `yaml:"size"`     // max queue size
	Timeout  time.Duration `yaml:"timeout"`  // pop timeout
}
