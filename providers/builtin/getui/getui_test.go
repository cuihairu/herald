package getui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

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

// baseConfig is the minimum configuration every constructor test extends.
func baseConfig() map[string]interface{} {
	return map[string]interface{}{
		"app_id":        "aaaaa",
		"app_key":       "appkey-123",
		"master_secret": "secret-456",
	}
}

// newProvider builds a provider with a fresh clock-independent config.
func newProvider(t *testing.T, extra map[string]interface{}) *Provider {
	t.Helper()
	cfg := baseConfig()
	for k, v := range extra {
		cfg[k] = v
	}
	p, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p.(*Provider)
}

// getuiServer stands in for the whole Getui host: /auth hands out tokens,
// every other path goes to push (nil for auth-only tests). One server is
// used because the provider derives both URLs from its single endpoint.
type getuiServer struct {
	*httptest.Server
	authCalls int32
	authSeen  authRequest
}

// newGetuiServer starts the stub; push may be nil when only /auth is hit.
func newGetuiServer(t *testing.T, push http.Handler) *getuiServer {
	t.Helper()
	s := &getuiServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, authPath) {
			atomic.AddInt32(&s.authCalls, 1)
			body, _ := readBody(r)
			_ = json.Unmarshal(body, &s.authSeen)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w,
				`{"code":0,"msg":"ok","data":{"token":%q,"expire_time":"%d"}}`,
				fmt.Sprintf("token-%d", atomic.LoadInt32(&s.authCalls)),
				time.Now().Add(24*time.Hour).UnixMilli())
			return
		}
		if push == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		push.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// authCallsNow returns the number of /auth requests served.
func (s *getuiServer) authCallsNow() int32 {
	return atomic.LoadInt32(&s.authCalls)
}

func TestNewProviderValidation(t *testing.T) {
	t.Run("missing app_id", func(t *testing.T) {
		cfg := baseConfig()
		cfg["app_id"] = ""
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "getui: app_id is required" {
			t.Errorf("NewProvider = %v, want app_id required", err)
		}
	})

	t.Run("missing app_key", func(t *testing.T) {
		cfg := baseConfig()
		delete(cfg, "app_key")
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "getui: app_key is required" {
			t.Errorf("NewProvider = %v, want app_key required", err)
		}
	})

	t.Run("missing master_secret", func(t *testing.T) {
		cfg := baseConfig()
		cfg["master_secret"] = ""
		_, err := NewProvider(cfg)
		if err == nil || err.Error() != "getui: master_secret is required" {
			t.Errorf("NewProvider = %v, want master_secret required", err)
		}
	})

	t.Run("ttl below range", func(t *testing.T) {
		_, err := NewProvider(withTTL(baseConfig(), -2))
		if err == nil || !strings.Contains(err.Error(), "ttl must be between") {
			t.Errorf("NewProvider = %v, want ttl range error", err)
		}
	})

	t.Run("ttl above range", func(t *testing.T) {
		_, err := NewProvider(withTTL(baseConfig(), ttlMax+1))
		if err == nil || !strings.Contains(err.Error(), "ttl must be between") {
			t.Errorf("NewProvider = %v, want ttl range error", err)
		}
	})

	t.Run("ttl inside range", func(t *testing.T) {
		if _, err := NewProvider(withTTL(baseConfig(), -1)); err != nil {
			t.Errorf("NewProvider with ttl -1: %v", err)
		}
		if _, err := NewProvider(withTTL(baseConfig(), ttlMax)); err != nil {
			t.Errorf("NewProvider with ttl max: %v", err)
		}
	})

	t.Run("unsupported click_type", func(t *testing.T) {
		cfg := baseConfig()
		cfg["click_type"] = "url"
		_, err := NewProvider(cfg)
		if err == nil || !strings.Contains(err.Error(), "unsupported click_type") {
			t.Errorf("NewProvider = %v, want click_type error", err)
		}
	})

	t.Run("click_type defaults to none", func(t *testing.T) {
		if got := newProvider(t, nil).clickType; got != "none" {
			t.Errorf("clickType = %q, want none", got)
		}
	})

	t.Run("endpoint defaults and trims", func(t *testing.T) {
		p := newProvider(t, map[string]interface{}{"endpoint": "https://proxy.example.com/v2/"})
		if p.endpoint != "https://proxy.example.com/v2" {
			t.Errorf("endpoint = %q, want trimmed", p.endpoint)
		}
		if got := newProvider(t, nil).endpoint; got != defaultEndpoint {
			t.Errorf("default endpoint = %q", got)
		}
		if got := p.baseURL(); got != "https://proxy.example.com/v2/aaaaa" {
			t.Errorf("baseURL = %q, want appId appended", got)
		}
	})
}

