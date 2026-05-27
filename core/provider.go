package core

import (
	"context"
)

// Provider is the interface for all delivery providers
type Provider interface {
	Deliver(ctx context.Context, task *DeliveryTask) error
	Name() string
	Type() string
	Status() *ProviderStatus
}

// ProviderCapability describes what a provider can handle
type ProviderCapability struct {
	PayloadKinds     []PayloadKind
	ContentFormats   []string // html, markdown, plain, json
	SupportsBatch    bool
	SupportsTemplate bool // supports vendor-side template (e.g. SMS)
}

// CapableProvider is a provider that declares its capabilities
type CapableProvider interface {
	Provider
	Capability() ProviderCapability
}

// BuiltinProvider is a provider that runs directly in the core process
type BuiltinProvider interface {
	Provider
	Deliver(ctx context.Context, task *DeliveryTask) error
}

// WorkerProvider is a provider that runs in a separate worker process
type WorkerProvider interface {
	Provider
	WorkerID() string
	Capabilities() []string
}

// ProviderFactory creates provider instances
type ProviderFactory interface {
	Create(config map[string]interface{}) (Provider, error)
	Name() string
	Type() string
}
