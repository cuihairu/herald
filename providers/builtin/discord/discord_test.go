package discord

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
	t.Run("webhook_url", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_url": "http://example.com/webhook",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		discordProvider := provider.(*Provider)
		if discordProvider.webhookURL != "http://example.com/webhook" {
			t.Errorf("expected webhook url, got %s", discordProvider.webhookURL)
		}
	})

	t.Run("webhook_id and webhook_token", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_id":    "123456",
			"webhook_token": "abc-def",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		discordProvider := provider.(*Provider)
		expected := "https://discord.com/api/webhooks/123456/abc-def"
		if discordProvider.webhookURL != expected {
			t.Errorf("expected %s, got %s", expected, discordProvider.webhookURL)
		}
	})

	t.Run("webhook_id without webhook_token", func(t *testing.T) {
		config := map[string]interface{}{
			"webhook_id": "123456",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for missing webhook_token")
		}
	})

	t.Run("bot_token and channel_id", func(t *testing.T) {
		config := map[string]interface{}{
			"bot_token":  "bot-token",
			"channel_id": "123456",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		discordProvider := provider.(*Provider)
		if discordProvider.webhookURL != "" {
			t.Errorf("expected empty webhook url, got %s", discordProvider.webhookURL)
		}
		if discordProvider.botToken != "bot-token" {
			t.Errorf("expected bot token, got %s", discordProvider.botToken)
		}
		if discordProvider.channelID != "123456" {
			t.Errorf("expected channel id 123456, got %s", discordProvider.channelID)
		}
	})

	t.Run("missing all config", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{})
		if err == nil {
			t.Error("expected error for empty config")
		}
	})

	t.Run("bot_token without channel_id", func(t *testing.T) {
		config := map[string]interface{}{
			"bot_token": "bot-token",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for missing channel_id")
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
		"bot_token":   "bot-token",
		"channel_id":  "123456",
	}

	provider, _ := NewProvider(config)
	discordProvider := provider.(*Provider)
	retrieved := discordProvider.GetConfig()

	if retrieved["webhook_url"] != "http://example.com/webhook" {
		t.Errorf("expected webhook url, got %v", retrieved["webhook_url"])
	}
	if retrieved["bot_token"] != "bot-token" {
		t.Errorf("expected bot token, got %v", retrieved["bot_token"])
	}
	if retrieved["channel_id"] != "123456" {
		t.Errorf("expected channel id, got %v", retrieved["channel_id"])
	}
}

func TestProviderCapability(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	discordProvider := provider.(*Provider)

	capability := discordProvider.Capability()
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
	if provider.Name() != "discord" {
		t.Errorf("expected name discord, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	if provider.Type() != "discord" {
		t.Errorf("expected type discord, got %s", provider.Type())
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
	if status.Name != "discord" {
		t.Errorf("expected name discord, got %s", status.Name)
	}
	if status.Type != "discord" {
		t.Errorf("expected type discord, got %s", status.Type)
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
	discordProvider := provider.(*Provider)
	if err := discordProvider.Close(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestGetColor(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"webhook_url": "http://example.com/webhook",
	})
	discordProvider := provider.(*Provider)

	cases := map[string]int{
		"error":   16711680,
		"warning": 16776960,
		"info":    3447003,
		"":        9807270,
	}
	for level, expected := range cases {
		if got := discordProvider.getColor(level); got != expected {
			t.Errorf("level %q: expected %d, got %d", level, expected, got)
		}
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

func TestProviderDeliver(t *testing.T) {
	t.Run("webhook success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}

			var payload WebhookPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode payload: %v", err)
			}
			if payload.Username != "Herald" {
				t.Errorf("expected username Herald, got %s", payload.Username)
			}
			if len(payload.Embeds) != 1 {
				t.Fatalf("expected 1 embed, got %d", len(payload.Embeds))
			}
			embed := payload.Embeds[0]
			if embed.Title != "Test Alert" {
				t.Errorf("expected title Test Alert, got %s", embed.Title)
			}
			if embed.Description != "Test body" {
				t.Errorf("expected description Test body, got %s", embed.Description)
			}

			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})

		task := &core.DeliveryTask{
			ID:        "task-1",
			Level:     "error",
			CreatedAt: time.Now(),
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test Alert", Body: "Test body"},
			},
		}

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("webhook error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"code":50035,"message":"Invalid Form Body"}`))
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
			t.Error("expected error for discord API error response")
		}
	})

	t.Run("non-json response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
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

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
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

	t.Run("webhook preferred over bot api", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
			"bot_token":   "bot-token",
			"channel_id":  "123456",
		})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("bot api request failure", func(t *testing.T) {
		provider, _ := NewProvider(map[string]interface{}{
			"bot_token":  "bot-token",
			"channel_id": "123456",
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

	if factory.Name() != "discord" {
		t.Errorf("expected factory name discord, got %s", factory.Name())
	}
	if factory.Type() != "discord" {
		t.Errorf("expected factory type discord, got %s", factory.Type())
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
	if provider.Name() != "discord" {
		t.Errorf("expected provider name discord, got %s", provider.Name())
	}
}
