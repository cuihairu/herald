package fcm

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
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
	"github.com/cuihairu/herald/core/httpclient"
)

// newTestKey generates a fresh RSA key; every test uses its own so stub
// verifications cannot pass by accident of shared state.
func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

// pkcs8PEM encodes a key the way Google service-account JSON does.
func pkcs8PEM(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// newTestProvider builds a provider wired to a stub FCM endpoint; the stub
// and the requests it saw are returned for assertions.
func newTestProvider(t *testing.T, status int, respond func(w http.ResponseWriter, r *http.Request)) (*Provider, *httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if respond != nil {
			respond(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(ts.Close)

	p, err := NewProvider(map[string]interface{}{
		"project_id":   "proj-1",
		"client_email": "sa@proj-1.iam.gserviceaccount.com",
		"private_key":  pkcs8PEM(t, newTestKey(t)),
		"endpoint":     ts.URL,
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p.(*Provider), ts, &hits
}

// stubOAuth points the oauthURL seam at a stub token endpoint and restores
// it when the test ends.
func stubOAuth(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	orig := oauthURL
	oauthURL = ts.URL
	t.Cleanup(func() { oauthURL = orig })
	return ts
}

func oauthStub(t *testing.T) *httptest.Server {
	return stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok-1", ExpiresIn: 3600})
	})
}

func TestNewProviderValidation(t *testing.T) {
	pemKey := pkcs8PEM(t, newTestKey(t))

	base := map[string]interface{}{
		"project_id":   "proj-1",
		"client_email": "sa@proj-1.iam.gserviceaccount.com",
		"private_key":  pemKey,
	}
	if _, err := NewProvider(base); err != nil {
		t.Fatalf("NewProvider(valid) = %v, want nil", err)
	}

	for _, field := range []string{"project_id", "client_email", "private_key"} {
		cfg := map[string]interface{}{}
		for k, v := range base {
			if k != field {
				cfg[k] = v
			}
		}
		if _, err := NewProvider(cfg); err == nil {
			t.Errorf("NewProvider(missing %s) = nil error, want validation error", field)
		}
	}

	// Wrong-typed values must type-assert to empty and fail validation, not panic.
	if _, err := NewProvider(map[string]interface{}{
		"project_id": 1, "client_email": true, "private_key": []byte("x"),
	}); err == nil {
		t.Error("NewProvider(wrong types) = nil error, want validation error")
	}

	if _, err := NewProvider(map[string]interface{}{"private_key": "not pem", "project_id": "p", "client_email": "e"}); err == nil {
		t.Error("NewProvider(non-PEM key) = nil error, want PEM error")
	}

	junk := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk-der")})
	if _, err := NewProvider(map[string]interface{}{"private_key": string(junk), "project_id": "p", "client_email": "e"}); err == nil {
		t.Error("NewProvider(invalid DER) = nil error, want parse error")
	}

	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if _, err := NewProvider(map[string]interface{}{"private_key": pkcs8PEM(t, ecKey), "project_id": "p", "client_email": "e"}); err == nil {
		t.Error("NewProvider(ECDSA key) = nil error, want non-RSA error")
	}

	// A trailing slash on a custom endpoint is trimmed so joins stay clean.
	p, err := NewProvider(map[string]interface{}{
		"project_id": "p", "client_email": "e", "private_key": pemKey,
		"endpoint": "http://127.0.0.1:1/",
	})
	if err != nil {
		t.Fatalf("NewProvider(custom endpoint): %v", err)
	}
	if got := p.(*Provider).endpoint; got != "http://127.0.0.1:1" {
		t.Errorf("endpoint = %q, want trailing slash trimmed", got)
	}
}

func TestBuildAssertionVerifiable(t *testing.T) {
	key := newTestKey(t)
	now := time.Unix(1700000000, 0)

	assertion, err := buildAssertion("sa@proj-1.iam.gserviceaccount.com", key, "https://oauth2.example/token", now)
	if err != nil {
		t.Fatalf("buildAssertion: %v", err)
	}

	chunks := strings.Split(assertion, ".")
	if len(chunks) != 3 {
		t.Fatalf("assertion has %d segments, want 3 (JWT shape)", len(chunks))
	}

	sig, err := base64.RawURLEncoding.DecodeString(chunks[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	sum := sha256.Sum256([]byte(chunks[0] + "." + chunks[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(chunks[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "sa@proj-1.iam.gserviceaccount.com" {
		t.Errorf("iss = %q", claims.Iss)
	}
	if claims.Aud != "https://oauth2.example/token" {
		t.Errorf("aud = %q", claims.Aud)
	}
	if claims.Scope != messagingScope {
		t.Errorf("scope = %q, want %q", claims.Scope, messagingScope)
	}
	if claims.Iat != now.Unix() || claims.Exp != now.Add(assertionTTL).Unix() {
		t.Errorf("iat/exp = %d/%d, want %d/%d", claims.Iat, claims.Exp, now.Unix(), now.Add(assertionTTL).Unix())
	}
}

// tinyRSAKey builds a 256-bit RSA key from two real 128-bit primes. It is
// structurally valid (Validate and x509 Marshal/Parse all pass, so it would
// load from config) but rejected by rsa.SignPKCS1v15. rsa.GenerateKey
// refuses to build keys this small, hence the manual assembly.
func tinyRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	p, err := rand.Prime(rand.Reader, 128)
	if err != nil {
		t.Fatalf("prime p: %v", err)
	}
	q, err := rand.Prime(rand.Reader, 128)
	if err != nil {
		t.Fatalf("prime q: %v", err)
	}
	totient := new(big.Int).Mul(new(big.Int).Sub(p, big.NewInt(1)), new(big.Int).Sub(q, big.NewInt(1)))
	d := new(big.Int).ModInverse(big.NewInt(65537), totient)
	if d == nil {
		t.Fatal("no modular inverse for e=65537")
	}
	key := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{N: new(big.Int).Mul(p, q), E: 65537},
		D:         d,
		Primes:    []*big.Int{p, q},
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("tiny key invalid: %v", err)
	}
	return key
}

// TestBuildAssertionUndersizedKey drives the real signing path with the
// undersized key: the sign-error branch is unreachable with a full-size
// key, and an operator pasting an invalid service-account key must see the
// crypto rejection instead of a silent empty signature.
func TestBuildAssertionUndersizedKey(t *testing.T) {
	key := tinyRSAKey(t)
	_, err := buildAssertion("sa@proj-1.iam.gserviceaccount.com", key, "https://oauth2.example/token", time.Now())
	if err == nil {
		t.Fatal("buildAssertion with undersized key = nil error")
	}
	if !strings.Contains(err.Error(), "crypto/rsa") {
		t.Errorf("buildAssertion error = %v, want crypto/rsa rejection", err)
	}
}

// TestGetTokenSurfacesSigningFailure pins the operator-visible behavior:
// signing fails before any network call, and getToken wraps the crypto
// error as a failed assertion.
func TestGetTokenSurfacesSigningFailure(t *testing.T) {
	p, err := NewProvider(map[string]interface{}{
		"project_id":   "proj-1",
		"client_email": "sa@proj-1.iam.gserviceaccount.com",
		"private_key":  pkcs8PEM(t, tinyRSAKey(t)),
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("token endpoint must not be reached when signing fails")
	})
	_, err = p.(*Provider).getToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fcm: failed to sign assertion") {
		t.Errorf("getToken error = %v, want assertion failure wrapping crypto error", err)
	}
}

func TestGetTokenCachesAcrossDeliveries(t *testing.T) {
	var fetches atomic.Int32
	stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q", got)
		}
		if r.Form.Get("assertion") == "" {
			t.Error("assertion missing from token request")
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: fmt.Sprintf("tok-%d", fetches.Load()), ExpiresIn: 3600})
	})
	p, _, _ := newTestProvider(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx := context.Background()
	tok1, err := p.getToken(ctx)
	if err != nil {
		t.Fatalf("first getToken: %v", err)
	}
	tok2, err := p.getToken(ctx)
	if err != nil {
		t.Fatalf("second getToken: %v", err)
	}
	if tok1 != "tok-1" || tok2 != "tok-1" {
		t.Errorf("tokens = %q/%q, want cached tok-1", tok1, tok2)
	}
	if got := fetches.Load(); got != 1 {
		t.Errorf("token endpoint hit %d times, want 1 (cache)", got)
	}

	// expires_in is trimmed by five minutes for an early refresh.
	want := time.Now().Add(3300 * time.Second)
	if p.tokens.expireAt.Before(want.Add(-time.Minute)) || p.tokens.expireAt.After(want.Add(time.Minute)) {
		t.Errorf("expireAt = %v, want ~%v", p.tokens.expireAt, want)
	}
}

func TestGetTokenExpiryDefaults(t *testing.T) {
	stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		// No expires_in at all → the one-hour default minus nothing.
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok-d"})
	})
	p, _, _ := newTestProvider(t, http.StatusOK, nil)
	if _, err := p.getToken(context.Background()); err != nil {
		t.Fatalf("getToken: %v", err)
	}
	want := time.Now().Add(3300 * time.Second)
	if p.tokens.expireAt.Before(want.Add(-time.Minute)) || p.tokens.expireAt.After(want.Add(time.Minute)) {
		t.Errorf("expireAt = %v, want ~%v (default 3600 - 300 early refresh)", p.tokens.expireAt, want)
	}
}

func TestGetTokenShortExpiryKeptAsIs(t *testing.T) {
	stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "tok-s", ExpiresIn: 200})
	})
	p, _, _ := newTestProvider(t, http.StatusOK, nil)
	if _, err := p.getToken(context.Background()); err != nil {
		t.Fatalf("getToken: %v", err)
	}
	want := time.Now().Add(200 * time.Second)
	if p.tokens.expireAt.Before(want.Add(-time.Minute)) || p.tokens.expireAt.After(want.Add(time.Minute)) {
		t.Errorf("expireAt = %v, want ~%v (short TTL untouched)", p.tokens.expireAt, want)
	}
}

