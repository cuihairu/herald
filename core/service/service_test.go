package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/dedup"
	"github.com/cuihairu/herald/core/route"
	"github.com/cuihairu/herald/core/template"
	"github.com/google/uuid"
)

// mockProviderRuntime is a mock implementation of ProviderRuntime
type mockProviderRuntime struct {
	providers map[string]core.Provider
	enabled   map[string]bool
}

func newMockProviderRuntime() *mockProviderRuntime {
	return &mockProviderRuntime{
		providers: make(map[string]core.Provider),
		enabled:   make(map[string]bool),
	}
}

func (m *mockProviderRuntime) RegisterProvider(name string, provider core.Provider, enabled bool) {
	m.providers[name] = provider
	m.enabled[name] = enabled
}

func (m *mockProviderRuntime) GetProvider(name string) (core.Provider, error) {
	p, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return p, nil
}

func (m *mockProviderRuntime) IsEnabled(name string) bool {
	enabled, ok := m.enabled[name]
	return ok && enabled
}

// mockQueue is a simple in-memory queue
type mockQueue struct {
	tasks []*core.DeliveryTask
}

func newMockQueue() *mockQueue {
	return &mockQueue{tasks: make([]*core.DeliveryTask, 0)}
}

func (m *mockQueue) Push(ctx context.Context, task *core.DeliveryTask) error {
	m.tasks = append(m.tasks, task)
	return nil
}

func (m *mockQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	if len(m.tasks) == 0 {
		return nil, fmt.Errorf("empty queue")
	}
	task := m.tasks[0]
	m.tasks = m.tasks[1:]
	return task, nil
}

func (m *mockQueue) Ack(ctx context.Context, id string) error {
	return nil
}

func (m *mockQueue) Nack(ctx context.Context, id string, err error) error {
	return nil
}

func (m *mockQueue) Size() int {
	return len(m.tasks)
}

func (m *mockQueue) Close() error {
	return nil
}

// mockProvider is a simple provider for testing
type mockProvider struct {
	providerType string
	providerName string
	capability   core.ProviderCapability
}

func (m *mockProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	return nil
}

func (m *mockProvider) Name() string {
	if m.providerName != "" {
		return m.providerName
	}
	return m.providerType
}

func (m *mockProvider) Type() string {
	return m.providerType
}

func (m *mockProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{
		Name:   m.Name(),
		Type:   m.providerType,
		Status: "online",
	}
}

func (m *mockProvider) Capability() core.ProviderCapability {
	return m.capability
}

func TestNewNotificationService(t *testing.T) {
	t.Run("create service", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		dedupMgr := dedup.NewDedup(&dedup.Config{})

		service := NewNotificationService(templates, router, runtime, dedupMgr)

		if service == nil {
			t.Fatal("expected non-nil service")
		}
		if service.templates == nil {
			t.Error("expected non-nil templates")
		}
		if service.router == nil {
			t.Error("expected non-nil router")
		}
		if service.runtime == nil {
			t.Error("expected non-nil runtime")
		}
		if service.planner == nil {
			t.Error("expected non-nil planner")
		}
	})

	t.Run("create service with nil components", func(t *testing.T) {
		service := NewNotificationService(nil, nil, nil, nil)

		if service == nil {
			t.Fatal("expected non-nil service")
		}
		// Planner should still be created with nil templates
		if service.planner == nil {
			t.Error("expected non-nil planner")
		}
	})
}

