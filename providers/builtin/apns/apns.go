// Package apns implements the APNs (Apple Push Notification service)
// builtin provider: HTTP/2 sends to api.push.apple.com with either a
// provider token (ES256 JWT, key_id + team_id + .p8 key) or a TLS client
// certificate (PEM).
package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	// defaultEndpoint is Apple's production push endpoint. The sandbox
	// (https://api.sandbox.push.apple.com) is selectable via config for
	// development builds; the path and protocol are identical.
	defaultEndpoint = "https://api.push.apple.com"

	// tokenTTL bounds the provider JWT. Apple documents one hour as the
	// maximum accepted lifetime.
	tokenTTL = time.Hour

	// tokenRefreshBuffer refreshes the JWT shortly before expiry
	// (wechatmp/FCM convention) so an in-flight refresh never races the
	// server-side expiry.
	tokenRefreshBuffer = 5 * time.Minute
)

// buildProviderToken assembles the ES256 provider JWT:
// {"alg":"ES256","kid":keyID}.{"iss":teamID,"iat":..,"exp":..} signed as
// raw R||S (each 32 bytes for P-256), which is what Apple's decoder
// expects — ASN.1 DER signatures are rejected.
//
// It is a package-level variable as a test seam (same pattern as
// registerProvider / FCM's buildAssertion): tests inject a failing
// implementation to reach the provider-token error path.
var buildProviderToken = func(teamID, keyID string, key *ecdsa.PrivateKey, now time.Time) (string, error) {
	header, _ := json.Marshal(tokenHeader{Alg: "ES256", Kid: keyID})
	claims, _ := json.Marshal(tokenClaims{
		Iss: teamID,
		Iat: now.Unix(),
		Exp: now.Add(tokenTTL).Unix(),
	})

	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)

	sum := sha256.Sum256([]byte(signingInput))
	// Deterministic ECDSA on go1.26 never surfaces a signing error (the
	// nonce derivation does not read from rand, verified with an
	// always-failing reader), so the error slot is dropped rather than
	// carrying a branch no test can reach.
	r, s, _ := ecdsa.Sign(rand.Reader, key, sum[:])
	sig := append(padTo32(r.Bytes()), padTo32(s.Bytes())...)

	return signingInput + "." + enc.EncodeToString(sig), nil
}