func TestGetTokenErrors(t *testing.T) {
	t.Run("oauth error payload", func(t *testing.T) {
		stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(tokenResponse{Error: "invalid_grant", ErrorDescription: "bad key"})
		})
		p, _, _ := newTestProvider(t, http.StatusOK, nil)
		_, err := p.getToken(context.Background())
		if err == nil || err.Error() != "fcm: token error: invalid_grant: bad key" {
			t.Errorf("getToken error = %v, want invalid_grant message", err)
		}
	})

	t.Run("missing access token", func(t *testing.T) {
		stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{})
		})
		p, _, _ := newTestProvider(t, http.StatusOK, nil)
		_, err := p.getToken(context.Background())
		if err == nil || err.Error() != "fcm: token response missing access_token" {
			t.Errorf("getToken error = %v, want missing access_token", err)
		}
	})

	t.Run("non-json body", func(t *testing.T) {
		stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not json"))
		})
		p, _, _ := newTestProvider(t, http.StatusOK, nil)
		if _, err := p.getToken(context.Background()); err == nil {
			t.Error("getToken with non-JSON body = nil error")
		}
	})

	t.Run("http 500", func(t *testing.T) {
		stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
		p, _, _ := newTestProvider(t, http.StatusOK, nil)
		_, err := p.getToken(context.Background())
		if err == nil {
			t.Fatal("getToken with 500 = nil error")
		}
		if !httpclient.IsRetryable(err) {
			t.Errorf("getToken 500 error not marked retryable: %v", err)
		}
	})

	t.Run("assertion signing failure", func(t *testing.T) {
		stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {})
		p, _, _ := newTestProvider(t, http.StatusOK, nil)
		orig := buildAssertion
		buildAssertion = func(string, *rsa.PrivateKey, string, time.Time) (string, error) {
			return "", errors.New("sign boom")
		}
		defer func() { buildAssertion = orig }()
		_, err := p.getToken(context.Background())
		if err == nil || err.Error() != "fcm: failed to sign assertion: sign boom" {
			t.Errorf("getToken error = %v, want assertion failure", err)
		}
	})
}

