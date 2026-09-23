package wechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

// stubServer starts an HTTP stub and rewires the provider to send all
// requests to it, keeping every test offline.
func stubServer(t *testing.T, p *Provider, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	p.url = srv.URL
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	return srv
}

func contentTask(title, body, level string) *core.DeliveryTask {
	return &core.DeliveryTask{
		ID:       "task-1",
		Provider: "wechat",
		Level:    level,
		Payload: core.DeliveryPayload{
			Kind: core.PayloadContent,
			Content: &core.RenderedContent{
				Title:  title,
				Body:   body,
				Format: "markdown",
			},
		},
		CreatedAt: time.Now(),
	}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Service != "serverchan" {
		t.Errorf("Service = %q, want default serverchan", cfg.Service)
	}

	cfg, err = parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig(nil) error = %v", err)
	}
	if cfg.Service != "serverchan" {
		t.Errorf("Service = %q, want default serverchan", cfg.Service)
	}

	// Non-string values must be ignored, not panic.
	cfg, err = parseConfig(map[string]interface{}{
		"service":   42,
		"send_key":  true,
		"token":     []int{1},
		"app_token": 1.5,
		"uid":       nil,
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Service != "serverchan" || cfg.SendKey != "" || cfg.Token != "" || cfg.AppToken != "" || cfg.UID != "" {
		t.Errorf("parseConfig with non-string values = %+v, want all defaults", cfg)
	}
}

func TestParseConfigFull(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"service":   "wxpusher",
		"send_key":  "SCT123",
		"token":     "pp-token",
		"app_token": "AT123",
		"uid":       "UID999",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Service != "wxpusher" || cfg.SendKey != "SCT123" || cfg.Token != "pp-token" || cfg.AppToken != "AT123" || cfg.UID != "UID999" {
		t.Errorf("parseConfig() = %+v", cfg)
	}
}

func TestNewProviderValidation(t *testing.T) {
	cases := []struct {
		name    string
		config  map[string]interface{}
		wantErr string
	}{
		{"serverchan missing key", map[string]interface{}{"service": "serverchan"}, "send_key is required"},
		{"pushplus missing token", map[string]interface{}{"service": "pushplus"}, "token is required"},
		{"wxpusher missing app_token", map[string]interface{}{"service": "wxpusher"}, "app_token is required"},
		{"unsupported service", map[string]interface{}{"service": "carrier-pigeon"}, "unsupported service"},
	}
	for _, tc := range cases {
		_, err := NewProvider(tc.config)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: NewProvider() error = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestNewProviderServerChan(t *testing.T) {
	got, err := NewProvider(map[string]interface{}{"service": "serverchan", "send_key": "SCTabc"})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	p := got.(*Provider)
	if p.service != "serverchan" {
		t.Errorf("service = %q, want serverchan", p.service)
	}
	if !strings.Contains(p.url, "SCTabc") || !strings.HasSuffix(p.url, ".send") {
		t.Errorf("url = %q, want ServerChan send URL with key", p.url)
	}
	if p.Name() != "wechat" || p.Type() != "wechat" {
		t.Errorf("Name/Type = %q/%q, want wechat", p.Name(), p.Type())
	}

	st := p.Status()
	if st == nil || st.Status != "available" || st.Name != "wechat" || st.Type != "wechat" {
		t.Errorf("Status() = %+v, want available wechat", st)
	}
	if err := p.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	cfg := p.GetConfig()
	if cfg["service"] != "serverchan" {
		t.Errorf("GetConfig()[service] = %v, want serverchan", cfg["service"])
	}

	cap := p.Capability()
	if len(cap.PayloadKinds) != 1 || cap.PayloadKinds[0] != core.PayloadContent {
		t.Errorf("Capability().PayloadKinds = %v, want [content]", cap.PayloadKinds)
	}
}

func TestNewProviderPushPlus(t *testing.T) {
	got, err := NewProvider(map[string]interface{}{"service": "pushplus", "token": "pp-1"})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	p := got.(*Provider)
	if p.service != "pushplus" || p.appToken != "pp-1" {
		t.Errorf("service/appToken = %q/%q, want pushplus/pp-1", p.service, p.appToken)
	}
	if p.url != pushPlusURL {
		t.Errorf("url = %q, want %q", p.url, pushPlusURL)
	}
	if cfg := p.GetConfig(); cfg["app_token"] != "pp-1" {
		t.Errorf("GetConfig()[app_token] = %v, want pp-1", cfg["app_token"])
	}
}

func TestNewProviderWxPusher(t *testing.T) {
	got, err := NewProvider(map[string]interface{}{
		"service":   "wxpusher",
		"app_token": "AT-1",
		"uid":       "UID-7",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	p := got.(*Provider)
	if p.service != "wxpusher" || p.appToken != "AT-1" || p.uid != "UID-7" {
		t.Errorf("provider = %+v, want wxpusher AT-1 UID-7", p)
	}
	if p.url != wxpusherURL {
		t.Errorf("url = %q, want %q", p.url, wxpusherURL)
	}
	if cfg := p.GetConfig(); cfg["uid"] != "UID-7" {
		t.Errorf("GetConfig()[uid] = %v, want UID-7", cfg["uid"])
	}
}

func TestDeliverNilTask(t *testing.T) {
	p, _ := NewProvider(map[string]interface{}{"service": "serverchan", "send_key": "k"})
	if err := p.(*Provider).Deliver(context.Background(), nil); err == nil {
		t.Error("Deliver(nil) should return error")
	}
}

func TestDeliverUnknownService(t *testing.T) {
	p := &Provider{service: "smoke-signals"}
	err := p.Deliver(context.Background(), contentTask("t", "b", ""))
	if err == nil || !strings.Contains(err.Error(), "unknown service") {
		t.Errorf("Deliver() error = %v, want unknown service", err)
	}
}

func TestSendServerChan(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{"service": "serverchan", "send_key": "SCTx"})
	p := got.(*Provider)

	var received serverChanRequest
	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "message": ""})
	})

	if err := p.Deliver(context.Background(), contentTask("Disk", "93% used", "error")); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if received.Title != "Disk" {
		t.Errorf("Title = %q, want Disk", received.Title)
	}
	if !strings.HasPrefix(received.Desp, "🔴 93% used") {
		t.Errorf("Desp = %q, want error icon + body", received.Desp)
	}
	if received.Short != "93% used" {
		t.Errorf("Short = %q, want body", received.Short)
	}
}

