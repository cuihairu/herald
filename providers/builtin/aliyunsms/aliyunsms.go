package aliyunsms

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	defaultEndpoint = "dysmsapi.aliyuncs.com"
	defaultVersion  = "2017-05-25"
	defaultAction   = "SendSms"
)

// Provider is the Aliyun SMS provider
type Provider struct {
	accessKeyID     string
	accessKeySecret string
	signName        string
	region          string
	endpoint        string
	enabled         bool
	status          *core.ProviderStatus
	client          *httpclient.Client
}

// Config is the Aliyun SMS configuration
type Config struct {
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
	SignName        string `yaml:"sign_name"`
	Region          string `yaml:"region"`          // default: cn-hangzhou
	Endpoint        string `yaml:"endpoint"`        // default: dysmsapi.aliyuncs.com
	Enabled         bool   `yaml:"enabled"`         // default: true
}

// SendSmsRequest is the request to send SMS
type SendSmsRequest struct {
	PhoneNumbers  string `json:"phone_numbers"`
	SignName      string `json:"sign_name"`
	TemplateCode  string `json:"template_code"`
	TemplateParam string `json:"template_param,omitempty"`
}

// SendSmsResponse is the response from Aliyun
type SendSmsResponse struct {
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	BizId     string `json:"BizId"`
	RequestId string `json:"RequestId"`
}

// NewProvider creates a new Aliyun SMS provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	accessKeyID, ok := config["access_key_id"].(string)
	if !ok || accessKeyID == "" {
		return nil, fmt.Errorf("aliyunsms: access_key_id is required")
	}

	accessKeySecret, ok := config["access_key_secret"].(string)
	if !ok || accessKeySecret == "" {
		return nil, fmt.Errorf("aliyunsms: access_key_secret is required")
	}

	signName, _ := config["sign_name"].(string)
	if signName == "" {
		return nil, fmt.Errorf("aliyunsms: sign_name is required")
	}

	region := "cn-hangzhou"
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
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		signName:        signName,
		region:          region,
		endpoint:        endpoint,
		enabled:         enabled,
		status: &core.ProviderStatus{
			Name:    "aliyunsms",
			Type:    "builtin",
			Status:  "available",
			Enabled: enabled,
			Since:   time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// Deliver delivers a task to Aliyun SMS
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if !p.enabled {
		return fmt.Errorf("provider is disabled")
	}

	// Extract phone numbers from targets
	if len(task.Targets) == 0 {
		return fmt.Errorf("phone numbers are required")
	}
	phoneNumbers := strings.Join(task.Targets, ",")

	// Extract template code and params from provider template payload
	templateCode := ""
	var templateParam string
	if task.Payload.ProviderTemplate != nil {
		templateCode = task.Payload.ProviderTemplate.TemplateCode
		// Build template param from Params
		if params := task.Payload.ProviderTemplate.Params; params != nil {
			if m, ok := params.(map[string]interface{}); ok {
				if b, err := json.Marshal(m); err == nil {
					templateParam = string(b)
				}
			}
		}
	}
	if templateCode == "" {
		templateCode = "SMS_DEFAULT" // Default template
	}

	// Build request params
	params := map[string]string{
		"Action":          defaultAction,
		"Version":         defaultVersion,
		"AccessKeyId":     p.accessKeyID,
		"SignatureMethod": "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":  fmt.Sprintf("%d", time.Now().UnixNano()),
		"Timestamp":       time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Format":          "JSON",
		"RegionId":        p.region,
		"PhoneNumbers":    phoneNumbers,
		"SignName":        p.signName,
		"TemplateCode":    templateCode,
	}

	if templateParam != "" {
		params["TemplateParam"] = templateParam
	}

	// Sign request
	signature := p.sign(params, "POST")
	params["Signature"] = signature

	// Build URL
	reqURL := fmt.Sprintf("https://%s/", p.endpoint)

	// Send request
	resp, err := p.client.PostJSON(ctx, reqURL, params)
	if err != nil {
		return httpclient.WithRetry(err)
	}

	// Parse response
	var result SendSmsResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Code != "OK" {
		return fmt.Errorf("aliyun SMS error: %s - %s", result.Code, result.Message)
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

// sign signs the request parameters
func (p *Provider) sign(params map[string]string, method string) string {
	// Sort keys
	keys := make([]string, 0, len(params))
	for k := range params {
		if k != "Signature" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	// Build query string
	var query strings.Builder
	for i, k := range keys {
		if i > 0 {
			query.WriteString("&")
		}
		query.WriteString(specialEncode(k))
		query.WriteString("=")
		query.WriteString(specialEncode(params[k]))
	}

	// String to sign
	stringToSign := method + "&" + specialEncode("/") + "&" + specialEncode(query.String())

	// HMAC-SHA1
	h := hmac.New(sha1.New, []byte(p.accessKeySecret+"&"))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// specialEncode encodes a string for Aliyun signature
func specialEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "aliyunsms"
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

// Factory creates Aliyun SMS providers
type Factory struct{}

// Create creates a new Aliyun SMS provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "aliyunsms"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "builtin"
}
