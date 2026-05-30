package webhook

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
			"url": "http://example.com/webhook",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		webhookProvider := provider.(*Provider)
		if webhookProvider.url != "http://example.com/webhook" {
			t.Errorf("expected url http://example.com/webhook, got %s", webhookProvider.url)
		}
		if webhookProvider.method != "POST" {
			t.Errorf("expected default method POST, got %s", webhookProvider.method)
		}
	})

	t.Run("with method", func(t *testing.T) {
		config := map[string]interface{}{
			"url":    "http://example.com/webhook",
			"method": "PUT",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		webhookProvider := provider.(*Provider)
		if webhookProvider.method != "PUT" {
			t.Errorf("expected method PUT, got %s", webhookProvider.method)
		}
	})

	t.Run("with headers", func(t *testing.T) {
		config := map[string]interface{}{
			"url": "http://example.com/webhook",
			"headers": map[string]string{
				"Authorization": "Bearer token",
				"X-Custom":      "value",
			},
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		webhookProvider := provider.(*Provider)
		if len(webhookProvider.headers) != 2 {
			t.Errorf("expected 2 headers, got %d", len(webhookProvider.headers))
		}
		if webhookProvider.headers["Authorization"] != "Bearer token" {
			t.Errorf("expected Authorization header, got %v", webhookProvider.headers)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		config := map[string]interface{}{}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for missing url")
		}
	})

	t.Run("empty url", func(t *testing.T) {
		config := map[string]interface{}{
			"url": "",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty url")
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"url":    "http://example.com/webhook",
		"method": "PUT",
		"headers": map[string]string{
			"X-Auth": "token",
		},
	}

	provider, _ := NewProvider(config)
	webhookProvider := provider.(*Provider)
	retrievedConfig := webhookProvider.GetConfig()

	if retrievedConfig["url"] != "http://example.com/webhook" {
		t.Errorf("expected url http://example.com/webhook, got %v", retrievedConfig["url"])
	}
	if retrievedConfig["method"] != "PUT" {
		t.Errorf("expected method PUT, got %v", retrievedConfig["method"])
	}
}

func TestProviderCapability(t *testing.T) {
	config := map[string]interface{}{
		"url": "http://example.com/webhook",
	}

	provider, _ := NewProvider(config)
	webhookProvider := provider.(*Provider)
	capability := webhookProvider.Capability()

	if len(capability.PayloadKinds) != 2 {
		t.Errorf("expected 2 payload kinds, got %d", len(capability.PayloadKinds))
	}

	if len(capability.ContentFormats) != 1 {
		t.Errorf("expected 1 content format, got %d", len(capability.ContentFormats))
	}

	if capability.ContentFormats[0] != "json" {
		t.Errorf("expected json format, got %s", capability.ContentFormats[0])
	}
}

func TestProviderName(t *testing.T) {
	config := map[string]interface{}{
		"url": "http://example.com/webhook",
	}

	provider, _ := NewProvider(config)
	if provider.Name() != "webhook" {
		t.Errorf("expected name webhook, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	config := map[string]interface{}{
		"url": "http://example.com/webhook",
	}

	provider, _ := NewProvider(config)
	if provider.Type() != "webhook" {
		t.Errorf("expected type webhook, got %s", provider.Type())
	}
}

func TestProviderStatus(t *testing.T) {
	config := map[string]interface{}{
		"url": "http://example.com/webhook",
	}

	provider, _ := NewProvider(config)
	status := provider.Status()

	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "webhook" {
		t.Errorf("expected name webhook, got %s", status.Name)
	}
	if status.Type != "webhook" {
		t.Errorf("expected type webhook, got %s", status.Type)
	}
	if status.Status != "available" {
		t.Errorf("expected status available, got %s", status.Status)
	}
	if status.Since.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestProviderClose(t *testing.T) {
	config := map[string]interface{}{
		"url": "http://example.com/webhook",
	}

	provider, _ := NewProvider(config)
	webhookProvider := provider.(*Provider)
	err := webhookProvider.Close()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestProviderDeliver(t *testing.T) {
	t.Run("successful POST", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}

			var payload WebhookPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode payload: %v", err)
			}

			if payload.ID != "task-123" {
				t.Errorf("expected ID task-123, got %s", payload.ID)
			}
			if payload.Title != "Test Alert" {
				t.Errorf("expected title 'Test Alert', got %s", payload.Title)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url": server.URL,
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID:      "task-123",
			Level:   "warning",
			Targets: []string{"channel1"},
			Provider: "webhook",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test Alert",
					Body:  "Test body",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("with PUT method (uses POST via PostJSON)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Note: Current implementation uses PostJSON which always sends POST
			// regardless of method setting
			if r.Method != "POST" {
				t.Errorf("expected POST (implementation uses PostJSON), got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url":    server.URL,
			"method": "PUT",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID: "task-456",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("with raw payload", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload WebhookPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode payload: %v", err)
			}

			if payload.Raw == nil {
				t.Error("expected raw payload")
			}
			if payload.Raw["key"] != "value" {
				t.Errorf("expected raw payload key=value, got %v", payload.Raw)
			}

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url": server.URL,
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID: "task-789",
			Payload: core.DeliveryPayload{
				Raw: map[string]any{
					"key": "value",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("server returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url": server.URL,
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID: "task-error",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err == nil {
			t.Error("expected error for 500 status")
		}
	})

	t.Run("unsupported GET method", func(t *testing.T) {
		config := map[string]interface{}{
			"url":    "http://example.com/webhook",
			"method": "GET",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID: "task-get",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err == nil {
			t.Error("expected error for GET method")
		}
	})

	t.Run("unsupported DELETE method", func(t *testing.T) {
		config := map[string]interface{}{
			"url":    "http://example.com/webhook",
			"method": "DELETE",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID: "task-delete",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err == nil {
			t.Error("expected error for DELETE method")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url": server.URL,
		}

		provider, _ := NewProvider(config)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		task := &core.DeliveryTask{
			ID: "task-cancel",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err == nil {
			t.Error("expected error for cancelled context")
		}
	})
}

func TestExtractContent(t *testing.T) {
	t.Run("with content", func(t *testing.T) {
		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test Title",
					Body:  "Test Body",
				},
			},
		}

		title, body := extractContent(task)
		if title != "Test Title" {
			t.Errorf("expected title 'Test Title', got %s", title)
		}
		if body != "Test Body" {
			t.Errorf("expected body 'Test Body', got %s", body)
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

	t.Run("empty content", func(t *testing.T) {
		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{},
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

func TestFactory(t *testing.T) {
	factory := &Factory{}

	if factory.Name() != "webhook" {
		t.Errorf("expected factory name webhook, got %s", factory.Name())
	}

	if factory.Type() != "webhook" {
		t.Errorf("expected factory type webhook, got %s", factory.Type())
	}

	config := map[string]interface{}{
		"url": "http://example.com/webhook",
	}

	provider, err := factory.Create(config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "webhook" {
		t.Errorf("expected provider name webhook, got %s", provider.Name())
	}
}

func TestWebhookPayload(t *testing.T) {
	payload := &WebhookPayload{
		ID:        "task-123",
		Provider:  "webhook",
		Level:     "warning",
		Targets:   []string{"channel1"},
		Timestamp: "2024-01-01T00:00:00Z",
		Title:     "Test",
		Body:      "Test body",
		Raw:       map[string]any{"key": "value"},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	var decoded WebhookPayload
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if decoded.ID != payload.ID {
		t.Errorf("expected ID %s, got %s", payload.ID, decoded.ID)
	}
	if decoded.Title != payload.Title {
		t.Errorf("expected Title %s, got %s", payload.Title, decoded.Title)
	}
}
