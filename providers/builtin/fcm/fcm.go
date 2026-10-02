package fcm

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	// defaultEndpoint is the production FCM HTTP v1 API base. Google's
	// regional endpoints (e.g. fcm.googleapis.com) are stable, so only a
	// test/proxy override is configurable.
	defaultEndpoint = "https://fcm.googleapis.com"

	// messagingScope is the OAuth scope a service account needs to send
	// messages through the FCM v1 API.
	messagingScope = "https://www.googleapis.com/auth/firebase.messaging"

	// assertionTTL bounds the signed JWT used for the token exchange; an
	// hour is Google's documented maximum.
	assertionTTL = time.Hour
)

// oauthURL is the Google OAuth 2.0 token endpoint. It is a package-level
// variable purely as a test seam (same pattern as registerProvider /
// setReadDeadline): fcm tests point it at a stub server and restore it via
// t.Cleanup; production never reassigns it.
var oauthURL = "https://oauth2.googleapis.com/token"

// Provider sends push notifications through Firebase Cloud Messaging
// (HTTP v1 API). Authentication is a service account: an RS256 JWT
// (client_email + private_key) exchanged for a short-lived OAuth access
// token, cached until shortly before expiry.
type Provider struct {
	projectID   string
	clientEmail string
	privateKey  *rsa.PrivateKey
	endpoint    string
	tokens      tokenCache
	status      *core.ProviderStatus
	client      *httpclient.Client
}

// Config is the FCM provider configuration
type Config struct {
	ProjectID   string `yaml:"project_id"`
	ClientEmail string `yaml:"client_email"`
	PrivateKey  string `yaml:"private_key"` // PKCS8 PEM of the service account key
	Endpoint    string `yaml:"endpoint,omitempty"`
}

