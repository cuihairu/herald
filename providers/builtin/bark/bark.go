package bark

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

// defaultServer is the public Bark push endpoint; a self-hosted
// bark-server overrides it via the server config field.
const defaultServer = "https://api.day.app"

// Provider is a Bark (iOS push) provider
type Provider struct {
	deviceKey  string
	server     string
	group      string
	ttl        int
	sound      string
	levelSound map[string]string
	url        string
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Payload is a Bark push request body. Only the official fields Herald
// supports are set; icon is part of the API surface but has no config
// mapping yet.
type Payload struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Group     string `json:"group,omitempty"`
	TTL       int    `json:"ttl,omitempty"`
	Sound     string `json:"sound,omitempty"`
	Icon      string `json:"icon,omitempty"`
	URL       string `json:"url,omitempty"`
}

// Response is a Bark push response
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewProvider creates a new Bark provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	deviceKey, _ := config["device_key"].(string)
	if deviceKey == "" {
		return nil, fmt.Errorf("bark: device_key is required")
	}

	server, _ := config["server"].(string)
	if server == "" {
		server = defaultServer
	}
	group, _ := config["group"].(string)
	sound, _ := config["sound"].(string)
	url, _ := config["url"].(string)
	ttl := intFromConfig(config["ttl"])

	var levelSound map[string]string
	if m, ok := config["level_sound"].(map[string]interface{}); ok {
		levelSound = make(map[string]string, len(m))
		for level, name := range m {
			if s, ok := name.(string); ok {
				levelSound[level] = s
			}
		}
	}

	return &Provider{
		deviceKey:  deviceKey,
		server:     server,
		group:      group,
		ttl:        ttl,
		sound:      sound,
		levelSound: levelSound,
		url:        url,
		status: &core.ProviderStatus{
			Name:   "bark",
			Type:   "bark",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// intFromConfig reads an integer option that yaml may hand over as int,
// int64 or float64 depending on how the config was loaded.
func intFromConfig(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// GetConfig returns the provider configuration
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"device_key":  p.deviceKey,
		"server":      p.server,
		"group":       p.group,
		"ttl":         p.ttl,
		"sound":       p.sound,
		"level_sound": p.levelSound,
		"url":         p.url,
	}
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"plain"},
	}
}

// Deliver delivers a task to Bark
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	payload := p.buildPayload(task)
	return p.sendPush(ctx, payload)
}

// resolveSound picks the notification sound for a task: a level_sound
// entry for the task's level wins, otherwise the configured default.
func (p *Provider) resolveSound(level string) string {
	if name, ok := p.levelSound[level]; ok {
		return name
	}
	return p.sound
}

// buildPayload builds a Bark push body from a delivery task.
func (p *Provider) buildPayload(task *core.DeliveryTask) *Payload {
	var title, body string
	if task.Payload.Content != nil {
		title, body = task.Payload.Content.Title, task.Payload.Content.Body
	}
	return &Payload{
		DeviceKey: p.deviceKey,
		Title:     title,
		Body:      body,
		Group:     p.group,
		TTL:       p.ttl,
		Sound:     p.resolveSound(task.Level),
		URL:       p.url,
	}
}

// sendPush posts a payload to the Bark server; Bark answers with
// {"code":200} on success and any other code is a delivery failure.
func (p *Provider) sendPush(ctx context.Context, payload *Payload) error {
	resp, err := p.client.PostJSON(ctx, p.server+"/push", payload)
	if err != nil {
		return err
	}

	var result Response
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Code != 200 {
		return fmt.Errorf("bark API error: code %d %s", result.Code, result.Message)
	}

	return nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "bark"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "bark"
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

// Factory creates Bark providers
type Factory struct{}

// Create creates a new Bark provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "bark"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "bark"
}
