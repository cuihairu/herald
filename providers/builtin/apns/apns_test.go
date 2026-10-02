package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

// newTestECKey generates a fresh P-256 key; every test uses its own so
// signature verifications cannot pass by accident of shared state.
func newTestECKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	return key
}

// pkcs8PEM encodes an EC key the way Apple's .p8 download does.
func pkcs8PEM(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// sec1PEM encodes an EC key in SEC1 ("EC PRIVATE KEY") form, the shape
// some other tooling re-encodes keys into.
func sec1PEM(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal sec1: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

// newCertBundle mints a self-signed client certificate plus its key in
// one PEM bundle — the shape openssl pkcs12 -out produces.
func newCertBundle(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	key := newTestECKey(t)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "apns-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return string(certPEM) + string(keyPEM), key
}

// readBody drains the full request body.
func readBody(r *http.Request) ([]byte, error) {
	var out []byte
	buf := make([]byte, 1024)
	for {
		n, err := r.Body.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, nil
		}
	}
}

// baseConfig is the minimum token-mode configuration every constructor
// test extends.
func baseConfig() map[string]interface{} {
	return map[string]interface{}{
		"key_id":      "ABC123DEFG",
		"team_id":     "TEAM123456",
		"private_key": "",
		"topic":       "com.example.app",
	}
}

// newTokenProvider builds a token-mode provider with a fresh P-256 key.
func newTokenProvider(t *testing.T) *Provider {
	t.Helper()
	cfg := baseConfig()
	cfg["private_key"] = pkcs8PEM(t, newTestECKey(t))
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p.(*Provider)
}

func TestNewProviderValidation(t *testing.T) {
	t.Run("missing topic", func(t *testing.T) {
		cfg := baseConfig()
		cfg["private_key"] = pkcs8PEM(t, newTestECKey(t))
		cfg["topic"] = ""
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "apns: topic is required" {
			t.Errorf("NewProvider = %v, want topic required", err)
		}
	})

	t.Run("token mode missing key_id", func(t *testing.T) {
		cfg := baseConfig()
		cfg["private_key"] = pkcs8PEM(t, newTestECKey(t))
		cfg["key_id"] = ""
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "apns: key_id is required" {
			t.Errorf("NewProvider = %v, want key_id required", err)
		}
	})

	t.Run("token mode missing team_id", func(t *testing.T) {
		cfg := baseConfig()
		cfg["private_key"] = pkcs8PEM(t, newTestECKey(t))
		cfg["team_id"] = ""
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "apns: team_id is required" {
			t.Errorf("NewProvider = %v, want team_id required", err)
		}
	})

	t.Run("token mode missing private_key", func(t *testing.T) {
		_, err := NewProvider(baseConfig())
		if err == nil || err.Error() != "apns: private_key is required" {
			t.Errorf("NewProvider = %v, want private_key required", err)
		}
	})

	t.Run("non-pem private_key", func(t *testing.T) {
		cfg := baseConfig()
		cfg["private_key"] = "not pem at all"
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "apns: private_key is not valid PEM data" {
			t.Errorf("NewProvider = %v, want PEM error", err)
		}
	})

	t.Run("rsa key rejected", func(t *testing.T) {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate rsa: %v", err)
		}
		cfg := baseConfig()
		cfg["private_key"] = pkcs8PEM(t, rsaKey)
		_, err = NewProvider(cfg)
		if err == nil || !strings.Contains(err.Error(), "must be an EC key") {
			t.Errorf("NewProvider = %v, want EC key error", err)
		}
	})

	t.Run("p-384 key rejected", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			t.Fatalf("generate p384: %v", err)
		}
		cfg := baseConfig()
		cfg["private_key"] = pkcs8PEM(t, key)
		_, err = NewProvider(cfg)
		if err == nil || !strings.Contains(err.Error(), "must be a P-256 EC key") {
			t.Errorf("NewProvider = %v, want P-256 curve error", err)
		}
	})

	t.Run("sec1 key accepted", func(t *testing.T) {
		cfg := baseConfig()
		cfg["private_key"] = sec1PEM(t, newTestECKey(t))
		if _, err := NewProvider(cfg); err != nil {
			t.Errorf("NewProvider with SEC1 key: %v", err)
		}
	})

	t.Run("sec1 p-384 rejected", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			t.Fatalf("generate p384: %v", err)
		}
		cfg := baseConfig()
		cfg["private_key"] = sec1PEM(t, key)
		_, err = NewProvider(cfg)
		if err == nil || !strings.Contains(err.Error(), "must be a P-256 EC key") {
			t.Errorf("NewProvider = %v, want P-256 curve error", err)
		}
	})

	t.Run("unparseable key bytes", func(t *testing.T) {
		junk := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("garbage")}))
		cfg := baseConfig()
		cfg["private_key"] = junk
		_, err := NewProvider(cfg)
		if err == nil || !strings.Contains(err.Error(), "failed to parse private_key") {
			t.Errorf("NewProvider = %v, want parse failure", err)
		}
	})
}

