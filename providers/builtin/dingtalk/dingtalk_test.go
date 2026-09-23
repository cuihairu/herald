package dingtalk

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
		dingtalkProvider := provider.(*Provider)
		if dingtalkProvider.webhookURL != "http://example.com/webhook" {
			t.Errorf("expected webhook url, got %s", dingtalkProvider.webhookURL)
		}
	})

	t.Run("access_token", func(t *testing.T) {
		config := map[string]interface{}{
			"access_token": "abc123",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		dingtalkProvider := provider.(*Provider)
		expected := "https://oapi.dingtalk.com/robot/send?access_token=abc123"
		if dingtalkProvider.webhookURL != expected {
			t.Errorf("expected %s, got %s", expected, dingtalkProvider.webhookURL)
		}
	})

	t.Run("missing webhook_url and access_token", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing config")
		}
	})

	t.Run("empty webhook_url and access_token", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_url":  "",
			"access_token": "",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty config")
		}
	})

	t.Run("with secret", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_url": "http://example.com/webhook",
			"secret":      "SEC123",
		}

		provider, _ := NewProvider(config)
		dingtalkProvider := provider.(*Provider)
		if dingtalkProvider.secret != "SEC123" {
			t.Errorf("expected secret SEC123, got %s", dingtalkProvider.secret)
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
		"secret":      "SEC123",
	}

	provider, _ := NewProvider(config)
	dingtalkProvider := provider.(*Provider)
	retrieved := dingtalkProvider.GetConfig()

	if retrieved["webhook_url"] != "http://example.com/webhook" {
		t.Errorf("expected webhook url, got %v", retrieved["webhook_url"])
	}
	if retrieved["secret"] != "SEC123" {
		t.Errorf("expected secret, got %v", retrieved["secret"])
	}
}

func TestProviderCapability(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	dingtalkProvider := provider.(*Provider)

	capability := dingtalkProvider.Capability()
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
	if provider.Name() != "dingtalk" {
		t.Errorf("expected name dingtalk, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	if provider.Type() != "dingtalk" {
		t.Errorf("expected type dingtalk, got %s", provider.Type())
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
	if status.Name != "dingtalk" {
		t.Errorf("expected name dingtalk, got %s", status.Name)
	}
	if status.Type != "dingtalk" {
		t.Errorf("expected type dingtalk, got %s", status.Type)
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
	dingtalkProvider := provider.(*Provider)
	if err := dingtalkProvider.Close(); err != nil {
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
	dingtalkProvider := provider.(*Provider)

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
			prefix: "### <font color='#ff0000'>Title</font>\n\nBody\n\n",
		},
		{
			name: "warning level",
			task: &core.DeliveryTask{
				Level:   "warning",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title", Body: "Body"}},
			},
			prefix: "### <font color='#ff9900'>Title</font>\n\nBody\n\n",
		},
		{
			name: "info level",
			task: &core.DeliveryTask{
				Level:   "info",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title", Body: "Body"}},
			},
			prefix: "### Title\n\nBody\n\n",
		},
		{
			name: "unknown level",
			task: &core.DeliveryTask{
				Level:   "debug",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title", Body: "Body"}},
			},
			prefix: "### Title\n\nBody\n\n",
		},
		{
			name: "empty body",
			task: &core.DeliveryTask{
				Level:   "info",
				Payload: core.DeliveryPayload{Content: &core.RenderedContent{Title: "Title"}},
			},
			prefix: "### Title\n\n",
		},
		{
			name: "without content",
			task: &core.DeliveryTask{
				Level:   "info",
				Payload: core.DeliveryPayload{},
			},
			prefix: "### \n\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := dingtalkProvider.formatMarkdown(tc.task)
			if !strings.HasPrefix(content, tc.prefix) {
				t.Errorf("expected prefix %q, got %q", tc.prefix, content)
			}
			if !strings.Contains(content, "---\n\n> ") {
				t.Errorf("expected separator and timestamp, got %q", content)
			}
		})
	}
}

func TestBuildMessage(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	dingtalkProvider := provider.(*Provider)

	task := &core.DeliveryTask{
		Level: "error",
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "Title", Body: "Body"},
		},
	}

	message := dingtalkProvider.buildMessage(task)
	if message.MsgType != "markdown" {
		t.Errorf("expected msg type markdown, got %s", message.MsgType)
	}
	if message.Markdown == nil {
		t.Fatal("expected markdown content")
	}
	if message.Markdown.Title != "Title" {
		t.Errorf("expected title Title, got %s", message.Markdown.Title)
	}
	if !strings.HasPrefix(message.Markdown.Text, "### ") {
		t.Errorf("expected markdown text, got %q", message.Markdown.Text)
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
			if message.Markdown.Title != "Test Alert" {
				t.Errorf("expected title Test Alert, got %s", message.Markdown.Title)
			}
			if !strings.HasPrefix(message.Markdown.Text, "### <font color='#ff0000'>Test Alert</font>") {
				t.Errorf("unexpected text: %q", message.Markdown.Text)
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
			Provider:  "dingtalk",
			CreatedAt: time.Now(),
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test Alert", Body: "Test body"},
			},
		}

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("dingtalk error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errcode":310000,"errmsg":"sign not match"}`))
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
			t.Error("expected error for dingtalk API error response")
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

	if factory.Name() != "dingtalk" {
		t.Errorf("expected factory name dingtalk, got %s", factory.Name())
	}
	if factory.Type() != "dingtalk" {
		t.Errorf("expected factory type dingtalk, got %s", factory.Type())
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
	if provider.Name() != "dingtalk" {
		t.Errorf("expected provider name dingtalk, got %s", provider.Name())
	}
}