// withTTL copies cfg with a ttl entry of the given type.
func withTTL(cfg map[string]interface{}, ttl any) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range cfg {
		out[k] = v
	}
	out["ttl"] = ttl
	return out
}

func TestParseConfigTTLTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want int64
	}{
		{"int", 600000, 600000},
		{"int64", int64(600000), 600000},
		{"float64", float64(600000), 600000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseConfig(withTTL(baseConfig(), tc.in))
			if cfg.TTL == nil || *cfg.TTL != tc.want {
				t.Errorf("TTL = %v, want %d", cfg.TTL, tc.want)
			}
		})
	}

	t.Run("absent", func(t *testing.T) {
		cfg := parseConfig(baseConfig())
		if cfg.TTL != nil {
			t.Errorf("TTL = %v, want nil", *cfg.TTL)
		}
	})

	t.Run("wrong type ignored", func(t *testing.T) {
		cfg := parseConfig(withTTL(baseConfig(), "600000"))
		if cfg.TTL != nil {
			t.Errorf("TTL = %v, want nil for string input", *cfg.TTL)
		}
	})

	t.Run("strings and click_type", func(t *testing.T) {
		cfg := parseConfig(map[string]interface{}{
			"app_id":        "id",
			"app_key":       "key",
			"master_secret": "secret",
			"endpoint":      "https://proxy.example.com/v2",
			"click_type":    "startapp",
		})
		if cfg.AppID != "id" || cfg.AppKey != "key" || cfg.MasterSecret != "secret" {
			t.Errorf("identity = %+v", cfg)
		}
		if cfg.Endpoint != "https://proxy.example.com/v2" || cfg.ClickType != "startapp" {
			t.Errorf("endpoint/click_type = %q/%q", cfg.Endpoint, cfg.ClickType)
		}
	})
}

func TestBuildSign(t *testing.T) {
	got := buildSign("appkey-123", "1700000000000", "secret-456")
	want := sha256.Sum256([]byte("appkey-1231700000000000secret-456"))
	if got != hex.EncodeToString(want[:]) {
		t.Errorf("buildSign = %q, want sha256 of appkey+timestamp+secret", got)
	}
	if len(got) != 64 {
		t.Errorf("sign length = %d, want 64 hex chars", len(got))
	}
}

func TestExpireAt(t *testing.T) {
	now := time.Unix(1700000000, 0)

	t.Run("valid timestamp keeps margin", func(t *testing.T) {
		raw := flexString("1700003600000") // +1h
		got := expireAt(raw, now)
		want := time.UnixMilli(1700003600000).Add(-tokenRefreshMargin)
		if !got.Equal(want) {
			t.Errorf("expireAt = %v, want %v", got, want)
		}
	})

	t.Run("unparsable falls back to token TTL", func(t *testing.T) {
		got := expireAt("not-a-number", now)
		want := now.Add(tokenTTL - tokenRefreshMargin)
		if !got.Equal(want) {
			t.Errorf("expireAt = %v, want %v", got, want)
		}
	})

	t.Run("zero falls back to token TTL", func(t *testing.T) {
		if got := expireAt("0", now); !got.Equal(now.Add(tokenTTL - tokenRefreshMargin)) {
			t.Errorf("expireAt(0) = %v", got)
		}
	})

	t.Run("already expired gets a minute", func(t *testing.T) {
		got := expireAt("1600000000000", now)
		if !got.Equal(now.Add(time.Minute)) {
			t.Errorf("expireAt(past) = %v, want now+1m", got)
		}
	})

	t.Run("numeric json value", func(t *testing.T) {
		var f flexString
		if err := json.Unmarshal([]byte(`1700003600000`), &f); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if string(f) != "1700003600000" {
			t.Errorf("flexString = %q", f)
		}
	})
}

