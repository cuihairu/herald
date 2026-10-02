package getui

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	// defaultEndpoint is Getui's REST API v2 base. The appId is appended
	// per call (BaseUrl = https://restapi.getui.com/v2/$appId).
	defaultEndpoint = "https://restapi.getui.com/v2"

	// authPath obtains the interface token; pushPath is the toSingle
	// endpoint targeting one CID.
	authPath = "/auth"
	pushPath = "/push/single/cid"

	// tokenTTL is Getui's documented token validity (调用时间+1天); used
	// when the response omits a parsable expire_time.
	tokenTTL = 24 * time.Hour

	// tokenRefreshMargin refreshes the token this long before it expires.
	tokenRefreshMargin = 5 * time.Minute

	// transmissionLimit is Getui's cap on push_message.transmission and
	// notification.payload (≤ 3072 字).
	transmissionLimit = 3072

	// ttlMin/ttlMax bound settings.ttl (毫秒): -1 表示不设离线保留,
	// 上限 3 天.
	ttlMin = -1
	ttlMax = 3 * 24 * 60 * 60 * 1000

	// codeTokenExpired is Getui business code 10001 (token 错误/失效).
	// Docs recommend a passive refresh when a business call returns it.
	codeTokenExpired = 10001
)

// Provider sends push notifications through Getui's REST API v2
// (POST {BaseUrl}/push/single/cid). Authentication is a short-lived token
// obtained from /auth with sign = sha256(app_key + timestamp +
// master_secret). Each delivery target is one CID, sent individually so a
// bad CID is attributed to exactly that target.
type Provider struct {
	appID        string
	appKey       string
	masterSecret string
	endpoint     string
	ttl          *int64
	clickType    string
	status       *core.ProviderStatus
	client       *httpclient.Client

	tokens tokenCache
}

// Config is the Getui provider configuration
type Config struct {
	AppID        string
	AppKey       string
	MasterSecret string
	Endpoint     string
	// TTL is nil when unset (Getui then defaults to 2h offline retention).
	TTL *int64
	// ClickType is the notification click action; "" defaults to none.
	ClickType string
}

// tokenCache holds the interface token between calls. Getui caps /auth at
// 100 calls per minute, so the token is cached until shortly before it
// expires and the refresh is deduplicated.
//
// A plain Mutex covers both the read and the refresh path on purpose. The
// refresh holds the lock across the /auth round trip, so a separate read
// fast path would only save the nanoseconds of reading two fields while
// costing the deduplication that actually protects the rate limit: a caller
// that took the read lock instead would block behind the in-flight refresh
// anyway and then find the token fresh. Checking once under the single
// lock is what makes a concurrent cold start collapse to one /auth call,
// and it keeps that outcome assertable instead of schedule-dependent.
type tokenCache struct {
	mu       sync.Mutex
	token    string
	expireAt time.Time
}

// authRequest is the /auth body. Getui documents the signature as
// sha256(appkey + timestamp + mastersecret) in that fixed order.
type authRequest struct {
	Sign      string `json:"sign"`
	Timestamp string `json:"timestamp"`
	AppKey    string `json:"appkey"`
}

type authResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Token      flexString `json:"token"`
		ExpireTime flexString `json:"expire_time"`
	} `json:"data"`
}

// pushRequest is the toSingle body. audience selects the CID (the endpoint
// and the audience field must agree, per docs).
type pushRequest struct {
	RequestID   string       `json:"request_id"`
	Audience    audience     `json:"audience"`
	Settings    *settings    `json:"settings,omitempty"`
	PushMessage *pushMessage `json:"push_message"`
	PushChannel *pushChannel `json:"push_channel,omitempty"`
}

type audience struct {
	CID []string `json:"cid"`
}

type settings struct {
	TTL int64 `json:"ttl"`
}

// pushMessage is 个推在线通道的消息体。notification 与 transmission 互斥
// （官方：三选一，都填会报错），Herald 按有 Content 优先通知。
type pushMessage struct {
	Notification *notification `json:"notification,omitempty"`
	Transmission string        `json:"transmission,omitempty"`
}

type notification struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	ClickType string `json:"click_type"`
	Payload   string `json:"payload,omitempty"`
}

