package neteasesms

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	defaultEndpoint   = "api.netease.im"
	defaultAPIVersion = "v1"
)

// Provider is the NetEase (YunXin) SMS provider
type Provider struct {
	appKey    string
	appSecret string
	nonce     string
	endpoint  string
	enabled   bool
	status    *core.ProviderStatus
	client    *httpclient.Client
}

// Config is the NetEase SMS configuration
type Config struct {
	AppKey    string `yaml:"app_key"`
	AppSecret string `yaml:"app_secret"`
	Endpoint  string `yaml:"endpoint"` // default: api.netease.im
	Enabled   bool   `yaml:"enabled"`  // default: true
}

// SendSmsRequest is the request to send SMS
type SendSmsRequest struct {
	TemplateID string   `json:"templateid"`
	Mobiles    []string `json:"mobiles"`
	Params     []string `json:"params,omitempty"`
}

// SendSmsResponse is the response from NetEase
type SendSmsResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
	Object  string `json:"obj,omitempty"`
}

// SendCodeRequest is the request to send verification code
type SendCodeRequest struct {
	Mobile   string `json:"mobile"`
	AuthCode string `json:"authCode,omitempty"`
	DeviceID string `json:"deviceId,omitempty"`
}

// VerifyCodeRequest is the request to verify code
type VerifyCodeRequest struct {
	Mobile string `json:"mobile"`
	Code   string `json:"code"`
}

// NewProvider creates a new NetEase SMS provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	appKey, ok := config["app_key"].(string)
	if !ok || appKey == "" {
		return nil, fmt.Errorf("neteasesms: app_key is required")
	}

	appSecret, ok := config["app_secret"].(string)
	if !ok || appSecret == "" {
		return nil, fmt.Errorf("neteasesms: app_secret is required")
	}

	endpoint := defaultEndpoint
	if e, ok := config["endpoint"].(string); ok && e != "" {
		endpoint = e
	}

	enabled := true
	if e, ok := config["enabled"].(bool); ok {
		enabled = e
	}

	return &Provider{
		appKey:    appKey,
		appSecret: appSecret,
		nonce:     fmt.Sprintf("%d", time.Now().UnixNano()),
		endpoint:  endpoint,
		enabled:   enabled,
		status: &core.ProviderStatus{
			Name:    "neteasesms",
			Type:    "builtin",
			Status:  "available",
			Enabled: enabled,
			Since:   time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// GetConfig returns the provider configuration
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"app_key":    p.appKey,
		"app_secret": p.appSecret,
		"endpoint":   p.endpoint,
	}
}

// Deliver delivers a task to NetEase SMS
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	// Extract phone numbers from targets
	if len(task.Targets) == 0 {
		return fmt.Errorf("phone numbers are required")
	}

	// Clean and validate phone numbers
	mobiles := make([]string, 0, len(task.Targets))
	for _, num := range task.Targets {
		num = strings.TrimSpace(num)
		if num == "" {
			continue
		}
		mobiles = append(mobiles, num)
	}

	if len(mobiles) == 0 {
		return fmt.Errorf("no valid phone numbers")
	}

	// Extract template ID from provider template payload
	templateID := ""
	var params []string
	if task.Payload.ProviderTemplate != nil {
		templateID = task.Payload.ProviderTemplate.TemplateID
		if templateID == "" {
			templateID = task.Payload.ProviderTemplate.TemplateCode
		}
		// Build template params from Params
		if task.Payload.ProviderTemplate.Params != nil {
			if arr, ok := task.Payload.ProviderTemplate.Params.([]string); ok {
				params = arr
			} else if arr, ok := task.Payload.ProviderTemplate.Params.([]interface{}); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						params = append(params, s)
					}
				}
			}
		}
	}
	if templateID == "" {
		return fmt.Errorf("template_id is required")
	}

	// Build request
	reqBody := map[string]interface{}{
		"templateid": templateID,
		"mobiles":    strings.Join(mobiles, ","),
	}

	if len(params) > 0 {
		reqBody["params"] = strings.Join(params, ",")
	}

	// Send request
	err := p.sendRequest(ctx, "sendTemplateSms", reqBody)
	if err != nil {
		return httpclient.WithRetry(err)
	}

	return nil
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:     []core.PayloadKind{core.PayloadProviderTemplate},
		SupportsTemplate: true,
	}
}

// sendRequest sends the actual request to NetEase API
func (p *Provider) sendRequest(ctx context.Context, action string, body map[string]interface{}) error {
	// Calculate checksum
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := fmt.Sprintf("%d", time.Now().UnixNano())

	// Build checksum: sha1(appSecret + nonce + timestamp)
	checksum := p.calculateChecksum(nonce, timestamp)

	// Build URL
	reqURL := fmt.Sprintf("https://%s/%s/sms/%s", p.endpoint, defaultAPIVersion, action)

	// Build query parameters
	params := url.Values{}
	params.Set("appKey", p.appKey)
	params.Set("nonce", nonce)
	params.Set("curTime", timestamp)
	params.Set("checksum", checksum)

	// Add body parameters
	fullURL := reqURL + "?" + params.Encode()

	// Send request
	resp, err := p.client.PostJSON(ctx, fullURL, body)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Parse response
	var result SendSmsResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Code != 200 {
		return fmt.Errorf("neteasesms error: %d - %s", result.Code, result.Message)
	}

	return nil
}

// calculateChecksum calculates NetEase API checksum
func (p *Provider) calculateChecksum(nonce, timestamp string) string {
	// checksum = sha1(appSecret + nonce + curTime)
	data := p.appSecret + nonce + timestamp
	h := sha1.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// SendCode sends a verification code
func (p *Provider) SendCode(ctx context.Context, mobile, authCode, deviceID string) error {
	body := map[string]interface{}{
		"mobile": mobile,
	}

	if authCode != "" {
		body["authCode"] = authCode
	}
	if deviceID != "" {
		body["deviceId"] = deviceID
	}

	err := p.sendRequest(ctx, "sendCode", body)
	if err != nil {
		return httpclient.WithRetry(err)
	}

	return nil
}

// VerifyCode verifies a verification code
func (p *Provider) VerifyCode(ctx context.Context, mobile, code string) error {
	body := map[string]interface{}{
		"mobile": mobile,
		"code":   code,
	}

	err := p.sendRequest(ctx, "verifyCode", body)
	if err != nil {
		return err
	}

	return nil
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "neteasesms"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "neteasesms"
}

// Status returns the current status
func (p *Provider) Status() *core.ProviderStatus {
	p.status.Enabled = p.enabled
	return p.status
}

// Close closes the provider
func (p *Provider) Close() error {
	return nil
}

// Enable enables the provider
func (p *Provider) Enable() {
	p.enabled = true
	p.status.Enabled = true
}

// Disable disables the provider
func (p *Provider) Disable() {
	p.enabled = false
	p.status.Enabled = false
}

// IsEnabled returns whether the provider is enabled
func (p *Provider) IsEnabled() bool {
	return p.enabled
}

// Factory creates NetEase SMS providers
type Factory struct{}

// Create creates a new NetEase SMS provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "neteasesms"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "neteasesms"
}
