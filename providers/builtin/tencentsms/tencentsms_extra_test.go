package tencentsms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

func validConfig() map[string]interface{} {
	return map[string]interface{}{
		"secret_id":  "AKIDtest",
		"secret_key": "keytest",
		"app_id":     "1400000000",
		"sign_name":  "TestSign",
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

func withTransport(t *testing.T, rt http.RoundTripper) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = rt
	t.Cleanup(func() { http.DefaultTransport = old })
}

func insecureTransport() http.RoundTripper {
	return &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) { return 0, errors.New("boom") }

func templateTask(targets []string, templateID string, params interface{}) *core.DeliveryTask {
	return &core.DeliveryTask{
		ID:       "task-1",
		Provider: "tencentsms",
		Targets:  targets,
		Payload: core.DeliveryPayload{
			Kind: core.PayloadProviderTemplate,
			ProviderTemplate: &core.ProviderTemplatePayload{
				TemplateID: templateID,
				Params:     params,
			},
		},
		CreatedAt: time.Now(),
	}
}

func okResponse() string {
	return `{"Response":{"SendStatusSet":[{"SerialNo":"1","PhoneNumber":"+8613800138000","Fee":1,"Code":"Ok","Message":"send success"}],"RequestId":"req-1"}}`
}

func TestNewProviderConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		p, err := NewProvider(validConfig())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.region != "ap-guangzhou" {
			t.Errorf("expected default region ap-guangzhou, got %s", provider.region)
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
	})

	t.Run("missing secret_id", func(t *testing.T) {
		config := validConfig()
		delete(config, "secret_id")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "secret_id is required") {
			t.Errorf("expected secret_id error, got %v", err)
		}
	})

	t.Run("empty secret_id", func(t *testing.T) {
		config := validConfig()
		config["secret_id"] = ""
		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty secret_id")
		}
	})

	t.Run("non-string secret_id", func(t *testing.T) {
		config := validConfig()
		config["secret_id"] = 123
		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for non-string secret_id")
		}
	})

	t.Run("missing secret_key", func(t *testing.T) {
		config := validConfig()
		delete(config, "secret_key")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "secret_key is required") {
			t.Errorf("expected secret_key error, got %v", err)
		}
	})

	t.Run("missing app_id", func(t *testing.T) {
		config := validConfig()
		delete(config, "app_id")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "app_id is required") {
			t.Errorf("expected app_id error, got %v", err)
		}
	})

	t.Run("custom values", func(t *testing.T) {
		config := validConfig()
		config["region"] = "ap-shanghai"
		config["endpoint"] = "sms.example.com"
		config["enabled"] = false
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.region != "ap-shanghai" {
			t.Errorf("expected region ap-shanghai, got %s", provider.region)
		}
		if provider.endpoint != "sms.example.com" {
			t.Errorf("expected endpoint sms.example.com, got %s", provider.endpoint)
		}
		if provider.enabled {
			t.Error("expected disabled")
		}
	})

	t.Run("non-string sign_name", func(t *testing.T) {
		config := validConfig()
		config["sign_name"] = 123
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.(*Provider).signName != "" {
			t.Errorf("expected empty sign name, got %s", p.(*Provider).signName)
		}
	})

	t.Run("non-string region and endpoint", func(t *testing.T) {
		config := validConfig()
		config["region"] = 123
		config["endpoint"] = 456
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider := p.(*Provider)
		if provider.region != "ap-guangzhou" {
			t.Errorf("expected default region, got %s", provider.region)
		}
		if provider.endpoint != defaultEndpoint {
			t.Errorf("expected default endpoint, got %s", provider.endpoint)
		}
	})
}

func TestProviderGetConfigExtra(t *testing.T) {
	config := validConfig()
	config["region"] = "ap-beijing"
	config["endpoint"] = "sms.example.com"
	p, _ := NewProvider(config)
	got := p.(*Provider).GetConfig()

	if got["secret_id"] != "AKIDtest" {
		t.Errorf("expected secret_id AKIDtest, got %v", got["secret_id"])
	}
	if got["secret_key"] != "keytest" {
		t.Errorf("expected secret_key keytest, got %v", got["secret_key"])
	}
	if got["app_id"] != "1400000000" {
		t.Errorf("expected app_id 1400000000, got %v", got["app_id"])
	}
	if got["sign_name"] != "TestSign" {
		t.Errorf("expected sign_name TestSign, got %v", got["sign_name"])
	}
	if got["region"] != "ap-beijing" {
		t.Errorf("expected region ap-beijing, got %v", got["region"])
	}
	if got["endpoint"] != "sms.example.com" {
		t.Errorf("expected endpoint sms.example.com, got %v", got["endpoint"])
	}
}

