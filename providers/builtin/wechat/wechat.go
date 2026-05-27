package wechat

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	// ServerChan API
	serverChanURL = "https://sctapi.ftqq.com/%s.send"

	// PushPlus API
	pushPlusURL = "https://www.pushplus.plus/send"

	// WxPusher API
	wxpusherURL = "https://wxpusher.zjiecode.com/api/send/message"
)

// Provider is a WeChat Personal provider
type Provider struct {
	service    string
	appToken   string
	url        string
	uid        string // WxPusher UID
	status     *core.ProviderStatus
	client     *httpclient.Client
}

// Config is the WeChat Personal provider configuration
type Config struct {
	Service string `yaml:"service"` // serverchan, pushplus, wxpusher

	// ServerChan
	SendKey string `yaml:"send_key"`

	// PushPlus
	Token string `yaml:"token"`

	// WxPusher
	AppToken string `yaml:"app_token"`
	UID      string `yaml:"uid"` // optional, empty means send to all UIDs
}

// serverChanRequest is a ServerChan request
type serverChanRequest struct {
	Title   string `json:"title"`
	Desp    string `json:"desp"`
	Short   string `json:"short,omitempty"`
	Channel string `json:"channel,omitempty"`
	OpenID   string `json:"openid,omitempty"`
}

// serverChanResponse is a ServerChan response
type serverChanResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Timestamp int64  `json:"timestamp"`
		SendTime  string `json:"sendtime"`
	} `json:"data"`
}