func TestDeliverSendsNotificationToEachToken(t *testing.T) {
	oauthStub(t)
	p, _, hits := newTestProvider(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if want := "/v1/projects/proj-1/messages:send"; r.URL.Path != want {
			t.Errorf("path = %s, want %s", r.URL.Path, want)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("Authorization = %q, want bearer tok-1", got)
		}
		var msg fcmMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch msg.Message.Token {
		case "device-token-a":
			if msg.Message.Notification == nil || msg.Message.Notification.Title != "disk full" || msg.Message.Notification.Body != "on /dev/sda1" {
				t.Errorf("notification = %+v", msg.Message.Notification)
			}
			if msg.Message.Data["host"] != "node-1" {
				t.Errorf("data = %v, want host=node-1", msg.Message.Data)
			}
		case "device-token-b":
		default:
			t.Errorf("unexpected token %q", msg.Message.Token)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "projects/proj-1/messages/1"})
	})

	err := p.Deliver(context.Background(), &core.DeliveryTask{
		ID: "t1", Provider: "fcm",
		Targets: []string{"device-token-a", "device-token-b"},
		Payload: core.DeliveryPayload{
			Kind: core.PayloadContent,
			Content: &core.RenderedContent{Title: "disk full", Body: "on /dev/sda1", Format: "plain"},
			Raw: map[string]any{"host": "node-1"},
		},
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("FCM endpoint hit %d times, want 2", got)
	}
}

