package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/limiter"
	"github.com/cuihairu/herald/core/logstore"
	"github.com/cuihairu/herald/core/retry"
	"github.com/cuihairu/herald/core/rules"
)

// Manager manages provider runtimes.
type Manager struct {
	mu            sync.RWMutex
	providers     map[string]*registeredProvider
	factories     map[string]core.ProviderFactory
	logStore      *logstore.LogStore
	retryer       *retry.Retryer
	shadowSampler *rules.ShadowSampler
	limiters      *limiter.Manager
}

var providerSchemas = map[string]map[string]string{
	"telegram":   {"token": "string", "chat_id": "string", "parse_mode": "string", "api_url": "string"},
	"feishu":     {"webhook_url": "string", "sign_secret": "string"},
	"wecom":      {"webhook_url": "string", "key": "string"},
	"dingtalk":   {"webhook_url": "string", "access_token": "string", "secret": "string"},
	"slack":      {"webhook_url": "string"},
	"discord":    {"webhook_url": "string", "webhook_id": "string", "webhook_token": "string", "bot_token": "string", "channel_id": "string", "api_url": "string"},
	"email":      {"host": "string", "port": "number", "username": "string", "password": "string", "from": "string", "from_name": "string"},
	"webhook":    {"url": "string", "method": "string", "headers": "object"},
	"wechat":     {"service": "string", "send_key": "string", "token": "string", "app_token": "string", "uid": "string"},
	"wechatmp":   {"app_id": "string", "app_secret": "string", "template_id": "string", "default_url": "string"},
	"aliyunsms":  {"access_key_id": "string", "access_key_secret": "string", "sign_name": "string", "region": "string", "endpoint": "string"},
	"tencentsms": {"secret_id": "string", "secret_key": "string", "app_id": "string", "sign_name": "string", "region": "string", "endpoint": "string"},
	"neteasesms": {"app_key": "string", "app_secret": "string", "endpoint": "string"},
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
		providers:     make(map[string]*registeredProvider),
		factories:     make(map[string]core.ProviderFactory),
		logStore:      logstore.New(logLimit),
		retryer:       r,
		shadowSampler: rules.NewDefaultShadowSampler(),
	}
}

// RecordShadow implements the rule observer contract: it records a
// shadow-mode rule hit into the delivery log stream (status "shadow"),
// sampled per rule so high-QPS traffic does not flood the ring buffer.
func (m *Manager) RecordShadow(ruleID string, channels []string, n *core.Notification) {
	if !m.shadowSampler.ShouldRecord(ruleID) {
		return
	}
	now := time.Now()
	m.logStore.Add(&logstore.TaskLog{
		ID:        "shadow:" + ruleID + ":" + n.ID,
		Level:     n.Level,
		Status:    "shadow",
		CreatedAt: now,
		RuleID:    ruleID,
		WouldFire: true,
		MatchedAt: now,
		Channels:  channels,
	})
}

// RecordEvalError implements the rule observer contract: it records a
// rule expression that failed to evaluate. Errors are never sampled —
// they usually mean a misconfigured rule and every occurrence matters.
func (m *Manager) RecordEvalError(ruleID string, err error, n *core.Notification) {
	if err == nil {
		return
	}
	now := time.Now()
	m.logStore.Add(&logstore.TaskLog{
		ID:        "ruleerr:" + ruleID + ":" + n.ID,
		Level:     n.Level,
		Status:    "shadow",
		Error:     err.Error(),
		CreatedAt: now,
		RuleID:    ruleID,
		MatchedAt: now,
	})
}

// RecordForPending implements the rule observer contract: it records an
// event suppressed because the governing rule's "for" window was still
// running. Suppressions are sampled per rule like shadow hits — a pending
// window is exactly the noise-reduction scenario where volume is high.
func (m *Manager) RecordForPending(ruleID string, n *core.Notification) {
	if !m.shadowSampler.ShouldRecord(ruleID) {
		return
	}
	now := time.Now()
	m.logStore.Add(&logstore.TaskLog{
		ID:        "forpending:" + ruleID + ":" + n.ID,
		Level:     n.Level,
		Status:    "pending",
		CreatedAt: now,
		RuleID:    ruleID,
		MatchedAt: now,
	})
}

// ShadowRuleCount returns how many times a rule has matched during shadow
// evaluation so far — the exact total behind the sampled log entries.
func (m *Manager) ShadowRuleCount(ruleID string) uint64 {
	return m.shadowSampler.Count(ruleID)
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

// SetProviderLimiter attaches a rate limiter to a provider: every Deliver
// for that provider waits for a token before calling the provider. Calling
// it twice for the same provider keeps the first limiter (rate limits are
// set once at configuration time). Config is never nil — a nil config
// selects the limiter package default.
func (m *Manager) SetProviderLimiter(provider string, cfg *limiter.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.limiters == nil {
		m.limiters = limiter.NewManager()
	}
	_, err := m.limiters.GetOrCreate(provider, cfg)
	return err
}

// LimiterFor returns the rate limiter registered for a provider, if any.
// It exists for introspection (tests, dashboards); delivery goes through
// Deliver, which waits on this limiter automatically.
func (m *Manager) LimiterFor(provider string) (limiter.Limiter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.limiters == nil {
		return nil, false
	}
	return m.limiters.Get(provider)
}

// Deliver delivers a task through the named provider, honoring the
// provider's rate limiter (if configured) and the retry policy (if
// configured), and records the outcome into the delivery log.
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

	// Channel rate limiting: the rule engine can fan one notification out
	// to many channels, so a misconfigured rule must not turn into a
	// provider-side storm. Wait BEFORE any delivery attempt.
	m.mu.RLock()
	limiters := m.limiters
	m.mu.RUnlock()
	if limiters != nil {
		if lm, ok := limiters.Get(task.Provider); ok {
			if err := lm.Wait(ctx); err != nil {
				err = fmt.Errorf("rate limit wait aborted: %w", err)
				entry := logstore.NewTaskLog(task)
				m.logStore.Add(entry)
				m.logStore.UpdateStatus(task.ID, "failed", err.Error())
				return err
			}
		}
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