// tokenHeader is the JWT header: ES256 plus the signing key's ID.
type tokenHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// tokenClaims is the JWT payload Apple accepts: the team ID and issue
// timestamps (exp is optional but bounds cached reuse).
type tokenClaims struct {
	Iss string `json:"iss"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
}

// padTo32 left-pads a big-endian integer to P-256's 32-byte coordinate
// size. Signature halves are always ≤ 32 bytes (group order < 2^256).
func padTo32(b []byte) []byte {
	if len(b) >= 32 {
		return b
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

// Provider sends push notifications through APNs. Two authentication
// modes are supported: the provider token (ES256 JWT built from key_id,
// team_id and the .p8 key, cached until shortly before its one-hour
// expiry) or a TLS client certificate parsed from a PEM bundle.
type Provider struct {
	keyID    string
	teamID   string
	key      *ecdsa.PrivateKey // token mode
	certMode bool
	cert     tls.Certificate // cert mode

	topic    string
	pushType string // "" = derive from payload
	priority int    // 0 = derive from push type
	endpoint string

	jwtMu       sync.RWMutex
	jwt         string
	jwtExpireAt time.Time

	status *core.ProviderStatus
	client *httpclient.Client
}

// Config is the APNs provider configuration. Token mode requires
// key_id/team_id/private_key; certificate mode requires cert_pem.
// topic (the app bundle ID) is required in both modes.
type Config struct {
	KeyID      string `yaml:"key_id"`
	TeamID     string `yaml:"team_id"`
	PrivateKey string `yaml:"private_key"`
	CertPEM    string `yaml:"cert_pem"`
	Topic      string `yaml:"topic"`
	PushType   string `yaml:"push_type,omitempty"`
	Priority   int    `yaml:"priority,omitempty"`
	Endpoint   string `yaml:"endpoint,omitempty"`
}

// NewProvider creates an APNs provider
func NewProvider(config map[string]interface{}) (core.Provider, error) {
	// parseConfig only type-asserts each field and never fails; it keeps
	// the error return so the constructor signature stays uniform.
	cfg, _ := parseConfig(config)

	if cfg.Topic == "" {
		return nil, fmt.Errorf("apns: topic is required")
	}

	p := &Provider{
		topic:    cfg.Topic,
		pushType: cfg.PushType,
		priority: cfg.Priority,
		status: &core.ProviderStatus{
			Name:   "apns",
			Type:   "apns",
			Status: "available",
			Since:  time.Now(),
		},
		client: httpclient.NewClient(nil),
	}

	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	p.endpoint = strings.TrimRight(endpoint, "/")

	if cfg.CertPEM != "" {
		// Certificate mode: the TLS client certificate authenticates the
		// connection, so no per-request Authorization header is sent.
		cert, err := parseCertPair(cfg.CertPEM)
		if err != nil {
			return nil, err
		}
		p.certMode = true
		p.cert = cert
		p.client = p.client.WithClientCert(cert)
		return p, nil
	}

	if cfg.KeyID == "" {
		return nil, fmt.Errorf("apns: key_id is required")
	}
	if cfg.TeamID == "" {
		return nil, fmt.Errorf("apns: team_id is required")
	}
	if cfg.PrivateKey == "" {
		return nil, fmt.Errorf("apns: private_key is required")
	}

	key, err := parseECPrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	p.keyID = cfg.KeyID
	p.teamID = cfg.TeamID
	p.key = key
	return p, nil
}

// parseConfig parses the configuration
func parseConfig(config map[string]interface{}) (*Config, error) {
	cfg := &Config{}

	if v, ok := config["key_id"].(string); ok {
		cfg.KeyID = v
	}
	if v, ok := config["team_id"].(string); ok {
		cfg.TeamID = v
	}
	if v, ok := config["private_key"].(string); ok {
		cfg.PrivateKey = v
	}
	if v, ok := config["cert_pem"].(string); ok {
		cfg.CertPEM = v
	}
	if v, ok := config["topic"].(string); ok {
		cfg.Topic = v
	}
	if v, ok := config["push_type"].(string); ok {
		cfg.PushType = v
	}
	if v, ok := config["priority"].(float64); ok {
		cfg.Priority = int(v)
	}
	if v, ok := config["priority"].(int); ok {
		cfg.Priority = v
	}
	if v, ok := config["endpoint"].(string); ok {
		cfg.Endpoint = v
	}

	return cfg, nil
}

// parseECPrivateKey decodes the APNs .p8 key. Apple issues PKCS8
// ("PRIVATE KEY") PEM blocks; SEC1 ("EC PRIVATE KEY") is also accepted
// for keys re-encoded by other tooling, and the curve must be P-256 —
// that is the only curve APNs' ES256 verifier accepts.
func parseECPrivateKey(pemData string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("apns: private_key is not valid PEM data")
	}

	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("apns: private_key must be an EC key, got %T", parsed)
		}
		if key.Curve.Params().BitSize != 256 {
			return nil, fmt.Errorf("apns: private_key must be a P-256 EC key, got %d-bit curve", key.Curve.Params().BitSize)
		}
		return key, nil
	}

	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: failed to parse private_key: %w", err)
	}
	if key.Curve.Params().BitSize != 256 {
		return nil, fmt.Errorf("apns: private_key must be a P-256 EC key, got %d-bit curve", key.Curve.Params().BitSize)
	}
	return key, nil
}

// parseCertPair splits a PEM bundle holding the leaf certificate(s) plus
// the private key — the shape openssl's pkcs12 -out produces. Any number
// of CERTIFICATE blocks is kept (Apple's WWDR intermediate is often
// included); everything else is treated as the key.
func parseCertPair(pemData string) (tls.Certificate, error) {
	var certPEM, keyPEM []byte
	rest := []byte(pemData)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		encoded := pem.EncodeToMemory(block)
		if block.Type == "CERTIFICATE" {
			certPEM = append(certPEM, encoded...)
		} else {
			keyPEM = append(keyPEM, encoded...)
		}
	}

	if len(certPEM) == 0 {
		return tls.Certificate{}, fmt.Errorf("apns: cert_pem has no CERTIFICATE block")
	}
	if len(keyPEM) == 0 {
		return tls.Certificate{}, fmt.Errorf("apns: cert_pem has no private key block")
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("apns: cert_pem: %w", err)
	}
	return pair, nil
}

// GetConfig returns the provider configuration. private_key / cert_pem
// are masked by core.MaskConfig (see core.SensitiveFields) before API
// exposure; the placeholders here only feed non-masked surfaces.
func (p *Provider) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"key_id":      p.keyID,
		"team_id":     p.teamID,
		"private_key": "(PEM)",
		"cert_pem":    "(PEM)",
		"topic":       p.topic,
		"push_type":   p.pushType,
		"priority":    p.priority,
		"endpoint":    p.endpoint,
	}
}

// Capability returns the provider capabilities
func (p *Provider) Capability() core.ProviderCapability {
	return core.ProviderCapability{
		PayloadKinds:   []core.PayloadKind{core.PayloadContent, core.PayloadRaw},
		ContentFormats: []string{"plain"},
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "apns"
}

// Type returns the provider type
func (p *Provider) Type() string {
	return "apns"
}

// Status returns the provider status
func (p *Provider) Status() *core.ProviderStatus {
	return p.status
}

// Close releases the provider's resources
func (p *Provider) Close() error {
	return nil
}

// getProviderJWT returns the cached provider token, re-signing it when
// missing or within the refresh buffer of expiry. Signing is pure local
// computation, so the refresh path makes no network call and a second
// check under the write lock would be unreachable.
func (p *Provider) getProviderJWT() (string, error) {
	p.jwtMu.RLock()
	if p.jwt != "" && time.Now().Before(p.jwtExpireAt) {
		jwt := p.jwt
		p.jwtMu.RUnlock()
		return jwt, nil
	}
	p.jwtMu.RUnlock()

	p.jwtMu.Lock()
	defer p.jwtMu.Unlock()

	jwt, err := buildProviderToken(p.teamID, p.keyID, p.key, time.Now())
	if err != nil {
		return "", fmt.Errorf("apns: failed to sign provider token: %w", err)
	}

	p.jwt = jwt
	p.jwtExpireAt = time.Now().Add(tokenTTL - tokenRefreshBuffer)
	return jwt, nil
}

// Deliver delivers a task to the given APNs device tokens. It follows
// the wechatmp/FCM convention: per-target sends with a partial-success
// aggregation error, so the pool's retryer sees one error for the whole
// task.
func (p *Provider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	if task == nil {
		return fmt.Errorf("apns: task is nil")
	}

	// APNs is per-token: every target is one device token.
	if len(task.Targets) == 0 {
		return fmt.Errorf("apns: at least one target (device token) is required")
	}
	for _, target := range task.Targets {
		if target == "" {
			return fmt.Errorf("apns: empty target found in targets")
		}
	}

	title, body := extractContent(task)
	hasContent := title != "" || body != ""
	if !hasContent && len(task.Payload.Raw) == 0 {
		return fmt.Errorf("apns: payload needs content or raw data")
	}

	auth := ""
	if !p.certMode {
		jwt, err := p.getProviderJWT()
		if err != nil {
			return fmt.Errorf("apns: failed to get provider token: %w", err)
		}
		auth = jwt
	}

	payload := buildPayload(task, title, body, hasContent)
	pushType := p.resolvePushType(hasContent)
	priority := p.resolvePriority(pushType)

	var errs []string
	successCount := 0
	for _, target := range task.Targets {
		if err := p.send(ctx, auth, target, payload, pushType, priority); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", truncate(target, 8), err))
		} else {
			successCount++
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("apns: %d/%d succeeded - failed: %s", successCount, len(task.Targets), strings.Join(errs, "; "))
	}

	return nil
}

// buildPayload renders the APNs JSON body: an aps dictionary (alert
// section for content, content-available for data-only pushes) plus any
// Raw entries as top-level custom keys — APNs carries arbitrary keys
// beside aps. An "aps" key in Raw would clobber the computed section and
// is skipped.
func buildPayload(task *core.DeliveryTask, title, body string, hasContent bool) map[string]interface{} {
	aps := map[string]interface{}{}
	if hasContent {
		alert := map[string]string{}
		if title != "" {
			alert["title"] = title
		}
		if body != "" {
			alert["body"] = body
		}
		aps["alert"] = alert
		aps["sound"] = "default"
	} else {
		aps["content-available"] = 1
	}

	payload := map[string]interface{}{"aps": aps}
	for k, v := range task.Payload.Raw {
		if k == "aps" {
			continue
		}
		payload[k] = v
	}
	return payload
}

// resolvePushType picks apns-push-type: the configured override wins,
// then alert when there is display content, else background.
func (p *Provider) resolvePushType(hasContent bool) string {
	if p.pushType != "" {
		return p.pushType
	}
	if hasContent {
		return "alert"
	}
	return "background"
}

// resolvePriority picks apns-priority: the configured override wins,
// then Apple's convention — immediate (10) for display alerts, power-
// saving (5) for background delivery.
func (p *Provider) resolvePriority(pushType string) int {
	if p.priority > 0 {
		return p.priority
	}
	if pushType == "background" {
		return 5
	}
	return 10
}

// extractContent extracts title and body from a DeliveryTask
func extractContent(task *core.DeliveryTask) (title, body string) {
	if task.Payload.Content != nil {
		return task.Payload.Content.Title, task.Payload.Content.Body
	}
	return "", ""
}

// send posts the payload for one device token. Non-2xx responses
// surface through httpclient's statusError, which already applies the
// repo-wide retry semantics (408/429/5xx retryable) and carries the APNs
// error body ({"reason":"BadDeviceToken",...}) for diagnostics.
func (p *Provider) send(ctx context.Context, auth, target string, payload map[string]interface{}, pushType string, priority int) error {
	sendURL := fmt.Sprintf("%s/3/device/%s", p.endpoint, target)
	headers := map[string]string{
		"apns-topic":     p.topic,
		"apns-push-type": pushType,
		"apns-priority":  strconv.Itoa(priority),
	}
	if auth != "" {
		headers["authorization"] = "bearer " + auth
	}
	_, err := p.client.PostJSONWithHeaders(ctx, sendURL, payload, headers)
	return err
}

// truncate truncates a string to max length
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return s
}

// Factory creates APNs providers
type Factory struct{}

// Create creates a new APNs provider
func (f *Factory) Create(config map[string]interface{}) (core.Provider, error) {
	return NewProvider(config)
}

// Name returns the factory name
func (f *Factory) Name() string {
	return "apns"
}

// Type returns the factory type
func (f *Factory) Type() string {
	return "apns"
}
