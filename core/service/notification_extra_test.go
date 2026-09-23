package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/template"
)

func TestProcessWithNilQueue(t *testing.T) {
	service := NewNotificationService(template.NewManager(), route.NewRouter(&route.Config{}), newMockProviderRuntime(), nil, nil)

	result, err := service.Process(context.Background(), &core.Notification{Channels: []string{"email"}})
	if err == nil {
		t.Fatal("expected error when queue is not configured")
	}
	if !strings.Contains(err.Error(), "queue is not configured") {
		t.Errorf("expected 'queue is not configured' error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestProcessAppliesTemplateLevel(t *testing.T) {
	templates := template.NewManager()
	if err := templates.Register(&template.Template{
		ID:    "level-template",
		Name:  "Level Template",
		Title: "Alert: {{.title}}",
		Level: "critical",
		Fields: []template.Field{
			{Label: "title", Value: "{{.title}}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	runtime := newMockProviderRuntime()
	runtime.RegisterProvider("email", &mockProvider{
		providerType: "email",
		capability: core.ProviderCapability{
			PayloadKinds:   []core.PayloadKind{core.PayloadContent},
			ContentFormats: []string{"plain"},
		},
	}, true)

	service := NewNotificationService(templates, route.NewRouter(&route.Config{}), runtime, nil, newMockQueue())

	notification := &core.Notification{
		Channels:    []string{"email"},
		TemplateRef: "level-template",
		Params:      map[string]any{"title": "disk full"},
	}
	if _, err := service.Process(context.Background(), notification); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// The notification starts without a level and inherits the template's one.
	if notification.Level != "critical" {
		t.Errorf("expected notification level to inherit template level 'critical', got %q", notification.Level)
	}
}

func TestProcessRecordsPlannerFailure(t *testing.T) {
	runtime := newMockProviderRuntime()
	// Registered and enabled, but declares no payload kind at all, so the
	// planner fails while building the payload for this channel.
	runtime.RegisterProvider("broken", &mockProvider{
		providerType: "broken",
		capability:   core.ProviderCapability{PayloadKinds: []core.PayloadKind{}},
	}, true)

	service := NewNotificationService(
		template.NewManager(),
		route.NewRouter(&route.Config{}),
		runtime,
		nil,
		newMockQueue(),
	)

	notification := &core.Notification{
		Channels: []string{"broken"},
		Content:  &core.DirectContent{Title: "Test", Body: "Body"},
	}

	result, err := service.Process(context.Background(), notification)
	if err == nil {
		t.Fatal("expected error when every channel fails to plan")
	}
	if !strings.Contains(err.Error(), "all channels failed") {
		t.Errorf("expected 'all channels failed' error, got %v", err)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("expected 1 failed channel, got %d", len(result.Failed))
	}
	if result.Failed[0].Channel != "broken" {
		t.Errorf("expected failed channel 'broken', got %q", result.Failed[0].Channel)
	}
	if !strings.Contains(result.Failed[0].Error, "no compatible payload kind") {
		t.Errorf("expected planner failure reason, got %q", result.Failed[0].Error)
	}
	if len(result.TaskIDs) != 0 || len(result.Accepted) != 0 {
		t.Errorf("expected no accepted tasks, got tasks=%v accepted=%v", result.TaskIDs, result.Accepted)
	}
}
