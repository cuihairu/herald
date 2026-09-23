package aliyunsms

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

// The provider hardcodes "https://" and builds its own http.Transport inside
// httpclient.NewClient, so requests cannot be intercepted with a custom
// RoundTripper. Instead we start a single httptest TLS server for the whole
// test binary and point the default Go x509 root pool (SSL_CERT_FILE) at its
// certificate before any test runs, so the provider's transport trusts it.
var (
	stubMu     sync.Mutex
	stubFunc   http.HandlerFunc
	stubServer *httptest.Server
)

const okResponse = `{"Code":"OK","Message":"OK","BizId":"9006197469364984040","RequestId":"F655A8D5-B967-440B-8683-DAD6FF8DE990"}`

func TestMain(m *testing.M) {
	stubServer = httptest.NewTLSServer(http.HandlerFunc(handleStub))

	dir, err := os.MkdirTemp("", "aliyunsms-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create temp dir:", err)
		os.Exit(1)
	}
	certPath := filepath.Join(dir, "roots.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: stubServer.Certificate().Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "failed to write test certificate:", err)
		os.Exit(1)
	}
	os.Setenv("SSL_CERT_FILE", certPath)

	code := m.Run()

	// Force-close any connection still parked in a handler that waits on the
	// request context (e.g. the canceled-context test) so Close cannot hang.
	stubServer.CloseClientConnections()
	stubServer.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

