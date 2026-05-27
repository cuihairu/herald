package tencentsms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	defaultEndpoint = "sms.tencentcloudapi.com"
	defaultVersion  = "2021-01-11"
	defaultAction   = "SendSms"
)

// Provider is the Tencent SMS provider
type Provider struct {
	secretID  string
	secretKey string
	appID     string
	signName  string
	region    string
	endpoint  string
	enabled   bool
	status    *core.ProviderStatus
	client    *httpclient.Client
}

// Config is the Tencent SMS configuration
type Config struct {
	SecretID  string `yaml:"secret_id"`
	SecretKey string `yaml:"secret_key"`
	AppID     string `yaml:"app_id"`
	SignName  string `yaml:"sign_name"`
	Region    string `yaml:"region"`    // default: ap-guangzhou
	Endpoint  string `yaml:"endpoint"`  // default: sms.tencentcloudapi.com
	Enabled   bool   `yaml:"enabled"`   // default: true
}

// SendSmsRequest is the request to send SMS
type SendSmsRequest struct {
	SmsSdkAppId      string   `json:"SmsSdkAppId"`
	SignName         string   `json:"SignName"`
	PhoneNumberSet   []string `json:"PhoneNumberSet"`
	TemplateID       string   `json:"TemplateId"`
	TemplateParamSet []string `json:"TemplateParamSet,omitempty"`
	SessionContext   string   `json:"SessionContext,omitempty"`
}

// SendSmsResponse is the response from Tencent
type SendSmsResponse struct {
	Response struct {
		SendStatusSet []struct {
			SerialNo     string `json:"SerialNo"`
			PhoneNumber  string `json:"PhoneNumber"`
			Fee          int    `json:"Fee"`
			SessionContext string `json:"SessionContext"`
			Code         string `json:"Code"`
			Message      string `json:"Message"`
		} `json:"SendStatusSet"`
		RequestId string `json:"RequestId"`
	} `json:"Response"`
	Error *struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	} `json:"Error,omitempty"`
}

// NewProvider creates a new Tencent SMS provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	secretID, ok := config["secret_id"].(string)
	if !ok || secretID == "" {
		return nil, fmt.Errorf("tencentsms: secret_id is required")
	}

	secretKey, ok := config["secret_key"].(string)
	if !ok || secretKey == "" {
		return nil, fmt.Errorf("tencentsms: secret_key is required")
	}

	appID, ok := config["app_id"].(string)
	if !ok || appID == "" {
		return nil, fmt.Errorf("tencentsms: app_id is required")
	}

	signName, _ := config["sign_name"].(string)

	region := "ap-guangzhou"
	if r, ok := config["region"].(string); ok && r != "" {
		region = r
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
		secretID:  secretID,
		secretKey: secretKey,
		appID:     appID,
		signName:  signName,
		region:    region,
		endpoint:  endpoint,
		enabled:   enabled,
		status: &core.ProviderStatus{
			Name:    "tencentsms",
			Type:    "builtin",
			Status:  "available",
			Enabled: enabled,
			Since:   time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// Deliver delivers a task to Tencent SMS
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if !p.enabled {
		return fmt.Errorf("provider is disabled")
	}

	// Extract phone numbers from targets and add +86 prefix if not present
	if len(task.Targets) == 0 {
		return fmt.Errorf("phone numbers are required")
	}
	phoneNumberSet := make([]string, 0, len(task.Targets))
	for _, num := range task.Targets {
		num = strings.TrimSpace(num)
		if !strings.HasPrefix(num, "+") {
			num = "+86" + num
		}
		phoneNumberSet = append(phoneNumberSet, num)
	}

	// Extract template ID from provider template payload
	templateID := ""
	templateParamSet := []string{}
	if task.Payload.ProviderTemplate != nil {
		templateID = task.Payload.ProviderTemplate.TemplateID
		// Build template params from Params
		if params := task.Payload.ProviderTemplate.Params; params != nil {
			if arr, ok := params.([]string); ok {
				templateParamSet = arr
			} else if arr, ok := params.([]interface{}); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						templateParamSet = append(templateParamSet, s)
					}
				}
			}
		}
	}
	if templateID == "" {
		return fmt.Errorf("template_id is required")
	}

	// Build request
	reqBody := SendSmsRequest{
		SmsSdkAppId:      p.appID,
		SignName:         p.signName,
		PhoneNumberSet:   phoneNumberSet,
		TemplateID:       templateID,
		TemplateParamSet: templateParamSet,
	}

	// Send request
	err := p.sendRequest(ctx, reqBody)
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

