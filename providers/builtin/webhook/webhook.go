package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

	method := http.MethodPost
	if m, ok := config["method"].(string); ok && m != "" {
		method = strings.ToUpper(strings.TrimSpace(m))
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
	default:
		return nil, fmt.Errorf("webhook: method must be one of GET, POST, PUT, DELETE, got %q", method)
	}

	headers := make(map[string]string)
	// The YAML loader decodes nested blocks as map[string]interface{}, so
	// accept both shapes — a map[string]string only shows up when the
	// provider is built from Go literals (tests, programmatic setups).
	switch h := config["headers"].(type) {
	case map[string]string:
		headers = h
	case map[string]interface{}:
		for k, v := range h {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
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

// GetConfig returns the provider configuration
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"url":     p.url,
		"method":  p.method,
		"headers": p.headers,
	}
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

	var resp *httpclient.Response
	var err error

	switch p.method {
	case http.MethodGet:
		// GET carries no body: flatten the payload into query parameters.
		target, qerr := getURL(p.url, payload)
		if qerr != nil {
			return qerr
		}
		resp, err = p.client.GetWithHeaders(ctx, target, p.headers)
	case http.MethodDelete:
		resp, err = p.client.DoJSON(ctx, http.MethodDelete, p.url, payload, p.headers)
	case http.MethodPost, http.MethodPut:
		resp, err = p.client.DoJSON(ctx, p.method, p.url, payload, p.headers)
	}

	if err != nil {
		return err
	}

	// Log response
	httpclient.LogResponse("webhook", resp, nil)
	return nil
}

// getURL appends the payload as query parameters to the target URL. Values
// that survive as non-empty are included; the raw map is JSON-encoded.
func getURL(base string, payload *WebhookPayload) (string, error) {
	q := url.Values{}
	for key, value := range map[string]string{
		"id":        payload.ID,
		"provider":  payload.Provider,
		"level":     payload.Level,
		"timestamp": payload.Timestamp,
		"title":     payload.Title,
		"body":      payload.Body,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	if len(payload.Targets) > 0 {
		q.Set("targets", strings.Join(payload.Targets, ","))
	}
	if len(payload.Raw) > 0 {
		encoded, err := json.Marshal(payload.Raw)
		if err != nil {
			return "", fmt.Errorf("webhook: failed to encode raw payload as query: %w", err)
		}
		q.Set("raw", string(encoded))
	}

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + q.Encode(), nil
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
