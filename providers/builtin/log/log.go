package log

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/internal/logger"
)

// Provider is a log provider for testing
type Provider struct {
	name   string
	status *core.ProviderStatus
}

// NewProvider creates a new log provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	name := "log"
	if n, ok := config["name"].(string); ok {
		name = n
	}

	return &Provider{
		name: name,
		status: &core.ProviderStatus{
			Name:   name,
			Type:   "builtin",
			Status: "available",
			Since:  time.Now(),
		},
	}, nil
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"plain"},
	}
}

// Deliver delivers a task
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	title, body := extractContent(task)

	logger.Info("delivering task",
		"provider", p.name,
		"task_id", task.ID,
		"title", title,
		"body", body,
		"level", task.Level,
	)
	fmt.Printf("[%s] %s: %s\n", task.Level, title, body)
	return nil
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// Name returns the provider name
func (p *Provider) Name() string {
	return p.name
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "builtin"
}

// Status returns the current status
func (p *Provider) Status() *core.ProviderStatus {
	p.status.Status = "available"
	return p.status
}

// Close closes the provider
func (p *Provider) Close() error {
	return nil
}

// Factory creates log providers
type Factory struct{}

// Create creates a new log provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "log"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "builtin"
}
