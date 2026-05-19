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
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Config is the Feishu provider configuration
type Config struct {
	WebhookURL string `yaml:"webhook_url"`
	SignSecret string `yaml:"sign_secret"`
}

// Message is a Feishu message
type Message struct {
	MsgType string      `json:"msg_type"`
	Content interface{} `json:"content"`
}

// TextContent is text content
type TextContent struct {
	Text string `json:"text"`
}

// PostContent is post content
type PostContent struct {
	Post map[string]struct {
		Title   string                   `json:"title,omitempty"`
		Content [][]map[string]interface{} `json:"content"`
	} `json:"post"`
}

// Response is a Feishu webhook response
type Response struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
}

// NewProvider creates a new Feishu provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	webhookURL, ok := config["webhook_url"].(string)
	if !ok || webhookURL == "" {
		return nil, fmt.Errorf("feishu: webhook_url is required")
	}

	signSecret, _ := config["sign_secret"].(string)

	return &Provider{
		webhookURL: webhookURL,
		signSecret: signSecret,
		status: &core.ProviderStatus{
			Name:     "feishu",
			Type:     "builtin",
			Status:   "available",
			Since:    time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// Deliver delivers a task to Feishu
func (p *Provider) Deliver(ctx context.Context, task *core.Task) error {
	// Build message
	message := p.buildMessage(task)

	// Send message
	return p.sendMessage(ctx, message)
}

// buildMessage builds a Feishu message from a task
func (p *Provider) buildMessage(task *core.Task) *Message {
	// Build content
	content := p.formatMessage(task)

	return &Message{
		MsgType: "text",
		Content: TextContent{
			Text: content,
		},
	}
}

// formatMessage formats the task as a Feishu message
func (p *Provider) formatMessage(task *core.Task) string {
	message := ""

	// Add level indicator
	switch task.Level {
	case "error":
		message += "[错误] "
	case "warning":
		message += "[警告] "
	case "info":
		message += "[信息] "
	}

	// Add title
	message += task.Title + "\n\n"

	// Add body
	if task.Body != "" {
		message += task.Body
	}

	return message
}

// sendMessage sends a message to Feishu
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
	return "builtin"
}