// pushPlusRequest is a PushPlus request
type pushPlusRequest struct {
	Token     string            `json:"token"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Template  string            `json:"template,omitempty"` // html, json, txt
	Topic     string            `json:"topic,omitempty"`
	Channel   string            `json:"channel,omitempty"`
	CallbackURL string           `json:"callbackUrl,omitempty"`
	Timestamp  string            `json:"timestamp,omitempty"`
	MD5        string            `json:"md5,omitempty"`
	Extras     map[string]string `json:"extras,omitempty"`
}

// pushPlusResponse is a PushPlus response
type pushPlusResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data string `json:"data"`
}

// wxpusherRequest is a WxPusher request
type wxpusherRequest struct {
	AppToken    string `json:"appToken"`
	Content     string `json:"content"`
	Summary     string `json:"summary"`     // 消息摘要，显示在微信通知标题
	ContentType int    `json:"contentType"` // 1:文字  2:HTML  3:Markdown
	UIDs        string `json:"uids"`        // 要接收消息的用户ID，如果不为空则以此为准
	URL         string `json:"url,omitempty"` // 原文链接，点击消息跳转
}

// wxpusherResponse is a WxPusher response
type wxpusherResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data string `json:"data"`
	Success bool `json:"success"`
}

// NewProvider creates a new WeChat Personal provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, err
	}

	p := &Provider{
		status: &core.ProviderStatus{
			Name:     "wechat",
			Type:     "builtin",
			Status:   "available",
			Since:    time.Now(),
		},
		client: httpclient.NewClient(nil),
	}

	switch cfg.Service {
	case "serverchan":
		if cfg.SendKey == "" {
			return nil, fmt.Errorf("wechat: send_key is required for serverchan")
		}
		p.service = "serverchan"
		p.url = fmt.Sprintf(serverChanURL, cfg.SendKey)

	case "pushplus":
		if cfg.Token == "" {
			return nil, fmt.Errorf("wechat: token is required for pushplus")
		}
		p.service = "pushplus"
		p.url = pushPlusURL
		p.appToken = cfg.Token

	case "wxpusher":
		if cfg.AppToken == "" {
			return nil, fmt.Errorf("wechat: app_token is required for wxpusher")
		}
		p.service = "wxpusher"
		p.url = wxpusherURL
		p.appToken = cfg.AppToken
		p.uid = cfg.UID

	default:
		return nil, fmt.Errorf("wechat: unsupported service '%s', use: serverchan, pushplus, or wxpusher", cfg.Service)
	}

	return p, nil
}

// parseConfig parses the configuration
func parseConfig(config map[string]interface{}) (*Config, error) {
	cfg := &Config{}

	if service, ok := config["service"].(string); ok {
		cfg.Service = service
	} else {
		cfg.Service = "serverchan" // default
	}

	if sendKey, ok := config["send_key"].(string); ok {
		cfg.SendKey = sendKey
	}
	if token, ok := config["token"].(string); ok {
		cfg.Token = token
	}
	if appToken, ok := config["app_token"].(string); ok {
		cfg.AppToken = appToken
	}
	if uid, ok := config["uid"].(string); ok {
		cfg.UID = uid
	}

	return cfg, nil
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent},
		ContentFormats: []string{"plain"},
	}
}

// Deliver delivers a task to WeChat Personal
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("wechat: task is nil")
	}

	switch p.service {
	case "serverchan":
		return p.sendServerChan(ctx, task)
	case "pushplus":
		return p.sendPushPlus(ctx, task)
	case "wxpusher":
		return p.sendWxPusher(ctx, task)
	default:
		return fmt.Errorf("wechat: unknown service '%s'", p.service)
	}
}

// sendServerChan sends via ServerChan
func (p *Provider) sendServerChan(ctx context.Context, task *core.DeliveryTask) error {
	title, body := extractContent(task)
	content := formatContent(task)

	req := &serverChanRequest{
		Title: title,
		Desp:  content,
	}

	// Use body as short description if available
	if body != "" {
		runes := []rune(body)
		if len(runes) > 64 {
			req.Short = string(runes[:64])
		} else {
			req.Short = body
		}
	}

	resp, err := p.client.PostJSON(ctx, p.url, req)
	if err != nil {
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	var result serverChanResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("wechat: failed to parse response: %w", err)
	}

	if result.Code != 0 {
		return fmt.Errorf("wechat: serverchan error: %s", result.Message)
	}

	return nil
}

// sendPushPlus sends via PushPlus
func (p *Provider) sendPushPlus(ctx context.Context, task *core.DeliveryTask) error {
	title, _ := extractContent(task)
	content := formatContent(task)

	req := &pushPlusRequest{
		Token:   p.appToken,
		Title:   title,
		Content: content,
	}

	resp, err := p.client.PostJSON(ctx, p.url, req)
	if err != nil {
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	var result pushPlusResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("wechat: failed to parse response: %w", err)
	}

	if result.Code != 200 {
		return fmt.Errorf("wechat: pushplus error: %s", result.Msg)
	}

	return nil
}

// sendWxPusher sends via WxPusher
func (p *Provider) sendWxPusher(ctx context.Context, task *core.DeliveryTask) error {
	title, _ := extractContent(task)
	content := formatContent(task)

	req := &wxpusherRequest{
		AppToken:    p.appToken,
		Content:     content,
		Summary:     title,
		ContentType: 3, // Markdown
	}

	if p.uid != "" {
		req.UIDs = p.uid
	}

	resp, err := p.client.PostJSON(ctx, p.url, req)
	if err != nil {
		if httpclient.IsRetryable(err) {
			return httpclient.WithRetry(err)
		}
		return err
	}

	var result wxpusherResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("wechat: failed to parse response: %w", err)
	}

	if result.Code != 1000 && result.Code != 0 {
		return fmt.Errorf("wechat: wxpusher error: %s", result.Msg)
	}

	return nil
}

// formatContent formats the task content
func formatContent(task *core.DeliveryTask) string {
	_, body := extractContent(task)

	content := ""

	// Add level indicator
	if task.Level != "" {
		levelIcon := ""
		switch task.Level {
		case "error", "critical":
			levelIcon = "🔴"
		case "warning":
			levelIcon = "🟡"
		case "info":
			levelIcon = "🟢"
		case "debug":
			levelIcon = "⚪"
		}
		if levelIcon != "" {
			content += levelIcon + " "
		}
	}

	// Add body
	if body != "" {
		content += body
	}

	// Add timestamp
	content += fmt.Sprintf("\n\n---\n%s", time.Now().Format("2006-01-02 15:04:05"))

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
	return "wechat"
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

// Factory creates WeChat providers
type Factory struct{}

// Create creates a new WeChat provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "wechat"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "builtin"
}