func TestDeliverValidation(t *testing.T) {
	p := newProvider(t, nil)
	ctx := context.Background()

	if err := p.Deliver(ctx, nil); err == nil || err.Error() != "getui: task is nil" {
		t.Errorf("Deliver(nil) = %v", err)
	}
	if err := p.Deliver(ctx, &core.DeliveryTask{}); err == nil || err.Error() != "getui: at least one target (cid) is required" {
		t.Errorf("Deliver without targets = %v", err)
	}
	if err := p.Deliver(ctx, &core.DeliveryTask{Targets: []string{""}}); err == nil || err.Error() != "getui: empty target found in targets" {
		t.Errorf("Deliver empty target = %v", err)
	}
	err := p.Deliver(ctx, &core.DeliveryTask{Targets: []string{"cid-1"}})
	if err == nil || err.Error() != "getui: payload needs content or raw data" {
		t.Errorf("Deliver empty payload = %v", err)
	}
}

func TestBuildRequestNotification(t *testing.T) {
	t.Run("title and body with settings", func(t *testing.T) {
		p := newProvider(t, map[string]interface{}{"ttl": 600000})
		req, err := p.buildRequest(contentTask("磁盘告警", "/dev/sda1 95%"))
		if err != nil {
			t.Fatalf("buildRequest: %v", err)
		}
		n := req.PushMessage.Notification
		if n == nil || n.Title != "磁盘告警" || n.Body != "/dev/sda1 95%" || n.ClickType != "none" {
			t.Fatalf("notification = %+v", n)
		}
		if req.Settings == nil || req.Settings.TTL != 600000 {
			t.Errorf("settings = %+v, want ttl 600000", req.Settings)
		}
		// iOS does not show 个推通道 notifications, so aps must carry it.
		ios := req.PushChannel.IOS.APS
		if ios.Alert.Title != "磁盘告警" || ios.Alert.Body != "/dev/sda1 95%" || ios.Sound != "default" {
			t.Errorf("ios aps = %+v", ios)
		}
	})

	t.Run("body only borrows the title", func(t *testing.T) {
		p := newProvider(t, nil)
		req, err := p.buildRequest(contentTask("", "只有正文"))
		if err != nil {
			t.Fatalf("buildRequest: %v", err)
		}
		if req.PushMessage.Notification.Title != "只有正文" || req.PushMessage.Notification.Body != "只有正文" {
			t.Errorf("notification = %+v", req.PushMessage.Notification)
		}
		if req.Settings != nil {
			t.Errorf("settings = %+v, want nil without ttl", req.Settings)
		}
	})

	t.Run("title only borrows the body", func(t *testing.T) {
		p := newProvider(t, nil)
		req, err := p.buildRequest(contentTask("只有标题", ""))
		if err != nil {
			t.Fatalf("buildRequest: %v", err)
		}
		if req.PushMessage.Notification.Body != "只有标题" {
			t.Errorf("notification body = %q, want borrowed title", req.PushMessage.Notification.Body)
		}
	})

	t.Run("raw rides along as click payload", func(t *testing.T) {
		p := newProvider(t, map[string]interface{}{"click_type": "payload"})
		task := contentTask("标题", "正文")
		task.Payload.Raw = map[string]interface{}{"order_id": 42}
		req, err := p.buildRequest(task)
		if err != nil {
			t.Fatalf("buildRequest: %v", err)
		}
		if got := req.PushMessage.Notification.Payload; got != `{"order_id":42}` {
			t.Errorf("payload = %q, want raw JSON", got)
		}
	})

	t.Run("oversized raw is dropped from the payload", func(t *testing.T) {
		p := newProvider(t, map[string]interface{}{"click_type": "payload_custom"})
		task := contentTask("标题", "正文")
		task.Payload.Raw = map[string]interface{}{"blob": strings.Repeat("x", transmissionLimit+10)}
		req, err := p.buildRequest(task)
		if err != nil {
			t.Fatalf("buildRequest: %v", err)
		}
		if req.PushMessage.Notification.Payload != "" {
			t.Errorf("payload = %q, want dropped when over %d chars", req.PushMessage.Notification.Payload, transmissionLimit)
		}
	})

	t.Run("no raw with payload click_type", func(t *testing.T) {
		p := newProvider(t, map[string]interface{}{"click_type": "payload"})
		req, err := p.buildRequest(contentTask("标题", "正文"))
		if err != nil {
			t.Fatalf("buildRequest: %v", err)
		}
		if req.PushMessage.Notification.Payload != "" {
			t.Errorf("payload = %q, want empty", req.PushMessage.Notification.Payload)
		}
	})
}