func TestNotificationService_Process(t *testing.T) {
	t.Run("process notification with content", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		// Register a provider
		provider := &mockProvider{
			providerType: "test",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain", "html"},
			},
		}
		runtime.RegisterProvider("test-provider", provider, true)

		// Add a route
		router.SetRoute("alert", []string{"test-provider"})

		service := NewNotificationService(templates, router, runtime, nil)

		notification := &core.Notification{
			Type:     "alert",
			Level:    "high",
			Channels: []string{"test-provider"},
			Content: &core.DirectContent{
				Title: "Test Alert",
				Body:  "This is a test alert",
			},
		}

		result, err := service.Process(context.Background(), notification, queue)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if result.NotificationID == "" {
			t.Error("expected notification ID to be set")
		}
		if len(result.Accepted) != 1 {
			t.Errorf("expected 1 accepted channel, got %d", len(result.Accepted))
		}
		if len(result.TaskIDs) != 1 {
			t.Errorf("expected 1 task ID, got %d", len(result.TaskIDs))
		}
		if len(queue.tasks) != 1 {
			t.Errorf("expected 1 task in queue, got %d", len(queue.tasks))
		}
	})

	t.Run("process notification with disabled provider", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		provider := &mockProvider{
			providerType: "test",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}
		runtime.RegisterProvider("disabled-provider", provider, false) // disabled

		service := NewNotificationService(templates, router, runtime, nil)

		notification := &core.Notification{
			Type:     "alert",
			Channels: []string{"disabled-provider"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		result, err := service.Process(context.Background(), notification, queue)
		if err == nil {
			t.Error("expected error when provider is disabled")
		}
		if len(result.Failed) != 1 {
			t.Errorf("expected 1 failed channel, got %d", len(result.Failed))
		}
	})

	t.Run("process notification with multiple channels", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		// Register multiple providers
		for i := 0; i < 3; i++ {
			provider := &mockProvider{
				providerType: "test",
				capability: core.ProviderCapability{
					PayloadKinds:   []core.PayloadKind{core.PayloadContent},
					ContentFormats: []string{"plain"},
				},
			}
			runtime.RegisterProvider(fmt.Sprintf("provider-%d", i), provider, true)
		}

		service := NewNotificationService(templates, router, runtime, nil)

		notification := &core.Notification{
			Channels: []string{"provider-0", "provider-1", "provider-2"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		result, err := service.Process(context.Background(), notification, queue)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Accepted) != 3 {
			t.Errorf("expected 3 accepted channels, got %d", len(result.Accepted))
		}
		if len(queue.tasks) != 3 {
			t.Errorf("expected 3 tasks in queue, got %d", len(queue.tasks))
		}
	})

	t.Run("auto-generate notification ID", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		provider := &mockProvider{
			providerType: "test",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}
		runtime.RegisterProvider("test", provider, true)

		service := NewNotificationService(templates, router, runtime, nil)

		notification := &core.Notification{
			Channels: []string{"test"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		result, _ := service.Process(context.Background(), notification, queue)

		if result.NotificationID == "" {
			t.Error("expected auto-generated notification ID")
		}
		// Verify it's a valid UUID
		_, err := uuid.Parse(result.NotificationID)
		if err != nil {
			t.Errorf("expected valid UUID, got error: %v", err)
		}
	})

	t.Run("set CreatedAt timestamp", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		provider := &mockProvider{
			providerType: "test",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}
		runtime.RegisterProvider("test", provider, true)

		service := NewNotificationService(templates, router, runtime, nil)

		notification := &core.Notification{
			Channels: []string{"test"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		before := time.Now()
		service.Process(context.Background(), notification, queue)
		after := time.Now()

		if notification.CreatedAt.Before(before) || notification.CreatedAt.After(after) {
			t.Error("expected CreatedAt to be set to current time")
		}
	})
}

func TestNewDeliveryPlanner(t *testing.T) {
	t.Run("create planner", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		if planner == nil {
			t.Fatal("expected non-nil planner")
		}
		if planner.templates != templates {
			t.Error("expected templates to be set")
		}
	})

	t.Run("create planner with nil templates", func(t *testing.T) {
		planner := NewDeliveryPlanner(nil)

		if planner == nil {
			t.Fatal("expected non-nil planner")
		}
	})
}

func TestDeliveryPlanner_Plan(t *testing.T) {
	t.Run("plan content delivery", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		provider := &mockProvider{
			providerType: "email",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain", "html"},
			},
		}

		notification := &core.Notification{
			Content: &core.DirectContent{
				Title: "Test Subject",
				Body:  "Test body",
			},
			Level: "high",
		}

		task, err := planner.Plan(context.Background(), provider, notification, nil, []string{"test@example.com"}, "email")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if task.ID == "" {
			t.Error("expected task ID to be set")
		}
		if task.Provider != "email" {
			t.Errorf("expected provider 'email', got '%s'", task.Provider)
		}
		if len(task.Targets) != 1 {
			t.Errorf("expected 1 target, got %d", len(task.Targets))
		}
		if task.Payload.Kind != core.PayloadContent {
			t.Errorf("expected payload kind '%s', got '%s'", core.PayloadContent, task.Payload.Kind)
		}
		if task.Payload.Content == nil {
			t.Error("expected content to be set")
		}
	})

	t.Run("plan raw delivery", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		provider := &mockProvider{
			providerType: "webhook",
			capability: core.ProviderCapability{
				PayloadKinds: []core.PayloadKind{core.PayloadRaw},
			},
		}

		notification := &core.Notification{
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Body",
			},
			Params: map[string]any{"key": "value"},
		}

		task, err := planner.Plan(context.Background(), provider, notification, nil, nil, "webhook")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if task.Payload.Kind != core.PayloadRaw {
			t.Errorf("expected payload kind '%s', got '%s'", core.PayloadRaw, task.Payload.Kind)
		}
		if task.Payload.Raw == nil {
			t.Error("expected raw payload to be set")
		}
	})

	t.Run("plan with provider template", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		provider := &mockProvider{
			providerType: "aliyunsms",
			capability: core.ProviderCapability{
				PayloadKinds: []core.PayloadKind{core.PayloadProviderTemplate},
			},
		}

		renderedData := &template.RenderedData{
			TemplateID: "test-template",
			Fields: []template.RenderedField{
				{Label: "code", Value: "123456"},
			},
		}

		// Add a template with binding - must include all required fields
		_ = templates.Register(&template.Template{
			ID:    "test-template",
			Name:  "Test Template",
			Title: "Test",
			Fields: []template.Field{
				{Label: "code", Value: "{{.code}}"},
			},
			Bindings: map[string]template.Binding{
				"aliyunsms": {
					TemplateCode: "SMS_123",
					Params:       map[string]string{"code": "code"},
				},
			},
		})

		notification := &core.Notification{
			Params: map[string]any{},
		}

		task, err := planner.Plan(context.Background(), provider, notification, renderedData, []string{"1234567890"}, "aliyunsms")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if task.Payload.Kind != core.PayloadProviderTemplate {
			t.Errorf("expected payload kind '%s', got '%s'", core.PayloadProviderTemplate, task.Payload.Kind)
		}
		if task.Payload.ProviderTemplate == nil {
			t.Error("expected provider template to be set")
		}
	})

	t.Run("plan with incompatible binding", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		provider := &mockProvider{
			providerType: "webhook",
			capability: core.ProviderCapability{
				PayloadKinds: []core.PayloadKind{core.PayloadRaw},
			},
		}

		renderedData := &template.RenderedData{
			TemplateID: "test-template",
		}

		_ = templates.Register(&template.Template{
			ID:    "test-template",
			Name:  "Test Template",
			Title: "Test",
			Fields: []template.Field{
				{Label: "message", Value: "test"},
			},
			Bindings: map[string]template.Binding{
				"webhook": {
					TemplateCode: "TPL_123",
				},
			},
		})

		notification := &core.Notification{}

		_, err := planner.Plan(context.Background(), provider, notification, renderedData, nil, "webhook")
		if err == nil {
			t.Error("expected error when binding specifies template but provider doesn't support it")
		}
	})

	t.Run("plan with no compatible payload kind", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		provider := &mockProvider{
			providerType: "unknown",
			capability: core.ProviderCapability{
				PayloadKinds: []core.PayloadKind{},
			},
		}

		notification := &core.Notification{
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Body",
			},
		}

		_, err := planner.Plan(context.Background(), provider, notification, nil, nil, "unknown")
		if err == nil {
			t.Error("expected error when provider has no compatible payload kind")
		}
	})
}

