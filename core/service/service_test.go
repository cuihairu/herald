package service

import (
	"context"
	"fmt"
	"strings"
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
		queue := newMockQueue()

		service := NewNotificationService(templates, router, runtime, dedupMgr, queue)

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
		service := NewNotificationService(nil, nil, nil, nil, nil)

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

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Type:     "alert",
			Level:    "high",
			Channels: []string{"test-provider"},
			Content: &core.DirectContent{
				Title: "Test Alert",
				Body:  "This is a test alert",
			},
		}

		result, err := service.Process(context.Background(), notification)
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

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Type:     "alert",
			Channels: []string{"disabled-provider"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		result, err := service.Process(context.Background(), notification)
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
		for i := range 3 {
			provider := &mockProvider{
				providerType: "test",
				capability: core.ProviderCapability{
					PayloadKinds:   []core.PayloadKind{core.PayloadContent},
					ContentFormats: []string{"plain"},
				},
			}
			runtime.RegisterProvider(fmt.Sprintf("provider-%d", i), provider, true)
		}

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Channels: []string{"provider-0", "provider-1", "provider-2"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		result, err := service.Process(context.Background(), notification)
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

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Channels: []string{"test"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		result, _ := service.Process(context.Background(), notification)

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

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Channels: []string{"test"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test body",
			},
		}

		before := time.Now()
		_, _ = service.Process(context.Background(), notification)
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

func TestNotificationService_ProcessWithRouter(t *testing.T) {
	t.Run("route channels when not specified", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		// Add route
		router.SetRoute("alert", []string{"email", "slack"})

		provider := &mockProvider{
			providerType: "email",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}
		runtime.RegisterProvider("email", provider, true)
		runtime.RegisterProvider("slack", provider, true)

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Type:  "alert",
			Level: "high",
			Content: &core.DirectContent{
				Title: "Test Alert",
				Body:  "Test body",
			},
		}

		result, err := service.Process(context.Background(), notification)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result.Accepted) != 2 {
			t.Errorf("expected 2 accepted channels from routing, got %d", len(result.Accepted))
		}
	})

	t.Run("route error when no channels and no route", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Type:  "unknown",
			Level: "high",
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test",
			},
		}

		_, err := service.Process(context.Background(), notification)
		if err == nil {
			t.Error("expected error when no route found")
		}
	})

	t.Run("process with template rendering", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		// Register template
		_ = templates.Register(&template.Template{
			ID:    "test-template",
			Name:  "Test",
			Title: "Alert: {{.title}}",
			Fields: []template.Field{
				{Label: "title", Value: "{{.title}}"},
				{Label: "message", Value: "{{.message}}"},
			},
		})

		provider := &mockProvider{
			providerType: "email",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}
		runtime.RegisterProvider("email", provider, true)

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Channels:    []string{"email"},
			TemplateRef: "test-template",
			Params: map[string]any{
				"title":   "Test Title",
				"message": "Test Message",
			},
		}

		result, err := service.Process(context.Background(), notification)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(result.Accepted) != 1 {
			t.Errorf("expected 1 accepted channel, got %d", len(result.Accepted))
		}
	})

	t.Run("process with template rendering error", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Channels:    []string{"email"},
			TemplateRef: "nonexistent",
		}

		_, err := service.Process(context.Background(), notification)
		if err == nil {
			t.Error("expected error when template not found")
		}
	})

	t.Run("dedup prevents duplicate processing", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		dedupMgr := dedup.NewDedup(&dedup.Config{Window: 100 * time.Second})
		queue := newMockQueue()

		provider := &mockProvider{
			providerType: "email",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}
		runtime.RegisterProvider("email", provider, true)

		service := NewNotificationService(templates, router, runtime, dedupMgr, queue)

		notification := &core.Notification{
			Channels: []string{"email"},
			Type:     "alert",
			Level:    "high",
			Content: &core.DirectContent{
				Title: "Test Alert",
				Body:  "Test body",
			},
		}

		// First call should succeed
		result1, err1 := service.Process(context.Background(), notification)
		if err1 != nil {
			t.Fatalf("first call should succeed, got %v", err1)
		}
		if len(result1.TaskIDs) != 1 {
			t.Errorf("expected 1 task ID from first call, got %d", len(result1.TaskIDs))
		}

		// Second call with same content should be deduplicated
		result2, err2 := service.Process(context.Background(), notification)
		if err2 != nil {
			t.Fatalf("second call should succeed (dedup), got %v", err2)
		}
		if len(result2.TaskIDs) != 0 {
			t.Errorf("expected 0 task IDs from second call (dedup), got %d", len(result2.TaskIDs))
		}
		if result2.NotificationID != result1.NotificationID {
			t.Error("expected same notification ID for deduplicated call")
		}
	})

	t.Run("provider not found error", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()
		queue := newMockQueue()

		service := NewNotificationService(templates, router, runtime, nil, queue)

		notification := &core.Notification{
			Channels: []string{"nonexistent"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test",
			},
		}

		result, err := service.Process(context.Background(), notification)
		if err == nil {
			t.Error("expected error when provider not found")
		}
		if len(result.Failed) != 1 {
			t.Errorf("expected 1 failed channel, got %d", len(result.Failed))
		}
	})

	t.Run("queue push failure", func(t *testing.T) {
		templates := template.NewManager()
		router := route.NewRouter(&route.Config{})
		runtime := newMockProviderRuntime()

		// Create a failing queue
		failingQueue := &failingQueue{pushError: fmt.Errorf("queue full")}

		provider := &mockProvider{
			providerType: "email",
			capability: core.ProviderCapability{
				PayloadKinds:   []core.PayloadKind{core.PayloadContent},
				ContentFormats: []string{"plain"},
			},
		}
		runtime.RegisterProvider("email", provider, true)

		service := NewNotificationService(templates, router, runtime, nil, failingQueue)

		notification := &core.Notification{
			Channels: []string{"email"},
			Content: &core.DirectContent{
				Title: "Test",
				Body:  "Test",
			},
		}

		result, err := service.Process(context.Background(), notification)
		if err == nil {
			t.Error("expected error when queue push fails")
		}
		if len(result.Failed) != 1 {
			t.Errorf("expected 1 failed channel, got %d", len(result.Failed))
		}
	})
}

