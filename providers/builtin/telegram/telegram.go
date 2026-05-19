package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	defaultAPIURL = "https://api.telegram.org/bot%s/%s"
	sendMessageMethod = "sendMessage"
)

// Provider is a Telegram provider
type Provider struct {
	token     string
	chatID    string
	parseMode string
	status    *core.ProviderStatus
	client    *httpclient.Client
}

// Config is the Telegram provider configuration
type Config struct {
	Token     string `yaml:"token"`
	ChatID    string `yaml:"chat_id"`
	ParseMode string `yaml:"parse_mode"` // markdown, html, empty
}

// SendMessageRequest is the request for sendMessage
type SendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// SendMessageResponse is the response for sendMessage
type SendMessageResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

// NewProvider creates a new Telegram provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	token, ok := config["token"].(string)
	if !ok || token == "" {
		return nil, fmt.Errorf("telegram: token is required")
	}

	chatID, ok := config["chat_id"].(string)
	if !ok || chatID == "" {
		return nil, fmt.Errorf("telegram: chat_id is required")
	}

	parseMode, _ := config["parse_mode"].(string)

	return &Provider{
		token:     token,
		chatID:    chatID,
		parseMode: parseMode,
		status: &core.ProviderStatus{
			Name:     "telegram",
			Type:     "builtin",
			Status:   "available",
			Since:    time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// Deliver delivers a task to Telegram
func (p *Provider) Deliver(ctx context.Context, task *core.Task) error {
	// Build message
	message := p.formatMessage(task)

	// Send message
	return p.sendMessage(ctx, message)
}

// formatMessage formats the task as a Telegram message
func (p *Provider) formatMessage(task *core.Task) string {
	message := ""

	// Add level emoji
	switch task.Level {
	case "error":
		message += "🔴 "
	case "warning":
		message += "🟡 "
	case "info":
		message += "🔵 "
	}

	// Add title
	message += "*" + task.Title + "*\n\n"

	// Add body
	if task.Body != "" {
		message += task.Body
	}

	return message
}

// sendMessage sends a message to Telegram
func (p *Provider) sendMessage(ctx context.Context, message string) error {
	url := fmt.Sprintf(defaultAPIURL, p.token, sendMessageMethod)

	req := &SendMessageRequest{
		ChatID:    p.chatID,
		Text:      message,
		ParseMode: p.parseMode,
	}

	resp, err := p.client.PostJSON(ctx, url, req)
	if err != nil {
		// Check if error is retryable
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	// Parse response
	var result SendMessageResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("telegram API error: %s", result.Description)
	}

	return nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "telegram"
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

// Factory creates Telegram providers
type Factory struct{}

// Create creates a new Telegram provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "telegram"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "builtin"
}
