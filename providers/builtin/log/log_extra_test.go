package log

import (
	"context"
	"testing"

	"github.com/cuihairu/herald/core"
)

// TestExtractContentNilContent covers the fallback of extractContent when a
// task carries no rendered content: both title and body must be empty.
func TestExtractContentNilContent(t *testing.T) {
	task := &core.DeliveryTask{
		ID:       "task-raw-1",
		Provider: "log",
		Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]any{"key": "value"},
		},
	}

	title, body := extractContent(task)
	if title != "" || body != "" {
		t.Errorf("extractContent(nil content) = %q/%q, want empty strings", title, body)
	}
}

// TestDeliverNilContent ensures Deliver completes for a task without rendered
// content (empty title and body) instead of failing or panicking.
func TestDeliverNilContent(t *testing.T) {
	provider, err := NewProvider(nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	task := &core.DeliveryTask{
		ID:       "task-raw-2",
		Provider: "log",
		Level:    "warning",
		Payload: core.DeliveryPayload{
			Kind: core.PayloadRaw,
			Raw:  map[string]any{"key": "value"},
		},
	}

	if err := provider.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
