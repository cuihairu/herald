package feishu

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

// Provider is a Feishu provider
type Provider struct {
	webhookURL string
	signSecret string
	// interactive renders alert tasks as cards with an acknowledge button
	// instead of plain text; the button carries the task's alert_id so the
	// card callback can acknowledge the alert.
	interactive bool
	status      *core.ProviderStatus
	client      *httpclient.Client
}

// Config is the Feishu provider configuration
type Config struct {
	WebhookURL  string `yaml:"webhook_url"`
	SignSecret  string `yaml:"sign_secret"`
	Interactive bool   `yaml:"interactive_cards"`
}

// Message is a Feishu message. Text and post messages use content;
// interactive cards use card.
type Message struct {
	MsgType string      `json:"msg_type"`
	Content interface{} `json:"content,omitempty"`
	Card    interface{} `json:"card,omitempty"`
}

// TextContent is text content
type TextContent struct {
	Text string `json:"text"`
}

// PostContent is post content
type PostContent struct {
	Post map[string]struct {
		Title   string                     `json:"title,omitempty"`
		Content [][]map[string]interface{} `json:"content"`
	} `json:"post"`
}

// Response is a Feishu webhook response
type Response struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// NewProvider creates a new Feishu provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	webhookURL, ok := config["webhook_url"].(string)
	if !ok || webhookURL == "" {
		return nil, fmt.Errorf("feishu: webhook_url is required")
	}

	signSecret, _ := config["sign_secret"].(string)
	interactive, _ := config["interactive_cards"].(bool)

	return &Provider{
		webhookURL:  webhookURL,
		signSecret:  signSecret,
		interactive: interactive,
		status: &core.ProviderStatus{
			Name:   "feishu",
			Type:   "feishu",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// GetConfig returns the provider configuration
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"webhook_url":       p.webhookURL,
		"sign_secret":       p.signSecret,
		"interactive_cards": p.interactive,
	}
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"plain"},
	}
}

// Deliver delivers a task to Feishu
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	// Build message
	message := p.buildMessage(task)

	// Send message
	return p.sendMessage(ctx, message)
}

// buildMessage builds a Feishu message from a task: a card with an
// acknowledge button when interactive cards are on and the task carries
// an alert id, plain text otherwise.
func (p *Provider) buildMessage(task *core.DeliveryTask) *Message {
	if p.interactive && task.AlertID != "" {
		return &Message{MsgType: "interactive", Card: p.buildCard(task)}
	}
	content := p.formatMessage(task)
	return &Message{
		MsgType: "text",
		Content: TextContent{
			Text: content,
		},
	}
}

// cardButtonValue is the payload the acknowledge button sends back to the
// card callback endpoint; alert_id is the acknowledgement identity shared
// with the ack API, escalation and the incident ledger.
type cardButtonValue struct {
	AlertID string `json:"alert_id"`
}

// buildCard renders the task as an interactive card: a level-colored
// header, the body text, and one acknowledge button carrying the alert id.
func (p *Provider) buildCard(task *core.DeliveryTask) map[string]interface{} {
	title, body := extractContent(task)
	if title == "" {
		title = task.AlertID
	}

	header := map[string]interface{}{
		"title": map[string]interface{}{
			"tag":     "plain_text",
			"content": levelPrefix(task.Level) + title,
		},
	}
	switch task.Level {
	case "error", "critical":
		header["template"] = "red"
	case "warning":
		header["template"] = "orange"
	default:
		header["template"] = "blue"
	}

	elements := make([]interface{}, 0, 2)
	if body != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": body,
			},
		})
	}
	elements = append(elements, map[string]interface{}{
		"tag": "action",
		"actions": []interface{}{
			map[string]interface{}{
				"tag":  "button",
				"text": map[string]interface{}{"tag": "plain_text", "content": "确认告警"},
				"type": "primary",
				"value": cardButtonValue{
					AlertID: task.AlertID,
				},
			},
		},
	})

	return map[string]interface{}{
		"config":   map[string]interface{}{"wide_screen_mode": true},
		"header":   header,
		"elements": elements,
	}
}

// levelPrefix returns the same level indicator the text format uses.
func levelPrefix(level string) string {
	switch level {
	case "error":
		return "[错误] "
	case "warning":
		return "[警告] "
	case "info":
		return "[信息] "
	}
	return ""
}

// formatMessage formats the task as a Feishu message
func (p *Provider) formatMessage(task *core.DeliveryTask) string {
	title, body := extractContent(task)

	message := ""

	// Add level indicator
	message += levelPrefix(task.Level)

	// Add title
	message += title + "\n\n"

	// Add body
	if body != "" {
		message += body
	}

	return message
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// sendMessage sends a message to Feishu
func (p *Provider) sendMessage(ctx context.Context, message *Message) error {
	resp, err := p.client.PostJSON(ctx, p.webhookURL, message)
	if err != nil {
		return err
	}

	// Parse response
	var result Response
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Feishu returns 0 for success
	if result.Code != 0 {
		return fmt.Errorf("feishu API error: %s", result.Msg)
	}

	return nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "feishu"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "feishu"
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

// Factory creates Feishu providers
type Factory struct{}

// Create creates a new Feishu provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "feishu"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "feishu"
}
