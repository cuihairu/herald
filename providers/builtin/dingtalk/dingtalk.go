package dingtalk

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	webhookURLFormat = "https://oapi.dingtalk.com/robot/send?access_token=%s"
)

// Provider is a DingTalk provider
type Provider struct {
	webhookURL string
	secret     string
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Config is the DingTalk provider configuration
type Config struct {
	WebhookURL  string `yaml:"webhook_url"`  // Full webhook URL
	AccessToken string `yaml:"access_token"` // Access token
	Secret      string `yaml:"secret"`       // Signing secret (optional)
}

// Message is a DingTalk message
type Message struct {
	MsgType  string    `json:"msgtype"`
	Text     *Text     `json:"text,omitempty"`
	Markdown *Markdown `json:"markdown,omitempty"`
	Link     *Link     `json:"link,omitempty"`
	At       *At       `json:"at,omitempty"`
}

// Text is text content
type Text struct {
	Content string `json:"content"`
}

// Markdown is markdown content
type Markdown struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Link is link message
type Link struct {
	MessageUrl string `json:"messageUrl"`
	PicUrl     string `json:"picUrl,omitempty"`
	Title      string `json:"title"`
	Text       string `json:"text"`
}

// At is at info
type At struct {
	AtMobiles []string `json:"atMobiles,omitempty"`
	AtUserIds []string `json:"atUserIds,omitempty"`
	IsAtAll   bool     `json:"isAtAll,omitempty"`
}

// Response is a DingTalk API response
type Response struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// NewProvider creates a new DingTalk provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	var webhookURL string

	// Try full webhook URL first
	if url, ok := config["webhook_url"].(string); ok && url != "" {
		webhookURL = url
	} else if token, ok := config["access_token"].(string); ok && token != "" {
		webhookURL = fmt.Sprintf(webhookURLFormat, token)
	} else {
		return nil, fmt.Errorf("dingtalk: webhook_url or access_token is required")
	}

	secret, _ := config["secret"].(string)

	return &Provider{
		webhookURL: webhookURL,
		secret:     secret,
		status: &core.ProviderStatus{
			Name:   "dingtalk",
			Type:   "dingtalk",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// GetConfig returns the provider configuration
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"webhook_url": p.webhookURL,
		"secret":      p.secret,
	}
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"markdown", "plain"},
	}
}

// Deliver delivers a task to DingTalk
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	message := p.buildMessage(task)

	resp, err := p.client.PostJSON(ctx, p.webhookURL, message)
	if err != nil {
		// Check if error is retryable
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	// Check for DingTalk error response
	var result Response
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("dingtalk API error: %s", result.ErrMsg)
	}

	return nil
}

// buildMessage builds a DingTalk message from a task
func (p *Provider) buildMessage(task *core.DeliveryTask) *Message {
	title, _ := extractContent(task)

	// Build markdown content
	content := p.formatMarkdown(task)

	return &Message{
		MsgType: "markdown",
		Markdown: &Markdown{
			Title: title,
			Text:  content,
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
		content += "### <font color='#ff0000'>" + title + "</font>\n\n"
	case "warning":
		content += "### <font color='#ff9900'>" + title + "</font>\n\n"
	case "info":
		content += "### " + title + "\n\n"
	default:
		content += "### " + title + "\n\n"
	}

	// Add body
	if body != "" {
		content += body + "\n\n"
	}

	// Add separator
	content += "---\n\n"

	// Add timestamp
	content += "> " + time.Now().Format("2006-01-02 15:04:05")

	return content
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
	return "dingtalk"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "dingtalk"
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

// Factory creates DingTalk providers
type Factory struct{}

// Create creates a new DingTalk provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "dingtalk"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "dingtalk"
}
