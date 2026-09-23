package wechatmp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/httpclient"
)

const (
	tokenPath = "/cgi-bin/token"
	sendPath  = "/cgi-bin/message/template/send"
)

// recordedRequest is a captured outbound request.
type recordedRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
}

// stubHandler answers a captured request with an HTTP status and body, or an
// error to simulate transport-level failures.
type stubHandler func(seq int, req recordedRequest) (int, string, error)

// stubTransport is a RoundTripper that records requests and answers them from
// a handler without touching the network.
type stubTransport struct {
	mu       sync.Mutex
	requests []recordedRequest
	handler  stubHandler
}

func (s *stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		body = b
	}
	rec := recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.Query(), body: body}

	s.mu.Lock()
	s.requests = append(s.requests, rec)
	handler := s.handler
	s.mu.Unlock()

	status, respBody, err := handler(len(s.requests), rec)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(respBody)),
		Request:    r,
	}, nil
}

func (s *stubTransport) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.requests {
		if r.path == path {
			n++
		}
	}
	return n
}

func (s *stubTransport) recorded() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}

// replaceTransport swaps the RoundTripper inside the provider's HTTP client so
// every outbound request to the hardcoded api.weixin.qq.com endpoints is
// answered locally without any real network traffic.
func replaceTransport(t *testing.T, client *httpclient.Client, rt http.RoundTripper) {
	t.Helper()
	field := reflect.ValueOf(client).Elem().FieldByName("client")
	inner := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	hc := inner.Interface().(*http.Client)
	prev := hc.Transport
	hc.Transport = rt
	t.Cleanup(func() { hc.Transport = prev })
}

func newStubProviderWithConfig(t *testing.T, config map[string]interface{}, handler stubHandler) (*Provider, *stubTransport) {
	t.Helper()
	p, err := NewProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	provider := p.(*Provider)
	st := &stubTransport{handler: handler}
	replaceTransport(t, provider.client, st)
	return provider, st
}

func newStubProvider(t *testing.T, handler stubHandler) (*Provider, *stubTransport) {
	t.Helper()
	return newStubProviderWithConfig(t, validConfig(), handler)
}

func jsonStub(status int, body string) (int, string, error) {
	return status, body, nil
}

func tokenStub(token string, expiresIn int) (int, string, error) {
	body, _ := json.Marshal(tokenResponse{
		ErrCode:     0,
		ErrMsg:      "ok",
		AccessToken: token,
		ExpiresIn:   expiresIn,
	})
	return http.StatusOK, string(body), nil
}

func contentTask(targets []string) *core.DeliveryTask {
	return &core.DeliveryTask{
		ID:       "task-1",
		Provider: "wechatmp",
		Targets:  targets,
		Level:    "error",
		Payload: core.DeliveryPayload{
			Kind:    core.PayloadContent,
			Content: &core.RenderedContent{Title: "服务异常", Body: "磁盘使用率超过 90%"},
		},
	}
}

func TestDeliverValidation(t *testing.T) {
	p, st := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		t.Error("unexpected outbound request")
		return jsonStub(http.StatusOK, "{}")
	})
	ctx := context.Background()

	t.Run("nil task", func(t *testing.T) {
		err := p.Deliver(ctx, nil)
		if err == nil || !strings.Contains(err.Error(), "task is nil") {
			t.Errorf("expected nil task error, got %v", err)
		}
	})

	t.Run("no targets", func(t *testing.T) {
		err := p.Deliver(ctx, contentTask(nil))
		if err == nil || !strings.Contains(err.Error(), "at least one target (OpenID) is required") {
			t.Errorf("expected targets error, got %v", err)
		}
	})

	t.Run("empty target", func(t *testing.T) {
		err := p.Deliver(ctx, contentTask([]string{"openid-A", ""}))
		if err == nil || !strings.Contains(err.Error(), "empty target found in targets") {
			t.Errorf("expected empty target error, got %v", err)
		}
	})

	if len(st.recorded()) != 0 {
		t.Errorf("expected no outbound requests for validation failures, got %d", len(st.recorded()))
	}
}