func TestParseCertPair(t *testing.T) {
	t.Run("valid bundle", func(t *testing.T) {
		bundle, _ := newCertBundle(t)
		pair, err := parseCertPair(bundle)
		if err != nil {
			t.Fatalf("parseCertPair: %v", err)
		}
		if len(pair.Certificate) == 0 || pair.PrivateKey == nil {
			t.Error("parsed pair missing certificate or key")
		}
	})

	t.Run("cert mode provider", func(t *testing.T) {
		bundle, _ := newCertBundle(t)
		p, err := NewProvider(map[string]interface{}{
			"topic":    "com.example.app",
			"cert_pem": bundle,
		})
		if err != nil {
			t.Fatalf("NewProvider cert mode: %v", err)
		}
		prov := p.(*Provider)
		if !prov.certMode {
			t.Error("certMode = false, want true")
		}
		if prov.key != nil {
			t.Error("cert mode should keep no token key")
		}
	})

	t.Run("missing certificate block", func(t *testing.T) {
		_, key := newCertBundle(t)
		keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
		keyOnly := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
		_, err := parseCertPair(keyOnly)
		if err == nil || err.Error() != "apns: cert_pem has no CERTIFICATE block" {
			t.Errorf("parseCertPair = %v, want missing CERTIFICATE", err)
		}
	})

	t.Run("missing key block", func(t *testing.T) {
		bundle, _ := newCertBundle(t)
		certOnly := bundle[:strings.Index(bundle, "BEGIN PRIVATE KEY")]
		_, err := parseCertPair(certOnly)
		if err == nil || err.Error() != "apns: cert_pem has no private key block" {
			t.Errorf("parseCertPair = %v, want missing key", err)
		}
	})

	t.Run("mismatched cert and key", func(t *testing.T) {
		bundleA, _ := newCertBundle(t)
		bundleB, _ := newCertBundle(t)
		certPart := bundleA[:strings.Index(bundleA, "BEGIN PRIVATE KEY")]
		keyPart := bundleB[strings.Index(bundleB, "BEGIN PRIVATE KEY"):]
		_, err := parseCertPair(certPart + keyPart)
		if err == nil || !strings.Contains(err.Error(), "apns: cert_pem:") {
			t.Errorf("parseCertPair = %v, want X509KeyPair error wrap", err)
		}
	})

	t.Run("constructor surfaces cert failure", func(t *testing.T) {
		// The same failure through NewProvider: construction stops before
		// a provider is built.
		_, err := NewProvider(map[string]interface{}{
			"topic":    "com.example.app",
			"cert_pem": "garbage without blocks",
		})
		if err == nil || !strings.Contains(err.Error(), "apns: cert_pem") {
			t.Errorf("NewProvider = %v, want cert_pem error", err)
		}
	})
}

func TestPadTo32(t *testing.T) {
	short := padTo32([]byte{0x01, 0x02})
	if len(short) != 32 {
		t.Fatalf("padTo32 short = %d bytes, want 32", len(short))
	}
	if short[31] != 0x02 || short[30] != 0x01 || short[0] != 0 {
		t.Errorf("padTo32 short = %v, want right-aligned", short)
	}

	full := make([]byte, 32)
	full[0] = 0xff
	if got := padTo32(full); len(got) != 32 || got[0] != 0xff {
		t.Errorf("padTo32 full = %v, want unchanged", got)
	}
}

