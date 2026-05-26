package core

import (
	"context"
	"time"
)

// Queue is the interface for event queuing
type Queue interface {
	// Push pushes an event to the queue
	Push(ctx context.Context, event *Event) error

	// Pop pops an event from the queue
	Pop(ctx context.Context) (*Event, error)

	// PushTask pushes a task to the queue
	PushTask(ctx context.Context, task *Task) error

	// PopTask pops a task from the queue
	PopTask(ctx context.Context) (*Task, error)

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

// Event represents an event to be delivered
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Labels    map[string]string      `json:"labels"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// Task represents a delivery task
type Task struct {
	ID         string                 `json:"id"`
	Provider   string                 `json:"provider"`
	Title      string                 `json:"title"`
	Body       string                 `json:"body"`
	Level      string                 `json:"level,omitempty"`
	Target     string                 `json:"target,omitempty"`
	Data       map[string]interface{} `json:"data,omitempty"`
	RenderFormat string               `json:"render_format,omitempty"` // Content format: html, markdown, plain, json
	RetryCount int                    `json:"retry_count"`
	CreatedAt  time.Time              `json:"created_at"`
}

// TaskResult represents the result of a task execution
type TaskResult struct {
	TaskID  string    `json:"task_id"`
	Success bool      `json:"success"`
	Error   string    `json:"error,omitempty"`
	At      time.Time `json:"at"`
}

// ProviderStatus represents the status of a provider
type ProviderStatus struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Status   string    `json:"status"`
	Enabled  bool      `json:"enabled"`
	WorkerID string    `json:"worker_id,omitempty"`
	Since    time.Time `json:"since"`
}
