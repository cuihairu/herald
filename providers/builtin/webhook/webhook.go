package webhook

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

// Provider is a generic webhook provider
type Provider struct {
	url     string
	method  string
	headers map[string]string
	status  *core.ProviderStatus
	client  *httpclient.Client
}

// Config is the webhook provider configuration
type Config struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"` // GET, POST, PUT, DELETE
	Headers map[string]string `yaml:"headers"`
}

// WebhookPayload is the payload sent to the webhook
type WebhookPayload struct {
	ID        string         `json:"id"`
	Provider  string         `json:"provider,omitempty"`
	Level     string         `json:"level,omitempty"`
	Targets   []string       `json:"targets,omitempty"`
	Timestamp string         `json:"timestamp"`
	Title     string         `json:"title,omitempty"`
	Body      string         `json:"body,omitempty"`
	Raw       map[string]any `json:"raw,omitempty"`
}

// NewProvider creates a new webhook provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	url, ok := config["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("webhook: url is required")
	}

	method := "POST"
	if m, ok := config["method"].(string); ok && m != "" {
		method = m
	}

	headers := make(map[string]string)
	if h, ok := config["headers"].(map[string]string); ok {
		headers = h
	}

	return &Provider{
		url:     url,
		method:  method,
		headers: headers,
		status: &core.ProviderStatus{
			Name:   "webhook",
			Type:   "webhook",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent, core.PayloadRaw},
		ContentFormats: []string{"json"},
	}
}

// Deliver delivers a task to a webhook
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	// Build payload
	title, body := extractContent(task)
	payload := &WebhookPayload{
		ID:        task.ID,
		Provider:  task.Provider,
		Level:     task.Level,
		Targets:   task.Targets,
		Timestamp: task.CreatedAt.Format(time.RFC3339),
		Title:     title,
		Body:      body,
		Raw:       task.Payload.Raw,
	}

	// For POST/PUT, send JSON
	if p.method == "POST" || p.method == "PUT" {
		resp, err := p.client.PostJSON(ctx, p.url, payload)
		if err != nil {
			// Check if error is retryable
			if httpclient.IsRetryable(err) {
				return httpclient.WithRetry(err)
			}
			return err
		}

		// Log response
		httpclient.LogResponse("webhook", resp, nil)
		return nil
	}

	// For GET, add as query params (simplified)
	return fmt.Errorf("method %s not yet implemented", p.method)
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
	return "webhook"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "webhook"
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

// Factory creates webhook providers
type Factory struct{}

// Create creates a new webhook provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "webhook"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "webhook"
}
