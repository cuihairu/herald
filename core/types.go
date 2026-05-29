package core

import (
	"context"
	"time"
)

// PayloadKind determines how a provider interprets the delivery payload
type PayloadKind string

const (
	PayloadContent          PayloadKind = "content"
	PayloadProviderTemplate PayloadKind = "provider_template"
	PayloadRaw              PayloadKind = "raw"
)

// Notification is the top-level notification intent from the API
type Notification struct {
	ID          string              `json:"id"`
	Type        string              `json:"type"`
	Level       string              `json:"level,omitempty"`
	Channels    []string            `json:"channels"`
	Recipients  map[string][]string `json:"recipients,omitempty"`
	TemplateRef string              `json:"template,omitempty"`
	Params      map[string]any      `json:"params,omitempty"`
	Content     *DirectContent      `json:"content,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
}

// DirectContent holds inline content when no template is used
type DirectContent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// DeliveryTask is a single provider-bound delivery task
type DeliveryTask struct {
	ID         string          `json:"id"`
	Provider   string          `json:"provider"`
	Targets    []string        `json:"targets"`
	Payload    DeliveryPayload `json:"payload"`
	Level      string          `json:"level,omitempty"`
	RetryCount int             `json:"retry_count"`
	CreatedAt  time.Time       `json:"created_at"`
}

// DeliveryPayload wraps the actual content sent to a provider
type DeliveryPayload struct {
	Kind             PayloadKind              `json:"kind"`
	Content          *RenderedContent         `json:"content,omitempty"`
	ProviderTemplate *ProviderTemplatePayload `json:"provider_template,omitempty"`
	Raw              map[string]any           `json:"raw,omitempty"`
}

// RenderedContent is the rendered text in a specific format
type RenderedContent struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Format string `json:"format"` // html, markdown, plain, json
}

// ProviderTemplatePayload is the normalized vendor template payload
type ProviderTemplatePayload struct {
	TemplateCode string `json:"template_code,omitempty"` // aliyun
	TemplateID   string `json:"template_id,omitempty"`   // tencent, netease
	Params       any    `json:"params,omitempty"`        // map[string]string or []string
}

// ProviderStatus represents the status of a provider
type ProviderStatus struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Status   string    `json:"status"`
	Enabled  bool      `json:"enabled"`
	WorkerID string    `json:"worker_id,omitempty"`
	Since    time.Time `json:"since"`
}

// Queue is the interface for task queuing
type Queue interface {
	Push(ctx context.Context, task *DeliveryTask) error
	Pop(ctx context.Context) (*DeliveryTask, error)
	Size() int
	Close() error
}

// SensitiveFields returns a set of field names that should be masked in API responses
var SensitiveFields = map[string]bool{
	"token": true, "secret": true, "password": true, "secret_key": true,
	"access_key_secret": true, "app_secret": true, "bot_token": true,
	"sign_secret": true,
}

// MaskConfig masks sensitive fields in a config map
func MaskConfig(config map[string]interface{}) map[string]interface{} {
	masked := make(map[string]interface{}, len(config))
	for k, v := range config {
		if SensitiveFields[k] {
			masked[k] = "******"
		} else {
			masked[k] = v
		}
	}
	return masked
}
