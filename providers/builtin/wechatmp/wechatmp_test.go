package wechatmp

import (
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func validConfig() map[string]interface{} {
	return map[string]interface{}{
		"app_id":      "wx-app-id",
		"app_secret":  "wx-app-secret",
		"template_id": "wx-template-id",
	}
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	p, err := NewProvider(validConfig())
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return p.(*Provider)
}

func TestNewProvider(t *testing.T) {
	t.Run("success with all fields", func(t *testing.T) {
		config := validConfig()
		config["default_url"] = "https://example.com/landing"

		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		provider, ok := p.(*Provider)
		if !ok {
			t.Fatalf("expected *Provider, got %T", p)
		}
		if provider.appID != "wx-app-id" {
			t.Errorf("expected app id wx-app-id, got %s", provider.appID)
		}
		if provider.appSecret != "wx-app-secret" {
			t.Errorf("expected app secret wx-app-secret, got %s", provider.appSecret)
		}
		if provider.templateID != "wx-template-id" {
			t.Errorf("expected template id wx-template-id, got %s", provider.templateID)
		}
		if provider.defaultURL != "https://example.com/landing" {
			t.Errorf("expected default url, got %s", provider.defaultURL)
		}
		if provider.client == nil {
			t.Error("expected non-nil client")
		}
		if provider.tokenCache == nil {
			t.Error("expected non-nil token cache")
		}
		if provider.status == nil {
			t.Fatal("expected non-nil status")
		}
		if provider.status.Status != "available" {
			t.Errorf("expected status available, got %s", provider.status.Status)
		}
		if provider.status.Since.IsZero() {
			t.Error("expected non-zero since timestamp")
		}
	})

	t.Run("success without default_url", func(t *testing.T) {
		p, err := NewProvider(validConfig())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.(*Provider).defaultURL != "" {
			t.Errorf("expected empty default url, got %s", p.(*Provider).defaultURL)
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

	t.Run("empty app_id", func(t *testing.T) {
		config := validConfig()
		config["app_id"] = ""
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "app_id is required") {
			t.Errorf("expected app_id error, got %v", err)
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
		if err == nil || !strings.Contains(err.Error(), "app_secret is required") {
			t.Errorf("expected app_secret error, got %v", err)
		}
	})

	t.Run("missing template_id", func(t *testing.T) {
		config := validConfig()
		delete(config, "template_id")
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "template_id is required") {
			t.Errorf("expected template_id error, got %v", err)
		}
	})

	t.Run("empty template_id", func(t *testing.T) {
		config := validConfig()
		config["template_id"] = ""
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "template_id is required") {
			t.Errorf("expected template_id error, got %v", err)
		}
	})

	t.Run("non-string values are ignored", func(t *testing.T) {
		config := map[string]interface{}{
			"app_id":      123,
			"app_secret":  true,
			"template_id": []string{"nope"},
		}
		_, err := NewProvider(config)
		if err == nil || !strings.Contains(err.Error(), "app_id is required") {
			t.Errorf("expected app_id error for non-string values, got %v", err)
		}
	})

	t.Run("nil config", func(t *testing.T) {
		_, err := NewProvider(nil)
		if err == nil || !strings.Contains(err.Error(), "app_id is required") {
			t.Errorf("expected app_id error for nil config, got %v", err)
		}
	})

	t.Run("non-string default_url is ignored", func(t *testing.T) {
		config := validConfig()
		config["default_url"] = 456
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.(*Provider).defaultURL != "" {
			t.Errorf("expected empty default url, got %s", p.(*Provider).defaultURL)
		}
	})
}

func TestParseConfig(t *testing.T) {
	cfg, err := parseConfig(map[string]interface{}{
		"app_id":      "id",
		"app_secret":  "secret",
		"template_id": "template",
		"default_url": "https://example.com",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.AppID != "id" || cfg.AppSecret != "secret" || cfg.TemplateID != "template" || cfg.DefaultURL != "https://example.com" {
		t.Errorf("unexpected config: %+v", cfg)
	}

	t.Run("empty and non-string keys", func(t *testing.T) {
		cfg, err := parseConfig(map[string]interface{}{
			"app_id":      1,
			"app_secret":  nil,
			"template_id": struct{}{},
			"default_url": 2.5,
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cfg.AppID != "" || cfg.AppSecret != "" || cfg.TemplateID != "" || cfg.DefaultURL != "" {
			t.Errorf("expected zero-value config, got %+v", cfg)
		}
	})

	t.Run("nil map", func(t *testing.T) {
		cfg, err := parseConfig(nil)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		if cfg.AppID != "" || cfg.DefaultURL != "" {
			t.Errorf("expected zero-value config, got %+v", cfg)
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	p := newTestProvider(t)
	got := p.GetConfig()

	if got["app_id"] != "wx-app-id" {
		t.Errorf("expected app_id wx-app-id, got %v", got["app_id"])
	}
	if got["app_secret"] != "wx-app-secret" {
		t.Errorf("expected app_secret wx-app-secret, got %v", got["app_secret"])
	}
	if got["template_id"] != "wx-template-id" {
		t.Errorf("expected template_id wx-template-id, got %v", got["template_id"])
	}
	if got["default_url"] != "" {
		t.Errorf("expected empty default_url, got %v", got["default_url"])
	}
}

func TestProviderMetadata(t *testing.T) {
	p := newTestProvider(t)

	if p.Name() != "wechatmp" {
		t.Errorf("expected name wechatmp, got %s", p.Name())
	}
	if p.Type() != "wechatmp" {
		t.Errorf("expected type wechatmp, got %s", p.Type())
	}
	if err := p.Close(); err != nil {
		t.Errorf("expected no error on close, got %v", err)
	}

	status := p.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "wechatmp" {
		t.Errorf("expected status name wechatmp, got %s", status.Name)
	}
	if status.Type != "wechatmp" {
		t.Errorf("expected status type wechatmp, got %s", status.Type)
	}
	if status.Status != "available" {
		t.Errorf("expected status available, got %s", status.Status)
	}
	if status.Since.IsZero() {
		t.Error("expected non-zero since timestamp")
	}
}

func TestProviderCapability(t *testing.T) {
	p := newTestProvider(t)
	capability := p.Capability()

	if len(capability.PayloadKinds) != 2 {
		t.Fatalf("expected 2 payload kinds, got %d", len(capability.PayloadKinds))
	}
	if capability.PayloadKinds[0] != core.PayloadProviderTemplate {
		t.Errorf("expected provider_template kind, got %s", capability.PayloadKinds[0])
	}
	if capability.PayloadKinds[1] != core.PayloadContent {
		t.Errorf("expected content kind, got %s", capability.PayloadKinds[1])
	}
	if !capability.SupportsTemplate {
		t.Error("expected SupportsTemplate true")
	}
}

func TestFactory(t *testing.T) {
	factory := &Factory{}

	if factory.Name() != "wechatmp" {
		t.Errorf("expected factory name wechatmp, got %s", factory.Name())
	}
	if factory.Type() != "wechatmp" {
		t.Errorf("expected factory type wechatmp, got %s", factory.Type())
	}

	p, err := factory.Create(validConfig())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p == nil || p.Name() != "wechatmp" {
		t.Errorf("expected wechatmp provider, got %v", p)
	}

	if _, err := factory.Create(map[string]interface{}{}); err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{name: "shorter than max", input: "hello", maxLen: 10, want: "hello"},
		{name: "exact length", input: "hello", maxLen: 5, want: "hello"},
		{name: "longer than max", input: "hello world", maxLen: 5, want: "hello"},
		{name: "empty string", input: "", maxLen: 5, want: ""},
		{name: "multi-byte runes", input: "你好世界再见", maxLen: 3, want: "你好世"},
		{name: "multi-byte exact", input: "你好", maxLen: 3, want: "你好"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.input, tc.maxLen); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

func TestExtractPayloadContent(t *testing.T) {
	t.Run("with content", func(t *testing.T) {
		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Title", Body: "Body"},
			},
		}
		title, body := extractPayloadContent(task)
		if title != "Title" {
			t.Errorf("expected title Title, got %s", title)
		}
		if body != "Body" {
			t.Errorf("expected body Body, got %s", body)
		}
	})

	t.Run("without content", func(t *testing.T) {
		task := &core.DeliveryTask{}
		title, body := extractPayloadContent(task)
		if title != "" {
			t.Errorf("expected empty title, got %s", title)
		}
		if body != "" {
			t.Errorf("expected empty body, got %s", body)
		}
	})
}

func TestBuildTemplateMessage(t *testing.T) {
	t.Run("full content with level", func(t *testing.T) {
		p := newTestProvider(t)
		task := &core.DeliveryTask{
			Level: "error",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: strings.Repeat("标", 25),
					Body:  strings.Repeat("容", 40),
				},
			},
		}

		msg := p.buildTemplateMessage(task)
		if msg == nil {
			t.Fatal("expected non-nil message")
		}
		if msg.TemplateID != "wx-template-id" {
			t.Errorf("expected configured template id, got %s", msg.TemplateID)
		}
		if msg.URL != "" {
			t.Errorf("expected empty url for provider without default, got %s", msg.URL)
		}
		if msg.ToUser != "" {
			t.Errorf("expected empty toUser before send, got %s", msg.ToUser)
		}
		if msg.MiniProgram != nil {
			t.Error("expected nil miniprogram")
		}
		if got := msg.Data["thing1"].Value; got != strings.Repeat("标", 20) {
			t.Errorf("expected thing1 truncated to 20 runes, got %q (%d runes)", got, len([]rune(got)))
		}
		if got := msg.Data["thing2"].Value; got != strings.Repeat("容", 30) {
			t.Errorf("expected thing2 truncated to 30 runes, got %q (%d runes)", got, len([]rune(got)))
		}
		if got := msg.Data["character_string1"]; got.Value != "error" {
			t.Errorf("expected character_string1 error, got %v", got)
		}
		timestamp := msg.Data["time3"].Value
		if _, err := time.Parse("2006-01-02 15:04:05", timestamp); err != nil {
			t.Errorf("expected formatted timestamp, got %q: %v", timestamp, err)
		}
	})

	t.Run("without level and without content", func(t *testing.T) {
		p := newTestProvider(t)
		msg := p.buildTemplateMessage(&core.DeliveryTask{})

		if _, ok := msg.Data["character_string1"]; ok {
			t.Error("expected no character_string1 without level")
		}
		if got := msg.Data["thing1"].Value; got != "" {
			t.Errorf("expected empty thing1, got %q", got)
		}
		if got := msg.Data["thing2"].Value; got != "" {
			t.Errorf("expected empty thing2, got %q", got)
		}
		if msg.Data["time3"].Value == "" {
			t.Error("expected time3 to be set")
		}
	})

	t.Run("default url is used", func(t *testing.T) {
		config := validConfig()
		config["default_url"] = "https://example.com/landing"
		p, err := NewProvider(config)
		if err != nil {
			t.Fatalf("failed to create provider: %v", err)
		}
		msg := p.(*Provider).buildTemplateMessage(&core.DeliveryTask{})
		if msg.URL != "https://example.com/landing" {
			t.Errorf("expected default url, got %s", msg.URL)
		}
	})

	t.Run("task provider template overrides configured one", func(t *testing.T) {
		p := newTestProvider(t)
		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				ProviderTemplate: &core.ProviderTemplatePayload{TemplateID: "override-id"},
			},
		}
		msg := p.buildTemplateMessage(task)
		if msg.TemplateID != "override-id" {
			t.Errorf("expected override template id, got %s", msg.TemplateID)
		}
	})

	t.Run("empty task template id falls back to configured one", func(t *testing.T) {
		p := newTestProvider(t)
		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				ProviderTemplate: &core.ProviderTemplatePayload{TemplateID: ""},
			},
		}
		msg := p.buildTemplateMessage(task)
		if msg.TemplateID != "wx-template-id" {
			t.Errorf("expected configured template id, got %s", msg.TemplateID)
		}
	})
}
