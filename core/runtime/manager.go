package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/logstore"
	"github.com/cuihairu/herald/core/retry"
)

// Manager manages provider runtimes.
type Manager struct {
	mu        sync.RWMutex
	providers map[string]*registeredProvider
	factories map[string]core.ProviderFactory
	logStore  *logstore.LogStore
	retryer   *retry.Retryer
}

var providerSchemas = map[string]map[string]string{
	"telegram":   {"token": "string", "chat_id": "string"},
	"feishu":     {"webhook_url": "string"},
	"wecom":      {"webhook_url": "string"},
	"dingtalk":   {"access_token": "string", "secret": "string"},
	"slack":      {"webhook_url": "string"},
	"discord":    {"webhook_url": "string"},
	"email":      {"host": "string", "port": "number", "username": "string", "password": "string", "from": "string"},
	"webhook":    {"url": "string"},
	"wechat":     {"service": "string", "send_key": "string", "token": "string", "app_token": "string", "uid": "string"},
	"wechatmp":   {"app_id": "string", "app_secret": "string", "template_id": "string", "default_url": "string"},
	"aliyunsms":  {"access_key_id": "string", "access_key_secret": "string", "sign_name": "string"},
	"tencentsms": {"secret_id": "string", "secret_key": "string", "app_id": "string", "sign_name": "string"},
	"neteasesms": {"app_key": "string", "app_secret": "string"},
}

type registeredProvider struct {
	name     string
	provider core.Provider
	enabled  bool
}

// NewManager creates a new runtime manager.
func NewManager(logLimit int, retryCfg ...*retry.Config) *Manager {
	var r *retry.Retryer
	if len(retryCfg) > 0 && retryCfg[0] != nil {
		r = retry.NewRetryer(retryCfg[0])
	}
	return &Manager{
		providers: make(map[string]*registeredProvider),
		factories: make(map[string]core.ProviderFactory),
		logStore:  logstore.New(logLimit),
		retryer:   r,
	}
}

// RegisterFactory registers a provider factory keyed by provider type.
func (m *Manager) RegisterFactory(factory core.ProviderFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.factories[factory.Name()] = factory
}

// RegisterProvider registers a provider instance using the supplied instance name.
func (m *Manager) RegisterProvider(name string, provider core.Provider, enabled ...bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; exists {
		return fmt.Errorf("provider already registered: %s", name)
	}

	isEnabled := true
	if len(enabled) > 0 {
		isEnabled = enabled[0]
	}

	m.providers[name] = &registeredProvider{
		name:     name,
		provider: provider,
		enabled:  isEnabled,
	}
	return nil
}

// CreateProvider creates a provider from config using its provider type.
func (m *Manager) CreateProvider(providerType string, config map[string]interface{}) (core.Provider, error) {
	m.mu.RLock()
	factory, ok := m.factories[providerType]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("factory not found: %s", providerType)
	}

	return factory.Create(config)
}

// GetProviderType returns the provider type for a registered instance.
func (m *Manager) GetProviderType(name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.providers[name]
	if !ok {
		return "", fmt.Errorf("provider not found: %s", name)
	}

	return entry.provider.Type(), nil
}

// GetProviderSchema returns the config schema for a provider type.
func (m *Manager) GetProviderSchema(providerType string) map[string]string {
	if schema, ok := providerSchemas[providerType]; ok {
		result := make(map[string]string, len(schema))
		for k, v := range schema {
			result[k] = v
		}
		return result
	}
	return map[string]string{"config": "object"}
}

// GetProvider returns a provider by instance name.
func (m *Manager) GetProvider(name string) (core.Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}

	return entry.provider, nil
}

// GetProviders returns all registered providers.
func (m *Manager) GetProviders() []core.Provider {
	m.mu.RLock()
	defer m.mu.RUnlock()

	providers := make([]core.Provider, 0, len(m.providers))
	for _, entry := range m.providers {
		providers = append(providers, entry.provider)
	}

	return providers
}

// GetProviderStatus returns the status of all providers.
func (m *Manager) GetProviderStatus() []*core.ProviderStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]*core.ProviderStatus, 0, len(m.providers))
	for name, entry := range m.providers {
		s := entry.provider.Status()
		s.Name = name
		s.Enabled = entry.enabled
		statuses = append(statuses, s)
	}

	return statuses
}

// Deliver delivers a DeliveryTask to its target provider.
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

// Enable enables a provider instance.
func (m *Manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.providers[name]
	if !exists {
		return fmt.Errorf("provider not found: %s", name)
	}

	entry.enabled = true
	return nil
}

// Disable disables a provider instance.
func (m *Manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.providers[name]
	if !exists {
		return fmt.Errorf("provider not found: %s", name)
	}

	entry.enabled = false
	return nil
}

// IsEnabled checks if a provider instance is enabled.
func (m *Manager) IsEnabled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.providers[name]
	return ok && entry.enabled
}

// Close closes all providers.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range m.providers {
		if closer, ok := entry.provider.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}

	return nil
}

// GetLogs returns logs with optional filtering.
func (m *Manager) GetLogs(offset, limit int, filter *logstore.Filter) []*logstore.TaskLog {
	return m.logStore.Get(offset, limit, filter)
}

// GetLogsCount returns the total count of logs.
func (m *Manager) GetLogsCount(filter *logstore.Filter) int {
	return m.logStore.Count(filter)
}

// GetLogByID returns a log entry by ID.
func (m *Manager) GetLogByID(id string) *logstore.TaskLog {
	return m.logStore.GetByID(id)
}

// GetLogsStats returns log statistics.
func (m *Manager) GetLogsStats() *logstore.Stats {
	return m.logStore.Stats()
}

// ClearLogs clears all logs.
func (m *Manager) ClearLogs() {
	m.logStore.Clear()
}

// ReplaceProvider replaces an existing provider instance.
func (m *Manager) ReplaceProvider(name string, provider core.Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, exists := m.providers[name]
	if !exists {
		return fmt.Errorf("provider not found: %s", name)
	}

	if closer, ok := entry.provider.(interface{ Close() error }); ok {
		_ = closer.Close()
	}

	entry.provider = provider
	return nil
}
