package slack

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
			"webhook_url": "http://example.com/slack",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		slackProvider := provider.(*Provider)
		if slackProvider.webhookURL != "http://example.com/slack" {
			t.Errorf("expected webhook url http://example.com/slack, got %s", slackProvider.webhookURL)
		}
	})

	t.Run("missing webhook_url", func(t *testing.T) {
		provider, err := NewProvider(map[string]interface{}{})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		slackProvider := provider.(*Provider)
		if slackProvider.webhookURL != "" {
			t.Errorf("expected empty webhook url, got %s", slackProvider.webhookURL)
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"webhook_url": "http://example.com/slack",
	}

	provider, _ := NewProvider(config)
	slackProvider := provider.(*Provider)
	retrieved := slackProvider.GetConfig()

	if retrieved["webhook_url"] != "http://example.com/slack" {
		t.Errorf("expected webhook url http://example.com/slack, got %v", retrieved["webhook_url"])
	}
}

func TestProviderCapability(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{})
	slackProvider := provider.(*Provider)

	capability := slackProvider.Capability()
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
	provider, _ := NewProvider(map[string]interface{}{})
	if provider.Name() != "slack" {
		t.Errorf("expected name slack, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{})
	if provider.Type() != "slack" {
		t.Errorf("expected type slack, got %s", provider.Type())
	}
}

func TestProviderStatus(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{})
	status := provider.Status()

	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "slack" {
		t.Errorf("expected name slack, got %s", status.Name)
	}
	if status.Type != "slack" {
		t.Errorf("expected type slack, got %s", status.Type)
	}
	if status.Status != "available" {
		t.Errorf("expected status available, got %s", status.Status)
	}
	if status.Since.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestProviderClose(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{})
	slackProvider := provider.(*Provider)
	if err := slackProvider.Close(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestGetColor(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{})
	slackProvider := provider.(*Provider)

	cases := map[string]string{
		"error":   "danger",
		"warning": "warning",
		"info":    "#36a64f",
		"":        "#808080",
	}
	for level, expected := range cases {
		if got := slackProvider.getColor(level); got != expected {
			t.Errorf("level %q: expected %s, got %s", level, expected, got)
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

func TestBuildPayload(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{})
	slackProvider := provider.(*Provider)

	t.Run("with level and provider", func(t *testing.T) {
		task := &core.DeliveryTask{
			Level:     "warning",
			Provider:  "slack",
			CreatedAt: time.Now(),
		}
		payload := slackProvider.buildPayload(task)

		if payload.Username != "Herald" {
			t.Errorf("expected username Herald, got %s", payload.Username)
		}
		if len(payload.Attachments) != 1 {
			t.Fatalf("expected 1 attachment, got %d", len(payload.Attachments))
		}
		att := payload.Attachments[0]
		if att.Color != "warning" {
			t.Errorf("expected color warning, got %s", att.Color)
		}
		if len(att.Fields) != 2 {
			t.Errorf("expected 2 fields, got %d", len(att.Fields))
		}
		if att.Fields[0].Title != "Level" || att.Fields[0].Value != "warning" {
			t.Errorf("unexpected level field: %+v", att.Fields[0])
		}
		if att.Fields[1].Title != "Provider" || att.Fields[1].Value != "slack" {
			t.Errorf("unexpected provider field: %+v", att.Fields[1])
		}
	})

	t.Run("without level and provider", func(t *testing.T) {
		task := &core.DeliveryTask{}
		payload := slackProvider.buildPayload(task)

		if len(payload.Attachments[0].Fields) != 0 {
			t.Errorf("expected 0 fields, got %d", len(payload.Attachments[0].Fields))
		}
	})
}

func TestProviderDeliver(t *testing.T) {
	t.Run("successful delivery", func(t *testing.T) {
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
			if len(payload.Attachments) != 1 {
				t.Fatalf("expected 1 attachment, got %d", len(payload.Attachments))
			}
			att := payload.Attachments[0]
			if att.Title != "Test Alert" {
				t.Errorf("expected title Test Alert, got %s", att.Title)
			}
			if att.Text != "Test body" {
				t.Errorf("expected text Test body, got %s", att.Text)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"webhook_url": server.URL,
		})

		task := &core.DeliveryTask{
			ID:       "task-1",
			Level:    "error",
			Provider: "slack",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test Alert", Body: "Test body"},
			},
			CreatedAt: time.Now(),
		}

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("slack error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_payload"}`))
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
			t.Error("expected error for slack API error response")
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

	t.Run("empty webhook url", func(t *testing.T) {
		provider, _ := NewProvider(map[string]interface{}{})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		if err := provider.Deliver(context.Background(), task); err == nil {
			t.Error("expected error for empty webhook url")
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

	if factory.Name() != "slack" {
		t.Errorf("expected factory name slack, got %s", factory.Name())
	}
	if factory.Type() != "slack" {
		t.Errorf("expected factory type slack, got %s", factory.Type())
	}

	provider, err := factory.Create(map[string]interface{}{
		"webhook_url": "http://example.com/slack",
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "slack" {
		t.Errorf("expected provider name slack, got %s", provider.Name())
	}
}
