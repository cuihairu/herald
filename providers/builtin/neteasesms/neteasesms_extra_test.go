package neteasesms

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

func sha1Hex(data string) string {
	sum := sha1.Sum([]byte(data))
	return hex.EncodeToString(sum[:])
}

// newProviderAtEndpoint builds a provider dialing a specific endpoint (used for
// closed-port network error tests).
func newProviderAtEndpoint(t *testing.T, endpoint string) *Provider {
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

// closedEndpoint returns the host:port of a recently closed local TLS server,
// giving instant connection refusal without any real outbound traffic.
func closedEndpoint(t *testing.T) string {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint := strings.TrimPrefix(srv.URL, "https://")
	srv.Close()
	return endpoint
}

func parseQueryBody(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("failed to decode body: %v", err)
		return nil
	}
	return body
}

func TestDeliverValidation(t *testing.T) {
	p := newTestProvider(t)

	t.Run("no targets", func(t *testing.T) {
		err := p.Deliver(context.Background(), templateTask(nil, "10001", "", nil))
		if err == nil || !strings.Contains(err.Error(), "phone numbers are required") {
			t.Errorf("expected phone numbers error, got %v", err)
		}
		if httpclient.IsRetryable(err) {
			t.Error("validation errors should not be retryable")
		}
	})

	t.Run("whitespace-only targets", func(t *testing.T) {
		err := p.Deliver(context.Background(), templateTask([]string{"", "   ", "\t"}, "10001", "", nil))
		if err == nil || !strings.Contains(err.Error(), "no valid phone numbers") {
			t.Errorf("expected no valid phone numbers error, got %v", err)
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

	t.Run("empty template id and code", func(t *testing.T) {
		err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "", "", nil))
		if err == nil || !strings.Contains(err.Error(), "template_id is required") {
			t.Errorf("expected template_id error, got %v", err)
		}
	})
}

func TestDeliverSuccess(t *testing.T) {
	setHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/sms/sendTemplateSms" {
			t.Errorf("expected path /v1/sms/sendTemplateSms, got %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		q := r.URL.Query()
		if q.Get("appKey") != "fake_app_key_for_test" {
			t.Errorf("expected appKey fake_app_key_for_test, got %s", q.Get("appKey"))
		}
		nonce := q.Get("nonce")
		curTime := q.Get("curTime")
		if nonce == "" || curTime == "" {
			t.Error("expected nonce and curTime query params")
			return
		}
		if _, err := strconv.ParseInt(nonce, 10, 64); err != nil {
			t.Errorf("expected numeric nonce, got %s", nonce)
		}
		ts, err := strconv.ParseInt(curTime, 10, 64)
		if err != nil {
			t.Errorf("expected numeric curTime, got %s", curTime)
		} else if delta := time.Now().Unix() - ts; delta < -5 || delta > 5 {
			t.Errorf("expected curTime near now, got %d", ts)
		}
		// checksum = sha1(appSecret + nonce + curTime), verified end-to-end
		// against the values actually sent on the wire.
		if q.Get("checksum") != sha1Hex("fake_secret_unit_test"+nonce+curTime) {
			t.Errorf("unexpected checksum %s for nonce=%s curTime=%s", q.Get("checksum"), nonce, curTime)
		}

		body := parseQueryBody(t, r)
		if body == nil {
			return
		}
		if body["templateid"] != "10001" {
			t.Errorf("expected templateid 10001, got %v", body["templateid"])
		}
		if body["mobiles"] != "13800138000,13800138001" {
			t.Errorf("expected joined mobiles, got %v", body["mobiles"])
		}
		if body["params"] != "1,2" {
			t.Errorf("expected params \"1,2\", got %v", body["params"])
		}
		if len(body) != 3 {
			t.Errorf("expected 3 body entries, got %d: %v", len(body), body)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, okResponse())
	})

	p := newTestProvider(t)

	task := templateTask([]string{"13800138000", " 13800138001 ", "", "   "}, "10001", "", []string{"1", "2"})
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverTemplateCodeFallback(t *testing.T) {
	setHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body := parseQueryBody(t, r)
		if body == nil {
			return
		}
		if body["templateid"] != "code-from-other-vendor" {
			t.Errorf("expected templateid from TemplateCode fallback, got %v", body["templateid"])
		}
		if _, ok := body["params"]; ok {
			t.Errorf("expected no params entry, got %v", body["params"])
		}
		fmt.Fprint(w, okResponse())
	})

	p := newTestProvider(t)

	task := templateTask([]string{"13800138000"}, "", "code-from-other-vendor", nil)
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverTemplateParamVariants(t *testing.T) {
	setHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body := parseQueryBody(t, r)
		if body == nil {
			return
		}
		if body["params"] != "a,b" {
			t.Errorf("expected only string params joined \"a,b\", got %v", body["params"])
		}
		fmt.Fprint(w, okResponse())
	})

	p := newTestProvider(t)

	task := templateTask([]string{"13800138000"}, "10001", "", []interface{}{"a", 42, "b", true, nil})
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverParamsUnsupportedType(t *testing.T) {
	setHandler(t, func(w http.ResponseWriter, r *http.Request) {
		body := parseQueryBody(t, r)
		if body == nil {
			return
		}
		if _, ok := body["params"]; ok {
			t.Errorf("expected no params entry for unsupported param type, got %v", body["params"])
		}
		fmt.Fprint(w, okResponse())
	})

	p := newTestProvider(t)

	task := templateTask([]string{"13800138000"}, "10001", "", map[string]string{"k": "v"})
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverBusinessError(t *testing.T) {
	setHandler(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":416,"msg":"params invalid"}`)
	})

	p := newTestProvider(t)

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", "", nil))
	if err == nil || !strings.Contains(err.Error(), "neteasesms error: 416 - params invalid") {
		t.Errorf("expected business error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected Deliver to wrap business errors as retryable")
	}
}

