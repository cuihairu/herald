package wecom

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

func TestNewProvider(t *testing.T) {
	t.Run("webhook_url", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_url": "http://example.com/webhook",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		wecomProvider := provider.(*Provider)
		if wecomProvider.webhookURL != "http://example.com/webhook" {
			t.Errorf("expected webhook url, got %s", wecomProvider.webhookURL)
		}
	})

	t.Run("key", func(t *testing.T) {
		config := map[string]interface{}{
			"key": "mykey",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		wecomProvider := provider.(*Provider)
		expected := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=mykey"
		if wecomProvider.webhookURL != expected {
			t.Errorf("expected %s, got %s", expected, wecomProvider.webhookURL)
		}
	})

	t.Run("missing webhook_url and key", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing config")
		}
	})

	t.Run("empty webhook_url and key", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_url": "",
			"key":         "",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty config")
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	}

	provider, _ := NewProvider(config)
	wecomProvider := provider.(*Provider)
	retrieved := wecomProvider.GetConfig()

	if retrieved["webhook_url"] != "http://example.com/webhook" {
		t.Errorf("expected webhook url, got %v", retrieved["webhook_url"])
	}
}

func TestProviderCapability(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	wecomProvider := provider.(*Provider)

	capability := wecomProvider.Capability()
	if len(capability.PayloadKinds) != 1 {
		t.Errorf("expected 1 payload kind, got %d", len(capability.PayloadKinds))
	}
	if capability.PayloadKinds[0] != core.PayloadContent {
		t.Errorf("expected content payload kind, got %s", capability.PayloadKinds[0])
	}
	if len(capability.ContentFormats) != 2 {
		t.Errorf("expected 2 content formats, got %d", len(capability.ContentFormats))
	}
}

func TestProviderName(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	if provider.Name() != "wecom" {
		t.Errorf("expected name wecom, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	if provider.Type() != "wecom" {
		t.Errorf("expected type wecom, got %s", provider.Type())
	}
}

func TestProviderStatus(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	status := provider.Status()

	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "wecom" {
		t.Errorf("expected name wecom, got %s", status.Name)
	}
	if status.Type != "wecom" {
		t.Errorf("expected type wecom, got %s", status.Type)
	}
	if status.Status != "available" {
		t.Errorf("expected status available, got %s", status.Status)
	}
	if status.Since.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestProviderClose(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	wecomProvider := provider.(*Provider)
	if err := wecomProvider.Close(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestExtractContent(t *testing.T) {
	t.Run("with content", func(t *testing.T) {
		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Title", Body: "Body"},
			},
		}
		title, body := extractContent(task)
		if title != "Title" {
			t.Errorf("expected title Title, got %s", title)
		}
		if body != "Body" {
			t.Errorf("expected body Body, got %s", body)
		}
	})

	t.Run("without content", func(t *testing.T) {
		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Raw: map[string]any{"key": "value"},
			},
		}
		title, body := extractContent(task)
		if title != "" {
			t.Errorf("expected empty title, got %s", title)
		}
		if body != "" {
			t.Errorf("expected empty body, got %s", body)
		}
	})
}

func TestFormatMarkdown(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	wecomProvider := provider.(*Provider)

	cases := []struct {
		name   string
		task   *core.DeliveryTask
		prefix string
	}{
		{
			name: "error level",
			task: &core.DeliveryTask{
				Level:   "error",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title", Body: "Body"}},
			},
			prefix: "<font color='warning'>**Title**</font>\n\nBody",
		},
		{
			name: "warning level",
			task: &core.DeliveryTask{
				Level:   "warning",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title", Body: "Body"}},
			},
			prefix: "<font color='info'>**Title**</font>\n\nBody",
		},
		{
			name: "info level",
			task: &core.DeliveryTask{
				Level:   "info",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title", Body: "Body"}},
			},
			prefix: "**Title**\n\nBody",
		},
		{
			name: "unknown level",
			task: &core.DeliveryTask{
				Level:   "debug",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title", Body: "Body"}},
			},
			prefix: "**Title**\n\nBody",
		},
		{
			name: "empty body",
			task: &core.DeliveryTask{
				Level:   "info",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title"}},
			},
			prefix: "**Title**\n\n",
		},
		{
			name: "without content",
			task: &core.DeliveryTask{
				Level:   "info",
				Payload: core.DeliveryPayload{},
			},
			prefix: "****\n\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := wecomProvider.formatMarkdown(tc.task)
			if !strings.HasPrefix(content, tc.prefix) {
				t.Errorf("expected prefix %q, got %q", tc.prefix, content)
			}
			if !strings.HasSuffix(content, "\n\n> ") && !strings.Contains(content, "\n\n> ") {
				t.Errorf("expected timestamp suffix, got %q", content)
			}
		})
	}
}

func TestBuildMessage(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	wecomProvider := provider.(*Provider)

	task := &core.DeliveryTask{
		Level: "error",
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "Title", Body: "Body"},
		},
	}

	message := wecomProvider.buildMessage(task)
	if message.MsgType != "markdown" {
		t.Errorf("expected msg type markdown, got %s", message.MsgType)
	}
	if message.Markdown == nil {
		t.Fatal("expected markdown content")
	}
	if !strings.HasPrefix(message.Markdown.Content, "<font color='warning'>") {
		t.Errorf("expected markdown content, got %q", message.Markdown.Content)
	}
}

func TestProviderDeliver(t *testing.T) {
	t.Run("successful delivery", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}

			var message Message
			if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
				t.Errorf("failed to decode message: %v", err)
			}
			if message.MsgType != "markdown" {
				t.Errorf("expected msg type markdown, got %s", message.MsgType)
			}
			if message.Markdown == nil {
				t.Fatal("expected markdown content")
			}
			if !strings.HasPrefix(message.Markdown.Content, "<font color='warning'>**Test Alert**</font>") {
				t.Errorf("unexpected content: %q", message.Markdown.Content)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})

		task := &core.DeliveryTask{
			ID:        "task-1",
			Level:     "error",
			Provider:  "wecom",
			CreatedAt: time.Now(),
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test Alert", Body: "Test body"},
			},
		}

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("wecom error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(context.Background(), task)
		if err == nil {
			t.Error("expected error for wecom API error response")
		}
	})

	t.Run("invalid response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(context.Background(), task)
		if err == nil {
			t.Error("expected error for invalid response body")
		}
	})

	t.Run("server returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		if err := provider.Deliver(context.Background(), task); err == nil {
			t.Error("expected error for 500 status")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		if err := provider.Deliver(ctx, task); err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

func TestFactory(t *testing.T) {
	factory := &Factory{}

	if factory.Name() != "wecom" {
		t.Errorf("expected factory name wecom, got %s", factory.Name())
	}
	if factory.Type() != "wecom" {
		t.Errorf("expected factory type wecom, got %s", factory.Type())
	}

	provider, err := factory.Create(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "wecom" {
		t.Errorf("expected provider name wecom, got %s", provider.Name())
	}
}
