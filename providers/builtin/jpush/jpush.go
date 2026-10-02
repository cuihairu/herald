package jpush

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	// defaultEndpoint is the JPush REST API v3 base. The endpoint is
	// stable, so only a test/proxy override is configurable.
	defaultEndpoint = "https://api.jpush.cn"

	// pushPath is the create-push API path (docs.jiguang.cn: 创建推送
	// API, endpoint https://api.jpush.cn/v3/push).
	pushPath = "/v3/push"
)

// Provider sends push notifications through the JPush REST API v3
// (POST /v3/push). Authentication is HTTP Basic: base64(app_key:master_secret).
// Each delivery target is one registration_id, sent individually so a
// dead device (JPush error 1011) is attributed to exactly that target.
type Provider struct {
	appKey         string
	masterSecret   string
	platform       any // normalized: "all" or []string
	apnsProduction bool
	timeToLive     *int
	endpoint       string
	status         *core.ProviderStatus
	client         *httpclient.Client
}

// Config is the JPush provider configuration
type Config struct {
	AppKey       string
	MasterSecret string
	Platform     any // string or list as configured, normalized by parseConfig
	// APNsProduction is nil when unset (JPush then defaults to production).
	APNsProduction *bool
	// TimeToLive is nil when unset (JPush then defaults to 86400s).
	TimeToLive *int
	Endpoint   string
}

// jpushBody is the create-push request body. Sections are all optional
// except platform and audience: notification renders to the status bar,
// message is pass-through data the SDK hands to the app.
type jpushBody struct {
	Platform     any           `json:"platform"`
	Audience     audience      `json:"audience"`
	Notification *notification `json:"notification,omitempty"`
	Message      *messageBody  `json:"message,omitempty"`
	Options      *pushOptions  `json:"options,omitempty"`
}

// audience targets devices; per JPush docs registration_id entries are
// OR-combined. The provider sends one target per call, so the array has
// exactly one element.
type audience struct {
	RegistrationID []string `json:"registration_id"`
}

// notification is the display section. Alert is shared by all platforms;
// android adds the title line, ios carries the default sound so alert
// pushes make noise like the APNs provider does.
type notification struct {
	Alert   string       `json:"alert,omitempty"`
	Android *androidPart `json:"android,omitempty"`
	IOS     *iosPart     `json:"ios,omitempty"`
}

type androidPart struct {
	Alert string `json:"alert"`
	Title string `json:"title,omitempty"`
}

type iosPart struct {
	Alert string `json:"alert,omitempty"`
	Sound string `json:"sound,omitempty"`
}

// messageBody is JPush's pass-through (自定义消息) section: never shown
// in the status bar, delivered to the app by the SDK. msg_content is
// required by the API.
type messageBody struct {
	MsgContent string            `json:"msg_content"`
	Extras     map[string]string `json:"extras,omitempty"`
}

// pushOptions maps the two knobs this provider exposes. apns_production
// selects the APNs environment for iOS targets (unset means production,
// so the field is always sent). time_to_live overrides offline retention.
type pushOptions struct {
	APNsProduction bool `json:"apns_production"`
	TimeToLive     *int `json:"time_to_live,omitempty"`
}

// NewProvider creates a new JPush provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	// parseConfig only type-asserts each field and never fails; it keeps
	// the error return so the constructor signature stays uniform.
	cfg, _ := parseConfig(config)

	if cfg.AppKey == "" {
		return nil, fmt.Errorf("jpush: app_key is required")
	}
	if cfg.MasterSecret == "" {
		return nil, fmt.Errorf("jpush: master_secret is required")
	}
	if cfg.TimeToLive != nil && *cfg.TimeToLive < 0 {
		return nil, fmt.Errorf("jpush: time_to_live must be >= 0")
	}

	platform := cfg.Platform
	if platform == nil {
		platform = any("all")
	}
	apnsProduction := true
	if cfg.APNsProduction != nil {
		apnsProduction = *cfg.APNsProduction
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	return &Provider{
		appKey:         cfg.AppKey,
		masterSecret:   cfg.MasterSecret,
		platform:       platform,
		apnsProduction: apnsProduction,
		timeToLive:     cfg.TimeToLive,
		endpoint:       strings.TrimRight(endpoint, "/"),
		status: &core.ProviderStatus{
			Name:   "jpush",
			Type:   "jpush",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// parseConfig parses the configuration
func parseConfig(config map[string]interface{}) (*Config, error) {
	cfg := &Config{}

	if v, ok := config["app_key"].(string); ok {
		cfg.AppKey = v
	}
	if v, ok := config["master_secret"].(string); ok {
		cfg.MasterSecret = v
	}
	if v, ok := config["endpoint"].(string); ok {
		cfg.Endpoint = v
	}

	// platform accepts a comma-separated string ("all", "android,ios") or
	// a YAML list; JPush documents the JSON shape as "all" or an array.
	switch v := config["platform"].(type) {
	case string:
		if v != "" {
			cfg.Platform = normalizePlatform(v)
		}
	case []string:
		cfg.Platform = platformValue(v)
	case []interface{}:
		names := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				names = append(names, s)
			}
		}
		cfg.Platform = platformValue(names)
	}

	// apns_production: YAML gives bool; the dashboard's schema renders it
	// as a text input, so the string form ("true"/"false") parses too.
	switch v := config["apns_production"].(type) {
	case bool:
		cfg.APNsProduction = &v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.APNsProduction = &b
		}
	}

	// time_to_live: YAML gives int, JSON APIs give float64.
	switch v := config["time_to_live"].(type) {
	case int:
		ttl := v
		cfg.TimeToLive = &ttl
	case int64:
		ttl := int(v)
		cfg.TimeToLive = &ttl
	case float64:
		ttl := int(v)
		cfg.TimeToLive = &ttl
	}

	return cfg, nil
}

