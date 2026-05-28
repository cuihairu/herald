package discord

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	defaultAPIURL    = "https://discord.com/api/v10/channels/%s/messages"
	webhookURLFormat = "https://discord.com/api/webhooks/%s/%s"
)

// Provider is a Discord provider
type Provider struct {
	webhookURL string
	botToken   string
	channelID  string
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Config is the Discord provider configuration
type Config struct {
	WebhookURL   string `yaml:"webhook_url"`   // Full webhook URL
	WebhookID    string `yaml:"webhook_id"`    // Webhook ID
	WebhookToken string `yaml:"webhook_token"` // Webhook Token
	BotToken     string `yaml:"bot_token"`     // Bot token (for bot API)
	ChannelID    string `yaml:"channel_id"`    // Channel ID (for bot API)
}

// WebhookPayload is the Discord webhook payload
type WebhookPayload struct {
	Username  string  `json:"username,omitempty"`
	AvatarURL string  `json:"avatar_url,omitempty"`
	Content   string  `json:"content,omitempty"`
	Embeds    []Embed `json:"embeds,omitempty"`
}

// Embed is a Discord embed
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// EmbedField is an embed field
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// BotMessage is a message sent via bot API
type BotMessage struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// ErrorResponse is a Discord error response
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewProvider creates a new Discord provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	var webhookURL string

	// Try full webhook URL first
	if url, ok := config["webhook_url"].(string); ok && url != "" {
		webhookURL = url
	} else if id, tokenOk := config["webhook_id"].(string); tokenOk {
		token, ok := config["webhook_token"].(string)
		if !ok {
			return nil, fmt.Errorf("discord: webhook_token is required when using webhook_id")
		}
		webhookURL = fmt.Sprintf(webhookURLFormat, id, token)
	}

	botToken, _ := config["bot_token"].(string)
	channelID, _ := config["channel_id"].(string)

	// Need either webhook or bot token
	if webhookURL == "" && (botToken == "" || channelID == "") {
		return nil, fmt.Errorf("discord: either webhook_url or bot_token+channel_id is required")
	}

	return &Provider{
		webhookURL: webhookURL,
		botToken:   botToken,
		channelID:  channelID,
		status: &core.ProviderStatus{
			Name:   "discord",
			Type:   "discord",
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

// Deliver delivers a task to Discord
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	// Prefer webhook
	if p.webhookURL != "" {
		return p.sendWebhook(ctx, task)
	}

	// Fall back to bot API
	return p.sendBotMessage(ctx, task)
}

// sendWebhook sends a message via webhook
func (p *Provider) sendWebhook(ctx context.Context, task *core.DeliveryTask) error {
	payload := p.buildWebhookPayload(task)

	resp, err := p.client.PostJSON(ctx, p.webhookURL, payload)
	if err != nil {
		// Check if error is retryable
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	// Check for Discord error response
	var errResp ErrorResponse
	if err := resp.JSON(&errResp); err == nil && errResp.Code != 0 {
		return fmt.Errorf("discord API error: %s", errResp.Message)
	}

	return nil
}

// sendBotMessage sends a message via bot API
func (p *Provider) sendBotMessage(ctx context.Context, task *core.DeliveryTask) error {
	embed := p.buildEmbed(task)
	payload := &BotMessage{
		Embeds: []Embed{embed},
	}

	url := fmt.Sprintf(defaultAPIURL, p.channelID)

	req, err := httpclient.NewClient(nil).PostJSON(ctx, url, payload)
	if err != nil {
		return err
	}

	// Check for Discord error response
	var errResp ErrorResponse
	if err := req.JSON(&errResp); err == nil && errResp.Code != 0 {
		return fmt.Errorf("discord API error: %s", errResp.Message)
	}

	return nil
}

// buildWebhookPayload builds a webhook payload
func (p *Provider) buildWebhookPayload(task *core.DeliveryTask) *WebhookPayload {
	embed := p.buildEmbed(task)

	return &WebhookPayload{
		Username: "Herald",
		Content:  "",
		Embeds:   []Embed{embed},
	}
}

// buildEmbed builds an embed from a task
func (p *Provider) buildEmbed(task *core.DeliveryTask) Embed {
	title, body := extractContent(task)

	// Determine color based on level
	color := p.getColor(task.Level)

	embed := Embed{
		Title:       title,
		Description: body,
		Color:       color,
		Timestamp:   task.CreatedAt.Format(time.RFC3339),
	}

	return embed
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// getColor returns a color code based on level
func (p *Provider) getColor(level string) int {
	switch level {
	case "error":
		return 16711680 // Red
	case "warning":
		return 16776960 // Orange
	case "info":
		return 3447003 // Blue
	default:
		return 9807270 // Gray
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "discord"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "discord"
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

// Factory creates Discord providers
type Factory struct{}

// Create creates a new Discord provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "discord"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "discord"
}
