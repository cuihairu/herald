package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNewProvider(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		config := map[string]interface{}{
			"token":      "123456:ABC",
			"chat_id":    "-100200300",
			"parse_mode": "Markdown",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		telegramProvider := provider.(*Provider)
		if telegramProvider.token != "123456:ABC" {
			t.Errorf("expected token 123456:ABC, got %s", telegramProvider.token)
		}
		if telegramProvider.chatID != "-100200300" {
			t.Errorf("expected chat id -100200300, got %s", telegramProvider.chatID)
		}
		if telegramProvider.parseMode != "Markdown" {
			t.Errorf("expected parse mode Markdown, got %s", telegramProvider.parseMode)
		}
	})

	t.Run("valid config without parse_mode", func(t *testing.T) {
		config := map[string]interface{}{
			"token":   "123456:ABC",
			"chat_id": "-100200300",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		telegramProvider := provider.(*Provider)
		if telegramProvider.parseMode != "" {
			t.Errorf("expected empty parse mode, got %s", telegramProvider.parseMode)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing token")
		}
	})

	t.Run("empty token", func(t *testing.T) {
		config := map[string]interface{}{
			"token":   "",
			"chat_id": "-100200300",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty token")
		}
	})

	t.Run("missing chat_id", func(t *testing.T) {
		config := map[string]interface{}{
			"token": "123456:ABC",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for missing chat_id")
		}
	})

	t.Run("empty chat_id", func(t *testing.T) {
		config := map[string]interface{}{
			"token":   "123456:ABC",
			"chat_id": "",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty chat_id")
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"token":      "123456:ABC",
		"chat_id":    "-100200300",
		"parse_mode": "HTML",
	}

	provider, _ := NewProvider(config)
	telegramProvider := provider.(*Provider)
	retrieved := telegramProvider.GetConfig()

	if retrieved["token"] != "123456:ABC" {
		t.Errorf("expected token, got %v", retrieved["token"])
	}
	if retrieved["chat_id"] != "-100200300" {
		t.Errorf("expected chat id, got %v", retrieved["chat_id"])
	}
	if retrieved["parse_mode"] != "HTML" {
		t.Errorf("expected parse mode HTML, got %v", retrieved["parse_mode"])
	}
}

func TestProviderCapability(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	telegramProvider := provider.(*Provider)

	capability := telegramProvider.Capability()
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
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	if provider.Name() != "telegram" {
		t.Errorf("expected name telegram, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	if provider.Type() != "telegram" {
		t.Errorf("expected type telegram, got %s", provider.Type())
	}
}

func TestProviderStatus(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	status := provider.Status()

	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "telegram" {
		t.Errorf("expected name telegram, got %s", status.Name)
	}
	if status.Type != "telegram" {
		t.Errorf("expected type telegram, got %s", status.Type)
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
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	telegramProvider := provider.(*Provider)
	if err := telegramProvider.Close(); err != nil {
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
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	telegramProvider := provider.(*Provider)

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
			expected: "🔴 *Title*\n\nBody",
		},
		{
			name: "warning level",
			task: &core.DeliveryTask{
				Level: "warning",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title", Body: "Body"},
				},
			},
			expected: "🟡 *Title*\n\nBody",
		},
		{
			name: "info level",
			task: &core.DeliveryTask{
				Level: "info",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title", Body: "Body"},
				},
			},
			expected: "🔵 *Title*\n\nBody",
		},
		{
			name: "unknown level",
			task: &core.DeliveryTask{
				Level: "debug",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title", Body: "Body"},
				},
			},
			expected: "*Title*\n\nBody",
		},
		{
			name: "empty body",
			task: &core.DeliveryTask{
				Level: "error",
				Payload: core.DeliveryPayload{
					Content: &core.RenderedContent{Title: "Title"},
				},
			},
			expected: "🔴 *Title*\n\n",
		},
		{
			name: "without content",
			task: &core.DeliveryTask{
				Level:   "error",
				Payload: core.DeliveryPayload{},
			},
			expected: "🔴 **\n\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := telegramProvider.formatMessage(tc.task); got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestSendMessage(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	telegramProvider := provider.(*Provider)

	t.Run("request failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := telegramProvider.sendMessage(ctx, "hello")
		if err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

// newStubTelegramAPI starts a Bot API stub; the returned func reports the
// request path and decoded body of the last call.
func newStubTelegramAPI(t *testing.T, response string) (*httptest.Server, func() (string, SendMessageRequest)) {
	t.Helper()
	var last SendMessageRequest
	var path string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &last)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(ts.Close)
	return ts, func() (string, SendMessageRequest) { return path, last }
}

// TestSendMessageCustomAPIURL exercises the success path through a stub Bot
// API, proving api_url overrides the endpoint and the request is well formed.
func TestSendMessageCustomAPIURL(t *testing.T) {
	ts, lastReq := newStubTelegramAPI(t, `{"ok":true}`)

	provider, err := NewProvider(map[string]interface{}{
		"token":      "123456:ABC",
		"chat_id":    "-100200300",
		"parse_mode": "markdown",
		"api_url":    ts.URL,
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	if err := provider.(*Provider).sendMessage(context.Background(), "hello"); err != nil {
		t.Fatalf("sendMessage() error = %v", err)
	}
	path, req := lastReq()
	if want := "/bot123456:ABC/sendMessage"; path != want {
		t.Errorf("request path = %q, want %q", path, want)
	}
	if req.ChatID != "-100200300" || req.Text != "hello" || req.ParseMode != "markdown" {
		t.Errorf("request = %+v, want chat_id -100200300, text hello, parse_mode markdown", req)
	}
}

func TestSendMessageAPIError(t *testing.T) {
	ts, _ := newStubTelegramAPI(t, `{"ok":false,"description":"chat not found"}`)

	provider, _ := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
		"api_url": ts.URL,
	})

	err := provider.(*Provider).sendMessage(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "telegram API error: chat not found") {
		t.Errorf("sendMessage() error = %v, want api error", err)
	}
}

func TestSendMessageBadResponse(t *testing.T) {
	ts, _ := newStubTelegramAPI(t, `not json`)

	provider, _ := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
		"api_url": ts.URL,
	})

	err := provider.(*Provider).sendMessage(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("sendMessage() error = %v, want parse failure", err)
	}
}

// TestProviderAPIURLConfig verifies api_url round-trips through GetConfig.
func TestProviderAPIURLConfig(t *testing.T) {
	provider, err := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
		"api_url": "https://mirror.example.com",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	if got := provider.(*Provider).apiURL; got != "https://mirror.example.com" {
		t.Errorf("apiURL = %q, want https://mirror.example.com", got)
	}
	if got := provider.(*Provider).GetConfig()["api_url"]; got != "https://mirror.example.com" {
		t.Errorf("GetConfig()[api_url] = %v, want the configured mirror", got)
	}
}

func TestProviderDeliver(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task := &core.DeliveryTask{
		ID:        "task-1",
		Level:     "error",
		CreatedAt: time.Now(),
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "Test Alert", Body: "Test body"},
		},
	}

	if err := provider.Deliver(ctx, task); err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestFactory(t *testing.T) {
	factory := &Factory{}

	if factory.Name() != "telegram" {
		t.Errorf("expected factory name telegram, got %s", factory.Name())
	}
	if factory.Type() != "telegram" {
		t.Errorf("expected factory type telegram, got %s", factory.Type())
	}

	provider, err := factory.Create(map[string]interface{}{
		"token":   "123456:ABC",
		"chat_id": "-100200300",
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "telegram" {
		t.Errorf("expected provider name telegram, got %s", provider.Name())
	}
}