// normalizePlatform maps the comma-separated string form onto the JSON
// shape JPush accepts.
func normalizePlatform(s string) any {
	parts := strings.Split(s, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			names = append(names, p)
		}
	}
	return platformValue(names)
}

// platformValue canonicalizes a platform list: nothing configured falls
// back to "all", and a lone "all" is folded to the string form.
func platformValue(names []string) any {
	switch {
	case len(names) == 0:
		return "all"
	case len(names) == 1 && strings.EqualFold(names[0], "all"):
		return "all"
	default:
		return names
	}
}

// GetConfig returns the provider configuration. master_secret is masked
// by core.MaskConfig (see core.SensitiveFields) before API exposure.
func (p *Provider) GetConfig() map[string]interface{} {
	cfg := map[string]interface{}{
		"app_key":         p.appKey,
		"master_secret":   p.masterSecret,
		"platform":        p.platform,
		"apns_production": p.apnsProduction,
		"endpoint":        p.endpoint,
	}
	if p.timeToLive != nil {
		cfg["time_to_live"] = *p.timeToLive
	}
	return cfg
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent, core.PayloadRaw},
		ContentFormats: []string{"plain"},
	}
}

// Deliver delivers a task to the given registration_ids. It follows the
// fcm/apns convention: per-target sends with a partial-success
// aggregation error, so the pool's retryer sees one error for the task.
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("jpush: task is nil")
	}
	if len(task.Targets) == 0 {
		return fmt.Errorf("jpush: at least one target (registration_id) is required")
	}
	for _, target := range task.Targets {
		if target == "" {
			return fmt.Errorf("jpush: empty target found in targets")
		}
	}

	payload, err := p.buildPayload(task)
	if err != nil {
		return err
	}

	var errs []string
	successCount := 0
	for _, target := range task.Targets {
		if err := p.sendPush(ctx, target, payload); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", truncate(target, 8), err))
		} else {
			successCount++
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("jpush: %d/%d succeeded - failed: %s", successCount, len(task.Targets), strings.Join(errs, "; "))
	}

	return nil
}

// buildPayload renders the shared message skeleton (audience is filled
// per target by sendPush). Content becomes the display notification;
// Raw becomes the pass-through message section.
func (p *Provider) buildPayload(task *core.DeliveryTask) (*jpushBody, error) {
	title, body := extractContent(task)
	alert := body
	if alert == "" {
		alert = title
	}

	payload := &jpushBody{
		Platform: p.platform,
		Options: &pushOptions{
			APNsProduction: p.apnsProduction,
			TimeToLive:     p.timeToLive,
		},
	}

	if title != "" || body != "" {
		n := &notification{Alert: alert}
		if title != "" {
			// The android section is only needed to carry the title line;
			// JPush requires alert alongside it.
			n.Android = &androidPart{Alert: alert, Title: title}
		}
		n.IOS = &iosPart{Alert: alert, Sound: "default"}
		payload.Notification = n
	}

	if len(task.Payload.Raw) > 0 {
		extras := make(map[string]string, len(task.Payload.Raw))
		for k, v := range task.Payload.Raw {
			extras[k] = fmt.Sprintf("%v", v)
		}
		// msg_content is required by the API; with no human-readable
		// content the raw payload itself becomes the message body.
		msgContent := alert
		if msgContent == "" {
			encoded, err := json.Marshal(task.Payload.Raw)
			if err != nil {
				return nil, fmt.Errorf("jpush: failed to encode raw payload: %w", err)
			}
			msgContent = string(encoded)
		}
		payload.Message = &messageBody{MsgContent: msgContent, Extras: extras}
	}

	if payload.Notification == nil && payload.Message == nil {
		return nil, fmt.Errorf("jpush: payload needs content or raw data")
	}

	return payload, nil
}

// sendPush posts the payload for one registration_id. Non-2xx responses
// surface through httpclient's statusError, which already applies the
// repo-wide retry semantics (408/429/5xx retryable — JPush answers 429
// with code 2002 on rate limits) and carries the JPush error body for
// diagnostics.
func (p *Provider) sendPush(ctx context.Context, target string, payload *jpushBody) error {
	payload.Audience = audience{RegistrationID: []string{target}}

	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(p.appKey+":"+p.masterSecret))
	_, err := p.client.PostJSONWithHeaders(ctx, p.endpoint+pushPath, payload, map[string]string{
		"Authorization": auth,
	})
	return err
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// truncate truncates a string to max length
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return s
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "jpush"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "jpush"
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

// Factory creates JPush providers
type Factory struct{}

// Create creates a new JPush provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "jpush"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "jpush"
}
