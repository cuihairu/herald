package slack

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	webhookURLFormat = "https://hooks.slack.com/services/%s/%s/%s"
)

// Provider is a Slack provider
type Provider struct {
	webhookURL string
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Config is the Slack provider configuration
type Config struct {
	WebhookURL string `yaml:"webhook_url"` // Full webhook URL
	Workspace  string `yaml:"workspace"`   // Workspace ID
	AppID      string `yaml:"app_id"`      // App ID
	AppSecret  string `yaml:"app_secret"`  // App Secret
}

// WebhookPayload is the Slack webhook payload
type WebhookPayload struct {
	Username    string            `json:"username,omitempty"`
	IconURL     string            `json:"icon_url,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
	Channel     string            `json:"channel,omitempty"`
	Text        string            `json:"text,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Blocks      []Block           `json:"blocks,omitempty"`
}

// Attachment is a Slack attachment
type Attachment struct {
	Color   string   `json:"color,omitempty"`
	Title   string   `json:"title,omitempty"`
	Text    string   `json:"text,omitempty"`
	Fields  []Field  `json:"fields,omitempty"`
	Footer  string   `json:"footer,omitempty"`
	Ts      int64    `json:"ts,omitempty"`
}

// Field is an attachment field
type Field struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short,omitempty"`
}

// Block is a Slack block
type Block struct {
	Type    string      `json:"type"`
	Text    *TextObject `json:"text,omitempty"`
	Fields  []Field     `json:"fields,omitempty"`
}

// TextObject is a Slack text object
type TextObject struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ErrorResponse is a Slack error response
type ErrorResponse struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// NewProvider creates a new Slack provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	var webhookURL string

	// Try full webhook URL first
	if url, ok := config["webhook_url"].(string); ok && url != "" {
		webhookURL = url
	}

	return &Provider{
		webhookURL: webhookURL,
		status: &core.ProviderStatus{
			Name:     "slack",
			Type:     "builtin",
			Status:   "available",
			Since:    time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// Deliver delivers a task to Slack
func (p *Provider) Deliver(ctx context.Context, task *core.Task) error {
	payload := p.buildPayload(task)

	resp, err := p.client.PostJSON(ctx, p.webhookURL, payload)
	if err != nil {
		// Check if error is retryable
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	// Check for Slack error response
	var errResp ErrorResponse
	if err := resp.JSON(&errResp); err == nil && !errResp.OK {
		return fmt.Errorf("slack API error: %s", errResp.Error)
	}

	return nil
}

// buildPayload builds a webhook payload
func (p *Provider) buildPayload(task *core.Task) *WebhookPayload {
	color := p.getColor(task.Level)

	attachment := Attachment{
		Color:  color,
		Title:  task.Title,
		Text:   task.Body,
		Fields: []Field{},
		Footer: "Herald",
		Ts:     task.CreatedAt.Unix(),
	}

	// Add level field
	if task.Level != "" {
		attachment.Fields = append(attachment.Fields, Field{
			Title: "Level",
			Value: task.Level,
			Short: true,
		})
	}

	// Add provider field
	if task.Provider != "" {
		attachment.Fields = append(attachment.Fields, Field{
			Title: "Provider",
			Value: task.Provider,
			Short: true,
		})
	}

	// Add custom fields from data
	if task.Data != nil {
		for k, v := range task.Data {
			attachment.Fields = append(attachment.Fields, Field{
				Title: k,
				Value: fmt.Sprintf("%v", v),
				Short: true,
			})
		}
	}

	return &WebhookPayload{
		Username:  "Herald",
		IconEmoji: ":bell:",
		Text:      "",
		Attachments: []Attachment{attachment},
	}
}

// getColor returns a color based on level
func (p *Provider) getColor(level string) string {
	switch level {
	case "error":
		return "danger"
	case "warning":
		return "warning"
	case "info":
		return "#36a64f" // Green
	default:
		return "#808080" // Gray
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "slack"
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

// Factory creates Slack providers
type Factory struct{}

// Create creates a new Slack provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "slack"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "builtin"
}