func TestDeliverInvalidJSON(t *testing.T) {
	setHandler(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not-json")
	})

	p := newTestProvider(t)

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", "", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("expected parse error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestDeliverNetworkError(t *testing.T) {
	p := newProviderAtEndpoint(t, closedEndpoint(t))

	err := p.Deliver(context.Background(), templateTask([]string{"13800138000"}, "10001", "", nil))
	if err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("expected send request error, got %v", err)
	}
	if !httpclient.IsRetryable(err) {
		t.Error("expected retryable error")
	}
}

func TestSendCode(t *testing.T) {
	t.Run("with auth code and device id", func(t *testing.T) {
		setHandler(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/sms/sendCode" {
				t.Errorf("expected path /v1/sms/sendCode, got %s", r.URL.Path)
			}
			body := parseQueryBody(t, r)
			if body == nil {
				return
			}
			if body["mobile"] != "13800138000" {
				t.Errorf("expected mobile 13800138000, got %v", body["mobile"])
			}
			if body["authCode"] != "1234" {
				t.Errorf("expected authCode 1234, got %v", body["authCode"])
			}
			if body["deviceId"] != "device-1" {
				t.Errorf("expected deviceId device-1, got %v", body["deviceId"])
			}
			fmt.Fprint(w, okResponse())
		})

		p := newTestProvider(t)
		if err := p.SendCode(context.Background(), "13800138000", "1234", "device-1"); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("without optional fields", func(t *testing.T) {
		setHandler(t, func(w http.ResponseWriter, r *http.Request) {
			body := parseQueryBody(t, r)
			if body == nil {
				return
			}
			if len(body) != 1 || body["mobile"] != "13800138000" {
				t.Errorf("expected only mobile in body, got %v", body)
			}
			fmt.Fprint(w, okResponse())
		})

		p := newTestProvider(t)
		if err := p.SendCode(context.Background(), "13800138000", "", ""); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("business error is retryable", func(t *testing.T) {
		setHandler(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"code":301,"msg":"frequency limit"}`)
		})

		p := newTestProvider(t)
		err := p.SendCode(context.Background(), "13800138000", "", "")
		if err == nil || !strings.Contains(err.Error(), "neteasesms error: 301 - frequency limit") {
			t.Errorf("expected business error, got %v", err)
		}
		if !httpclient.IsRetryable(err) {
			t.Error("expected SendCode to wrap business errors as retryable")
		}
	})

	t.Run("network error is retryable", func(t *testing.T) {
		p := newProviderAtEndpoint(t, closedEndpoint(t))
		err := p.SendCode(context.Background(), "13800138000", "", "")
		if err == nil || !strings.Contains(err.Error(), "failed to send request") {
			t.Errorf("expected send request error, got %v", err)
		}
		if !httpclient.IsRetryable(err) {
			t.Error("expected retryable error")
		}
	})
}

func TestVerifyCode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		setHandler(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/sms/verifyCode" {
				t.Errorf("expected path /v1/sms/verifyCode, got %s", r.URL.Path)
			}
			body := parseQueryBody(t, r)
			if body == nil {
				return
			}
			if body["mobile"] != "13800138000" || body["code"] != "5678" {
				t.Errorf("expected mobile and code in body, got %v", body)
			}
			if len(body) != 2 {
				t.Errorf("expected 2 body entries, got %d: %v", len(body), body)
			}
			fmt.Fprint(w, okResponse())
		})

		p := newTestProvider(t)
		if err := p.VerifyCode(context.Background(), "13800138000", "5678"); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("business error is not retryable", func(t *testing.T) {
		setHandler(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"code":415,"msg":"code mismatch"}`)
		})

		p := newTestProvider(t)
		err := p.VerifyCode(context.Background(), "13800138000", "5678")
		if err == nil || !strings.Contains(err.Error(), "neteasesms error: 415 - code mismatch") {
			t.Errorf("expected business error, got %v", err)
		}
		if httpclient.IsRetryable(err) {
			t.Error("expected VerifyCode errors to not be retryable")
		}
	})

	t.Run("network error", func(t *testing.T) {
		p := newProviderAtEndpoint(t, closedEndpoint(t))
		err := p.VerifyCode(context.Background(), "13800138000", "5678")
		if err == nil || !strings.Contains(err.Error(), "failed to send request") {
			t.Errorf("expected send request error, got %v", err)
		}
	})
}

// TestSendRequestQueryEncoding pins the exact wire format of the auth query:
// alphabetical url.Values encoding over a single https URL built from the
// endpoint, default API version and action.
func TestSendRequestQueryEncoding(t *testing.T) {
	setHandler(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if len(q) != 4 {
			t.Errorf("expected exactly 4 query params, got %d: %v", len(q), q)
		}
		for _, key := range []string{"appKey", "nonce", "curTime", "checksum"} {
			if q.Get(key) == "" {
				t.Errorf("expected non-empty %s param", key)
			}
		}
		// url.Values.Encode sorts keys alphabetically.
		want := url.Values{"appKey": {q.Get("appKey")}, "nonce": {q.Get("nonce")}, "curTime": {q.Get("curTime")}, "checksum": {q.Get("checksum")}}.Encode()
		if got := r.URL.RawQuery; got != want {
			t.Errorf("expected sorted raw query %q, got %q", want, got)
		}
		fmt.Fprint(w, okResponse())
	})

	p := newTestProvider(t)

	err := p.sendRequest(context.Background(), "sendTemplateSms", map[string]interface{}{"mobiles": "1"})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
