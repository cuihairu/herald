package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNewProvider(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_url": "http://example.com/webhook",
			"sign_secret": "secret",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		feishuProvider := provider.(*Provider)
		if feishuProvider.webhookURL != "http://example.com/webhook" {
			t.Errorf("expected webhook url, got %s", feishuProvider.webhookURL)
		}
		if feishuProvider.signSecret != "secret" {
			t.Errorf("expected sign secret, got %s", feishuProvider.signSecret)
		}
	})

	t.Run("missing webhook_url", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing webhook_url")
		}
	})

	t.Run("empty webhook_url", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_url": "",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty webhook_url")
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
		"sign_secret": "secret",
	}

	provider, _ := NewProvider(config)
	feishuProvider := provider.(*Provider)
	retrieved := feishuProvider.GetConfig()

	if retrieved["webhook_url"] != "http://example.com/webhook" {
		t.Errorf("expected webhook url, got %v", retrieved["webhook_url"])
	}
	if retrieved["sign_secret"] != "secret" {
		t.Errorf("expected sign secret, got %v", retrieved["sign_secret"])
	}
}

func TestProviderCapability(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	feishuProvider := provider.(*Provider)

	capability := feishuProvider.Capability()
	if len(capability.PayloadKinds) != 1 {
		t.Errorf("expected 1 payload kind, got %d", len(capability.PayloadKinds))
	}
	if capability.PayloadKinds[0] != core.PayloadContent {
		t.Errorf("expected content payload kind, got %s", capability.PayloadKinds[0])
	}
	if len(capability.ContentFormats) != 1 {
		t.Errorf("expected 1 content format, got %d", len(capability.ContentFormats))
	}
	if capability.ContentFormats[0] != "plain" {
		t.Errorf("expected plain format, got %s", capability.ContentFormats[0])
	}
}

func TestProviderName(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	if provider.Name() != "feishu" {
		t.Errorf("expected name feishu, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	if provider.Type() != "feishu" {
		t.Errorf("expected type feishu, got %s", provider.Type())
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
	if status.Name != "feishu" {
		t.Errorf("expected name feishu, got %s", status.Name)
	}
	if status.Type != "feishu" {
		t.Errorf("expected type feishu, got %s", status.Type)
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
	feishuProvider := provider.(*Provider)
	if err := feishuProvider.Close(); err != nil {
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

func TestFormatMessage(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	feishuProvider := provider.(*Provider)

	cases := []struct {
		name     string
		task     *core.DeliveryTask
		expected string
	}{
		{
			name: "error level",
			task: &core.DeliveryTask{
				Level: "error",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title", Body: "Body"},
				},
			},
			expected: "[错误] Title\n\nBody",
		},
		{
			name: "warning level",
			task: &core.DeliveryTask{
				Level: "warning",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title", Body: "Body"},
				},
			},
			expected: "[警告] Title\n\nBody",
		},
		{
			name: "info level",
			task: &core.DeliveryTask{
				Level: "info",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title", Body: "Body"},
				},
			},
			expected: "[信息] Title\n\nBody",
		},
		{
			name: "unknown level",
			task: &core.DeliveryTask{
				Level: "debug",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title", Body: "Body"},
				},
			},
			expected: "Title\n\nBody",
		},
		{
			name: "empty body",
			task: &core.DeliveryTask{
				Level: "error",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title"},
				},
			},
			expected: "[错误] Title\n\n",
		},
		{
			name: "without content",
			task: &core.DeliveryTask{
				Level:   "error",
				Payload: core.DeliveryPayload{},
			},
			expected: "[错误] \n\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := feishuProvider.formatMessage(tc.task); got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestBuildMessage(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	feishuProvider := provider.(*Provider)

	task := &core.DeliveryTask{
		Level: "error",
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "Title", Body: "Body"},
		},
	}

	message := feishuProvider.buildMessage(task)
	if message.MsgType != "text" {
		t.Errorf("expected msg type text, got %s", message.MsgType)
	}
	text, ok := message.Content.(TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", message.Content)
	}
	if text.Text != "[错误] Title\n\nBody" {
		t.Errorf("unexpected text: %q", text.Text)
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
			if message.MsgType != "text" {
				t.Errorf("expected msg type text, got %s", message.MsgType)
			}
			content, ok := message.Content.(map[string]interface{})
			if !ok {
				t.Fatalf("expected map content, got %T", message.Content)
			}
			if content["text"] != "[错误] Test Alert\n\nTest body" {
				t.Errorf("unexpected text: %v", content["text"])
			}

			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"code":0,"msg":"success"}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})

		task := &core.DeliveryTask{
			ID:        "task-1",
			Level:     "error",
			Provider:  "feishu",
			CreatedAt: time.Now(),
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test Alert", Body: "Test body"},
			},
		}

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("feishu error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"code":19021,"msg":"sign match fail"}`))
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
			t.Error("expected error for feishu API error response")
		}
	})

	t.Run("invalid response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`not json`))
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

	if factory.Name() != "feishu" {
		t.Errorf("expected factory name feishu, got %s", factory.Name())
	}
	if factory.Type() != "feishu" {
		t.Errorf("expected factory type feishu, got %s", factory.Type())
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
	if provider.Name() != "feishu" {
		t.Errorf("expected provider name feishu, got %s", provider.Name())
	}
}