func TestDeliverDataOnlyOmitsNotification(t *testing.T) {
	oauthStub(t)
	var bodies []fcmMessage
	p, _, _ := newTestProvider(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		var msg fcmMessage
		_ = json.NewDecoder(r.Body).Decode(&msg)
		bodies = append(bodies, msg)
		_, _ = w.Write([]byte(`{}`))
	})

	err := p.Deliver(context.Background(), &core.DeliveryTask{
		ID: "t2", Provider: "fcm",
		Targets: []string{"tok-x"},
		Payload: core.DeliveryPayload{Kind: core.PayloadRaw, Raw: map[string]any{"event": "deploy", "code": 200}},
	})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("captured %d bodies, want 1", len(bodies))
	}
	if bodies[0].Message.Notification != nil {
		t.Errorf("notification = %+v, want nil for data-only payload", bodies[0].Message.Notification)
	}
	if bodies[0].Message.Data["event"] != "deploy" || bodies[0].Message.Data["code"] != "200" {
		t.Errorf("data = %v, want stringified raw payload", bodies[0].Message.Data)
	}

	// Neither content nor raw: notification stays nil and data stays empty.
	err = p.Deliver(context.Background(), &core.DeliveryTask{
		ID: "t3", Provider: "fcm", Targets: []string{"tok-y"},
		Payload: core.DeliveryPayload{Kind: core.PayloadContent},
	})
	if err != nil {
		t.Fatalf("Deliver(empty content): %v", err)
	}
	if len(bodies) != 2 || bodies[1].Message.Notification != nil || bodies[1].Message.Data != nil {
		t.Errorf("empty-payload body = %+v", bodies)
	}
}

func TestDeliverValidatesTargets(t *testing.T) {
	oauthStub(t)
	p, _, _ := newTestProvider(t, http.StatusOK, nil)
	ctx := context.Background()

	if err := p.Deliver(ctx, nil); err == nil || err.Error() != "fcm: task is nil" {
		t.Errorf("Deliver(nil) = %v", err)
	}
	if err := p.Deliver(ctx, &core.DeliveryTask{ID: "t", Payload: core.DeliveryPayload{}}); err == nil || err.Error() != "fcm: at least one target (device token) is required" {
		t.Errorf("Deliver(no targets) = %v", err)
	}
	err := p.Deliver(ctx, &core.DeliveryTask{
		ID: "t", Targets: []string{"ok", ""},
		Payload: core.DeliveryPayload{},
	})
	if err == nil || err.Error() != "fcm: empty target found in targets" {
		t.Errorf("Deliver(empty target) = %v", err)
	}
}