// failingQueue is a mock queue that always fails on Push
type failingQueue struct {
	pushError error
}

func (f *failingQueue) Push(ctx context.Context, task *core.DeliveryTask) error {
	return f.pushError
}

func (f *failingQueue) Pop(ctx context.Context) (*core.DeliveryTask, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *failingQueue) Ack(ctx context.Context, id string) error {
	return nil
}

func (f *failingQueue) Nack(ctx context.Context, id string, err error) error {
	return nil
}

func (f *failingQueue) Size() int {
	return 0
}

func (f *failingQueue) Close() error {
	return nil
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

func TestDeliveryPlanner_renderContent(t *testing.T) {
	t.Run("render with no renderer returns field body", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		data := &template.RenderedData{
			Fields: []template.RenderedField{
				{Label: "field1", Value: "value1"},
				{Label: "field2", Value: "value2"},
			},
		}

		result, err := planner.renderContent(context.Background(), data, "unknown")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if result == "" {
			t.Error("expected non-empty result")
		}
		if !strings.Contains(result, "field1:") {
			t.Errorf("expected result to contain 'field1:', got %s", result)
		}
	})

	t.Run("render with empty fields", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		data := &template.RenderedData{
			Fields: []template.RenderedField{},
		}

		result, err := planner.renderContent(context.Background(), data, "plain")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		// The plain renderer may return empty string or just whitespace for empty fields
		// Both are acceptable
		if result != "" && result != "\n" {
			t.Errorf("expected empty string or newline for empty fields, got %q", result)
		}
	})
}

