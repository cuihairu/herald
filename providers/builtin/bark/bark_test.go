package bark

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
			"device_key": "test-key",
			"group":      "herald",
			"ttl":        600,
			"sound":      "minuet",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		barkProvider := provider.(*Provider)
		if barkProvider.deviceKey != "test-key" {
			t.Errorf("expected device key, got %s", barkProvider.deviceKey)
		}
		if barkProvider.server != defaultServer {
			t.Errorf("expected default server %s, got %s", defaultServer, barkProvider.server)
		}
		if barkProvider.group != "herald" {
			t.Errorf("expected group herald, got %s", barkProvider.group)
		}
		if barkProvider.ttl != 600 {
			t.Errorf("expected ttl 600, got %d", barkProvider.ttl)
		}
		if barkProvider.sound != "minuet" {
			t.Errorf("expected sound minuet, got %s", barkProvider.sound)
		}
	})

	t.Run("ttl as int64 and float64", func(t *testing.T) {
		// yaml unmarshals into int64; encoding/json into float64.
		provider, err := NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"ttl":        int64(3600),
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got := provider.(*Provider).ttl; got != 3600 {
			t.Errorf("expected ttl 3600 from int64, got %d", got)
		}

		provider, err = NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"ttl":        float64(1800),
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if got := provider.(*Provider).ttl; got != 1800 {
			t.Errorf("expected ttl 1800 from float64, got %d", got)
		}
	})

	t.Run("missing device_key", func(t *testing.T) {
		_, err := NewProvider(map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing device_key")
		}
	})

	t.Run("empty device_key", func(t *testing.T) {
		config := map[string]interface{}{
			"device_key": "",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty device_key")
		}
	})

	t.Run("custom server", func(t *testing.T) {
		config := map[string]interface{}{
			"device_key": "test-key",
			"server":     "https://bark.example.com",
		}

		provider, _ := NewProvider(config)
		barkProvider := provider.(*Provider)
		if barkProvider.server != "https://bark.example.com" {
			t.Errorf("expected custom server, got %s", barkProvider.server)
		}
	})

	t.Run("level_sound map", func(t *testing.T) {
		config := map[string]interface{}{
			"device_key": "test-key",
			"level_sound": map[string]interface{}{
				"error":   "alarm",
				"warning": "bell",
			},
		}

		provider, _ := NewProvider(config)
		barkProvider := provider.(*Provider)
		if barkProvider.levelSound["error"] != "alarm" {
			t.Errorf("expected error sound alarm, got %s", barkProvider.levelSound["error"])
		}
		if barkProvider.levelSound["warning"] != "bell" {
			t.Errorf("expected warning sound bell, got %s", barkProvider.levelSound["warning"])
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]interface{}{
		"device_key": "test-key",
		"group":      "herald",
		"url":        "https://example.com",
	}

	provider, _ := NewProvider(config)
	barkProvider := provider.(*Provider)
	retrieved := barkProvider.GetConfig()

	if retrieved["device_key"] != "test-key" {
		t.Errorf("expected device key, got %v", retrieved["device_key"])
	}
	if retrieved["group"] != "herald" {
		t.Errorf("expected group, got %v", retrieved["group"])
	}
	if retrieved["url"] != "https://example.com" {
		t.Errorf("expected url, got %v", retrieved["url"])
	}
	if _, ok := retrieved["level_sound"]; !ok {
		t.Errorf("expected level_sound to be present in GetConfig")
	}
	// device_key must be masked when the config goes through the API layer
	masked := core.MaskConfig(retrieved)
	if masked["device_key"] != "******" {
		t.Errorf("expected device_key to be masked, got %v", masked["device_key"])
	}
}

func TestProviderCapability(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"device_key": "test-key",
	})
	barkProvider := provider.(*Provider)

	capability := barkProvider.Capability()
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
	provider, _ := NewProvider(map[string]interface{}{"device_key": "k"})
	if provider.Name() != "bark" {
		t.Errorf("expected name bark, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{"device_key": "k"})
	if provider.Type() != "bark" {
		t.Errorf("expected type bark, got %s", provider.Type())
	}
}

func TestProviderStatus(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{"device_key": "k"})
	status := provider.Status()

	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "bark" {
		t.Errorf("expected name bark, got %s", status.Name)
	}
	if status.Type != "bark" {
		t.Errorf("expected type bark, got %s", status.Type)
	}
	if status.Status != "available" {
		t.Errorf("expected status available, got %s", status.Status)
	}
	if status.Since.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestProviderClose(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{"device_key": "k"})
	barkProvider := provider.(*Provider)
	if err := barkProvider.Close(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestResolveSound(t *testing.T) {
	provider := &Provider{
		sound: "minuet",
		levelSound: map[string]string{
			"error":   "alarm",
			"warning": "bell",
		},
	}

	cases := []struct {
		level    string
		expected string
	}{
		{"error", "alarm"},
		{"warning", "bell"},
		{"info", "minuet"},
		{"debug", "minuet"},
	}

	for _, tc := range cases {
		if got := provider.resolveSound(tc.level); got != tc.expected {
			t.Errorf("level %s: expected sound %q, got %q", tc.level, tc.expected, got)
		}
	}

	// Without a level match the configured default sound applies.
	plain := &Provider{sound: "minuet"}
	for _, level := range []string{"error", "warning", "info"} {
		if got := plain.resolveSound(level); got != "minuet" {
			t.Errorf("level %s: expected default minuet, got %q", level, got)
		}
	}
}

func TestBuildPayload(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"device_key": "test-key",
		"group":      "herald",
		"ttl":        600,
		"sound":      "minuet",
		"url":        "https://example.com",
	})
	barkProvider := provider.(*Provider)

	task := &core.DeliveryTask{
		Level: "error",
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "Title", Body: "Body"},
		},
	}

	payload := barkProvider.buildPayload(task)
	if payload.DeviceKey != "test-key" {
		t.Errorf("expected device key test-key, got %s", payload.DeviceKey)
	}
	if payload.Title != "Title" {
		t.Errorf("expected title Title, got %s", payload.Title)
	}
	if payload.Body != "Body" {
		t.Errorf("expected body Body, got %s", payload.Body)
	}
	if payload.Group != "herald" {
		t.Errorf("expected group herald, got %s", payload.Group)
	}
	if payload.TTL != 600 {
		t.Errorf("expected ttl 600, got %d", payload.TTL)
	}
	// The configured default sound applies when level_sound has no
	// entry for the task level.
	if payload.Sound != "minuet" {
		t.Errorf("expected default sound minuet, got %s", payload.Sound)
	}
	if payload.URL != "https://example.com" {
		t.Errorf("expected url, got %s", payload.URL)
	}
}