// sendRequest sends the actual request to Tencent Cloud API
func (p *Provider) sendRequest(ctx context.Context, reqBody SendSmsRequest) error {
	// Build request URL
	reqURL := fmt.Sprintf("https://%s/", p.endpoint)

	// Build query parameters
	timestamp := time.Now().Unix()
	params := url.Values{}
	params.Set("Action", defaultAction)
	params.Set("Version", defaultVersion)
	params.Set("Region", p.region)
	params.Set("Timestamp", fmt.Sprintf("%d", timestamp))
	params.Set("Nonce", fmt.Sprintf("%d", timestamp))
	params.Set("SecretId", p.secretID)

	// Build body
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL+"?"+params.Encode(), strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", p.endpoint)
	req.Header.Set("X-TC-Action", defaultAction)
	req.Header.Set("X-TC-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-TC-Version", defaultVersion)
	req.Header.Set("X-TC-Region", p.region)

	// Calculate signature
	authorization := p.calculateAuthorization(req, string(bodyBytes))
	req.Header.Set("Authorization", authorization)

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var result SendSmsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return fmt.Errorf("tencent SMS error: %s - %s", result.Error.Code, result.Error.Message)
	}

	// Check individual status
	for _, status := range result.Response.SendStatusSet {
		if status.Code != "Ok" {
			return fmt.Errorf("tencent SMS error for %s: %s - %s", status.PhoneNumber, status.Code, status.Message)
		}
	}

	return nil
}

// calculateAuthorization calculates the Tencent Cloud API signature
func (p *Provider) calculateAuthorization(req *http.Request, body string) string {
	timestamp := req.Header.Get("X-TC-Timestamp")

	// Build canonical request
	canonicalHeaders := "content-type:" + req.Header.Get("Content-Type") + "\n" +
		"host:" + req.Header.Get("Host") + "\n" +
		"x-tc-action:" + req.Header.Get("X-TC-Action") + "\n" +
		"x-tc-timestamp:" + timestamp + "\n" +
		"x-tc-version:" + req.Header.Get("X-TC-Version") + "\n"

	signedHeaders := "content-type;host;x-tc-action;x-tc-timestamp;x-tc-version"

	hash := sha256.New()
	hash.Write([]byte(body))
	payloadHash := hex.EncodeToString(hash.Sum(nil))

	canonicalRequest := req.Method + "\n" +
		"/" + "\n" +
		"" + "\n" +
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		payloadHash

	// Build string to sign
	hash = sha256.New()
	hash.Write([]byte(canonicalRequest))
	canonicalRequestHash := hex.EncodeToString(hash.Sum(nil))

	credentialScope := timestamp + "/" + "sms/tc3_request"
	stringToSign := "TC3-HMAC-SHA256" + "\n" +
		timestamp + "\n" +
		credentialScope + "\n" +
		canonicalRequestHash

	// Calculate signature
	secretDate := hmacSha256("TC3"+p.secretKey, timestamp)
	secretService := hmacSha256(secretDate, "sms")
	secretSigning := hmacSha256(secretService, "tc3_request")
	signature := hmacSha256(secretSigning, stringToSign)

	// Build authorization
	authorization := fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.secretID, credentialScope, signedHeaders, signature)

	return authorization
}

func hmacSha256(key, data string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "tencentsms"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "builtin"
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

// Factory creates Tencent SMS providers
type Factory struct{}

// Create creates a new Tencent SMS provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "tencentsms"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "builtin"
}