// pushChannel carries platform-specific payloads. 个推通道的 notification
// 只在 Android/鸿蒙 展示，iOS 需要 aps 才有系统通知。
type pushChannel struct {
	IOS *iosChannel `json:"ios,omitempty"`
}

type iosChannel struct {
	APS iosAPS `json:"aps"`
}

type iosAPS struct {
	Alert iosAlert `json:"alert"`
	Sound string   `json:"sound,omitempty"`
}

type iosAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// apiResponse is Getui's common response envelope.
type apiResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// flexString accepts both string and numeric JSON values: expire_time is
// documented as a string but is emitted as a number by some deployments.
type flexString string

// UnmarshalJSON decodes a JSON string or number into s.
func (f *flexString) UnmarshalJSON(data []byte) error {
	*f = flexString(strings.Trim(string(data), `"`))
	return nil
}

// NewProvider creates a new Getui provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	// parseConfig only type-asserts each field and never fails.
	cfg := parseConfig(config)

	if cfg.AppID == "" {
		return nil, fmt.Errorf("getui: app_id is required")
	}
	if cfg.AppKey == "" {
		return nil, fmt.Errorf("getui: app_key is required")
	}
	if cfg.MasterSecret == "" {
		return nil, fmt.Errorf("getui: master_secret is required")
	}
	if cfg.TTL != nil && (*cfg.TTL < ttlMin || *cfg.TTL > ttlMax) {
		return nil, fmt.Errorf("getui: ttl must be between %d and %d ms", ttlMin, ttlMax)
	}
	clickType := cfg.ClickType
	if clickType == "" {
		clickType = "none"
	}
	if !supportedClickTypes[clickType] {
		return nil, fmt.Errorf("getui: unsupported click_type %q (want one of none, startapp, payload, payload_custom)", clickType)
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	return &Provider{
		appID:        cfg.AppID,
		appKey:       cfg.AppKey,
		masterSecret: cfg.MasterSecret,
		endpoint:     strings.TrimRight(endpoint, "/"),
		ttl:          cfg.TTL,
		clickType:    clickType,
		status: &core.ProviderStatus{
			Name:   "getui",
			Type:   "getui",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// supportedClickTypes are the click actions that need no extra fields.
// url/intent would require a url/intent value, which Herald does not carry.
var supportedClickTypes = map[string]bool{
	"none":           true,
	"startapp":       true,
	"payload":        true,
	"payload_custom": true,
}

// parseConfig parses the configuration
func parseConfig(config map[string]interface{}) *Config {
	cfg := &Config{}

	if v, ok := config["app_id"].(string); ok {
		cfg.AppID = v
	}
	if v, ok := config["app_key"].(string); ok {
		cfg.AppKey = v
	}
	if v, ok := config["master_secret"].(string); ok {
		cfg.MasterSecret = v
	}
	if v, ok := config["endpoint"].(string); ok {
		cfg.Endpoint = v
	}
	if v, ok := config["click_type"].(string); ok {
		cfg.ClickType = v
	}

	// ttl: YAML gives int, JSON APIs give float64.
	switch v := config["ttl"].(type) {
	case int:
		ttl := int64(v)
		cfg.TTL = &ttl
	case int64:
		ttl := v
		cfg.TTL = &ttl
	case float64:
		ttl := int64(v)
		cfg.TTL = &ttl
	}

	return cfg
}

// GetConfig returns the provider configuration. master_secret is masked by
// core.MaskConfig (see core.SensitiveFields) before API exposure.
func (p *Provider) GetConfig() map[string]interface{} {
	cfg := map[string]interface{}{
		"app_id":        p.appID,
		"app_key":       p.appKey,
		"master_secret": p.masterSecret,
		"click_type":    p.clickType,
		"endpoint":      p.endpoint,
	}
	if p.ttl != nil {
		cfg["ttl"] = *p.ttl
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

// Deliver delivers a task to the given CIDs. It follows the fcm/jpush
// convention: per-target sends with a partial-success aggregation error,
// so the pool's retryer sees one error for the whole task.
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("getui: task is nil")
	}
	if len(task.Targets) == 0 {
		return fmt.Errorf("getui: at least one target (cid) is required")
	}
	for _, target := range task.Targets {
		if target == "" {
			return fmt.Errorf("getui: empty target found in targets")
		}
	}

	req, err := p.buildRequest(task)
	if err != nil {
		return err
	}

	var errs []string
	successCount := 0
	for _, target := range task.Targets {
		if err := p.sendPush(ctx, target, req); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", truncate(target, 8), err))
		} else {
			successCount++
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("getui: %d/%d succeeded - failed: %s", successCount, len(task.Targets), strings.Join(errs, "; "))
	}

	return nil
}

// buildRequest renders the shared request skeleton (audience and
// request_id are filled per target by sendPush).
func (p *Provider) buildRequest(task *core.DeliveryTask) (*pushRequest, error) {
	title, body := extractContent(task)
	msg := &pushMessage{}
	var channel *pushChannel

	if title != "" || body != "" {
		n := &notification{Title: title, Body: body, ClickType: p.clickType}
		// Getui marks both title and body as required, so a title-only or
		// body-only message borrows the other field.
		if n.Title == "" {
			n.Title = body
		}
		if n.Body == "" {
			n.Body = title
		}
		// Raw keys ride along as the click payload: Getui rejects
		// notification + transmission in one request, and only
		// click_type=payload/payload_custom carries them.
		if p.clickType == "payload" || p.clickType == "payload_custom" {
			if raw, err := encodeRaw(task.Payload.Raw); err != nil {
				return nil, err
			} else if utf8.RuneCountInString(raw) <= transmissionLimit {
				n.Payload = raw
			}
		}
		msg.Notification = n
		// 个推通道通知只在 Android/鸿蒙 展示，iOS 走 APNs 通道。
		channel = &pushChannel{IOS: &iosChannel{
			APS: iosAPS{
				Alert: iosAlert{Title: n.Title, Body: n.Body},
				Sound: "default",
			},
		}}
	} else if len(task.Payload.Raw) > 0 {
		raw, err := encodeRaw(task.Payload.Raw)
		if err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(raw) > transmissionLimit {
			return nil, fmt.Errorf("getui: transmission exceeds %d chars: %d", transmissionLimit, utf8.RuneCountInString(raw))
		}
		msg.Transmission = raw
	} else {
		return nil, fmt.Errorf("getui: payload needs content or raw data")
	}

	req := &pushRequest{PushMessage: msg, PushChannel: channel}
	if p.ttl != nil {
		req.Settings = &settings{TTL: *p.ttl}
	}
	return req, nil
}

// sendPush posts the request for one CID. A business code 10001 (token
// 失效) triggers the passive refresh Getui's docs recommend, then one
// retry with the same request_id so a duplicate id cannot drop the push.
func (p *Provider) sendPush(ctx context.Context, target string, req *pushRequest) error {
	token, err := p.getToken(ctx)
	if err != nil {
		return err
	}

	req.RequestID = newRequestID()
	req.Audience = audience{CID: []string{target}}

	resp, err := p.postPush(ctx, token, req)
	if err != nil && responseCode(resp) == codeTokenExpired {
		p.invalidateToken()
		fresh, ferr := p.getToken(ctx)
		if ferr == nil {
			resp, err = p.postPush(ctx, fresh, req)
		}
	}
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// postPush sends one request; non-2xx responses surface through
// httpclient's statusError, which applies the repo-wide retry semantics
// (408/429/5xx retryable).
func (p *Provider) postPush(ctx context.Context, token string, req *pushRequest) (*httpclient.Response, error) {
	return p.client.PostJSONWithHeaders(ctx, p.baseURL()+pushPath, req, map[string]string{
		"token": token,
	})
}

// baseURL returns the app-scoped BaseUrl.
func (p *Provider) baseURL() string {
	return p.endpoint + "/" + p.appID
}

// getToken returns a cached interface token, fetching a new one when
// missing or within the refresh margin of expiry. The refresh runs under
// the cache lock, so concurrent callers arriving on a cold cache queue up
// and only the first one reaches /auth.
func (p *Provider) getToken(ctx context.Context) (string, error) {
	p.tokens.mu.Lock()
	defer p.tokens.mu.Unlock()

	if p.tokens.token != "" && time.Now().Before(p.tokens.expireAt) {
		return p.tokens.token, nil
	}

	token, expireAt, err := p.fetchToken(ctx)
	if err != nil {
		return "", err
	}

	p.tokens.token = token
	p.tokens.expireAt = expireAt
	return token, nil
}

// invalidateToken drops the cached token so the next getToken refetches.
func (p *Provider) invalidateToken() {
	p.tokens.mu.Lock()
	p.tokens.token = ""
	p.tokens.expireAt = time.Time{}
	p.tokens.mu.Unlock()
}

// fetchToken exchanges the app credentials for an interface token.
func (p *Provider) fetchToken(ctx context.Context) (string, time.Time, error) {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	body := authRequest{
		Sign:      buildSign(p.appKey, timestamp, p.masterSecret),
		Timestamp: timestamp,
		AppKey:    p.appKey,
	}

	resp, err := p.client.PostJSON(ctx, p.baseURL()+authPath, body)
	if err != nil {
		return "", time.Time{}, err
	}

	var result authResponse
	if err := resp.JSON(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("getui: failed to parse auth response: %w", err)
	}
	if result.Code != 0 {
		return "", time.Time{}, fmt.Errorf("getui: auth failed (code %d): %s", result.Code, result.Msg)
	}
	if result.Data.Token == "" {
		return "", time.Time{}, fmt.Errorf("getui: auth response missing token")
	}

	return string(result.Data.Token), expireAt(result.Data.ExpireTime, time.Now()), nil
}

// expireAt converts the response expire_time (ms timestamp, per docs
// "接口调用时间+1天") into a local refresh deadline, keeping the refresh
// margin. Unparsable values fall back to the documented token lifetime.
func expireAt(raw flexString, now time.Time) time.Time {
	millis, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || millis <= 0 {
		return now.Add(tokenTTL - tokenRefreshMargin)
	}
	deadline := time.UnixMilli(millis).Add(-tokenRefreshMargin)
	if !deadline.After(now) {
		return now.Add(time.Minute)
	}
	return deadline
}

// buildSign computes the /auth signature: sha256 over appkey, timestamp
// and mastersecret concatenated in Getui's fixed order.
func buildSign(appKey, timestamp, masterSecret string) string {
	sum := sha256.Sum256([]byte(appKey + timestamp + masterSecret))
	return hex.EncodeToString(sum[:])
}

// checkResponse surfaces a business-level failure: Getui answers HTTP 200
// with code 0 on success and a non-zero code (with the matching HTTP
// status) otherwise.
func checkResponse(resp *httpclient.Response) error {
	if resp == nil {
		return nil
	}
	var result apiResponse
	if err := resp.JSON(&result); err != nil {
		return fmt.Errorf("getui: failed to parse push response: %w", err)
	}
	if result.Code == 0 {
		return nil
	}
	if result.Msg == "" {
		return fmt.Errorf("getui: push failed (code %d)", result.Code)
	}
	return fmt.Errorf("getui: push failed (code %d): %s", result.Code, result.Msg)
}

// responseCode reads the business code from a response body, or -1 when the
// body is absent or unparsable.
func responseCode(resp *httpclient.Response) int {
	if resp == nil {
		return -1
	}
	var result apiResponse
	if err := resp.JSON(&result); err != nil {
		return -1
	}
	return result.Code
}

// newRequestID returns a per-request unique id (Getui requires 10-32
// chars; repeated ids make the server drop the push).
//
// It is a package-level variable as a test seam so payload assertions can
// pin the field.
var newRequestID = func() string {
	buf := make([]byte, 12)
	if _, err := randRead(buf); err != nil {
		// crypto/rand does not fail on supported platforms; fall back to a
		// timestamp-derived id rather than sending an empty one.
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

// randRead is a test seam for the id entropy source.
var randRead = rand.Read

// encodeRaw renders the Raw payload as the JSON string Getui's
// transmission/payload fields carry.
func encodeRaw(raw map[string]interface{}) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("getui: failed to encode raw payload: %w", err)
	}
	return string(encoded), nil
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
	return "getui"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "getui"
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

// Factory creates Getui providers
type Factory struct{}

// Create creates a new Getui provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "getui"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "getui"
}