func TestDeliverSuccess(t *testing.T) {
	config := validConfig()
	config["default_url"] = "https://example.com/landing"
	p, st := newStubProviderWithConfig(t, config, func(seq int, req recordedRequest) (int, string, error) {
		switch req.path {
		case tokenPath:
			return tokenStub("STUB_TOKEN", 7200)
		case sendPath:
			return jsonStub(http.StatusOK, `{"errcode":0,"errmsg":"ok","msgid":123456}`)
		default:
			t.Errorf("unexpected request path %s", req.path)
			return jsonStub(http.StatusNotFound, "{}")
		}
	})

	task := contentTask([]string{"openid-A", "openid-B"})
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got := st.count(tokenPath); got != 1 {
		t.Errorf("expected 1 token request, got %d", got)
	}
	if got := st.count(sendPath); got != 2 {
		t.Errorf("expected 2 send requests, got %d", got)
	}

	requests := st.recorded()
	tokenReq := requests[0]
	if tokenReq.method != http.MethodGet {
		t.Errorf("expected GET token request, got %s", tokenReq.method)
	}
	if got := tokenReq.query.Get("grant_type"); got != "client_credential" {
		t.Errorf("expected grant_type client_credential, got %s", got)
	}
	if got := tokenReq.query.Get("appid"); got != "wx-app-id" {
		t.Errorf("expected appid wx-app-id, got %s", got)
	}
	if got := tokenReq.query.Get("secret"); got != "wx-app-secret" {
		t.Errorf("expected secret wx-app-secret, got %s", got)
	}

	wantTargets := []string{"openid-A", "openid-B"}
	for i, want := range wantTargets {
		rec := requests[i+1]
		if rec.method != http.MethodPost {
			t.Errorf("expected POST send request, got %s", rec.method)
		}
		if got := rec.query.Get("access_token"); got != "STUB_TOKEN" {
			t.Errorf("expected access_token STUB_TOKEN, got %s", got)
		}
		var msg templateMessageRequest
		if err := json.Unmarshal(rec.body, &msg); err != nil {
			t.Fatalf("failed to decode send body: %v", err)
		}
		if msg.ToUser != want {
			t.Errorf("send[%d]: expected touser %s, got %s", i, want, msg.ToUser)
		}
		if msg.TemplateID != "wx-template-id" {
			t.Errorf("send[%d]: expected template id wx-template-id, got %s", i, msg.TemplateID)
		}
		if msg.URL != "https://example.com/landing" {
			t.Errorf("send[%d]: expected default url, got %s", i, msg.URL)
		}
		if got := msg.Data["thing1"].Value; got != "服务异常" {
			t.Errorf("send[%d]: expected thing1 服务异常, got %s", i, got)
		}
		if got := msg.Data["thing2"].Value; got != "磁盘使用率超过 90%" {
			t.Errorf("send[%d]: expected thing2 body, got %s", i, got)
		}
		if got := msg.Data["character_string1"].Value; got != "error" {
			t.Errorf("send[%d]: expected character_string1 error, got %s", i, got)
		}
		if msg.Data["time3"].Value == "" {
			t.Errorf("send[%d]: expected time3 to be set", i)
		}
	}
}

func TestDeliverTokenAPIError(t *testing.T) {
	p, st := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		return jsonStub(http.StatusOK, `{"errcode":40013,"errmsg":"invalid appid"}`)
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil || !strings.Contains(err.Error(), "failed to get access token") {
		t.Fatalf("expected access token error, got %v", err)
	}
	if !strings.Contains(err.Error(), "token error 40013: invalid appid") {
		t.Errorf("expected token error detail, got %v", err)
	}
	if st.count(sendPath) != 0 {
		t.Errorf("expected no send requests, got %d", st.count(sendPath))
	}
}

func TestDeliverTokenNetworkError(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		return 0, "", errors.New("dial tcp: connection refused")
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil || !strings.Contains(err.Error(), "failed to get access token") {
		t.Fatalf("expected access token error, got %v", err)
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("expected network error detail, got %v", err)
	}
}

func TestDeliverTokenBadJSON(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		return jsonStub(http.StatusOK, "not-json")
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil || !strings.Contains(err.Error(), "failed to get access token") {
		t.Fatalf("expected access token error, got %v", err)
	}
}

func TestDeliverTokenHTTP500(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		return jsonStub(http.StatusInternalServerError, "{}")
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil || !strings.Contains(err.Error(), "failed to get access token") {
		t.Fatalf("expected access token error, got %v", err)
	}
	if !strings.Contains(err.Error(), "unexpected status code: 500") {
		t.Errorf("expected status code detail, got %v", err)
	}
}