func TestSendServerChanShortTruncated(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{"service": "serverchan", "send_key": "SCTx"})
	p := got.(*Provider)

	long := strings.Repeat("字", 100)
	var received serverChanRequest
	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"code":0}`))
	})

	if err := p.Deliver(context.Background(), contentTask("t", long, "")); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if got := len([]rune(received.Short)); got != 64 {
		t.Errorf("Short rune length = %d, want 64", got)
	}
}

func TestSendServerChanFailures(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{"service": "serverchan", "send_key": "SCTx"})
	p := got.(*Provider)

	// Business error.
	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":40001,"message":"invalid key"}`))
	})
	err := p.Deliver(context.Background(), contentTask("t", "b", ""))
	if err == nil || !strings.Contains(err.Error(), "serverchan error: invalid key") {
		t.Errorf("Deliver() error = %v, want business error", err)
	}

	// Invalid JSON response.
	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json at all"))
	})
	if err := p.Deliver(context.Background(), contentTask("t", "b", "")); err == nil {
		t.Error("Deliver() with invalid JSON should fail")
	}

	// Network failure.
	p.url = "http://127.0.0.1:1/send"
	err = p.Deliver(context.Background(), contentTask("t", "b", ""))
	if err == nil || !strings.Contains(err.Error(), "failed to send request") {
		t.Errorf("Deliver() error = %v, want network failure", err)
	}
}

func TestSendPushPlus(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{"service": "pushplus", "token": "pp-9"})
	p := got.(*Provider)

	var received pushPlusRequest
	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok"}`))
	})

	if err := p.Deliver(context.Background(), contentTask("Build", "green", "info")); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if received.Token != "pp-9" || received.Title != "Build" {
		t.Errorf("request = %+v, want token pp-9 title Build", received)
	}
	if !strings.HasPrefix(received.Content, "🟢 green") {
		t.Errorf("Content = %q, want info icon + body", received.Content)
	}
}