func TestDeliverPartialFailureAggregates(t *testing.T) {
	oauthStub(t)
	p, _, _ := newTestProvider(t, http.StatusOK, func(w http.ResponseWriter, r *http.Request) {
		var msg fcmMessage
		_ = json.NewDecoder(r.Body).Decode(&msg)
		// Two failures: a short token (exercises truncate's pass-through)
		// and a long one (exercises truncation to eight runes).
		if msg.Message.Token == "bad1" || msg.Message.Token == "bad-token-that-is-considerably-longer" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"Requested entity was not found.","status":"NOT_FOUND"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"projects/p/messages/1"}`))
	})

	err := p.Deliver(context.Background(), &core.DeliveryTask{
		ID: "t4", Provider: "fcm",
		Targets: []string{"good-tok", "bad1", "bad-token-that-is-considerably-longer"},
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "t", Body: "b", Format: "plain"},
		},
	})
	if err == nil {
		t.Fatal("Deliver with failing targets = nil error")
	}
	want := "fcm: 1/3 succeeded - failed: bad1: unexpected status code: 404"
	if !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Deliver error = %q, want prefix %q", err.Error(), want)
	}
	if !strings.Contains(err.Error(), "bad-toke: unexpected status code: 404") {
		t.Errorf("Deliver error = %q, want truncated long-token entry", err.Error())
	}

	// A 404 is a deterministic client error: it must NOT be marked retryable.
	if httpclient.IsRetryable(err) {
		t.Error("aggregated 404 error marked retryable")
	}
}

func TestDeliverTokenFailureWrapped(t *testing.T) {
	stubOAuth(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	p, _, _ := newTestProvider(t, http.StatusOK, nil)
	err := p.Deliver(context.Background(), &core.DeliveryTask{
		ID: "t5", Targets: []string{"tok"},
		Payload: core.DeliveryPayload{Kind: core.PayloadContent},
	})
	if err == nil || !strings.Contains(err.Error(), "fcm: failed to get access token") {
		t.Errorf("Deliver with failing token exchange = %v, want wrapped token error", err)
	}
}

func TestProviderSurface(t *testing.T) {
	p, err := NewProvider(map[string]interface{}{
		"project_id":   "proj-1",
		"client_email": "sa@proj-1.iam.gserviceaccount.com",
		"private_key":  pkcs8PEM(t, newTestKey(t)),
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if got := p.Name(); got != "fcm" {
		t.Errorf("Name() = %q", got)
	}
	if got := p.Type(); got != "fcm" {
		t.Errorf("Type() = %q", got)
	}
	if st := p.Status(); st == nil || st.Status != "available" {
		t.Errorf("Status() = %+v", st)
	}
	// Close is a convention outside the core.Provider interface.
	if err := p.(*Provider).Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}

	cfg := p.(*Provider).GetConfig()
	if cfg["project_id"] != "proj-1" || cfg["client_email"] != "sa@proj-1.iam.gserviceaccount.com" {
		t.Errorf("GetConfig() = %v", cfg)
	}
	if cfg["private_key"] != "(PEM)" {
		t.Errorf("GetConfig()[private_key] = %v, want placeholder", cfg["private_key"])
	}
	if cfg["endpoint"] != defaultEndpoint {
		t.Errorf("GetConfig()[endpoint] = %v, want default", cfg["endpoint"])
	}

	cap := p.(*Provider).Capability()
	if len(cap.PayloadKinds) != 2 || cap.ContentFormats[0] != "plain" {
		t.Errorf("Capability() = %+v", cap)
	}

	f := &Factory{}
	if f.Name() != "fcm" || f.Type() != "fcm" {
		t.Errorf("Factory name/type = %q/%q", f.Name(), f.Type())
	}
	built, err := f.Create(map[string]interface{}{
		"project_id": "p", "client_email": "e", "private_key": pkcs8PEM(t, newTestKey(t)),
	})
	if err != nil || built == nil {
		t.Errorf("Factory.Create() = %v, %v", built, err)
	}
	if _, err := f.Create(map[string]interface{}{}); err == nil {
		t.Error("Factory.Create(empty) = nil error")
	}
}
