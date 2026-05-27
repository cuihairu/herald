package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/logstore"
	"github.com/cuihairu/herald/core/retry"
)

// Manager manages provider runtimes
type Manager struct {
	mu        sync.RWMutex
	providers map[string]core.Provider
	factories map[string]core.ProviderFactory
	enabled   map[string]bool
	logStore  *logstore.LogStore
	retryer   *retry.Retryer
}

// NewManager creates a new runtime manager
func NewManager(logLimit int, retryCfg ...*retry.Config) *Manager {
	var r *retry.Retryer
	if len(retryCfg) > 0 && retryCfg[0] != nil {
		r = retry.NewRetryer(retryCfg[0])
	}
	return &Manager{
		providers: make(map[string]core.Provider),
		factories: make(map[string]core.ProviderFactory),
		enabled:   make(map[string]bool),
		logStore:  logstore.New(logLimit),
		retryer:   r,
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

// Deliver delivers a DeliveryTask to its target provider
func (m *Manager) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	if !m.IsEnabled(task.Provider) {
		return fmt.Errorf("provider is disabled: %s", task.Provider)
	}

	provider, err := m.GetProvider(task.Provider)
	if err != nil {
		return err
	}

	logEntry := logstore.NewTaskLog(task)
	m.logStore.Add(logEntry)

	deliverFn := func() error {
		return provider.Deliver(ctx, task)
	}

	var deliverErr error
	if m.retryer != nil {
		deliverErr = m.retryer.Execute(ctx, task, deliverFn)
	} else {
		deliverErr = deliverFn()
	}

	if deliverErr != nil {
		m.logStore.UpdateStatus(task.ID, "failed", deliverErr.Error())
		return deliverErr
	}

	m.logStore.UpdateStatus(task.ID, "success", "")
	return nil
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

	for _, p := range m.providers {
		if closer, ok := p.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	return nil
}

// GetLogs returns logs with optional filtering
func (m *Manager) GetLogs(offset, limit int, filter *logstore.Filter) []*logstore.TaskLog {
	return m.logStore.Get(offset, limit, filter)
}

// GetLogsCount returns the total count of logs
func (m *Manager) GetLogsCount(filter *logstore.Filter) int {
	return m.logStore.Count(filter)
}

// GetLogByID returns a log entry by ID
func (m *Manager) GetLogByID(id string) *logstore.TaskLog {
	return m.logStore.GetByID(id)
}

// GetLogsStats returns log statistics
func (m *Manager) GetLogsStats() *logstore.Stats {
	return m.logStore.Stats()
}

// ClearLogs clears all logs
func (m *Manager) ClearLogs() {
	m.logStore.Clear()
}

// ReplaceProvider replaces an existing provider
func (m *Manager) ReplaceProvider(name string, provider core.Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; !exists {
		return fmt.Errorf("provider not found: %s", name)
	}

	if old, ok := m.providers[name]; ok {
		if closer, ok := old.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	m.providers[name] = provider
	return nil
}