func TestResolveTargets(t *testing.T) {
	t.Run("with recipients map", func(t *testing.T) {
		notification := &core.Notification{
			Recipients: map[string][]string{
				"email":    {"user1@example.com", "user2@example.com"},
				"slack":    {"#general"},
				"telegram": {"123456"},
			},
		}

		targets := resolveTargets(notification, "email")
		if len(targets) != 2 {
			t.Errorf("expected 2 targets, got %d", len(targets))
		}
	})

	t.Run("with no recipients for channel", func(t *testing.T) {
		notification := &core.Notification{
			Recipients: map[string][]string{
				"email": {"user@example.com"},
			},
		}

		targets := resolveTargets(notification, "slack")
		if targets != nil {
			t.Errorf("expected nil targets, got %v", targets)
		}
	})

	t.Run("with nil recipients", func(t *testing.T) {
		notification := &core.Notification{}

		targets := resolveTargets(notification, "email")
		if targets != nil {
			t.Errorf("expected nil targets, got %v", targets)
		}
	})
}

func TestFormatChannelErrors(t *testing.T) {
	errs := []ChannelError{
		{Channel: "email", Error: "connection failed"},
		{Channel: "slack", Error: "invalid webhook"},
	}

	result := formatChannelErrors(errs)

	// Should contain both errors
	if result == "" {
		t.Error("expected formatted error string")
	}
}