func TestDeliverSendAPIError(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		switch req.path {
		case tokenPath:
			return tokenStub("STUB_TOKEN", 7200)
		case sendPath:
			return jsonStub(http.StatusOK, `{"errcode":40036,"errmsg":"invalid openid"}`)
		default:
			t.Errorf("unexpected request path %s", req.path)
			return jsonStub(http.StatusNotFound, "{}")
		}
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if !strings.Contains(err.Error(), "0/1 succeeded") {
		t.Errorf("expected 0/1 succeeded, got %v", err)
	}
	if !strings.Contains(err.Error(), "API error 40036: invalid openid") {
		t.Errorf("expected API error detail, got %v", err)
	}
}

func TestDeliverSendPartialFailure(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		switch req.path {
		case tokenPath:
			return tokenStub("STUB_TOKEN", 7200)
		case sendPath:
			if seq == 2 { // first send request
				return jsonStub(http.StatusOK, `{"errcode":0,"errmsg":"ok"}`)
			}
			return jsonStub(http.StatusOK, `{"errcode":40036,"errmsg":"invalid openid"}`)
		default:
			t.Errorf("unexpected request path %s", req.path)
			return jsonStub(http.StatusNotFound, "{}")
		}
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A", "openid-B-long"}))
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if !strings.Contains(err.Error(), "1/2 succeeded") {
		t.Errorf("expected 1/2 succeeded, got %v", err)
	}
	// The failing target is truncated to 8 runes in the aggregated message.
	if !strings.Contains(err.Error(), "openid-B: wechatmp: API error 40036: invalid openid") {
		t.Errorf("expected truncated failing target detail, got %v", err)
	}
}

func TestDeliverSendNetworkError(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		switch req.path {
		case tokenPath:
			return tokenStub("STUB_TOKEN", 7200)
		case sendPath:
			return 0, "", errors.New("dial tcp: connection refused")
		default:
			t.Errorf("unexpected request path %s", req.path)
			return jsonStub(http.StatusNotFound, "{}")
		}
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil {
		t.Fatal("expected aggregated error")
	}
	if !strings.Contains(err.Error(), "0/1 succeeded") {
		t.Errorf("expected 0/1 succeeded, got %v", err)
	}
	if !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("expected network error detail, got %v", err)
	}
}

func TestDeliverSendInvalidJSON(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		switch req.path {
		case tokenPath:
			return tokenStub("STUB_TOKEN", 7200)
		case sendPath:
			return jsonStub(http.StatusOK, "not-json")
		default:
			t.Errorf("unexpected request path %s", req.path)
			return jsonStub(http.StatusNotFound, "{}")
		}
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil || !strings.Contains(err.Error(), "failed to parse response") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestDeliverSendHTTP500(t *testing.T) {
	p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		switch req.path {
		case tokenPath:
			return tokenStub("STUB_TOKEN", 7200)
		case sendPath:
			return jsonStub(http.StatusInternalServerError, "{}")
		default:
			t.Errorf("unexpected request path %s", req.path)
			return jsonStub(http.StatusNotFound, "{}")
		}
	})

	err := p.Deliver(context.Background(), contentTask([]string{"openid-A"}))
	if err == nil || !strings.Contains(err.Error(), "unexpected status code: 500") {
		t.Fatalf("expected status code error, got %v", err)
	}
}

func TestGetAccessToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		p, st := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return tokenStub("STUB_TOKEN", 7200)
		})

		token, err := p.GetAccessToken()
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if token != "STUB_TOKEN" {
			t.Errorf("expected STUB_TOKEN, got %s", token)
		}

		requests := st.recorded()
		if len(requests) != 1 {
			t.Fatalf("expected 1 request, got %d", len(requests))
		}
		if requests[0].method != http.MethodGet {
			t.Errorf("expected GET, got %s", requests[0].method)
		}
		if got := requests[0].query.Get("appid"); got != "wx-app-id" {
			t.Errorf("expected appid wx-app-id, got %s", got)
		}
		if got := requests[0].query.Get("secret"); got != "wx-app-secret" {
			t.Errorf("expected secret wx-app-secret, got %s", got)
		}
		if got := requests[0].query.Get("grant_type"); got != "client_credential" {
			t.Errorf("expected grant_type client_credential, got %s", got)
		}
	})

	t.Run("api error", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return jsonStub(http.StatusOK, `{"errcode":40013,"errmsg":"invalid appid"}`)
		})
		_, err := p.GetAccessToken()
		if err == nil || !strings.Contains(err.Error(), "token error 40013: invalid appid") {
			t.Errorf("expected token error, got %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return jsonStub(http.StatusOK, "not-json")
		})
		_, err := p.GetAccessToken()
		if err == nil {
			t.Error("expected parse error")
		}
	})

	t.Run("http 500", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return jsonStub(http.StatusInternalServerError, "{}")
		})
		_, err := p.GetAccessToken()
		if err == nil || !strings.Contains(err.Error(), "unexpected status code: 500") {
			t.Errorf("expected status code error, got %v", err)
		}
	})

	t.Run("network error", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return 0, "", errors.New("dial tcp: connection refused")
		})
		_, err := p.GetAccessToken()
		if err == nil || !strings.Contains(err.Error(), "failed to send request") {
			t.Errorf("expected network error, got %v", err)
		}
	})
}