func TestBuildRequestTransmission(t *testing.T) {
	t.Run("raw only becomes transmission", func(t *testing.T) {
		p := newProvider(t, nil)
		task := &core.DeliveryTask{Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]interface{}{"event": "deploy", "code": 200},
		}}
		req, err := p.buildRequest(task)
		if err != nil {
			t.Fatalf("buildRequest: %v", err)
		}
		if req.PushMessage.Notification != nil {
			t.Errorf("notification = %+v, want nil", req.PushMessage.Notification)
		}
		if req.PushMessage.Transmission != `{"code":200,"event":"deploy"}` {
			t.Errorf("transmission = %q", req.PushMessage.Transmission)
		}
		if req.PushChannel != nil {
			t.Errorf("pushChannel = %+v, want nil for transmission-only", req.PushChannel)
		}
	})

	t.Run("oversized transmission rejected", func(t *testing.T) {
		p := newProvider(t, nil)
		task := &core.DeliveryTask{Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]interface{}{"blob": strings.Repeat("汉", transmissionLimit+1)},
		}}
		_, err := p.buildRequest(task)
		if err == nil || !strings.Contains(err.Error(), "transmission exceeds") {
			t.Errorf("buildRequest = %v, want transmission limit error", err)
		}
	})

	t.Run("unmarshalable raw rejected", func(t *testing.T) {
		p := newProvider(t, nil)
		task := &core.DeliveryTask{Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]interface{}{"bad": make(chan int)},
		}}
		if _, err := p.buildRequest(task); err == nil || !strings.Contains(err.Error(), "failed to encode raw payload") {
			t.Errorf("buildRequest = %v, want encode error", err)
		}
	})

	t.Run("unmarshalable click payload rejected", func(t *testing.T) {
		p := newProvider(t, map[string]interface{}{"click_type": "payload"})
		task := contentTask("标题", "正文")
		task.Payload.Raw = map[string]interface{}{"bad": make(chan int)}
		if _, err := p.buildRequest(task); err == nil || !strings.Contains(err.Error(), "failed to encode raw payload") {
			t.Errorf("buildRequest = %v, want encode error", err)
		}
	})
}

// TestGetTokenCaches checks the auth call happens once per token lifetime:
// Getui caps /auth at 100 calls per minute.
func TestGetTokenCaches(t *testing.T) {
	srv := newGetuiServer(t, nil)
	p := newProvider(t, map[string]interface{}{"endpoint": srv.URL})

	for i := 0; i < 3; i++ {
		token, err := p.getToken(context.Background())
		if err != nil || token != "token-1" {
			t.Fatalf("getToken = %q, %v", token, err)
		}
	}
	if got := srv.authCallsNow(); got != 1 {
		t.Errorf("auth calls = %d, want 1 (cached)", got)
	}

	// The signature must follow Getui's documented order and carry the
	// millisecond timestamp plus appkey.
	seen := srv.authSeen
	wantSign := buildSign("appkey-123", seen.Timestamp, "secret-456")
	if seen.Sign != wantSign {
		t.Errorf("sign = %q, want sha256(appkey+timestamp+secret)", seen.Sign)
	}
	if seen.AppKey != "appkey-123" {
		t.Errorf("appkey = %q", seen.AppKey)
	}
	if seen.Timestamp == "" {
		t.Error("timestamp must be sent")
	}
}

func TestGetTokenErrors(t *testing.T) {
	t.Run("auth http error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":10001,"msg":"token错误/失效"}`))
		}))
		t.Cleanup(ts.Close)

		p := newProvider(t, map[string]interface{}{"endpoint": ts.URL})
		if _, err := p.getToken(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
			t.Errorf("getToken = %v, want status error", err)
		}
	})

	t.Run("auth business error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":10001,"msg":"appkey与mastersecret不匹配"}`))
		}))
		t.Cleanup(ts.Close)

		p := newProvider(t, map[string]interface{}{"endpoint": ts.URL})
		_, err := p.getToken(context.Background())
		if err == nil || err.Error() != "getui: auth failed (code 10001): appkey与mastersecret不匹配" {
			t.Errorf("getToken = %v, want auth failure", err)
		}
	})

	t.Run("missing token in response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
		}))
		t.Cleanup(ts.Close)

		p := newProvider(t, map[string]interface{}{"endpoint": ts.URL})
		_, err := p.getToken(context.Background())
		if err == nil || err.Error() != "getui: auth response missing token" {
			t.Errorf("getToken = %v, want missing token", err)
		}
	})

	t.Run("unparsable auth response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		}))
		t.Cleanup(ts.Close)

		p := newProvider(t, map[string]interface{}{"endpoint": ts.URL})
		_, err := p.getToken(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to parse auth response") {
			t.Errorf("getToken = %v, want parse error", err)
		}
	})
}

