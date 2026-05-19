package log

import (
	"context"
	"testing"

	"github.com/cuihairu/herald/core"
)

func TestNewProvider(t *testing.T) {
	provider, err := NewProvider(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "log" {
		t.Errorf("expected name 'log', got %s", provider.Name())
	}
	if provider.Type() != "builtin" {
		t.Errorf("expected type 'builtin', got %s", provider.Type())
	}
}

func TestNewProviderWithName(t *testing.T) {
	config := map[string]interface{}{
		"name": "test-log",
	}

	provider, err := NewProvider(config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider.Name() != "test-log" {
		t.Errorf("expected name 'test-log', got %s", provider.Name())
	}
}

func TestProviderDeliver(t *testing.T) {
	provider, _ := NewProvider(nil)

	task := &core.Task{
		ID:       "test-1",
		Provider: "log",
		Title:    "Test Title",
		Body:     "Test Body",
		Level:    "info",
	}

	ctx := context.Background()
	err := provider.Deliver(ctx, task)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestProviderDeliverNilTask(t *testing.T) {
	provider, _ := NewProvider(nil)

	ctx := context.Background()
	err := provider.Deliver(ctx, nil)
	if err == nil {
		t.Error("expected error for nil task")
	}
}

func TestProviderStatus(t *testing.T) {
	provider, _ := NewProvider(nil)

	status := provider.Status()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.Name != "log" {
		t.Errorf("expected status name 'log', got %s", status.Name)
	}
	if status.Status != "available" {
		t.Errorf("expected status 'available', got %s", status.Status)
	}
	if status.Type != "builtin" {
		t.Errorf("expected type 'builtin', got %s", status.Type)
	}
}

func TestProviderClose(t *testing.T) {
	provider, _ := NewProvider(nil)

	// Use type assertion since Close is not part of Provider interface
	if closer, ok := provider.(interface{ Close() error }); ok {
		err := closer.Close()
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	}
}

func TestFactory(t *testing.T) {
	factory := &Factory{}

	if factory.Name() != "log" {
		t.Errorf("expected factory name 'log', got %s", factory.Name())
	}
	if factory.Type() != "builtin" {
		t.Errorf("expected factory type 'builtin', got %s", factory.Type())
	}
}

func TestFactoryCreate(t *testing.T) {
	factory := &Factory{}

	config := map[string]interface{}{
		"name": "factory-test",
	}

	provider, err := factory.Create(config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	if provider.Name() != "factory-test" {
		t.Errorf("expected provider name 'factory-test', got %s", provider.Name())
	}
}

func TestFactoryCreateNilConfig(t *testing.T) {
	factory := &Factory{}

	provider, err := factory.Create(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider.Name() != "log" {
		t.Errorf("expected default name 'log', got %s", provider.Name())
	}
}

func TestProviderDeliverDifferentLevels(t *testing.T) {
	provider, _ := NewProvider(nil)

	ctx := context.Background()
	levels := []string{"debug", "info", "warning", "error", "critical"}

	for _, level := range levels {
		task := &core.Task{
			ID:       "test-" + level,
			Provider: "log",
			Title:    "Test " + level,
			Body:     "Body for " + level,
			Level:    level,
		}

		err := provider.Deliver(ctx, task)
		if err != nil {
			t.Errorf("level %s: expected no error, got %v", level, err)
		}
	}
}

func TestProviderDeliverWithContext(t *testing.T) {
	provider, _ := NewProvider(nil)

	task := &core.Task{
		ID:       "test-ctx",
		Provider: "log",
		Title:    "Context Test",
		Body:     "Testing with context",
		Level:    "info",
	}

	ctx := context.Background()
	err := provider.Deliver(ctx, task)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestMultipleProviders(t *testing.T) {
	// Create multiple log providers with different names
	configs := []map[string]interface{}{
		{"name": "log-1"},
		{"name": "log-2"},
		{"name": "log-3"},
	}

	for _, config := range configs {
		provider, err := NewProvider(config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		expectedName := config["name"].(string)
		if provider.Name() != expectedName {
			t.Errorf("expected name %s, got %s", expectedName, provider.Name())
		}
	}
}