func cachedToken(c *TokenCache) (string, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token, c.expireTime
}

func TestTokenCacheGetTokenCachesAndRefreshes(t *testing.T) {
	p, st := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		return tokenStub("TOKEN-"+string(rune('0'+seq)), 7200)
	})
	cache := p.tokenCache

	tok1, err := cache.GetToken("cache-app", "cache-secret", p.client)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tok1 != "TOKEN-1" {
		t.Errorf("expected TOKEN-1, got %s", tok1)
	}

	token, expire := cachedToken(cache)
	if token != "TOKEN-1" {
		t.Errorf("expected cached TOKEN-1, got %s", token)
	}
	// 7200 seconds - 300 seconds early refresh buffer.
	if remain := time.Until(expire); remain < 6890*time.Second || remain > 6910*time.Second {
		t.Errorf("expected expiry in about 6900s, got %v", remain)
	}

	tok2, err := cache.GetToken("cache-app", "cache-secret", p.client)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tok2 != "TOKEN-1" {
		t.Errorf("expected cached token TOKEN-1, got %s", tok2)
	}
	if got := st.count(tokenPath); got != 1 {
		t.Errorf("expected cached token to avoid extra request, got %d requests", got)
	}

	// Force expiry and verify the token is fetched again.
	cache.mu.Lock()
	cache.expireTime = time.Now().Add(-time.Minute)
	cache.mu.Unlock()

	tok3, err := cache.GetToken("cache-app", "cache-secret", p.client)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if tok3 != "TOKEN-2" {
		t.Errorf("expected refreshed token TOKEN-2, got %s", tok3)
	}
	if got := st.count(tokenPath); got != 2 {
		t.Errorf("expected 2 token requests after expiry, got %d", got)
	}
}

func TestTokenCacheExpiresInHandling(t *testing.T) {
	cases := []struct {
		name      string
		expiresIn int
		wantMin   time.Duration
		wantMax   time.Duration
	}{
		{name: "zero defaults to 7200-300", expiresIn: 0, wantMin: 6890 * time.Second, wantMax: 6910 * time.Second},
		{name: "above threshold is reduced by 300", expiresIn: 7200, wantMin: 6890 * time.Second, wantMax: 6910 * time.Second},
		{name: "boundary 3601 is reduced", expiresIn: 3601, wantMin: 3291 * time.Second, wantMax: 3311 * time.Second},
		{name: "at threshold kept as is", expiresIn: 3600, wantMin: 3590 * time.Second, wantMax: 3610 * time.Second},
		{name: "short value kept as is", expiresIn: 60, wantMin: 50 * time.Second, wantMax: 70 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
				return tokenStub("TOKEN", tc.expiresIn)
			})
			cache := &TokenCache{}
			if _, err := cache.GetToken(p.appID, p.appSecret, p.client); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			_, expire := cachedToken(cache)
			remain := time.Until(expire)
			if remain < tc.wantMin || remain > tc.wantMax {
				t.Errorf("expected expiry between %v and %v, got %v", tc.wantMin, tc.wantMax, remain)
			}
		})
	}
}