// TestDeliverNotification pins the wire format for a visible notification:
// path, token header, request_id/audience envelope and the message body.
func TestDeliverNotification(t *testing.T) {
	var gotPath, gotToken string
	var gotBody []byte
	srv := newGetuiServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("token")
		gotBody, _ = readBody(r)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"task-1":{"cid-1":"successed_online"}}}`))
	}))

	p := newProvider(t, map[string]interface{}{"endpoint": srv.URL, "ttl": 7200000})
	pinRequestID(t)

	task := contentTask("订单已发货", "您的订单 SF123456 已发出")
	task.Targets = []string{"cid-1"}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if gotPath != "/"+p.appID+pushPath {
		t.Errorf("path = %q, want /%s%s", gotPath, p.appID, pushPath)
	}
	if gotToken != "token-1" {
		t.Errorf("token header = %q", gotToken)
	}

	var sent pushRequest
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if sent.RequestID != "req-000000000001" {
		t.Errorf("request_id = %q", sent.RequestID)
	}
	if len(sent.Audience.CID) != 1 || sent.Audience.CID[0] != "cid-1" {
		t.Errorf("audience = %+v", sent.Audience)
	}
	if sent.PushMessage.Notification.Title != "订单已发货" {
		t.Errorf("notification = %+v", sent.PushMessage.Notification)
	}
	if sent.PushChannel == nil || sent.PushChannel.IOS == nil {
		t.Fatal("ios channel missing, iOS would show no notification")
	}
	if sent.Settings == nil || sent.Settings.TTL != 7200000 {
		t.Errorf("settings = %+v", sent.Settings)
	}
}

// pinRequestID replaces the id seam so assertions can pin the field.
func pinRequestID(t *testing.T) {
	t.Helper()
	orig := newRequestID
	var n int32
	newRequestID = func() string {
		return fmt.Sprintf("req-%012d", atomic.AddInt32(&n, 1))
	}
	t.Cleanup(func() { newRequestID = orig })
}

// TestDeliverRefreshesExpiredToken covers Getui's documented passive
// refresh: business code 10001 invalidates the cached token and the push
// is retried once with the same request_id.
func TestDeliverRefreshesExpiredToken(t *testing.T) {
	var requestIDs []string
	srv := newGetuiServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readBody(r)
		var sent pushRequest
		_ = json.Unmarshal(body, &sent)
		requestIDs = append(requestIDs, sent.RequestID)

		if r.Header.Get("token") == "token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":10001,"msg":"token错误/失效"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))

	p := newProvider(t, map[string]interface{}{"endpoint": srv.URL})
	task := contentTask("标题", "正文")
	task.Targets = []string{"cid-1"}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if got := srv.authCallsNow(); got != 2 {
		t.Errorf("auth calls = %d, want 2 (refresh after 10001)", got)
	}
	if len(requestIDs) != 2 {
		t.Fatalf("push attempts = %d, want 2", len(requestIDs))
	}
	if requestIDs[0] != requestIDs[1] {
		t.Errorf("request_id changed across retry: %q vs %q", requestIDs[0], requestIDs[1])
	}
}

func TestDeliverPushBusinessError(t *testing.T) {
	srv := newGetuiServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":20001,"msg":"cid is invalid"}`))
	}))

	p := newProvider(t, map[string]interface{}{"endpoint": srv.URL})
	task := contentTask("标题", "正文")
	task.Targets = []string{"cid-bad"}
	err := p.Deliver(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "push failed (code 20001): cid is invalid") {
		t.Errorf("Deliver = %v, want business error surfaced", err)
	}
}

