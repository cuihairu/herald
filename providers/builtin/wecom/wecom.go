package wecom

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	defaultWebhookURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s"
)

// Provider is a WeCom (WeChat Work) provider
type Provider struct {
	webhookURL string
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Config is the WeCom provider configuration
type Config struct {
	WebhookURL string `yaml:"webhook_url"`
	Key        string `yaml:"key"`
}

// Message is a WeCom message
type Message struct {
	MsgType  string    `json:"msgtype"`
	Text     *Text     `json:"text,omitempty"`
	Markdown *Markdown `json:"markdown,omitempty"`
}

// Text is text content
type Text struct {
	Content             string   `json:"content"`
	MentionedList       []string `json:"mentioned_list,omitempty"`
	MentionedMobileList []string `json:"mentioned_mobile_list,omitempty"`
}

// Markdown is markdown content
type Markdown struct {
	Content string `json:"content"`
}

// Response is a WeCom webhook response
type Response struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// NewProvider creates a new WeCom provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	var webhookURL string

	// Try webhook_url first
	if url, ok := config["webhook_url"].(string); ok && url != "" {
		webhookURL = url
	} else if key, ok := config["key"].(string); ok && key != "" {
		webhookURL = fmt.Sprintf(defaultWebhookURL, key)
	} else {
		return nil, fmt.Errorf("wecom: webhook_url or key is required")
	}

	return &Provider{
		webhookURL: webhookURL,
		status: &core.ProviderStatus{
			Name:   "wecom",
			Type:   "builtin",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"markdown", "plain"},
	}
}

// Deliver delivers a task to WeCom
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	// Build message
	message := p.buildMessage(task)

	// Send message
	return p.sendMessage(ctx, message)
}

// buildMessage builds a WeCom message from a task
func (p *Provider) buildMessage(task *core.DeliveryTask) *Message {
	// Use markdown for rich formatting
	content := p.formatMarkdown(task)

	return &Message{
		MsgType: "markdown",
		Markdown: &Markdown{
			Content: content,
		},
	}
}

// formatMarkdown formats the task as markdown
func (p *Provider) formatMarkdown(task *core.DeliveryTask) string {
	title, body := extractContent(task)

	content := ""

	// Add level indicator
	switch task.Level {
	case "error":
		content += "<font color='warning'>**" + title + "**</font>\n\n"
	case "warning":
		content += "<font color='info'>**" + title + "**</font>\n\n"
	case "info":
		content += "**" + title + "**\n\n"
	default:
		content += "**" + title + "**\n\n"
	}

	// Add body
	if body != "" {
		content += body
	}

	// Add timestamp
	content += fmt.Sprintf("\n\n> %s", time.Now().Format("2006-01-02 15:04:05"))

	return content
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// sendMessage sends a message to WeCom
func (p *Provider) sendMessage(ctx context.Context, message *Message) error {
	resp, err := p.client.PostJSON(ctx, p.webhookURL, message)
	if err != nil {
		// Check if error is retryable
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	// Parse response
	var result Response
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// WeCom returns 0 for success
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom API error: %s", result.ErrMsg)
	}

	return nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "wecom"
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

// Factory creates WeCom providers
type Factory struct{}

// Create creates a new WeCom provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "wecom"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "builtin"
}
