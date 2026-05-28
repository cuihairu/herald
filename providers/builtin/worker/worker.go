package worker

import (
	"context"
	"time"

	"github.com/cuihairu/herald/core"
)

// Provider represents a remote worker-backed provider.
type Provider struct {
	name   string
	target string
	status *core.ProviderStatus
}

// NewProvider creates a worker provider.
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	name, _ := config["name"].(string)
	if name == "" {
		name = "worker"
	}
	target, _ := config["target"].(string)
	if target == "" {
		target = name
	}

	return &Provider{
		name:   name,
		target: target,
		status: &core.ProviderStatus{Name: name, Type: "worker", Status: "available", Since: time.Now()},
	}, nil
}

func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error { return nil }
func (p *Provider) Name() string { return p.name }
func (p *Provider) Type() string { return "worker" }
func (p *Provider) Status() *core.ProviderStatus { return p.status }
func (p *Provider) Close() error { return nil }

// Target returns the remote worker id.
func (p *Provider) Target() string { return p.target }

// Factory creates worker providers.
type Factory struct{}

func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) { return NewProvider(config) }
func (f *Factory) Name() string { return "worker" }
func (f *Factory) Type() string { return "worker" }