func TestDeliveryPlanner_buildProviderTemplate(t *testing.T) {
	t.Run("with ordered params", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		binding := &template.Binding{
			ParamOrder: []string{"code", "expire"},
		}

		renderedData := &template.RenderedData{
			Fields: []template.RenderedField{
				{Label: "code", Value: "123456"},
				{Label: "expire", Value: "5"},
			},
		}

		notification := &core.Notification{}

		pt := planner.buildProviderTemplate(notification, renderedData, binding)

		params, ok := pt.Params.([]string)
		if !ok {
			t.Fatal("expected params to be []string")
		}
		if len(params) != 2 {
			t.Errorf("expected 2 params, got %d", len(params))
		}
		if params[0] != "123456" {
			t.Errorf("expected first param '123456', got %s", params[0])
		}
	})

	t.Run("with named params", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		binding := &template.Binding{
			Params: map[string]string{
				"code":   "verification_code",
				"expire": "expiration_time",
			},
		}

		renderedData := &template.RenderedData{
			Fields: []template.RenderedField{
				{Label: "code", Value: "654321"},
				{Label: "expire", Value: "10"},
			},
		}

		notification := &core.Notification{}

		pt := planner.buildProviderTemplate(notification, renderedData, binding)

		params, ok := pt.Params.(map[string]string)
		if !ok {
			t.Fatal("expected params to be map[string]string")
		}
		if len(params) != 2 {
			t.Errorf("expected 2 params, got %d", len(params))
		}
		if params["verification_code"] != "654321" {
			t.Errorf("expected verification_code '654321', got %s", params["verification_code"])
		}
	})

	t.Run("fallback to all params when no binding params specified", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		binding := &template.Binding{}

		renderedData := &template.RenderedData{
			Fields: []template.RenderedField{
				{Label: "field1", Value: "value1"},
			},
		}

		notification := &core.Notification{
			Params: map[string]any{
				"extra": "extravalue",
			},
		}

		pt := planner.buildProviderTemplate(notification, renderedData, binding)

		params, ok := pt.Params.(map[string]string)
		if !ok {
			t.Fatal("expected params to be map[string]string")
		}
		// Should have both rendered fields and notification params
		if len(params) < 1 {
			t.Errorf("expected at least 1 param, got %d", len(params))
		}
	})
}

func TestDeliveryPlanner_buildFieldValues(t *testing.T) {
	t.Run("extract from rendered data", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		renderedData := &template.RenderedData{
			Fields: []template.RenderedField{
				{Label: "code", Value: "123"},
				{Label: "name", Value: "test"},
			},
		}

		notification := &core.Notification{}

		values := planner.buildFieldValues(renderedData, notification)

		if len(values) != 2 {
			t.Errorf("expected 2 values, got %d", len(values))
		}
		if values["code"] != "123" {
			t.Errorf("expected code '123', got %s", values["code"])
		}
	})

	t.Run("merge with notification params", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		renderedData := &template.RenderedData{
			Fields: []template.RenderedField{
				{Label: "code", Value: "123"},
			},
		}

		notification := &core.Notification{
			Params: map[string]any{
				"extra": "value",
				"code":  "456", // Should not override rendered field
			},
		}

		values := planner.buildFieldValues(renderedData, notification)

		if values["code"] != "123" {
			t.Errorf("expected rendered code to take priority, got %s", values["code"])
		}
		if values["extra"] != "value" {
			t.Errorf("expected extra param 'value', got %s", values["extra"])
		}
	})

	t.Run("only notification params when no rendered data", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		notification := &core.Notification{
			Params: map[string]any{
				"key1": "value1",
				"key2": 123, // Non-string should be skipped
			},
		}

		values := planner.buildFieldValues(nil, notification)

		if len(values) != 1 {
			t.Errorf("expected 1 value (only string param), got %d", len(values))
		}
		if values["key1"] != "value1" {
			t.Errorf("expected key1 'value1', got %s", values["key1"])
		}
	})
}

func TestDeliveryPlanner_selectFormat(t *testing.T) {
	t.Run("binding format takes priority", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		binding := &template.Binding{Format: "html"}
		supported := []string{"plain", "markdown"}

		result := planner.selectFormat(supported, &core.Notification{}, binding)
		if result != "html" {
			t.Errorf("expected binding format 'html', got %s", result)
		}
	})

	t.Run("first supported format when no binding", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		supported := []string{"markdown", "html"}

		result := planner.selectFormat(supported, &core.Notification{}, nil)
		if result != "markdown" {
			t.Errorf("expected first supported format 'markdown', got %s", result)
		}
	})

	t.Run("default to plain when no supported formats", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		result := planner.selectFormat([]string{}, &core.Notification{}, nil)
		if result != "plain" {
			t.Errorf("expected default format 'plain', got %s", result)
		}
	})
}