func TestBuildProviderToken(t *testing.T) {
	key := newTestECKey(t)
	now := time.Unix(1700000000, 0)

	jwt, err := buildProviderToken("TEAM123456", "ABC123DEFG", key, now)
	if err != nil {
		t.Fatalf("buildProviderToken: %v", err)
	}

	chunks := strings.Split(jwt, ".")
	if len(chunks) != 3 {
		t.Fatalf("token has %d segments, want 3 (JWT shape)", len(chunks))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(chunks[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header tokenHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Alg != "ES256" || header.Kid != "ABC123DEFG" {
		t.Errorf("header = %+v, want ES256 kid=ABC123DEFG", header)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(chunks[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims tokenClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "TEAM123456" {
		t.Errorf("iss = %q", claims.Iss)
	}
	if claims.Iat != now.Unix() || claims.Exp != now.Add(tokenTTL).Unix() {
		t.Errorf("iat/exp = %d/%d, want %d/%d", claims.Iat, claims.Exp, now.Unix(), now.Add(tokenTTL).Unix())
	}

	sig, err := base64.RawURLEncoding.DecodeString(chunks[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	// Apple requires raw R||S (64 bytes for P-256); ASN.1 DER would be a
	// variable 70-72 bytes and is rejected server-side.
	if len(sig) != 64 {
		t.Fatalf("signature = %d bytes, want 64 (raw R||S, not DER)", len(sig))
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	sum := sha256.Sum256([]byte(chunks[0] + "." + chunks[1]))
	if !ecdsa.Verify(&key.PublicKey, sum[:], r, s) {
		t.Error("signature verification failed")
	}
}

func TestGetProviderJWT(t *testing.T) {
	t.Run("caches across deliveries", func(t *testing.T) {
		var calls int32
		orig := buildProviderToken
		buildProviderToken = func(string, string, *ecdsa.PrivateKey, time.Time) (string, error) {
			atomic.AddInt32(&calls, 1)
			return "jwt-1", nil
		}
		defer func() { buildProviderToken = orig }()

		p := newTokenProvider(t)
		for i := 0; i < 2; i++ {
			jwt, err := p.getProviderJWT()
			if err != nil || jwt != "jwt-1" {
				t.Fatalf("getProviderJWT = %q, %v", jwt, err)
			}
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("signing calls = %d, want 1 (cached)", got)
		}
	})

	t.Run("re-signs after expiry", func(t *testing.T) {
		var calls int32
		orig := buildProviderToken
		buildProviderToken = func(string, string, *ecdsa.PrivateKey, time.Time) (string, error) {
			atomic.AddInt32(&calls, 1)
			return fmt.Sprintf("jwt-%d", atomic.LoadInt32(&calls)), nil
		}
		defer func() { buildProviderToken = orig }()

		p := newTokenProvider(t)
		if _, err := p.getProviderJWT(); err != nil {
			t.Fatal(err)
		}
		p.jwtExpireAt = time.Now().Add(-time.Second)
		jwt, err := p.getProviderJWT()
		if err != nil || jwt != "jwt-2" {
			t.Fatalf("getProviderJWT after expiry = %q, %v, want fresh token", jwt, err)
		}
		if got := atomic.LoadInt32(&calls); got != 2 {
			t.Errorf("signing calls = %d, want 2", got)
		}
	})

	t.Run("signing failure wraps error", func(t *testing.T) {
		orig := buildProviderToken
		buildProviderToken = func(string, string, *ecdsa.PrivateKey, time.Time) (string, error) {
			return "", errors.New("sign boom")
		}
		defer func() { buildProviderToken = orig }()

		p := newTokenProvider(t)
		_, err := p.getProviderJWT()
		if err == nil || err.Error() != "apns: failed to sign provider token: sign boom" {
			t.Errorf("getProviderJWT = %v, want wrapped sign failure", err)
		}
	})
}

func TestDeliverValidation(t *testing.T) {
	p := newTokenProvider(t)
	ctx := context.Background()

	if err := p.Deliver(ctx, nil); err == nil || err.Error() != "apns: task is nil" {
		t.Errorf("Deliver(nil) = %v", err)
	}
	if err := p.Deliver(ctx, &core.DeliveryTask{}); err == nil || err.Error() != "apns: at least one target (device token) is required" {
		t.Errorf("Deliver without targets = %v", err)
	}
	err := p.Deliver(ctx, &core.DeliveryTask{Targets: []string{""}})
	if err == nil || err.Error() != "apns: empty target found in targets" {
		t.Errorf("Deliver empty target = %v", err)
	}
	err = p.Deliver(ctx, &core.DeliveryTask{Targets: []string{"tok"}})
	if err == nil || err.Error() != "apns: payload needs content or raw data" {
		t.Errorf("Deliver empty payload = %v", err)
	}
}

// TestDeliverAlertPayload pins the token-mode wire format for a display
// alert: path, auth header, apns-* headers, and the aps alert body.
func TestDeliverAlertPayload(t *testing.T) {
	var gotPath, gotAuth, gotTopic, gotPushType, gotPriority string
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("authorization")
		gotTopic = r.Header.Get("apns-topic")
		gotPushType = r.Header.Get("apns-push-type")
		gotPriority = r.Header.Get("apns-priority")
		gotBody, _ = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTokenProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"device-token-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "磁盘告警", Body: "/dev/sda1 95%", Format: "plain"},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if gotPath != "/3/device/device-token-1" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "bearer ") {
		t.Errorf("authorization = %q, want bearer JWT", gotAuth)
	}
	if gotTopic != "com.example.app" {
		t.Errorf("apns-topic = %q", gotTopic)
	}
	if gotPushType != "alert" {
		t.Errorf("apns-push-type = %q, want alert", gotPushType)
	}
	if gotPriority != "10" {
		t.Errorf("apns-priority = %q, want 10", gotPriority)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	aps, _ := payload["aps"].(map[string]interface{})
	if aps == nil {
		t.Fatalf("body = %s, want aps section", gotBody)
	}
	alert, _ := aps["alert"].(map[string]interface{})
	if alert["title"] != "磁盘告警" || alert["body"] != "/dev/sda1 95%" {
		t.Errorf("alert = %v", alert)
	}
	if aps["sound"] != "default" {
		t.Errorf("sound = %v, want default", aps["sound"])
	}
}

// TestDeliverBackgroundRawOnly pins data-only pushes: background type,
// priority 5, content-available, raw keys at top level with the reserved
// "aps" key skipped.
func TestDeliverBackgroundRawOnly(t *testing.T) {
	var gotPushType, gotPriority string
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPushType = r.Header.Get("apns-push-type")
		gotPriority = r.Header.Get("apns-priority")
		gotBody, _ = readBody(r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTokenProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"device-token-1"},
		Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]any{"event": "deploy", "code": 200, "aps": "clobber-attempt"},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if gotPushType != "background" {
		t.Errorf("apns-push-type = %q, want background", gotPushType)
	}
	if gotPriority != "5" {
		t.Errorf("apns-priority = %q, want 5", gotPriority)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(gotBody, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	aps, _ := payload["aps"].(map[string]interface{})
	if aps["content-available"] != float64(1) {
		t.Errorf("content-available = %v, want 1", aps["content-available"])
	}
	if _, ok := aps["alert"]; ok {
		t.Error("background push must not carry an alert section")
	}
	if payload["event"] != "deploy" || payload["code"] != float64(200) {
		t.Errorf("custom keys = %v", payload)
	}
	if payload["aps"].(map[string]interface{})["content-available"] == nil {
		t.Error("raw aps key must not clobber the computed section")
	}
	if _, ok := payload["aps"].(string); ok {
		t.Error("raw aps key leaked into payload")
	}
}

// TestDeliverConfiguredPushTypePriority checks the explicit overrides
// win over the derived defaults.
func TestDeliverConfiguredPushTypePriority(t *testing.T) {
	var gotPushType, gotPriority string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPushType = r.Header.Get("apns-push-type")
		gotPriority = r.Header.Get("apns-priority")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	// Configured through NewProvider so the push_type/priority parsing
	// path is exercised end to end.
	cfg := baseConfig()
	cfg["private_key"] = pkcs8PEM(t, newTestECKey(t))
	cfg["push_type"] = "liveactivity"
	cfg["priority"] = 5
	prov, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	p := prov.(*Provider)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"device-token-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if gotPushType != "liveactivity" {
		t.Errorf("apns-push-type = %q, want configured override", gotPushType)
	}
	if gotPriority != "5" {
		t.Errorf("apns-priority = %q, want configured override", gotPriority)
	}
}

// TestDeliverCertMode pins certificate authentication: no Authorization
// header is sent because the TLS client certificate carries identity.
func TestDeliverCertMode(t *testing.T) {
	var gotAuth string
	var sawRequest bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		gotAuth = r.Header.Get("authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	bundle, _ := newCertBundle(t)
	p, err := NewProvider(map[string]interface{}{
		"topic":    "com.example.app",
		"cert_pem": bundle,
		"endpoint": ts.URL,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	task := &core.DeliveryTask{
		Targets: []string{"device-token-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	if err := p.(*Provider).Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if !sawRequest {
		t.Fatal("stub endpoint not reached")
	}
	if gotAuth != "" {
		t.Errorf("authorization = %q, want empty in cert mode", gotAuth)
	}
}

func TestDeliverPartialFailureAggregates(t *testing.T) {
	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "bad-device-token") || strings.HasSuffix(r.URL.Path, "/bad1") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"reason":"BadDeviceToken"}`))
			return
		}
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	p := newTokenProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"bad-device-token", "good-device-token", "bad1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	err := p.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("Deliver with failing targets = nil error")
	}
	// Long failing targets are truncated at 8 runes; the short one passes
	// through whole — both arms of truncate must appear in the aggregate.
	want := "apns: 1/3 succeeded - failed: bad-devi"
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error = %q, want prefix %q", err.Error(), want)
	}
	if !strings.Contains(err.Error(), "bad1:") {
		t.Errorf("error = %q, want untruncated short target", err.Error())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("successful sends = %d, want 1", calls)
	}
}

// TestDeliverProviderTokenFailure ensures a signing failure surfaces as
// the wrapped provider-token error before any network call.
func TestDeliverProviderTokenFailure(t *testing.T) {
	orig := buildProviderToken
	buildProviderToken = func(string, string, *ecdsa.PrivateKey, time.Time) (string, error) {
		return "", errors.New("sign boom")
	}
	defer func() { buildProviderToken = orig }()

	p := newTokenProvider(t)
	task := &core.DeliveryTask{
		Targets: []string{"device-token-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	err := p.Deliver(context.Background(), task)
	if err == nil || err.Error() != "apns: failed to get provider token: apns: failed to sign provider token: sign boom" {
		t.Errorf("Deliver = %v, want wrapped token failure", err)
	}
}

// TestDeliverHTTPErrorSurfacesBody checks the APNs reason body reaches
// the aggregated error for diagnostics (retry semantics stay with
// httpclient's statusError).
func TestDeliverHTTPErrorSurfacesBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"reason":"TooManyRequests"}`))
	}))
	t.Cleanup(ts.Close)

	p := newTokenProvider(t)
	p.endpoint = ts.URL

	task := &core.DeliveryTask{
		Targets: []string{"device-token-1"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	}
	err := p.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("Deliver against 429 = nil error")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "TooManyRequests") {
		t.Errorf("error = %v, want status and APNs reason", err)
	}
	if strings.Contains(err.Error(), "succeeded") == false {
		t.Errorf("error = %v, want aggregated form", err)
	}
}

func TestParseConfigPriorityTypes(t *testing.T) {
	cfg, _ := parseConfig(map[string]interface{}{"priority": float64(5)})
	if cfg.Priority != 5 {
		t.Errorf("priority from float64 = %d, want 5", cfg.Priority)
	}
	cfg, _ = parseConfig(map[string]interface{}{"priority": 10})
	if cfg.Priority != 10 {
		t.Errorf("priority from int = %d, want 10", cfg.Priority)
	}
}

func TestProviderSurface(t *testing.T) {
	p := newTokenProvider(t)

	if got := p.Name(); got != "apns" {
		t.Errorf("Name() = %q", got)
	}
	if got := p.Type(); got != "apns" {
		t.Errorf("Type() = %q", got)
	}
	if st := p.Status(); st == nil || st.Status != "available" {
		t.Errorf("Status() = %+v", st)
	}
	// Close is a convention outside the core.Provider interface.
	if err := p.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}

	cfg := p.GetConfig()
	if cfg["key_id"] != "ABC123DEFG" || cfg["team_id"] != "TEAM123456" {
		t.Errorf("GetConfig identity = %v", cfg)
	}
	if cfg["topic"] != "com.example.app" {
		t.Errorf("GetConfig topic = %v", cfg["topic"])
	}
	if cfg["private_key"] != "(PEM)" || cfg["cert_pem"] != "(PEM)" {
		t.Errorf("GetConfig placeholders = %v", cfg)
	}
	if cfg["endpoint"] != defaultEndpoint {
		t.Errorf("GetConfig endpoint = %v", cfg["endpoint"])
	}

	cap := p.Capability()
	if len(cap.PayloadKinds) != 2 || cap.ContentFormats[0] != "plain" {
		t.Errorf("Capability() = %+v", cap)
	}

	f := &Factory{}
	if f.Name() != "apns" || f.Type() != "apns" {
		t.Errorf("Factory name/type = %q/%q", f.Name(), f.Type())
	}
	built, err := f.Create(map[string]interface{}{
		"key_id": "ABC123DEFG", "team_id": "TEAM123456",
		"private_key": pkcs8PEM(t, newTestECKey(t)),
		"topic":       "com.example.app",
	})
	if err != nil || built == nil {
		t.Errorf("Factory.Create() = %v, %v", built, err)
	}
	if _, err := f.Create(map[string]interface{}{}); err == nil {
		t.Error("Factory.Create(empty) = nil error")
	}
}
