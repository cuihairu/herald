package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/cuihairu/herald/core"
)

// Manager manages provider runtimes
type Manager struct {
	mu        sync.RWMutex
	providers map[string]core.Provider
	factories map[string]core.ProviderFactory
	enabled   map[string]bool // Track enabled providers
}

// NewManager creates a new runtime manager
func NewManager() *Manager {
	return &Manager{
		providers: make(map[string]core.Provider),
		factories: make(map[string]core.ProviderFactory),
		enabled:   make(map[string]bool),
	}
}

// RegisterFactory registers a provider factory
func (m *Manager) RegisterFactory(factory core.ProviderFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.factories[factory.Name()] = factory
}

// RegisterProvider registers a provider
func (m *Manager) RegisterProvider(provider core.Provider, enabled ...bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := provider.Name()
	if _, exists := m.providers[name]; exists {
		return fmt.Errorf("provider already registered: %s", name)
	}

	m.providers[name] = provider
	// Default to enabled if not specified
	isEnabled := true
	if len(enabled) > 0 {
		isEnabled = enabled[0]
	}
	m.enabled[name] = isEnabled
	return nil
}

// CreateProvider creates a provider from config
func (m *Manager) CreateProvider(name string, config map[string]interface{}) (core.Provider, error) {
	m.mu.RLock()
	factory, ok := m.factories[name]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("factory not found: %s", name)
	}

	return factory.Create(config)
}

// GetProvider returns a provider by name
func (m *Manager) GetProvider(name string) (core.Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}

	return provider, nil
}

// GetProviders returns all registered providers
func (m *Manager) GetProviders() []core.Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := make([]core.Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}

	return providers
}

// GetProviderStatus returns the status of all providers
func (m *Manager) GetProviderStatus() []*core.ProviderStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]*core.ProviderStatus, 0, len(m.providers))
	for _, p := range m.providers {
		statuses = append(statuses, p.Status())
	}

	return statuses
}

// Deliver delivers a task to a provider
func (m *Manager) Deliver(ctx context.Context, task *core.Task) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	// Check if provider is enabled
	if !m.IsEnabled(task.Provider) {
		return fmt.Errorf("provider is disabled: %s", task.Provider)
	}

	provider, err := m.GetProvider(task.Provider)
	if err != nil {
		return err
	}

	return provider.Deliver(ctx, task)
}

// Enable enables a provider
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; !exists {
		return fmt.Errorf("provider not found: %s", name)
	}

	m.enabled[name] = true
	return nil
}

// Disable disables a provider
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; !exists {
		return fmt.Errorf("provider not found: %s", name)
	}

	m.enabled[name] = false
	return nil
}

// IsEnabled checks if a provider is enabled
func (m *Manager) IsEnabled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled[name]
}

// Close closes all providers
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close all providers that implement io.Closer
	for _, p := range m.providers {
		if closer, ok := p.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	return nil
}