func TestBuildPayloadLevelSoundOverride(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{
		"device_key": "test-key",
		"sound":      "minuet",
		"level_sound": map[string]interface{}{
			"error": "alarm",
		},
	})
	barkProvider := provider.(*Provider)

	task := &core.DeliveryTask{
		Level: "error",
		Payload: core.DeliveryPayload{
			Content: &core.RenderedContent{Title: "T"},
		},
	}
	if got := barkProvider.buildPayload(task).Sound; got != "alarm" {
		t.Errorf("expected error level sound alarm, got %s", got)
	}

	task.Level = "info"
	if got := barkProvider.buildPayload(task).Sound; got != "minuet" {
		t.Errorf("expected non-matching level to keep default minuet, got %s", got)
	}
}

func TestBuildPayloadWithoutContent(t *testing.T) {
	provider, _ := NewProvider(map[string]interface{}{"device_key": "test-key"})
	barkProvider := provider.(*Provider)

	payload := barkProvider.buildPayload(&core.DeliveryTask{Level: "info"})
	if payload.Title != "" || payload.Body != "" {
		t.Errorf("expected empty title and body, got %q/%q", payload.Title, payload.Body)
	}
}

func TestProviderDeliver(t *testing.T) {
	t.Run("successful delivery", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/push" {
				t.Errorf("expected /push path, got %s", r.URL.Path)
			}

			var payload Payload
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("failed to decode payload: %v", err)
			}
			if payload.DeviceKey != "test-key" {
				t.Errorf("expected device key test-key, got %s", payload.DeviceKey)
			}
			if payload.Title != "Test Alert" {
				t.Errorf("expected title Test Alert, got %s", payload.Title)
			}
			if payload.Body != "Test body" {
				t.Errorf("expected body Test body, got %s", payload.Body)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"server":     server.URL,
		})

		task := &core.DeliveryTask{
			ID:        "task-1",
			Level:     "info",
			Provider:  "bark",
			CreatedAt: time.Now(),
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test Alert", Body: "Test body"},
			},
		}

		if err := provider.Deliver(context.Background(), task); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("bark error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":400,"message":"bad device key"}`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"server":     server.URL,
		})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(context.Background(), task)
		if err == nil {
			t.Error("expected error for bark error response")
		}
	})

	t.Run("http error status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"server":     server.URL,
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

	t.Run("network error", func(t *testing.T) {
		// A server that is closed immediately refuses all connections.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"server":     server.URL,
		})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		if err := provider.Deliver(context.Background(), task); err == nil {
			t.Error("expected error for unreachable server")
		}
	})

	t.Run("invalid response body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`not json`))
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"server":     server.URL,
		})

		task := &core.DeliveryTask{
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{Title: "Test"},
			},
			CreatedAt: time.Now(),
		}

		if err := provider.Deliver(context.Background(), task); err == nil {
			t.Error("expected error for invalid response body")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		provider, _ := NewProvider(map[string]interface{}{
			"device_key": "test-key",
			"server":     server.URL,
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

	if factory.Name() != "bark" {
		t.Errorf("expected factory name bark, got %s", factory.Name())
	}
	if factory.Type() != "bark" {
		t.Errorf("expected factory type bark, got %s", factory.Type())
	}

	provider, err := factory.Create(map[string]interface{}{
		"device_key": "test-key",
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "bark" {
		t.Errorf("expected provider name bark, got %s", provider.Name())
	}

	if _, err := factory.Create(map[string]interface{}{}); err == nil {
		t.Error("expected error for missing device_key")
	}
}