func TestSendPushPlusFailures(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{"service": "pushplus", "token": "pp-9"})
	p := got.(*Provider)

	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":903,"msg":"invalid token"}`))
	})
	err := p.Deliver(context.Background(), contentTask("t", "b", ""))
	if err == nil || !strings.Contains(err.Error(), "pushplus error: invalid token") {
		t.Errorf("Deliver() error = %v, want business error", err)
	}

	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{broken`))
	})
	if err := p.Deliver(context.Background(), contentTask("t", "b", "")); err == nil {
		t.Error("Deliver() with invalid JSON should fail")
	}

	p.url = "http://127.0.0.1:1/send"
	if err := p.Deliver(context.Background(), contentTask("t", "b", "")); err == nil {
		t.Error("Deliver() with unreachable server should fail")
	}
}

func TestSendWxPusher(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{
		"service":   "wxpusher",
		"app_token": "AT-2",
		"uid":       "UID-3",
	})
	p := got.(*Provider)

	var received wxpusherRequest
	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"code":1000,"msg":"done","success":true}`))
	})

	if err := p.Deliver(context.Background(), contentTask("Deploy", "v1.2.3", "warning")); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if received.AppToken != "AT-2" || received.UIDs != "UID-3" {
		t.Errorf("request = %+v, want AT-2/UID-3", received)
	}
	if received.ContentType != 3 {
		t.Errorf("ContentType = %d, want 3 (markdown)", received.ContentType)
	}
	if received.Summary != "Deploy" || !strings.HasPrefix(received.Content, "🟡 v1.2.3") {
		t.Errorf("Summary/Content = %q/%q", received.Summary, received.Content)
	}
}

func TestSendWxPusherNoUID(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{"service": "wxpusher", "app_token": "AT-2"})
	p := got.(*Provider)

	var received wxpusherRequest
	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		_, _ = w.Write([]byte(`{"code":0}`))
	})

	if err := p.Deliver(context.Background(), contentTask("t", "b", "")); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if received.UIDs != "" {
		t.Errorf("UIDs = %q, want empty (broadcast to all)", received.UIDs)
	}
}

func TestSendWxPusherFailures(t *testing.T) {
	got, _ := NewProvider(map[string]interface{}{"service": "wxpusher", "app_token": "AT-2"})
	p := got.(*Provider)

	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":95001,"msg":"bad uid"}`))
	})
	err := p.Deliver(context.Background(), contentTask("t", "b", ""))
	if err == nil || !strings.Contains(err.Error(), "wxpusher error: bad uid") {
		t.Errorf("Deliver() error = %v, want business error", err)
	}

	stubServer(t, p, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<<not json>>`))
	})
	if err := p.Deliver(context.Background(), contentTask("t", "b", "")); err == nil {
		t.Error("Deliver() with invalid JSON should fail")
	}

	p.url = "http://127.0.0.1:1/send"
	if err := p.Deliver(context.Background(), contentTask("t", "b", "")); err == nil {
		t.Error("Deliver() with unreachable server should fail")
	}
}

func TestExtractContent(t *testing.T) {
	title, body := extractContent(contentTask("T", "B", ""))
	if title != "T" || body != "B" {
		t.Errorf("extractContent() = %q/%q, want T/B", title, body)
	}

	task := &core.DeliveryTask{}
	title, body = extractContent(task)
	if title != "" || body != "" {
		t.Errorf("extractContent(nil content) = %q/%q, want empty", title, body)
	}
}

func TestFormatContentLevels(t *testing.T) {
	cases := map[string]string{
		"error":    "🔴 ",
		"critical": "🔴 ",
		"warning":  "🟡 ",
		"info":     "🟢 ",
		"debug":    "⚪ ",
		"":         "",
		"trace":    "",
	}
	for level, icon := range cases {
		got := formatContent(contentTask("", "body", level))
		want := icon + "body"
		if !strings.HasPrefix(got, want) {
			t.Errorf("level %q: formatContent() = %q, want prefix %q", level, got, want)
		}
		if !strings.Contains(got, "\n\n---\n") {
			t.Errorf("level %q: formatContent() missing timestamp footer: %q", level, got)
		}
	}

	// Empty body: only icon (if any) and the timestamp footer.
	got := formatContent(contentTask("title-only", "", "info"))
	if !strings.HasPrefix(got, "🟢 \n\n---\n") {
		t.Errorf("formatContent() empty body = %q, want icon + footer only", got)
	}
}

func TestFactory(t *testing.T) {
	f := &Factory{}
	if f.Name() != "wechat" || f.Type() != "wechat" {
		t.Errorf("Factory Name/Type = %q/%q, want wechat", f.Name(), f.Type())
	}

	if _, err := f.Create(map[string]interface{}{"service": "bogus"}); err == nil {
		t.Error("Factory.Create(bogus) should fail")
	}

	got, err := f.Create(map[string]interface{}{"service": "pushplus", "token": "tok"})
	if err != nil {
		t.Fatalf("Factory.Create() error = %v", err)
	}
	if got.Name() != "wechat" {
		t.Errorf("Factory.Create().Name() = %q, want wechat", got.Name())
	}
}