func TestDeliverPartialFailureAggregates(t *testing.T) {
	srv := newGetuiServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readBody(r)
		var sent pushRequest
		_ = json.Unmarshal(body, &sent)
		cid := sent.Audience.CID[0]
		if strings.HasPrefix(cid, "dead") {
			_, _ = w.Write([]byte(`{"code":10002,"msg":"appId或ip在黑名单中"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))

	p := newProvider(t, map[string]interface{}{"endpoint": srv.URL})
	task := contentTask("标题", "正文")
	task.Targets = []string{"dead-cid-very-long", "good-cid", "bad"}
	err := p.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("Deliver with failing targets = nil error")
	}
	// The long failing target is truncated at 8 runes; the short ones
	// never appear because they succeeded.
	if !strings.HasPrefix(err.Error(), "getui: 2/3 succeeded - failed: dead-ci") {
		t.Errorf("error = %q, want truncated dead cid prefix", err.Error())
	}
	if strings.Contains(err.Error(), "good-cid:") {
		t.Errorf("error = %q, succeeded cids must not be listed", err.Error())
	}
}

func TestDeliverTokenErrors(t *testing.T) {
	t.Run("auth unavailable fails the target", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":10001,"msg":"appkey与mastersecret不匹配"}`))
		}))
		t.Cleanup(ts.Close)

		p := newProvider(t, map[string]interface{}{"endpoint": ts.URL})
		task := contentTask("标题", "正文")
		task.Targets = []string{"cid-1"}
		err := p.Deliver(context.Background(), task)
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Errorf("Deliver = %v, want auth failure surfaced per target", err)
		}
	})

	t.Run("refresh failure keeps the original error", func(t *testing.T) {
		// /auth succeeds once, then fails: the push retry never gets a
		// fresh token and the original 401 must surface.
		var authCalls int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, authPath) && atomic.AddInt32(&authCalls, 1) > 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":10003,"msg":"每分钟鉴权频率超限"}`))
				return
			}
			if strings.HasSuffix(r.URL.Path, authPath) {
				_, _ = fmt.Fprintf(w,
					`{"code":0,"data":{"token":"token-1","expire_time":"%d"}}`,
					time.Now().Add(24*time.Hour).UnixMilli())
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":10001,"msg":"token错误/失效"}`))
		}))
		t.Cleanup(ts.Close)

		p := newProvider(t, map[string]interface{}{"endpoint": ts.URL})
		task := contentTask("标题", "正文")
		task.Targets = []string{"cid-1"}
		err := p.Deliver(context.Background(), task)
		if err == nil || !strings.Contains(err.Error(), "401") {
			t.Errorf("Deliver = %v, want original 401 kept", err)
		}
	})
}

// TestGetTokenConcurrentRefresh covers the double-check under the write
// lock: a second caller that blocked on the mutex while the first was
// fetching must reuse the token instead of hitting /auth again.
func TestGetTokenConcurrentRefresh(t *testing.T) {
	var authCalls int32
	firstInFlight := make(chan struct{})
	var once sync.Once

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&authCalls, 1)
		// Announce the in-flight fetch, then stay busy long enough for the
		// second caller to arrive on the cold cache and queue on the token mutex.
		once.Do(func() { close(firstInFlight) })
		time.Sleep(150 * time.Millisecond)
		_, _ = fmt.Fprintf(w,
			`{"code":0,"data":{"token":"token-shared","expire_time":"%d"}}`,
			time.Now().Add(24*time.Hour).UnixMilli())
	}))
	t.Cleanup(ts.Close)

	p := newProvider(t, map[string]interface{}{"endpoint": ts.URL})

	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		tokens[0], errs[0] = p.getToken(context.Background())
	}()

	<-firstInFlight // the cache is cold and the refresh holds the token mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		tokens[1], errs[1] = p.getToken(context.Background())
	}()
	wg.Wait()

	for i := range tokens {
		if errs[i] != nil {
			t.Fatalf("getToken[%d]: %v", i, errs[i])
		}
		if tokens[i] != "token-shared" {
			t.Errorf("token[%d] = %q, want the shared token", i, tokens[i])
		}
	}
	if got := atomic.LoadInt32(&authCalls); got != 1 {
		t.Errorf("auth calls = %d, want 1 (refresh runs under the cache lock)", got)
	}
}

