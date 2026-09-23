package neteasesms

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

// The provider hardcodes "https://" in request URLs and httpclient.NewClient
// builds its own transport, so tests intercept outbound traffic with a single
// shared TLS server whose self-signed certificate is injected into the system
// root pool via SSL_CERT_FILE (read lazily by crypto/x509 on first use).

var (
	testServer *httptest.Server
	testHost   string

	handlerMu  sync.Mutex
	reqHandler func(http.ResponseWriter, *http.Request)
)

func TestMain(m *testing.M) {
	testServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerMu.Lock()
		h := reqHandler
		handlerMu.Unlock()
		if h == nil {
			http.Error(w, "no handler configured", http.StatusInternalServerError)
			return
		}
		h(w, r)
	}))
	defer testServer.Close()

	dir, err := os.MkdirTemp("", "neteasesms-test-cert")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: testServer.Certificate().Raw,
	})
	certPath := filepath.Join(dir, "test-cert.pem")
	if err := os.WriteFile(certPath, certPEM, 0600); err != nil {
		panic(err)
	}
	if err := os.Setenv("SSL_CERT_FILE", certPath); err != nil {
		panic(err)
	}

	testHost = strings.TrimPrefix(testServer.URL, "https://")
	os.Exit(m.Run())
}

func setHandler(t *testing.T, h func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	handlerMu.Lock()
	reqHandler = h
	handlerMu.Unlock()
	t.Cleanup(func() {
		handlerMu.Lock()
		reqHandler = nil
		handlerMu.Unlock()
	})
}

func validConfig() map[string]interface{} {
	return map[string]interface{}{
		"app_key":    "fake_app_key_for_test",
		"app_secret": "fake_secret_unit_test",
	}
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	config := validConfig()
	config["endpoint"] = testHost
	p, err := NewProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return p.(*Provider)
}

func templateTask(targets []string, templateID, templateCode string, params interface{}) *core.DeliveryTask {
	return &core.DeliveryTask{
		ID:       "task-1",
		Provider: "neteasesms",
		Targets:  targets,
		Payload: core.DeliveryPayload{
			Kind: core.PayloadProviderTemplate,
			ProviderTemplate: &core.ProviderTemplatePayload{
				TemplateID:   templateID,
				TemplateCode: templateCode,
				Params:       params,
			},
		},
		CreatedAt: time.Now(),
	}
}

func okResponse() string {
	return `{"code":200,"msg":"success","obj":"sendid-1"}`
}

func TestNewProviderConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		p, err := NewProvider(validConfig())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.appKey != "fake_app_key_for_test" {
			t.Errorf("expected app key fake_app_key_for_test, got %s", provider.appKey)
		}
		if provider.appSecret != "fake_secret_unit_test" {
			t.Errorf("expected app secret fake_secret_unit_test, got %s", provider.appSecret)
		}
		if provider.endpoint != defaultEndpoint {
			t.Errorf("expected default endpoint %s, got %s", defaultEndpoint, provider.endpoint)
		}
		if !provider.enabled {
			t.Error("expected enabled by default")
		}
		if provider.nonce == "" {
			t.Error("expected non-empty nonce")
		}
		if provider.client == nil {
			t.Error("expected non-nil client")
		}
		if provider.status == nil {
			t.Fatal("expected non-nil status")
		}
		if provider.status.Name != "neteasesms" || provider.status.Type != "builtin" || provider.status.Status != "available" {
			t.Errorf("unexpected status: %+v", provider.status)
		}
		if provider.status.Since.IsZero() {
			t.Error("expected non-zero Since")
		}
	})

	t.Run("missing app_key", func(t *testing.T) {
		config := validConfig()
		delete(config, "app_key")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "app_key is required") {
			t.Errorf("expected app_key error, got %v", err)
		}
	})

	t.Run("empty app_key", func(t *testing.T) {
		config := validConfig()
		config["app_key"] = ""
		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty app_key")
		}
	})

	t.Run("non-string app_key", func(t *testing.T) {
		config := validConfig()
		config["app_key"] = 123
		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for non-string app_key")
		}
	})

	t.Run("missing app_secret", func(t *testing.T) {
		config := validConfig()
		delete(config, "app_secret")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "app_secret is required") {
			t.Errorf("expected app_secret error, got %v", err)
		}
	})

	t.Run("empty app_secret", func(t *testing.T) {
		config := validConfig()
		config["app_secret"] = ""
		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty app_secret")
		}
	})

	t.Run("non-string app_secret", func(t *testing.T) {
		config := validConfig()
		config["app_secret"] = true
		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for non-string app_secret")
		}
	})

	t.Run("custom values", func(t *testing.T) {
		config := validConfig()
		config["endpoint"] = "sms.example.com"
		config["enabled"] = false
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.endpoint != "sms.example.com" {
			t.Errorf("expected endpoint sms.example.com, got %s", provider.endpoint)
		}
		if provider.enabled {
			t.Error("expected disabled")
		}
		if provider.status.Enabled {
			t.Error("expected status disabled")
		}
	})

	t.Run("empty endpoint falls back to default", func(t *testing.T) {
		config := validConfig()
		config["endpoint"] = ""
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.(*Provider).endpoint != defaultEndpoint {
			t.Errorf("expected default endpoint, got %s", p.(*Provider).endpoint)
		}
	})

	t.Run("non-string endpoint falls back to default", func(t *testing.T) {
		config := validConfig()
		config["endpoint"] = 123
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.(*Provider).endpoint != defaultEndpoint {
			t.Errorf("expected default endpoint, got %s", p.(*Provider).endpoint)
		}
	})

	t.Run("non-bool enabled falls back to default", func(t *testing.T) {
		config := validConfig()
		config["enabled"] = "yes"
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !p.(*Provider).enabled {
			t.Error("expected enabled default true")
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	p := newTestProvider(t)
	got := p.GetConfig()

	if got["app_key"] != "fake_app_key_for_test" {
		t.Errorf("expected app_key fake_app_key_for_test, got %v", got["app_key"])
	}
	if got["app_secret"] != "fake_secret_unit_test" {
		t.Errorf("expected app_secret fake_secret_unit_test, got %v", got["app_secret"])
	}
	if got["endpoint"] != testHost {
		t.Errorf("expected endpoint %s, got %v", testHost, got["endpoint"])
	}
}

func TestProviderCapability(t *testing.T) {
	p := newTestProvider(t)
	capability := p.Capability()
	if len(capability.PayloadKinds) != 1 {
		t.Fatalf("expected 1 payload kind, got %d", len(capability.PayloadKinds))
	}
	if capability.PayloadKinds[0] != core.PayloadProviderTemplate {
		t.Errorf("expected payload kind provider_template, got %s", capability.PayloadKinds[0])
	}
	if !capability.SupportsTemplate {
		t.Error("expected SupportsTemplate true")
	}
}

func TestProviderMetadata(t *testing.T) {
	p := newTestProvider(t)
	if p.Name() != "neteasesms" {
		t.Errorf("expected name neteasesms, got %s", p.Name())
	}
	if p.Type() != "neteasesms" {
		t.Errorf("expected type neteasesms, got %s", p.Type())
	}
	if err := p.Close(); err != nil {
		t.Errorf("expected no error on close, got %v", err)
	}

	status := p.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "neteasesms" {
		t.Errorf("expected status name neteasesms, got %s", status.Name)
	}
	if status.Type != "builtin" {
		t.Errorf("expected status type builtin, got %s", status.Type)
	}
	if status.Status != "available" {
		t.Errorf("expected status available, got %s", status.Status)
	}
	if !status.Enabled {
		t.Error("expected status enabled")
	}
}

func TestProviderEnableDisable(t *testing.T) {
	p := newTestProvider(t)
	if !p.IsEnabled() {
		t.Error("expected enabled initially")
	}

	p.Disable()
	if p.IsEnabled() {
		t.Error("expected disabled after Disable")
	}
	if p.Status().Enabled {
		t.Error("expected status disabled after Disable")
	}

	p.Enable()
	if !p.IsEnabled() {
		t.Error("expected enabled after Enable")
	}
	if !p.Status().Enabled {
		t.Error("expected status enabled after Enable")
	}
}

func TestFactory(t *testing.T) {
	factory := &Factory{}
	if factory.Name() != "neteasesms" {
		t.Errorf("expected factory name neteasesms, got %s", factory.Name())
	}
	if factory.Type() != "neteasesms" {
		t.Errorf("expected factory type neteasesms, got %s", factory.Type())
	}

	p, err := factory.Create(validConfig())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p == nil || p.Name() != "neteasesms" {
		t.Errorf("expected neteasesms provider, got %v", p)
	}

	invalid := validConfig()
	delete(invalid, "app_key")
	if _, err := factory.Create(invalid); err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestCalculateChecksum(t *testing.T) {
	p := newTestProvider(t)

	// Fixed vectors computed independently (sha1(appSecret + nonce + curTime)).
	got := p.calculateChecksum("456", "1234567890")
	if got != "774823985ff248a1f34387e00a8364028b84dcd5" {
		t.Errorf("checksum mismatch: got %s", got)
	}

	got = p.calculateChecksum("456", "1700000000")
	if got != "09cdf187d70480f915e1a54eee9adb7cdf5a34a0" {
		t.Errorf("checksum mismatch: got %s", got)
	}

	got = p.calculateChecksum("456", "1700000001")
	if got != "59044f8045b2d8464e8ff6d4cc19203727a98768" {
		t.Errorf("checksum mismatch: got %s", got)
	}

	// Consistency with direct sha1 of the documented concatenation order.
	h := sha1.Sum([]byte(p.appSecret + "nonce-value" + "curtime-value"))
	if p.calculateChecksum("nonce-value", "curtime-value") != hex.EncodeToString(h[:]) {
		t.Error("checksum should be sha1(appSecret + nonce + curTime)")
	}

	// Output must be lowercase hex of a SHA-1 digest.
	sum := p.calculateChecksum("a", "b")
	if len(sum) != 40 {
		t.Errorf("expected 40-char hex checksum, got %d chars: %s", len(sum), sum)
	}
}
