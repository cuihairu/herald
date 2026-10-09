package webhook

import (
	"context"
	"encoding/json"
	"io"
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
			ID:       "task-123",
			Level:    "warning",
			Targets:  []string{"channel1"},
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

	t.Run("with PUT method sends PUT", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				t.Errorf("expected PUT, got %s", r.Method)
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

	t.Run("GET method flattens payload into query params", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Errorf("expected GET, got %s", r.Method)
			}
			q := r.URL.Query()
			if q.Get("id") != "task-get" {
				t.Errorf("expected id task-get, got %q", q.Get("id"))
			}
			if q.Get("title") != "Test" {
				t.Errorf("expected title Test, got %q", q.Get("title"))
			}
			if q.Get("targets") != "channel1" {
				t.Errorf("expected targets channel1, got %q", q.Get("targets"))
			}
			if r.Body != nil {
				if body, _ := io.ReadAll(r.Body); len(body) > 0 {
					t.Errorf("expected empty body for GET, got %q", body)
				}
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID:      "task-get",
			Targets: []string{"channel1"},
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err != nil {
			t.Errorf("expected no error for GET method, got %v", err)
		}
	})

	t.Run("DELETE method sends JSON body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				t.Errorf("expected DELETE, got %s", r.Method)
			}
			var payload WebhookPayload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode payload: %v", err)
			}
			if payload.ID != "task-delete" {
				t.Errorf("expected ID task-delete, got %s", payload.ID)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url":    server.URL,
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
		if err != nil {
			t.Errorf("expected no error for DELETE method, got %v", err)
		}
	})

	t.Run("custom headers are sent", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer token" {
				t.Errorf("expected Authorization header, got %q", got)
			}
			if got := r.Header.Get("X-Custom"); got != "value" {
				t.Errorf("expected X-Custom header, got %q", got)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := map[string]interface{}{
			"url": server.URL,
			"headers": map[string]string{
				"Authorization": "Bearer token",
				"X-Custom":      "value",
			},
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID: "task-headers",
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err != nil {
			t.Errorf("expected no error with custom headers, got %v", err)
		}
	})

	t.Run("invalid method rejected at creation", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{
			"url":    "http://example.com/webhook",
			"method": "PATCH",
		})
		if err == nil {
			t.Error("expected error for unsupported method PATCH")
		}
	})

	t.Run("GET merges into existing query", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("existing") != "keep" {
				t.Errorf("expected existing=keep preserved, got %q", q.Get("existing"))
			}
			if q.Get("id") != "task-merge" {
				t.Errorf("expected id task-merge appended, got %q", q.Get("id"))
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"url":    server.URL + "/hook?existing=keep",
			"method": "GET",
		})

		err := provider.Deliver(context.Background(), &core.DeliveryTask{
			ID:        "task-merge",
			CreatedAt: time.Now(),
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("GET carries raw map as JSON query param", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.URL.Query().Get("raw")
			if raw != `{"key":"value"}` {
				t.Errorf("expected raw JSON query param, got %q", raw)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		})

		err := provider.Deliver(context.Background(), &core.DeliveryTask{
			ID:        "task-get-raw",
			CreatedAt: time.Now(),
			Payload: core.DeliveryPayload{
				Raw: map[string]any{"key": "value"},
			},
		})
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("GET rejects unencodable raw payload", func(t *testing.T) {
		provider, _ := NewProvider(map[string]interface{}{
			"url":    "http://example.com/webhook",
			"method": "GET",
		})

		err := provider.Deliver(context.Background(), &core.DeliveryTask{
			ID:        "task-get-badraw",
			CreatedAt: time.Now(),
			Payload: core.DeliveryPayload{
				Raw: map[string]any{"ch": make(chan int)},
			},
		})
		if err == nil {
			t.Error("expected error for unencodable raw payload")
		}
	})

	t.Run("negative status on GET is classified", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"url":    server.URL,
			"method": "GET",
		})

		err := provider.Deliver(context.Background(), &core.DeliveryTask{
			ID:        "task-get-err",
			CreatedAt: time.Now(),
		})
		if err == nil {
			t.Error("expected error for 500 status on GET")
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
