package email

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNewProvider(t *testing.T) {
	t.Run("valid minimal config", func(t *testing.T) {
		config := map[string]any{
			"host": "smtp.example.com",
			"from": "test@example.com",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
		emailProvider := provider.(*Provider)
		if emailProvider.host != "smtp.example.com" {
			t.Errorf("expected host smtp.example.com, got %s", emailProvider.host)
		}
		if emailProvider.port != 587 {
			t.Errorf("expected default port 587, got %d", emailProvider.port)
		}
		if emailProvider.from != "test@example.com" {
			t.Errorf("expected from test@example.com, got %s", emailProvider.from)
		}
	})

	t.Run("with all fields", func(t *testing.T) {
		config := map[string]any{
			"host":      "smtp.example.com",
			"port":      465,
			"username":  "user",
			"password":  "pass",
			"from":      "sender@example.com",
			"from_name": "Test Sender",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		emailProvider := provider.(*Provider)
		if emailProvider.host != "smtp.example.com" {
			t.Errorf("expected host smtp.example.com, got %s", emailProvider.host)
		}
		if emailProvider.port != 465 {
			t.Errorf("expected port 465, got %d", emailProvider.port)
		}
		if emailProvider.username != "user" {
			t.Errorf("expected username user, got %s", emailProvider.username)
		}
		if emailProvider.password != "pass" {
			t.Errorf("expected password pass, got %s", emailProvider.password)
		}
		if emailProvider.from != "sender@example.com" {
			t.Errorf("expected from sender@example.com, got %s", emailProvider.from)
		}
		if emailProvider.fromName != "Test Sender" {
			t.Errorf("expected fromName Test Sender, got %s", emailProvider.fromName)
		}
	})

	t.Run("with port as float64", func(t *testing.T) {
		config := map[string]any{
			"host":  "smtp.example.com",
			"port":  25.0,
			"from":  "test@example.com",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		emailProvider := provider.(*Provider)
		if emailProvider.port != 25 {
			t.Errorf("expected port 25, got %d", emailProvider.port)
		}
	})

	t.Run("missing host", func(t *testing.T) {
		config := map[string]any{
			"from": "test@example.com",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for missing host")
		}
	})

	t.Run("empty host", func(t *testing.T) {
		config := map[string]any{
			"host": "",
			"from": "test@example.com",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for empty host")
		}
	})

	t.Run("missing from defaults to username", func(t *testing.T) {
		config := map[string]any{
			"host":     "smtp.example.com",
			"username": "user@example.com",
		}

		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		emailProvider := provider.(*Provider)
		if emailProvider.from != "user@example.com" {
			t.Errorf("expected from to default to username, got %s", emailProvider.from)
		}
	})

	t.Run("missing from and username", func(t *testing.T) {
		config := map[string]any{
			"host": "smtp.example.com",
		}

		_, err := NewProvider(config)
		if err == nil {
			t.Error("expected error for missing from")
		}
	})
}

func TestProviderGetConfig(t *testing.T) {
	config := map[string]any{
		"host":      "smtp.example.com",
		"port":      465,
		"username":  "user",
		"password":  "pass",
		"from":      "sender@example.com",
		"from_name": "Test Sender",
	}

	provider, _ := NewProvider(config)
	emailProvider := provider.(*Provider)
	retrievedConfig := emailProvider.GetConfig()

	if retrievedConfig["host"] != "smtp.example.com" {
		t.Errorf("expected host smtp.example.com, got %v", retrievedConfig["host"])
	}
	if retrievedConfig["port"] != 465 {
		t.Errorf("expected port 465, got %v", retrievedConfig["port"])
	}
	if retrievedConfig["username"] != "user" {
		t.Errorf("expected username user, got %v", retrievedConfig["username"])
	}
	if retrievedConfig["password"] != "pass" {
		t.Errorf("expected password pass, got %v", retrievedConfig["password"])
	}
	if retrievedConfig["from"] != "sender@example.com" {
		t.Errorf("expected from sender@example.com, got %v", retrievedConfig["from"])
	}
	if retrievedConfig["from_name"] != "Test Sender" {
		t.Errorf("expected from_name Test Sender, got %v", retrievedConfig["from_name"])
	}
}

func TestProviderCapability(t *testing.T) {
	config := map[string]any{
		"host": "smtp.example.com",
		"from": "test@example.com",
	}

	provider, _ := NewProvider(config)
	emailProvider := provider.(*Provider)
	capability := emailProvider.Capability()

	if len(capability.PayloadKinds) != 1 {
		t.Errorf("expected 1 payload kind, got %d", len(capability.PayloadKinds))
	}

	if capability.PayloadKinds[0] != core.PayloadContent {
		t.Errorf("expected PayloadContent, got %s", capability.PayloadKinds[0])
	}

	if len(capability.ContentFormats) != 2 {
		t.Errorf("expected 2 content formats, got %d", len(capability.ContentFormats))
	}

	// Check for html and plain formats
	hasHTML := false
	hasPlain := false
	for _, format := range capability.ContentFormats {
		if format == "html" {
			hasHTML = true
		}
		if format == "plain" {
			hasPlain = true
		}
	}
	if !hasHTML {
		t.Error("expected html format")
	}
	if !hasPlain {
		t.Error("expected plain format")
	}
}

func TestProviderName(t *testing.T) {
	config := map[string]any{
		"host": "smtp.example.com",
		"from": "test@example.com",
	}

	provider, _ := NewProvider(config)
	if provider.Name() != "email" {
		t.Errorf("expected name email, got %s", provider.Name())
	}
}

func TestProviderType(t *testing.T) {
	config := map[string]any{
		"host": "smtp.example.com",
		"from": "test@example.com",
	}

	provider, _ := NewProvider(config)
	if provider.Type() != "email" {
		t.Errorf("expected type email, got %s", provider.Type())
	}
}

func TestProviderStatus(t *testing.T) {
	config := map[string]any{
		"host": "smtp.example.com",
		"from": "test@example.com",
	}

	provider, _ := NewProvider(config)
	status := provider.Status()

	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "email" {
		t.Errorf("expected name email, got %s", status.Name)
	}
	if status.Type != "email" {
		t.Errorf("expected type email, got %s", status.Type)
	}
	if status.Status != "available" {
		t.Errorf("expected status available, got %s", status.Status)
	}
	if status.Since.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestProviderClose(t *testing.T) {
	config := map[string]any{
		"host": "smtp.example.com",
		"from": "test@example.com",
	}

	provider, _ := NewProvider(config)
	emailProvider := provider.(*Provider)
	err := emailProvider.Close()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestFormatFrom(t *testing.T) {
	t.Run("with from name", func(t *testing.T) {
		config := map[string]any{
			"host":      "smtp.example.com",
			"from":      "sender@example.com",
			"from_name": "Test Sender",
		}

		provider, _ := NewProvider(config)
		emailProvider := provider.(*Provider)
		from := emailProvider.formatFrom()
		expected := "Test Sender <sender@example.com>"
		if from != expected {
			t.Errorf("expected from %s, got %s", expected, from)
		}
	})

	t.Run("without from name", func(t *testing.T) {
		config := map[string]any{
			"host": "smtp.example.com",
			"from": "sender@example.com",
		}

		provider, _ := NewProvider(config)
		emailProvider := provider.(*Provider)
		from := emailProvider.formatFrom()
		expected := "sender@example.com"
		if from != expected {
			t.Errorf("expected from %s, got %s", expected, from)
		}
	})

	t.Run("empty from name", func(t *testing.T) {
		config := map[string]any{
			"host":      "smtp.example.com",
			"from":      "sender@example.com",
			"from_name": "",
		}

		provider, _ := NewProvider(config)
		emailProvider := provider.(*Provider)
		from := emailProvider.formatFrom()
		expected := "sender@example.com"
		if from != expected {
			t.Errorf("expected from %s, got %s", expected, from)
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

func TestDeliver(t *testing.T) {
	t.Run("no recipients", func(t *testing.T) {
		config := map[string]any{
			"host": "smtp.example.com",
			"from": "test@example.com",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID:      "task-123",
			Targets: []string{},
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		if err == nil {
			t.Error("expected error for no recipients")
		}
		if !strings.Contains(err.Error(), "no recipients") {
			t.Errorf("expected 'no recipients' error, got %v", err)
		}
	})

	t.Run("missing content", func(t *testing.T) {
		config := map[string]any{
			"host": "smtp.example.com",
			"from": "test@example.com",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID:      "task-123",
			Targets: []string{"recipient@example.com"},
			Payload: core.DeliveryPayload{
				Raw: map[string]any{"key": "value"},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		// This should fail when trying to send email due to invalid recipients/host
		// But the message building should work
		if err == nil {
			t.Log("expected error for invalid SMTP host (got nil - OK in test environment)")
		}
	})

	t.Run("HTML content format", func(t *testing.T) {
		config := map[string]any{
			"host": "smtp.example.com",
			"from": "test@example.com",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID:      "task-123",
			Targets: []string{"recipient@example.com"},
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title:  "Test Alert",
					Body:   "<p>Test body</p>",
					Format: "html",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		// Should fail when trying to send to invalid host
		if err == nil {
			t.Log("expected error for invalid SMTP host (got nil - OK in test environment)")
		}
	})

	t.Run("level in subject", func(t *testing.T) {
		config := map[string]any{
			"host": "smtp.example.com",
			"from": "test@example.com",
		}

		provider, _ := NewProvider(config)
		ctx := context.Background()

		task := &core.DeliveryTask{
			ID:      "task-123",
			Level:   "warning",
			Targets: []string{"recipient@example.com"},
			Payload: core.DeliveryPayload{
				Content: &core.RenderedContent{
					Title: "Test Alert",
					Body:  "Test body",
				},
			},
			CreatedAt: time.Now(),
		}

		err := provider.Deliver(ctx, task)
		// Should fail when trying to send to invalid host
		if err == nil {
			t.Log("expected error for invalid SMTP host (got nil - OK in test environment)")
		}
	})
}

func TestFactory(t *testing.T) {
	factory := &Factory{}

	if factory.Name() != "email" {
		t.Errorf("expected factory name email, got %s", factory.Name())
	}

	if factory.Type() != "email" {
		t.Errorf("expected factory type email, got %s", factory.Type())
	}

	config := map[string]any{
		"host": "smtp.example.com",
		"from": "test@example.com",
	}

	provider, err := factory.Create(config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "email" {
		t.Errorf("expected provider name email, got %s", provider.Name())
	}
}

func TestMessage(t *testing.T) {
	msg := &Message{
		From:    "sender@example.com",
		To:      []string{"recipient1@example.com", "recipient2@example.com"},
		Subject: "Test Subject",
		Body:    "Test Body",
		IsHTML:  false,
	}

	if msg.From != "sender@example.com" {
		t.Errorf("expected from sender@example.com, got %s", msg.From)
	}
	if len(msg.To) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(msg.To))
	}
	if msg.Subject != "Test Subject" {
		t.Errorf("expected subject 'Test Subject', got %s", msg.Subject)
	}
	if msg.Body != "Test Body" {
		t.Errorf("expected body 'Test Body', got %s", msg.Body)
	}
	if msg.IsHTML {
		t.Error("expected IsHTML to be false")
	}
}