func TestDedupKey(t *testing.T) {
	t.Run("generate stable key", func(t *testing.T) {
		notification := &core.Notification{
			Type:     "alert",
			Level:    "high",
			Channels: []string{"email", "slack"},
			Content: &core.DirectContent{
				Title: "Test Alert",
				Body:  "Test body",
			},
			Params: map[string]any{"key": "value"},
		}

		key1 := dedupKey(notification)
		key2 := dedupKey(notification)

		if key1 != key2 {
			t.Error("expected same key for same notification")
		}

		// Key should be a hex string (SHA256)
		if len(key1) != 64 {
			t.Errorf("expected 64 character hex string, got %d", len(key1))
		}
	})

	t.Run("different notifications produce different keys", func(t *testing.T) {
		n1 := &core.Notification{
			Type:  "alert",
			Level: "high",
		}
		n2 := &core.Notification{
			Type:  "info",
			Level: "low",
		}

		key1 := dedupKey(n1)
		key2 := dedupKey(n2)

		if key1 == key2 {
			t.Error("expected different keys for different notifications")
		}
	})

	t.Run("channel order doesn't affect key", func(t *testing.T) {
		n1 := &core.Notification{
			Type:     "alert",
			Channels: []string{"email", "slack", "telegram"},
		}
		n2 := &core.Notification{
			Type:     "alert",
			Channels: []string{"telegram", "email", "slack"},
		}

		key1 := dedupKey(n1)
		key2 := dedupKey(n2)

		if key1 != key2 {
			t.Error("expected same key regardless of channel order")
		}
	})
}

func TestProcessResult(t *testing.T) {
	t.Run("create process result", func(t *testing.T) {
		result := &ProcessResult{
			NotificationID: "notif-123",
			TaskIDs:        []string{"task-1", "task-2"},
			Accepted:       []string{"email", "slack"},
			Failed: []ChannelError{
				{Channel: "telegram", Error: "timeout"},
			},
		}

		if result.NotificationID != "notif-123" {
			t.Errorf("expected notification ID 'notif-123', got '%s'", result.NotificationID)
		}
		if len(result.TaskIDs) != 2 {
			t.Errorf("expected 2 task IDs, got %d", len(result.TaskIDs))
		}
		if len(result.Accepted) != 2 {
			t.Errorf("expected 2 accepted channels, got %d", len(result.Accepted))
		}
		if len(result.Failed) != 1 {
			t.Errorf("expected 1 failed channel, got %d", len(result.Failed))
		}
	})
}

func TestHasKind(t *testing.T) {
	t.Run("kind exists in list", func(t *testing.T) {
		kinds := []core.PayloadKind{core.PayloadContent, core.PayloadRaw, core.PayloadProviderTemplate}

		if !hasKind(kinds, core.PayloadContent) {
			t.Error("expected to find PayloadContent in list")
		}
		if !hasKind(kinds, core.PayloadRaw) {
			t.Error("expected to find PayloadRaw in list")
		}
	})

	t.Run("kind not in list", func(t *testing.T) {
		kinds := []core.PayloadKind{core.PayloadContent}

		if hasKind(kinds, core.PayloadRaw) {
			t.Error("expected not to find PayloadRaw in list")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		kinds := []core.PayloadKind{}

		if hasKind(kinds, core.PayloadContent) {
			t.Error("expected not to find kind in empty list")
		}
	})
}