func TestProviderCapabilityExtra(t *testing.T) {
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

func TestProviderMetadata(t *testing.T) {
	p := newTestProvider(t, "")
	if p.Name() != "tencentsms" {
		t.Errorf("expected name tencentsms, got %s", p.Name())
	}
	if p.Type() != "tencentsms" {
		t.Errorf("expected type tencentsms, got %s", p.Type())
	}
	if err := p.Close(); err != nil {
		t.Errorf("expected no error on close, got %v", err)
	}

	status := p.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "tencentsms" {
		t.Errorf("expected status name tencentsms, got %s", status.Name)
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

func TestFactoryExtra(t *testing.T) {
	factory := &Factory{}
	if factory.Name() != "tencentsms" {
		t.Errorf("expected factory name tencentsms, got %s", factory.Name())
	}
	if factory.Type() != "tencentsms" {
		t.Errorf("expected factory type tencentsms, got %s", factory.Type())
	}

	p, err := factory.Create(validConfig())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p == nil || p.Name() != "tencentsms" {
		t.Errorf("expected tencentsms provider, got %v", p)
	}

	invalid := validConfig()
	delete(invalid, "secret_id")
	if _, err := factory.Create(invalid); err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestDeliverValidation(t *testing.T) {
	p := newTestProvider(t, "")

	t.Run("no targets", func(t *testing.T) {
		err := p.Deliver(context.Background(), templateTask(nil, "10001", nil))
		if err == nil || !strings.Contains(err.Error(), "phone numbers are required") {
			t.Errorf("expected phone numbers error, got %v", err)
		}
		if httpclient.IsRetryable(err) {
			t.Error("validation errors should not be retryable")
		}
	})

	t.Run("nil provider template", func(t *testing.T) {
		task := &core.DeliveryTask{
			Targets: []string{"13800138000"},
			Payload: core.DeliveryPayload{Kind: core.PayloadProviderTemplate},
		}
		err := p.Deliver(context.Background(), task)
		if err == nil || !strings.Contains(err.Error(), "template_id is required") {
			t.Errorf("expected template_id error, got %v", err)
		}
	})

	t.Run("empty template id", func(t *testing.T) {
		err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "", nil))
		if err == nil || !strings.Contains(err.Error(), "template_id is required") {
			t.Errorf("expected template_id error, got %v", err)
		}
	})
}

func TestDeliverSuccess(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Host != strings.TrimPrefix(server.URL, "https://") {
			t.Errorf("expected host %s, got %s", strings.TrimPrefix(server.URL, "https://"), r.Host)
		}

		q := r.URL.Query()
		if q.Get("Action") != "SendSms" {
			t.Errorf("expected Action SendSms, got %s", q.Get("Action"))
		}
		if q.Get("Version") != "2021-01-11" {
			t.Errorf("expected Version 2021-01-11, got %s", q.Get("Version"))
		}
		if q.Get("Region") != "ap-guangzhou" {
			t.Errorf("expected Region ap-guangzhou, got %s", q.Get("Region"))
		}
		if q.Get("SecretId") != "AKIDtest" {
			t.Errorf("expected SecretId AKIDtest, got %s", q.Get("SecretId"))
		}
		if q.Get("Timestamp") == "" || q.Get("Nonce") == "" {
			t.Error("expected Timestamp and Nonce query params")
		}

		if r.Header.Get("X-TC-Action") != "SendSms" {
			t.Errorf("expected X-TC-Action SendSms, got %s", r.Header.Get("X-TC-Action"))
		}
		if r.Header.Get("X-TC-Version") != "2021-01-11" {
			t.Errorf("expected X-TC-Version 2021-01-11, got %s", r.Header.Get("X-TC-Version"))
		}
		if r.Header.Get("X-TC-Region") != "ap-guangzhou" {
			t.Errorf("expected X-TC-Region ap-guangzhou, got %s", r.Header.Get("X-TC-Region"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "TC3-HMAC-SHA256 Credential=AKIDtest/") {
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}

		var body SendSmsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
			return
		}
		if body.SmsSdkAppId != "1400000000" {
			t.Errorf("expected SmsSdkAppId 1400000000, got %s", body.SmsSdkAppId)
		}
		if body.SignName != "TestSign" {
			t.Errorf("expected SignName TestSign, got %s", body.SignName)
		}
		if body.TemplateID != "10001" {
			t.Errorf("expected TemplateId 10001, got %s", body.TemplateID)
		}
		expected := []string{"+8613800138000", "+8613900139000", "+8613800138001"}
		if len(body.PhoneNumberSet) != len(expected) {
			t.Errorf("expected %d phone numbers, got %v", len(expected), body.PhoneNumberSet)
		} else {
			for i, num := range expected {
				if body.PhoneNumberSet[i] != num {
					t.Errorf("expected phone[%d]=%s, got %s", i, num, body.PhoneNumberSet[i])
				}
			}
		}
		if len(body.TemplateParamSet) != 2 || body.TemplateParamSet[0] != "1" || body.TemplateParamSet[1] != "2" {
			t.Errorf("expected template params [1 2], got %v", body.TemplateParamSet)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, okResponse())
	}))
	defer server.Close()

	withTransport(t, insecureTransport())
	p := newTestProvider(t, strings.TrimPrefix(server.URL, "https://"))

	task := templateTask([]string{"13800138000", "+8613900139000", " 13800138001 "}, "10001", []string{"1", "2"})
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverTemplateParamVariants(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body SendSmsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
			return
		}
		if len(body.TemplateParamSet) != 2 || body.TemplateParamSet[0] != "a" || body.TemplateParamSet[1] != "b" {
			t.Errorf("expected template params [a b], got %v", body.TemplateParamSet)
		}
		fmt.Fprint(w, okResponse())
	}))
	defer server.Close()

	withTransport(t, insecureTransport())
	p := newTestProvider(t, strings.TrimPrefix(server.URL, "https://"))

	task := templateTask([]string{"13800138000"}, "10001", []interface{}{"a", 42, "b"})
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverAPIError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Response":{},"Error":{"Code":"AuthFailure.SignatureFailure","Message":"invalid signature"}}`)
	}))
	defer server.Close()

	withTransport(t, insecureTransport())
	p := newTestProvider(t, strings.TrimPrefix(server.URL, "https://"))

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", nil))
	if err == nil || !strings.Contains(err.Error(), "AuthFailure.SignatureFailure") {
		t.Errorf("expected API error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestDeliverSendStatusError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Response":{"SendStatusSet":[{"Code":"FailedOperation","PhoneNumber":"+8613800138000","Message":"carrier rejected"}],"RequestId":"req-2"}}`)
	}))
	defer server.Close()

	withTransport(t, insecureTransport())
	p := newTestProvider(t, strings.TrimPrefix(server.URL, "https://"))

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", nil))
	if err == nil || !strings.Contains(err.Error(), "FailedOperation") || !strings.Contains(err.Error(), "carrier rejected") {
		t.Errorf("expected send status error, got %v", err)
	}
}

func TestDeliverInvalidJSON(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not-json")
	}))
	defer server.Close()

	withTransport(t, insecureTransport())
	p := newTestProvider(t, strings.TrimPrefix(server.URL, "https://"))

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestDeliverNetworkError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, okResponse())
	}))
	defer server.Close()

	p := newTestProvider(t, strings.TrimPrefix(server.URL, "https://"))

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("expected send error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestDeliverCreateRequestError(t *testing.T) {
	p := newTestProvider(t, "invalid host")

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to create request") {
		t.Errorf("expected create request error, got %v", err)
	}
}

func TestDeliverReadBodyError(t *testing.T) {
	withTransport(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(errorReader{}),
			Header:     http.Header{},
			Request:    req,
		}, nil
	}))
	p := newTestProvider(t, "sms.tencentcloudapi.com")

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to read response") {
		t.Errorf("expected read response error, got %v", err)
	}
}

func TestSha256Hex(t *testing.T) {
	sum := sha256.Sum256([]byte("hello"))
	if sha256Hex("hello") != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256Hex mismatch for hello: %s", sha256Hex("hello"))
	}
	emptySum := sha256.Sum256([]byte(""))
	if sha256Hex("") != hex.EncodeToString(emptySum[:]) {
		t.Errorf("sha256Hex mismatch for empty string: %s", sha256Hex(""))
	}
}

func TestHmacBytes(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write([]byte("data"))
	expected := mac.Sum(nil)

	got := hmacBytes([]byte("key"), "data")
	if !hmac.Equal(got, expected) {
		t.Errorf("hmac mismatch: got %x, expected %x", got, expected)
	}
}