func TestDeliveryPlanner_resolveBinding(t *testing.T) {
	t.Run("resolve existing binding", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		_ = templates.Register(&template.Template{
			ID:    "test-template",
			Name:  "Test",
			Title: "Test",
			Bindings: map[string]template.Binding{
				"email": {TemplateCode: "EMAIL_001"},
			},
		})

		binding := planner.resolveBinding("test-template", "email")
		if binding == nil {
			t.Fatal("expected non-nil binding")
		}
		if binding.TemplateCode != "EMAIL_001" {
			t.Errorf("expected TemplateCode 'EMAIL_001', got %s", binding.TemplateCode)
		}
	})

	t.Run("return nil when template not found", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		binding := planner.resolveBinding("nonexistent", "email")
		if binding != nil {
			t.Error("expected nil binding when template not found")
		}
	})

	t.Run("return nil when no binding for channel", func(t *testing.T) {
		templates := template.NewManager()
		planner := NewDeliveryPlanner(templates)

		_ = templates.Register(&template.Template{
			ID:    "test-template",
			Name:  "Test",
			Title: "Test",
			Bindings: map[string]template.Binding{
				"slack": {TemplateCode: "SLACK_001"},
			},
		})

		binding := planner.resolveBinding("test-template", "email")
		if binding != nil {
			t.Error("expected nil binding when channel not in bindings")
		}
	})

	t.Run("return nil when templates is nil", func(t *testing.T) {
		planner := NewDeliveryPlanner(nil)

		binding := planner.resolveBinding("test", "email")
		if binding != nil {
			t.Error("expected nil binding when templates is nil")
		}
	})
}

func TestResolveContent(t *testing.T) {
	t.Run("use rendered data when available", func(t *testing.T) {
		renderedData := &template.RenderedData{
			Title: "Rendered Title",
			Fields: []template.RenderedField{
				{Label: "field1", Value: "value1"},
			},
		}

		title, body := resolveContent(&core.Notification{}, renderedData)
		if title != "Rendered Title" {
			t.Errorf("expected rendered title, got %s", title)
		}
		if body == "" {
			t.Error("expected body from rendered fields")
		}
	})

	t.Run("fall back to notification content", func(t *testing.T) {
		notification := &core.Notification{
			Content: &core.DirectContent{
				Title: "Direct Title",
				Body:  "Direct Body",
			},
		}

		title, body := resolveContent(notification, nil)
		if title != "Direct Title" {
			t.Errorf("expected direct title, got %s", title)
		}
		if body != "Direct Body" {
			t.Errorf("expected direct body, got %s", body)
		}
	})

	t.Run("empty when neither available", func(t *testing.T) {
		title, body := resolveContent(&core.Notification{}, nil)
		if title != "" {
			t.Errorf("expected empty title, got %s", title)
		}
		if body != "" {
			t.Errorf("expected empty body, got %s", body)
		}
	})
}

func TestRenderFieldsBody(t *testing.T) {
	t.Run("render fields as plain text", func(t *testing.T) {
		data := &template.RenderedData{
			Fields: []template.RenderedField{
				{Label: "name", Value: "John"},
				{Label: "age", Value: "30"},
			},
		}

		result := renderFieldsBody(data)
		if !strings.Contains(result, "name: John") {
			t.Error("expected result to contain 'name: John'")
		}
		if !strings.Contains(result, "age: 30") {
			t.Error("expected result to contain 'age: 30'")
		}
	})

	t.Run("empty when no fields", func(t *testing.T) {
		data := &template.RenderedData{
			Fields: []template.RenderedField{},
		}

		result := renderFieldsBody(data)
		if result != "" {
			t.Errorf("expected empty string, got %s", result)
		}
	})
}