func handleStub(w http.ResponseWriter, r *http.Request) {
	stubMu.Lock()
	fn := stubFunc
	stubMu.Unlock()
	if fn != nil {
		fn(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, okResponse)
}

func setStub(t *testing.T, fn http.HandlerFunc) {
	t.Helper()
	stubMu.Lock()
	prev := stubFunc
	stubFunc = fn
	stubMu.Unlock()
	t.Cleanup(func() {
		stubMu.Lock()
		stubFunc = prev
		stubMu.Unlock()
	})
}

func stubEndpoint() string {
	return strings.TrimPrefix(stubServer.URL, "https://")
}

func validConfig() map[string]interface{} {
	return map[string]interface{}{
		"access_key_id":     "AKIDtest",
		"access_key_secret": "keytest",
		"sign_name":         "TestSign",
	}
}

func newTestProvider(t *testing.T, endpoint string) *Provider {
	t.Helper()
	config := validConfig()
	if endpoint != "" {
		config["endpoint"] = endpoint
	}
	p, err := NewProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return p.(*Provider)
}

func templateTask(targets []string, templateCode string, params interface{}) *core.DeliveryTask {
	return &core.DeliveryTask{
		ID:       "task-1",
		Provider: "aliyunsms",
		Targets:  targets,
		Payload: core.DeliveryPayload{
			Kind: core.PayloadProviderTemplate,
			ProviderTemplate: &core.ProviderTemplatePayload{
				TemplateCode: templateCode,
				Params:       params,
			},
		},
		CreatedAt: time.Now(),
	}
}

func TestNewProviderConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		p, err := NewProvider(validConfig())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.accessKeyID != "AKIDtest" {
			t.Errorf("expected accessKeyID AKIDtest, got %s", provider.accessKeyID)
		}
		if provider.accessKeySecret != "keytest" {
			t.Errorf("expected accessKeySecret keytest, got %s", provider.accessKeySecret)
		}
		if provider.signName != "TestSign" {
			t.Errorf("expected signName TestSign, got %s", provider.signName)
		}
		if provider.region != "cn-hangzhou" {
			t.Errorf("expected default region cn-hangzhou, got %s", provider.region)
		}
		if provider.endpoint != defaultEndpoint {
			t.Errorf("expected default endpoint %s, got %s", defaultEndpoint, provider.endpoint)
		}
		if !provider.enabled {
			t.Error("expected enabled by default")
		}
		if provider.client == nil {
			t.Error("expected non-nil client")
		}
		if provider.status == nil {
			t.Fatal("expected non-nil status")
		}
		if provider.status.Name != "aliyunsms" || provider.status.Type != "builtin" || provider.status.Status != "available" {
			t.Errorf("unexpected status fields: %+v", provider.status)
		}
	})

	t.Run("missing access_key_id", func(t *testing.T) {
		config := validConfig()
		delete(config, "access_key_id")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "access_key_id is required") {
			t.Errorf("expected access_key_id error, got %v", err)
		}
	})

	t.Run("empty access_key_id", func(t *testing.T) {
		config := validConfig()
		config["access_key_id"] = ""
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "access_key_id is required") {
			t.Errorf("expected access_key_id error, got %v", err)
		}
	})

	t.Run("non-string access_key_id", func(t *testing.T) {
		config := validConfig()
		config["access_key_id"] = 123
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "access_key_id is required") {
			t.Errorf("expected access_key_id error, got %v", err)
		}
	})

	t.Run("missing access_key_secret", func(t *testing.T) {
		config := validConfig()
		delete(config, "access_key_secret")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "access_key_secret is required") {
			t.Errorf("expected access_key_secret error, got %v", err)
		}
	})

	t.Run("empty access_key_secret", func(t *testing.T) {
		config := validConfig()
		config["access_key_secret"] = ""
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "access_key_secret is required") {
			t.Errorf("expected access_key_secret error, got %v", err)
		}
	})

	t.Run("non-string access_key_secret", func(t *testing.T) {
		config := validConfig()
		config["access_key_secret"] = true
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "access_key_secret is required") {
			t.Errorf("expected access_key_secret error, got %v", err)
		}
	})

	t.Run("missing sign_name", func(t *testing.T) {
		config := validConfig()
		delete(config, "sign_name")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "sign_name is required") {
			t.Errorf("expected sign_name error, got %v", err)
		}
	})

	t.Run("empty sign_name", func(t *testing.T) {
		config := validConfig()
		config["sign_name"] = ""
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "sign_name is required") {
			t.Errorf("expected sign_name error, got %v", err)
		}
	})

	t.Run("non-string sign_name", func(t *testing.T) {
		config := validConfig()
		config["sign_name"] = 123
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "sign_name is required") {
			t.Errorf("expected sign_name error, got %v", err)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		config := validConfig()
		config["region"] = "cn-beijing"
		config["endpoint"] = "sms.example.com"
		config["enabled"] = false
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.region != "cn-beijing" {
			t.Errorf("expected region cn-beijing, got %s", provider.region)
		}
		if provider.endpoint != "sms.example.com" {
			t.Errorf("expected endpoint sms.example.com, got %s", provider.endpoint)
		}
		if provider.enabled {
			t.Error("expected disabled")
		}
	})

	t.Run("empty region and endpoint fall back to defaults", func(t *testing.T) {
		config := validConfig()
		config["region"] = ""
		config["endpoint"] = ""
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.region != "cn-hangzhou" {
			t.Errorf("expected default region, got %s", provider.region)
		}
		if provider.endpoint != defaultEndpoint {
			t.Errorf("expected default endpoint, got %s", provider.endpoint)
		}
	})

	t.Run("non-string region and endpoint fall back to defaults", func(t *testing.T) {
		config := validConfig()
		config["region"] = 123
		config["endpoint"] = 456
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.region != "cn-hangzhou" {
			t.Errorf("expected default region, got %s", provider.region)
		}
		if provider.endpoint != defaultEndpoint {
			t.Errorf("expected default endpoint, got %s", provider.endpoint)
		}
	})

	t.Run("non-bool enabled falls back to true", func(t *testing.T) {
		config := validConfig()
		config["enabled"] = "yes"
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !p.(*Provider).enabled {
			t.Error("expected enabled true for non-bool enabled value")
		}
	})

	t.Run("nil config", func(t *testing.T) {
		_, err := NewProvider(nil)
		if err == nil || !strings.Contains(err.Error(), "access_key_id is required") {
			t.Errorf("expected access_key_id error, got %v", err)
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := validConfig()
	config["region"] = "cn-beijing"
	config["endpoint"] = "sms.example.com"
	p, err := NewProvider(config)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := p.(*Provider).GetConfig()

	if got["access_key_id"] != "AKIDtest" {
		t.Errorf("expected access_key_id AKIDtest, got %v", got["access_key_id"])
	}
	if got["access_key_secret"] != "keytest" {
		t.Errorf("expected access_key_secret keytest, got %v", got["access_key_secret"])
	}
	if got["sign_name"] != "TestSign" {
		t.Errorf("expected sign_name TestSign, got %v", got["sign_name"])
	}
	if got["region"] != "cn-beijing" {
		t.Errorf("expected region cn-beijing, got %v", got["region"])
	}
	if got["endpoint"] != "sms.example.com" {
		t.Errorf("expected endpoint sms.example.com, got %v", got["endpoint"])
	}
}

func TestProviderMetadata(t *testing.T) {
	p := newTestProvider(t, "")
	if p.Name() != "aliyunsms" {
		t.Errorf("expected name aliyunsms, got %s", p.Name())
	}
	if p.Type() != "aliyunsms" {
		t.Errorf("expected type aliyunsms, got %s", p.Type())
	}
	if err := p.Close(); err != nil {
		t.Errorf("expected no error on close, got %v", err)
	}

	status := p.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "aliyunsms" {
		t.Errorf("expected status name aliyunsms, got %s", status.Name)
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
	if status.Since.IsZero() {
		t.Error("expected non-zero Since")
	}
}

func TestProviderEnableDisable(t *testing.T) {
	p := newTestProvider(t, "")
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

func TestProviderCapability(t *testing.T) {
	p := newTestProvider(t, "")
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

func TestFactory(t *testing.T) {
	factory := &Factory{}
	if factory.Name() != "aliyunsms" {
		t.Errorf("expected factory name aliyunsms, got %s", factory.Name())
	}
	if factory.Type() != "aliyunsms" {
		t.Errorf("expected factory type aliyunsms, got %s", factory.Type())
	}

	p, err := factory.Create(validConfig())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p == nil || p.Name() != "aliyunsms" {
		t.Errorf("expected aliyunsms provider, got %v", p)
	}

	invalid := validConfig()
	delete(invalid, "access_key_id")
	if _, err := factory.Create(invalid); err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestSpecialEncode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"abc-123_456.dots", "abc-123_456.dots"},
		{"a b", "a%20b"},
		{"a+b", "a%2Bb"},
		{"a*b", "a%2Ab"},
		{"a~b", "a~b"},
		{"a&b=1", "a%26b%3D1"},
		{"/", "%2F"},
		{"签名", "%E7%AD%BE%E5%90%8D"},
		{"100%", "100%25"},
	}
	for _, c := range cases {
		if got := specialEncode(c.in); got != c.want {
			t.Errorf("specialEncode(%q) = %q, expected %q", c.in, got, c.want)
		}
	}
}

func TestSpecialEncodeSignatureRules(t *testing.T) {
	// RFC 3986 rules from the Aliyun RPC signature spec: unreserved characters
	// are A-Z a-z 0-9 - _ . ~ ; everything else is percent-encoded uppercase,
	// space must be %20 (not +) and * must be %2A (not left as-is).
	for _, r := range "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.~" {
		in := string(r)
		if got := specialEncode(in); got != in {
			t.Errorf("specialEncode(%q) = %q, expected unreserved passthrough", in, got)
		}
	}
}

func TestSignKnownAnswer(t *testing.T) {
	// Known-answer test built from the Aliyun official RPC signature algorithm
	// (HMAC-SHA1 over the percent-encoded canonicalized query, keyed with
	// "<accessKeySecret>&"), with fixed inputs.
	p := &Provider{accessKeySecret: "testsecret"}

	params := map[string]string{
		"AccessKeyId":      "testid",
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     "13800138000",
		"RegionId":         "cn-hangzhou",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   "45e15109-afc1-4d76-a53a-48c9e2e0fce4",
		"SignatureVersion": "1.0",
		"SignName":         "签名Test",
		"TemplateCode":     "SMS_12345678",
		"TemplateParam":    `{"code":"1234"}`,
		"Timestamp":        "2016-01-01T12:00:00Z",
		"Version":          "2017-05-25",
	}

	const expected = "Lc2QRiE4LPD+xkkay7xIqe7ZIGs="
	if got := p.sign(params, "POST"); got != expected {
		t.Errorf("sign(POST) = %q, expected %q", got, expected)
	}

	// The Signature key must be excluded from the signature computation.
	withSignature := map[string]string{
		"AccessKeyId":      "testid",
		"Action":           "SendSms",
		"Format":           "JSON",
		"PhoneNumbers":     "13800138000",
		"RegionId":         "cn-hangzhou",
		"Signature":        "STALE",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureNonce":   "45e15109-afc1-4d76-a53a-48c9e2e0fce4",
		"SignatureVersion": "1.0",
		"SignName":         "签名Test",
		"TemplateCode":     "SMS_12345678",
		"TemplateParam":    `{"code":"1234"}`,
		"Timestamp":        "2016-01-01T12:00:00Z",
		"Version":          "2017-05-25",
	}
	if got := p.sign(withSignature, "POST"); got != expected {
		t.Errorf("sign with Signature key = %q, expected %q (Signature must be excluded)", got, expected)
	}

	// The HTTP method is part of the string to sign.
	if got := p.sign(params, "GET"); got != "6bkcYWw1MBhqeoQvOGC1guGZgvY=" {
		t.Errorf("sign(GET) = %q, expected %q", got, "6bkcYWw1MBhqeoQvOGC1guGZgvY=")
	}

	// Empty params sign the minimal string to sign "POST&%2F&".
	if got := p.sign(map[string]string{}, "POST"); got != "0TS6mljAaR1otoyy5oJ3S3FnDhw=" {
		t.Errorf("sign(empty) = %q, expected %q", got, "0TS6mljAaR1otoyy5oJ3S3FnDhw=")
	}
}

func TestSignMatchesManualHMAC(t *testing.T) {
	p := &Provider{accessKeySecret: "keytest"}
	params := map[string]string{
		"Action":    "SendSms",
		"Format":    "JSON",
		"a b+c*d~e": "v",
	}

	got := p.sign(params, "POST")

	// Manually construct the string to sign per the Aliyun RPC spec.
	stringToSign := "POST&" + specialEncode("/") + "&" +
		specialEncode("Action=SendSms&Format=JSON&a%20b%2Bc%2Ad~e=v")

	h := hmac.New(sha1.New, []byte("keytest&"))
	h.Write([]byte(stringToSign))
	expected := base64.StdEncoding.EncodeToString(h.Sum(nil))

	if got != expected {
		t.Errorf("sign = %q, expected %q", got, expected)
	}
}

func TestDeliverValidation(t *testing.T) {
	p := newTestProvider(t, "")

	err := p.Deliver(context.Background(), templateTask(nil, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "phone numbers are required") {
		t.Errorf("expected phone numbers error, got %v", err)
	}
	if httpclient.IsRetryable(err) {
		t.Error("validation errors should not be retryable")
	}

	err = p.Deliver(context.Background(), templateTask([]string{}, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "phone numbers are required") {
		t.Errorf("expected phone numbers error for empty targets, got %v", err)
	}
}

func parseStubForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Errorf("failed to parse form: %v", err)
	}
	return r.PostForm
}

func TestDeliverSuccess(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Host != stubEndpoint() {
			t.Errorf("expected host %s, got %s", stubEndpoint(), r.Host)
		}
		if r.URL.Path != "/" {
			t.Errorf("expected path /, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("expected form content type, got %s", got)
		}

		form := parseStubForm(t, r)
		if form.Get("Action") != "SendSms" {
			t.Errorf("expected Action SendSms, got %s", form.Get("Action"))
		}
		if form.Get("Version") != "2017-05-25" {
			t.Errorf("expected Version 2017-05-25, got %s", form.Get("Version"))
		}
		if form.Get("Format") != "JSON" {
			t.Errorf("expected Format JSON, got %s", form.Get("Format"))
		}
		if form.Get("RegionId") != "cn-hangzhou" {
			t.Errorf("expected RegionId cn-hangzhou, got %s", form.Get("RegionId"))
		}
		if form.Get("AccessKeyId") != "AKIDtest" {
			t.Errorf("expected AccessKeyId AKIDtest, got %s", form.Get("AccessKeyId"))
		}
		if form.Get("SignName") != "TestSign" {
			t.Errorf("expected SignName TestSign, got %s", form.Get("SignName"))
		}
		if form.Get("PhoneNumbers") != "13800138000,13900139000" {
			t.Errorf("expected joined phone numbers, got %s", form.Get("PhoneNumbers"))
		}
		if form.Get("TemplateCode") != "SMS_12345678" {
			t.Errorf("expected TemplateCode SMS_12345678, got %s", form.Get("TemplateCode"))
		}
		if form.Get("TemplateParam") != `{"code":"9527"}` {
			t.Errorf("expected TemplateParam {\"code\":\"9527\"}, got %s", form.Get("TemplateParam"))
		}
		if form.Get("SignatureMethod") != "HMAC-SHA1" || form.Get("SignatureVersion") != "1.0" {
			t.Errorf("expected signature method params, got %s / %s", form.Get("SignatureMethod"), form.Get("SignatureVersion"))
		}
		if form.Get("SignatureNonce") == "" {
			t.Error("expected non-empty SignatureNonce")
		}
		if _, err := time.Parse("2006-01-02T15:04:05Z", form.Get("Timestamp")); err != nil {
			t.Errorf("expected UTC ISO timestamp, got %s (%v)", form.Get("Timestamp"), err)
		}

		// The submitted signature must match a fresh signature over the
		// submitted params (minus the signature itself).
		received := make(map[string]string, len(form))
		for k := range form {
			received[k] = form.Get(k)
		}
		signature := received["Signature"]
		delete(received, "Signature")
		if expected := p.sign(received, "POST"); signature != expected {
			t.Errorf("signature mismatch: got %s, expected %s", signature, expected)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, okResponse)
	})

	task := templateTask([]string{"13800138000", "13900139000"}, "SMS_12345678", map[string]string{"code": "9527"})
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverDefaultTemplateCode(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	var calls int
	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		form := parseStubForm(t, r)
		if form.Get("TemplateCode") != "SMS_DEFAULT" {
			t.Errorf("expected default TemplateCode SMS_DEFAULT, got %s", form.Get("TemplateCode"))
		}
		if _, ok := form["TemplateParam"]; ok {
			t.Errorf("expected no TemplateParam, got %s", form.Get("TemplateParam"))
		}
		fmt.Fprint(w, okResponse)
	})

	// Provider template present but empty code.
	if err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "", nil)); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	// No provider template at all.
	task := &core.DeliveryTask{
		ID:       "task-2",
		Provider: "aliyunsms",
		Targets:  []string{"13800138000"},
		Payload:  core.DeliveryPayload{Kind: core.PayloadProviderTemplate},
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 deliver calls, got %d", calls)
	}
}