func TestTokenCacheErrorPaths(t *testing.T) {
	t.Run("api error", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return jsonStub(http.StatusOK, `{"errcode":40001,"errmsg":"invalid credential"}`)
		})
		_, err := p.tokenCache.GetToken("a", "b", p.client)
		if err == nil || !strings.Contains(err.Error(), "token error 40001: invalid credential") {
			t.Errorf("expected token error, got %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return jsonStub(http.StatusOK, "not-json")
		})
		if _, err := p.tokenCache.GetToken("a", "b", p.client); err == nil {
			t.Error("expected parse error")
		}
	})

	t.Run("http 500", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return jsonStub(http.StatusInternalServerError, "{}")
		})
		_, err := p.tokenCache.GetToken("a", "b", p.client)
		if err == nil || !strings.Contains(err.Error(), "unexpected status code: 500") {
			t.Errorf("expected status code error, got %v", err)
		}
	})

	t.Run("network error", func(t *testing.T) {
		p, _ := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
			return 0, "", errors.New("dial tcp: connection refused")
		})
		_, err := p.tokenCache.GetToken("a", "b", p.client)
		if err == nil || !strings.Contains(err.Error(), "failed to send request") {
			t.Errorf("expected network error, got %v", err)
		}
	})
}

// TestTokenCacheConcurrentSingleFetch verifies the double-checked locking in
// GetToken: concurrent callers that already observed an expired cache must not
// trigger additional token fetches while one caller holds the write lock.
func TestTokenCacheConcurrentSingleFetch(t *testing.T) {
	p, st := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		time.Sleep(100 * time.Millisecond)
		return tokenStub("TOKEN-FRESH", 7200)
	})

	// Seed an expired cache with a stale token so every caller misses the
	// read-lock fast path.
	cache := &TokenCache{token: "stale-token", expireTime: time.Now().Add(-time.Minute)}

	const callers = 25
	start := make(chan struct{})
	var wg sync.WaitGroup
	tokens := make([]string, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			tokens[i], errs[i] = cache.GetToken(p.appID, p.appSecret, p.client)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: expected no error, got %v", i, err)
		}
		if tokens[i] != "TOKEN-FRESH" {
			t.Fatalf("caller %d: expected TOKEN-FRESH, got %s", i, tokens[i])
		}
	}
	if got := st.count(tokenPath); got != 1 {
		t.Errorf("expected exactly 1 token fetch, got %d", got)
	}
	token, _ := cachedToken(cache)
	if token != "TOKEN-FRESH" {
		t.Errorf("expected cached TOKEN-FRESH, got %s", token)
	}
}

// TestTokenCacheDoubleCheckUnderContention hammers the write-lock double check
// in GetToken: callers that already observed an expired cache must reuse the
// token fetched by whichever caller won the write lock, instead of fetching
// again. Exactly one fetch must happen per round.
func TestTokenCacheDoubleCheckUnderContention(t *testing.T) {
	p, st := newStubProvider(t, func(seq int, req recordedRequest) (int, string, error) {
		return tokenStub("TOKEN-FRESH", 7200)
	})

	cache := &TokenCache{}
	const callers = 8
	const rounds = 20

	tokens := make([]string, callers)
	errs := make([]error, callers)
	var running atomic.Int64
	var start atomic.Bool
	for round := 0; round < rounds; round++ {
		// Reset to an expired cache with a stale token so every caller in this
		// round misses the read-lock fast path.
		cache.mu.Lock()
		cache.token = "stale-token"
		cache.expireTime = time.Now().Add(-time.Minute)
		cache.mu.Unlock()

		st.mu.Lock()
		st.requests = nil
		st.mu.Unlock()

		start.Store(false)
		running.Store(0)
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				// Busy-wait until the start flag flips so every caller is
				// already on CPU and enters GetToken at (nearly) the same
				// instant, making the write-lock double check contend.
				running.Add(1)
				for !start.Load() {
				}
				tokens[i], errs[i] = cache.GetToken(p.appID, p.appSecret, p.client)
			}(i)
		}
		for running.Load() < callers {
			runtime.Gosched()
		}
		start.Store(true)
		wg.Wait()

		for i := 0; i < callers; i++ {
			if errs[i] != nil {
				t.Fatalf("round %d caller %d: expected no error, got %v", round, i, errs[i])
			}
			if tokens[i] != "TOKEN-FRESH" {
				t.Fatalf("round %d caller %d: expected TOKEN-FRESH, got %s", round, i, tokens[i])
			}
		}
		if got := st.count(tokenPath); got != 1 {
			t.Fatalf("round %d: expected exactly 1 token fetch, got %d", round, got)
		}
		token, _ := cachedToken(cache)
		if token != "TOKEN-FRESH" {
			t.Errorf("round %d: expected cached TOKEN-FRESH, got %s", round, token)
		}
	}
}