// jwtClaims is the assertion payload for the OAuth jwt-bearer grant
type jwtClaims struct {
	Iss   string `json:"iss"`
	Scope string `json:"scope"`
	Aud   string `json:"aud"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

// tokenResponse is the OAuth token endpoint response. On rejection Google
// answers 4xx (surfaced by httpclient as an error with the body) and a 200
// always carries an access_token; both error fields are parsed defensively.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// fcmMessage is the FCM v1 send request
type fcmMessage struct {
	Message fcmMessagePayload `json:"message"`
}

// fcmMessagePayload carries the device token plus the notification and/or
// data section; FCM rejects payloads with neither.
type fcmMessagePayload struct {
	Token        string            `json:"token"`
	Notification *fcmNotification  `json:"notification,omitempty"`
	Data         map[string]string `json:"data,omitempty"`
}

// fcmNotification is the display section rendered by the system tray
type fcmNotification struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// tokenCache holds the OAuth access token. The read fast path avoids a
// write lock on every delivery; on a miss the caller refreshes under the
// write lock without a second check — a duplicate fetch only happens when
// two deliveries race in the seconds around expiry, and the grant is
// idempotent, so unlike wechatmp there is no scheduling-dependent
// double-check branch to keep covered.
type tokenCache struct {
	mu       sync.RWMutex
	token    string
	expireAt time.Time
}

// NewProvider creates a new FCM provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	// parseConfig only type-asserts each field and never fails; it keeps
	// the error return so the constructor signature stays uniform.
	cfg, _ := parseConfig(config)

	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("fcm: project_id is required")
	}
	if cfg.ClientEmail == "" {
		return nil, fmt.Errorf("fcm: client_email is required")
	}
	if cfg.PrivateKey == "" {
		return nil, fmt.Errorf("fcm: private_key is required")
	}

	key, err := parsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	return &Provider{
		projectID:   cfg.ProjectID,
		clientEmail: cfg.ClientEmail,
		privateKey:  key,
		endpoint:    strings.TrimRight(endpoint, "/"),
		status: &core.ProviderStatus{
			Name:   "fcm",
			Type:   "fcm",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}, nil
}

// parseConfig parses the configuration
func parseConfig(config map[string]interface{}) (*Config, error) {
	cfg := &Config{}

	if v, ok := config["project_id"].(string); ok {
		cfg.ProjectID = v
	}
	if v, ok := config["client_email"].(string); ok {
		cfg.ClientEmail = v
	}
	if v, ok := config["private_key"].(string); ok {
		cfg.PrivateKey = v
	}
	if v, ok := config["endpoint"].(string); ok {
		cfg.Endpoint = v
	}

	return cfg, nil
}

// parsePrivateKey decodes the service account key. Google issues PKCS8
// ("PRIVATE KEY") PEM blocks; that is the only format accepted so a
// mismatched key fails loudly at startup instead of at first send.
func parsePrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("fcm: private_key is not valid PEM data")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("fcm: failed to parse private_key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("fcm: private_key must be an RSA key, got %T", parsed)
	}
	return key, nil
}

// GetConfig returns the provider configuration. private_key is masked by
// core.MaskConfig (see core.SensitiveFields) before API exposure.
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"project_id":   p.projectID,
		"client_email": p.clientEmail,
		"private_key":  "(PEM)",
		"endpoint":     p.endpoint,
	}
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent, core.PayloadRaw},
		ContentFormats: []string{"plain"},
	}
}

// Deliver delivers a task to the given FCM device tokens. It follows the
// wechatmp convention: per-target sends with a partial-success aggregation
// error, so the pool's retryer sees one error for the whole task.
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("fcm: task is nil")
	}

	// FCM v1 is per-token: every target is one registration token.
	if len(task.Targets) == 0 {
		return fmt.Errorf("fcm: at least one target (device token) is required")
	}
	for _, target := range task.Targets {
		if target == "" {
			return fmt.Errorf("fcm: empty target found in targets")
		}
	}

	token, err := p.getToken(ctx)
	if err != nil {
		return fmt.Errorf("fcm: failed to get access token: %w", err)
	}

	msg := p.buildMessage(task)

	var errs []string
	successCount := 0
	for _, target := range task.Targets {
		if err := p.sendMessage(ctx, token, target, msg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", truncate(target, 8), err))
		} else {
			successCount++
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("fcm: %d/%d succeeded - failed: %s", successCount, len(task.Targets), strings.Join(errs, "; "))
	}

	return nil
}

// buildMessage renders the shared message skeleton. Content becomes the
// display notification; Raw entries become FCM data key/values (values are
// stringified, since FCM data payloads are string-only).
func (p *Provider) buildMessage(task *core.DeliveryTask) *fcmMessage {
	title, body := extractContent(task)
	msg := &fcmMessage{Message: fcmMessagePayload{}}

	if title != "" || body != "" {
		msg.Message.Notification = &fcmNotification{Title: title, Body: body}
	}

	if len(task.Payload.Raw) > 0 {
		data := make(map[string]string, len(task.Payload.Raw))
		for k, v := range task.Payload.Raw {
			data[k] = fmt.Sprintf("%v", v)
		}
		msg.Message.Data = data
	}

	return msg
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// sendMessage posts the message for one registration token. Non-2xx
// responses surface through httpclient's statusError, which already
// applies the repo-wide retry semantics (408/429/5xx retryable) and
// carries the FCM error body for diagnostics.
func (p *Provider) sendMessage(ctx context.Context, token, target string, msg *fcmMessage) error {
	msg.Message.Token = target

	sendURL := fmt.Sprintf("%s/v1/projects/%s/messages:send", p.endpoint, p.projectID)
	_, err := p.client.PostJSONWithHeaders(ctx, sendURL, msg, map[string]string{
		"Authorization": "Bearer " + token,
	})
	return err
}

// getToken returns a cached access token, refreshing it through the
// jwt-bearer grant when missing or within five minutes of expiry.
func (p *Provider) getToken(ctx context.Context) (string, error) {
	p.tokens.mu.RLock()
	if p.tokens.token != "" && time.Now().Before(p.tokens.expireAt) {
		token := p.tokens.token
		p.tokens.mu.RUnlock()
		return token, nil
	}
	p.tokens.mu.RUnlock()

	p.tokens.mu.Lock()
	defer p.tokens.mu.Unlock()

	assertion, err := buildAssertion(p.clientEmail, p.privateKey, oauthURL, time.Now())
	if err != nil {
		return "", fmt.Errorf("fcm: failed to sign assertion: %w", err)
	}

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	resp, err := p.client.PostForm(ctx, oauthURL, form)
	if err != nil {
		return "", err
	}

	var result tokenResponse
	if err := resp.JSON(&result); err != nil {
		return "", fmt.Errorf("fcm: failed to parse token response: %w", err)
	}
	if result.Error != "" {
		msg := result.Error
		if result.ErrorDescription != "" {
			msg += ": " + result.ErrorDescription
		}
		return "", fmt.Errorf("fcm: token error: %s", msg)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("fcm: token response missing access_token")
	}

	// Cache with a five-minute buffer before expiry (wechatmp convention).
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	if expiresIn > 300 {
		expiresIn -= 300
	}

	p.tokens.token = result.AccessToken
	p.tokens.expireAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	return p.tokens.token, nil
}

// buildAssertion signs the RS256 JWT for the token exchange: header,
// claims, then a PKCS1v15 SHA-256 RSA signature over the signing input.
//
// It is a package-level variable as a test seam (same pattern as
// registerProvider / setReadDeadline): tests inject a failing
// implementation to reach getToken's assertion error path.
var buildAssertion = func(clientEmail string, key *rsa.PrivateKey, aud string, now time.Time) (string, error) {
	// Marshal of a fixed struct of strings and int64s cannot fail.
	claims, _ := json.Marshal(jwtClaims{
		Iss:   clientEmail,
		Scope: messagingScope,
		Aud:   aud,
		Iat:   now.Unix(),
		Exp:   now.Add(assertionTTL).Unix(),
	})

	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) +
		"." + enc.EncodeToString(claims)

	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		// Reachable with a structurally valid but undersized key (modulus
		// < hashLen+11 bytes): surface it so the operator sees the real
		// crypto failure instead of an opaque OAuth error from Google.
		return "", err
	}

	return signingInput + "." + enc.EncodeToString(sig), nil
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
	return "fcm"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "fcm"
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

// Factory creates FCM providers
type Factory struct{}

// Create creates a new FCM provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "fcm"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "fcm"
}