func TestDeliverTemplateParamVariants(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	var calls int
	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		form := parseStubForm(t, r)
		var got, has string
		if v, ok := form["TemplateParam"]; ok {
			got, has = v[0], "yes"
		}
		switch calls {
		case 1: // map[string]string params
			if has == "" || got != `{"code":"9527"}` {
				t.Errorf("case 1: expected TemplateParam {\"code\":\"9527\"}, got %q (present=%v)", got, has != "")
			}
		case 2: // map[string]interface{} params with non-string values
			if has == "" || got != `{"code":1234,"name":"herald"}` {
				t.Errorf("case 2: expected TemplateParam {\"code\":1234,\"name\":\"herald\"}, got %q", got)
			}
		case 3: // unsupported params type
			if has != "" {
				t.Errorf("case 3: expected no TemplateParam, got %q", got)
			}
		case 4: // params that fail to marshal
			if has != "" {
				t.Errorf("case 4: expected no TemplateParam for unmarshalable params, got %q", got)
			}
		}
		fmt.Fprint(w, okResponse)
	})

	if err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", map[string]string{"code": "9527"})); err != nil {
		t.Errorf("case 1: expected no error, got %v", err)
	}
	if err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", map[string]interface{}{"code": 1234, "name": "herald"})); err != nil {
		t.Errorf("case 2: expected no error, got %v", err)
	}
	if err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", []string{"unsupported"})); err != nil {
		t.Errorf("case 3: expected no error, got %v", err)
	}
	if err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", map[string]interface{}{"bad": make(chan int)})); err != nil {
		t.Errorf("case 4: expected no error, got %v", err)
	}
	if calls != 4 {
		t.Errorf("expected 4 deliver calls, got %d", calls)
	}
}

func TestDeliverAPIError(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"Triggered the frequency limit","RequestId":"req-1"}`)
	})

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "isv.BUSINESS_LIMIT_CONTROL") || !strings.Contains(err.Error(), "Triggered the frequency limit") {
		t.Errorf("expected aliyun SMS error, got %v", err)
	}
	if httpclient.IsRetryable(err) {
		t.Error("aliyun business errors should not be wrapped as retryable")
	}
}

func TestDeliverEmptyCodeError(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{}`)
	})

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "aliyun SMS error") {
		t.Errorf("expected aliyun SMS error for missing Code, got %v", err)
	}
}

func TestDeliverInvalidJSON(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not-json")
	})

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestDeliverHTTPErrorStatus(t *testing.T) {
	p := newTestProvider(t, stubEndpoint())

	setStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"Code":"InternalError","Message":"boom"}`)
	})

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "unexpected status code: 500") {
		t.Errorf("expected unexpected status code error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected retryable error for HTTP 500")
	}
}

func TestDeliverNetworkError(t *testing.T) {
	// Point at a closed local port: connection refused, no real outbound call.
	p := newTestProvider(t, "127.0.0.1:1")

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "SMS_12345678", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("expected send request error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected retryable error for network failure")
	}
}
