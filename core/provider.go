package core

import (
	"context"
)

// Provider is the interface for all delivery providers
type Provider interface {
	// Deliver delivers a task
	Deliver(ctx context.Context, task *Task) error

	// Name returns the provider name
	Name() string

	// Type returns the provider type (builtin or worker)
	Type() string

	// Status returns the current status
	Status() *ProviderStatus
}

// BuiltinProvider is a provider that runs directly in the core process
type BuiltinProvider interface {
	Provider
	Deliver(ctx context.Context, task *Task) error
}

// WorkerProvider is a provider that runs in a separate worker process
type WorkerProvider interface {
	Provider

	// WorkerID returns the worker ID
	WorkerID() string

	// Capabilities returns the provider capabilities
	Capabilities() []string
}

// ProviderFactory creates provider instances
type ProviderFactory interface {
	Create(config map[string]interface{}) (Provider, error)
	Name() string
	Type() string
}
