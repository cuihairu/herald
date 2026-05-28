package wechatmp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	tokenURL           = "https://api.weixin.qq.com/cgi-bin/token"
	templateMessageURL = "https://api.weixin.qq.com/cgi-bin/message/template/send"
)

// Provider is a WeChat Official Account provider
type Provider struct {
	appID      string
	appSecret  string
	templateID string
	defaultURL string
	tokenCache *TokenCache
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Config is the WeChat Official Account provider configuration
type Config struct {
	AppID      string `yaml:"app_id"`
	AppSecret  string `yaml:"app_secret"`
	TemplateID string `yaml:"template_id"`
	DefaultURL string `yaml:"default_url,omitempty"` // Optional: default link URL
}

// TokenCache manages access token caching
type TokenCache struct {
	token      string
	expireTime time.Time
	mu         sync.RWMutex
}

// tokenResponse is the access token API response
type tokenResponse struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// templateMessageRequest is the template message request
type templateMessageRequest struct {
	ToUser      string                  `json:"touser"`
	TemplateID  string                  `json:"template_id"`
	URL         string                  `json:"url,omitempty"`
	MiniProgram *MiniProgram            `json:"miniprogram,omitempty"`
	Data        map[string]TemplateData `json:"data"`
}

// MiniProgram is the mini program info
type MiniProgram struct {
	AppID    string `json:"appid"`
	PagePath string `json:"pagepath"`
}

// TemplateData is the template data item
type TemplateData struct {
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

// templateMessageResponse is the template message response
type templateMessageResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	MsgID   int64  `json:"msgid"`
}

// NewProvider creates a new WeChat Official Account provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	if cfg.AppID == "" {
		return nil, fmt.Errorf("wechatmp: app_id is required")
	}
	if cfg.AppSecret == "" {
		return nil, fmt.Errorf("wechatmp: app_secret is required")
	}
	if cfg.TemplateID == "" {
		return nil, fmt.Errorf("wechatmp: template_id is required")
	}

	return &Provider{
		appID:      cfg.AppID,
		appSecret:  cfg.AppSecret,
		templateID: cfg.TemplateID,
		defaultURL: cfg.DefaultURL,
		tokenCache: &TokenCache{},
		status: &core.ProviderStatus{
			Name:   "wechatmp",
			Type:   "wechatmp",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// parseConfig parses the configuration
func parseConfig(config map[string]interface{}) (*Config, error) {
	cfg := &Config{}

	if appID, ok := config["app_id"].(string); ok {
		cfg.AppID = appID
	}
	if appSecret, ok := config["app_secret"].(string); ok {
		cfg.AppSecret = appSecret
	}
	if templateID, ok := config["template_id"].(string); ok {
		cfg.TemplateID = templateID
	}
	if defaultURL, ok := config["default_url"].(string); ok {
		cfg.DefaultURL = defaultURL
	}

	return cfg, nil
}

// Deliver delivers a task to WeChat Official Account
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("wechatmp: task is nil")
	}

	// Get target OpenID (required for WeChat, single target)
	if len(task.Targets) == 0 || task.Targets[0] == "" {
		return fmt.Errorf("wechatmp: target (OpenID) is required")
	}
	toUser := task.Targets[0]

	// Get access token
	token, err := p.tokenCache.GetToken(p.appID, p.appSecret, p.client)
	if err != nil {
		return fmt.Errorf("wechatmp: failed to get access token: %w", err)
	}

	// Build template message
	msg := p.buildTemplateMessage(task)

	// Send template message
	return p.sendTemplateMessage(ctx, token, toUser, msg)
}

// buildTemplateMessage builds a template message from a task
func (p *Provider) buildTemplateMessage(task *core.DeliveryTask) *templateMessageRequest {
	// Format content for template
	title, content := extractPayloadContent(task)

	// Build template data with common fields
	data := map[string]TemplateData{
		"thing1": {Value: truncate(title, 20)},   // 事项/标题
		"thing2": {Value: truncate(content, 30)}, // 内容
	}

	// Add level if present
	if task.Level != "" {
		data["character_string1"] = TemplateData{Value: task.Level}
	}

	// Add timestamp
	data["time3"] = TemplateData{Value: time.Now().Format("2006-01-02 15:04:05")}

	// Use provider template TemplateID if available, otherwise use configured default
	tmplID := p.templateID
	if task.Payload.ProviderTemplate != nil && task.Payload.ProviderTemplate.TemplateID != "" {
		tmplID = task.Payload.ProviderTemplate.TemplateID
	}

	return &templateMessageRequest{
		TemplateID: tmplID,
		URL:        p.defaultURL,
		Data:       data,
	}
}

// extractPayloadContent extracts title and body from a DeliveryTask
func extractPayloadContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:     []core.PayloadKind{core.PayloadProviderTemplate, core.PayloadContent},
		SupportsTemplate: true,
	}
}

// sendTemplateMessage sends a template message
func (p *Provider) sendTemplateMessage(ctx context.Context, token, toUser string, msg *templateMessageRequest) error {
	url := fmt.Sprintf("%s?access_token=%s", templateMessageURL, token)

	// Set ToUser
	msg.ToUser = toUser

	resp, err := p.client.PostJSON(ctx, url, msg)
	if err != nil {
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	var result templateMessageResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("wechatmp: failed to parse response: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("wechatmp: API error %d: %s", result.ErrCode, result.ErrMsg)
	}

	return nil
}

// truncate truncates a string to max length
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return s
}

// GetAccessToken retrieves a new access token
func (p *Provider) GetAccessToken() (string, error) {
	url := fmt.Sprintf("%s?grant_type=client_credential&appid=%s&secret=%s",
		tokenURL, p.appID, p.appSecret)

	resp, err := p.client.Get(context.Background(), url)
	if err != nil {
		return "", err
	}

	var result tokenResponse
	if err := resp.JSON(&result); err != nil {
		return "", err
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechatmp: token error %d: %s", result.ErrCode, result.ErrMsg)
	}

	return result.AccessToken, nil
}

// GetToken returns a valid access token, using cache if available
func (c *TokenCache) GetToken(appID, appSecret string, client *httpclient.Client) (string, error) {
	c.mu.RLock()
	if time.Now().Before(c.expireTime) && c.token != "" {
		c.mu.RUnlock()
		return c.token, nil
	}
	c.mu.RUnlock()

	// Token expired or not set, get new one
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double check after acquiring write lock
	if time.Now().Before(c.expireTime) && c.token != "" {
		return c.token, nil
	}

	// Get new token
	url := fmt.Sprintf("%s?grant_type=client_credential&appid=%s&secret=%s",
		tokenURL, appID, appSecret)

	resp, err := client.Get(context.Background(), url)
	if err != nil {
		return "", err
	}

	var result tokenResponse
	if err := resp.JSON(&result); err != nil {
		return "", err
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("wechatmp: token error %d: %s", result.ErrCode, result.ErrMsg)
	}

	// Cache token with 5 minutes buffer before expiration
	expiresIn := result.ExpiresIn
	if expiresIn == 0 {
		expiresIn = 7200 // default 2 hours
	}
	if expiresIn > 3600 {
		expiresIn -= 300 // refresh 5 minutes early
	}

	c.token = result.AccessToken
	c.expireTime = time.Now().Add(time.Duration(expiresIn) * time.Second)

	return c.token, nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "wechatmp"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "wechatmp"
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

// Factory creates WeChat Official Account providers
type Factory struct{}

// Create creates a new WeChat Official Account provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "wechatmp"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "wechatmp"
}