func TestCheckResponse(t *testing.T) {
	if err := checkResponse(nil); err != nil {
		t.Errorf("checkResponse(nil) = %v, want nil", err)
	}

	resp := func(body string) *httpclient.Response {
		return &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(body)}
	}

	if err := checkResponse(resp(`{"code":0,"msg":"ok"}`)); err != nil {
		t.Errorf("checkResponse(ok) = %v", err)
	}
	err := checkResponse(resp(`{"code":30009}`))
	if err == nil || err.Error() != "getui: push failed (code 30009)" {
		t.Errorf("checkResponse(no msg) = %v", err)
	}
	err = checkResponse(resp(`{"code":20001,"msg":"cid is invalid"}`))
	if err == nil || err.Error() != "getui: push failed (code 20001): cid is invalid" {
		t.Errorf("checkResponse(msg) = %v", err)
	}
	if err = checkResponse(resp(`not json`)); err == nil || !strings.Contains(err.Error(), "failed to parse push response") {
		t.Errorf("checkResponse(bad json) = %v", err)
	}

	if got := responseCode(nil); got != -1 {
		t.Errorf("responseCode(nil) = %d, want -1", got)
	}
	if got := responseCode(resp(`{"code":10001}`)); got != 10001 {
		t.Errorf("responseCode = %d, want 10001", got)
	}
	if got := responseCode(resp(`{"code":0}`)); got != 0 {
		t.Errorf("responseCode = %d, want 0", got)
	}
	if got := responseCode(resp(`{"code":`)); got != -1 {
		t.Errorf("responseCode(bad json) = %d, want -1", got)
	}
}

// contentTask builds a Content delivery task.
func contentTask(title, body string) *core.DeliveryTask {
	return &core.DeliveryTask{Payload: core.DeliveryPayload{
		Kind:    core.PayloadContent,
		Content: &core.RenderedContent{Title: title, Body: body, Format: "plain"},
	}}
}

func TestNewRequestID(t *testing.T) {
	id := newRequestID()
	if len(id) != 24 {
		t.Errorf("request id = %q (len %d), want 24 hex chars", id, len(id))
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("request id = %q, want hex", id)
		}
	}
	if id == newRequestID() {
		t.Error("two ids collided")
	}

	orig := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	defer func() { randRead = orig }()

	fallback := newRequestID()
	if len(fallback) < 10 || len(fallback) > 32 {
		t.Errorf("fallback id = %q (len %d), want 10-32 chars", fallback, len(fallback))
	}
}

func TestProviderSurface(t *testing.T) {
	p := newProvider(t, map[string]interface{}{"ttl": 600000, "click_type": "startapp"})

	if got := p.Name(); got != "getui" {
		t.Errorf("Name() = %q", got)
	}
	if got := p.Type(); got != "getui" {
		t.Errorf("Type() = %q", got)
	}
	if st := p.Status(); st == nil || st.Status != "available" {
		t.Errorf("Status() = %+v", st)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}

	cfg := p.GetConfig()
	if cfg["app_id"] != "aaaaa" || cfg["app_key"] != "appkey-123" {
		t.Errorf("GetConfig identity = %v", cfg)
	}
	if cfg["master_secret"] != "secret-456" {
		t.Errorf("GetConfig master_secret = %v (masked by MaskConfig at the API edge)", cfg["master_secret"])
	}
	if cfg["click_type"] != "startapp" {
		t.Errorf("GetConfig click_type = %v", cfg["click_type"])
	}
	if cfg["ttl"] != int64(600000) {
		t.Errorf("GetConfig ttl = %v", cfg["ttl"])
	}
	if cfg["endpoint"] != defaultEndpoint {
		t.Errorf("GetConfig endpoint = %v", cfg["endpoint"])
	}
	if _, ok := newProvider(t, nil).GetConfig()["ttl"]; ok {
		t.Error("GetConfig must omit ttl when unset")
	}

	cap := p.Capability()
	if len(cap.PayloadKinds) != 2 || cap.ContentFormats[0] != "plain" {
		t.Errorf("Capability() = %+v", cap)
	}

	f := &Factory{}
	if f.Name() != "getui" || f.Type() != "getui" {
		t.Errorf("Factory name/type = %q/%q", f.Name(), f.Type())
	}
	built, err := f.Create(baseConfig())
	if err != nil || built == nil {
		t.Errorf("Factory.Create() = %v, %v", built, err)
	}
	if _, err := f.Create(map[string]interface{}{}); err == nil {
		t.Error("Factory.Create(empty) = nil error")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 8); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("a-very-long-cid", 8); got != "a-very-l" {
		t.Errorf("truncate long = %q", got)
	}
	if got := truncate("汉汉汉汉", 2); got != "汉汉" {
		t.Errorf("truncate runes = %q", got)
	}
}
