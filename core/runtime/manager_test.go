package runtime

import (
	"context"
	"testing"

	"github.com/cuihairu/herald/core"
)

type mockProvider struct {
	name   string
	pType  string
	status *core.ProviderStatus
}

func (m *mockProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	return nil
}

func (m *mockProvider) Name() string                 { return m.name }
func (m *mockProvider) Type() string                 { return m.pType }
func (m *mockProvider) Status() *core.ProviderStatus { return m.status }
func (m *mockProvider) Close() error                 { return nil }

type mockFactory struct {
	name  string
	pType string
}

func (f *mockFactory) Name() string { return f.name }
func (f *mockFactory) Type() string { return f.pType }
func (f *mockFactory) Create(config map[string]interface{}) (core.Provider, error) {
	return &mockProvider{
		name:  f.name,
		pType: f.pType,
		status: &core.ProviderStatus{
			Name:   f.name,
			Type:   f.pType,
			Status: "available",
		},
	}, nil
}

func TestNewManager(t *testing.T) {
	m := NewManager(100)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestRegisterFactory(t *testing.T) {
	m := NewManager(100)
	m.RegisterFactory(&mockFactory{name: "test", pType: "builtin"})

	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.factories["test"]; !ok {
		t.Error("expected factory to be registered")
	}
}

func TestRegisterProvider(t *testing.T) {
	m := NewManager(100)
	p := &mockProvider{name: "test-provider", status: &core.ProviderStatus{Name: "test-provider"}}

	if err := m.RegisterProvider("test-provider", p); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if err := m.RegisterProvider("test-provider", p); err == nil {
		t.Error("expected error when registering duplicate")
	}
}

func TestGetProvider(t *testing.T) {
	m := NewManager(100)
	p := &mockProvider{name: "test", status: &core.ProviderStatus{Name: "test"}}
	_ = m.RegisterProvider("test", p)

	retrieved, err := m.GetProvider("test")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrieved != p {
		t.Error("expected same provider instance")
	}
}

func TestGetProviderNotFound(t *testing.T) {
	m := NewManager(100)
	_, err := m.GetProvider("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent provider")
	}
}

func TestDeliver(t *testing.T) {
	m := NewManager(100)
	p := &mockProvider{name: "test", status: &core.ProviderStatus{Name: "test"}}
	_ = m.RegisterProvider("test", p)

	task := &core.DeliveryTask{
		ID:       "task-1",
		Provider: "test",
		Payload:  core.DeliveryPayload{Kind: core.PayloadContent},
	}

	if err := m.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverNilTask(t *testing.T) {
	m := NewManager(100)
	if err := m.Deliver(context.Background(), nil); err == nil {
		t.Error("expected error for nil task")
	}
}

func TestEnableDisable(t *testing.T) {
	m := NewManager(100)
	p := &mockProvider{name: "test", status: &core.ProviderStatus{Name: "test"}}
	_ = m.RegisterProvider("test", p)

	if err := m.Disable("test"); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if m.IsEnabled("test") {
		t.Error("expected provider to be disabled")
	}

	if err := m.Enable("test"); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !m.IsEnabled("test") {
		t.Error("expected provider to be enabled")
	}
}

func TestClose(t *testing.T) {
	m := NewManager(100)
	_ = m.RegisterProvider("p1", &mockProvider{name: "p1", status: &core.ProviderStatus{Name: "p1"}})
	_ = m.RegisterProvider("p2", &mockProvider{name: "p2", status: &core.ProviderStatus{Name: "p2"}})

	if err := m.Close(context.Background()); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCreateProvider(t *testing.T) {
	t.Run("create provider from factory", func(t *testing.T) {
		m := NewManager(100)
		m.RegisterFactory(&mockFactory{name: "test", pType: "builtin"})

		config := map[string]interface{}{"key": "value"}
		provider, err := m.CreateProvider("test", config)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if provider == nil {
			t.Error("expected non-nil provider")
		}
	})

	t.Run("factory not found", func(t *testing.T) {
		m := NewManager(100)
		config := map[string]interface{}{"key": "value"}
		_, err := m.CreateProvider("nonexistent", config)
		if err == nil {
			t.Error("expected error for non-existent factory")
		}
	})
}

func TestGetProviderType(t *testing.T) {
	t.Run("get existing provider type", func(t *testing.T) {
		m := NewManager(100)
		p := &mockProvider{name: "my-provider", pType: "builtin", status: &core.ProviderStatus{Name: "my-provider"}}
		_ = m.RegisterProvider("my-provider", p)

		pType, err := m.GetProviderType("my-provider")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if pType != "builtin" {
			t.Errorf("expected type 'builtin', got '%s'", pType)
		}
	})

	t.Run("provider not found", func(t *testing.T) {
		m := NewManager(100)
		_, err := m.GetProviderType("nonexistent")
		if err == nil {
			t.Error("expected error for non-existent provider")
		}
	})
}

func TestGetProviderSchema(t *testing.T) {
	t.Run("get schema for builtin provider", func(t *testing.T) {
		m := NewManager(100)
		m.RegisterFactory(&mockFactory{name: "test", pType: "builtin"})

		schema := m.GetProviderSchema("test")
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		// The builtin schema should have some fields (or default config field)
		if len(schema) == 0 {
			t.Error("expected schema to have fields")
		}
	})

	t.Run("unknown provider type returns default schema", func(t *testing.T) {
		m := NewManager(100)
		schema := m.GetProviderSchema("nonexistent")
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		// Should return default schema with "config": "object"
		if schema["config"] != "object" {
			t.Errorf("expected default schema, got %v", schema)
		}
	})
}

func TestGetProviders(t *testing.T) {
	m := NewManager(100)
	p1 := &mockProvider{name: "p1", pType: "type1", status: &core.ProviderStatus{Name: "p1", Type: "type1"}}
	p2 := &mockProvider{name: "p2", pType: "type2", status: &core.ProviderStatus{Name: "p2", Type: "type2"}}
	_ = m.RegisterProvider("p1", p1)
	_ = m.RegisterProvider("p2", p2)

	providers := m.GetProviders()
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}
	// Check if the providers are in the list
	foundP1 := false
	foundP2 := false
	for _, p := range providers {
		if p.Name() == "p1" {
			foundP1 = true
		}
		if p.Name() == "p2" {
			foundP2 = true
		}
	}
	if !foundP1 {
		t.Error("expected p1 to be in the list")
	}
	if !foundP2 {
		t.Error("expected p2 to be in the list")
	}
}

func TestGetProviderStatus(t *testing.T) {
	t.Run("get all provider statuses", func(t *testing.T) {
		m := NewManager(100)
		status1 := &core.ProviderStatus{
			Name:   "test1",
			Type:   "builtin",
			Status: "available",
		}
		status2 := &core.ProviderStatus{
			Name:   "test2",
			Type:   "custom",
			Status: "available",
		}
		p1 := &mockProvider{name: "test1", pType: "builtin", status: status1}
		p2 := &mockProvider{name: "test2", pType: "custom", status: status2}
		_ = m.RegisterProvider("test1", p1)
		_ = m.RegisterProvider("test2", p2)

		statuses := m.GetProviderStatus()
		if len(statuses) != 2 {
			t.Errorf("expected 2 statuses, got %d", len(statuses))
		}

		// Check status names
		foundTest1 := false
		foundTest2 := false
		for _, s := range statuses {
			if s.Name == "test1" {
				foundTest1 = true
				if s.Type != "builtin" {
					t.Errorf("expected type 'builtin' for test1, got '%s'", s.Type)
				}
			}
			if s.Name == "test2" {
				foundTest2 = true
				if s.Type != "custom" {
					t.Errorf("expected type 'custom' for test2, got '%s'", s.Type)
				}
			}
		}
		if !foundTest1 {
			t.Error("expected test1 status to be present")
		}
		if !foundTest2 {
			t.Error("expected test2 status to be present")
		}
	})

	t.Run("empty manager returns empty statuses", func(t *testing.T) {
		m := NewManager(100)
		statuses := m.GetProviderStatus()
		if len(statuses) != 0 {
			t.Errorf("expected 0 statuses, got %d", len(statuses))
		}
	})
}

func TestGetLogs(t *testing.T) {
	m := NewManager(100)
	logs := m.GetLogs(0, 10, nil)
	if logs == nil {
		t.Error("expected non-nil logs")
	}
}

func TestGetLogsCount(t *testing.T) {
	m := NewManager(100)
	count := m.GetLogsCount(nil)
	if count < 0 {
		t.Errorf("expected non-negative count, got %d", count)
	}
}

func TestGetLogByID(t *testing.T) {
	m := NewManager(100)
	// Get non-existent log should return nil
	log := m.GetLogByID("nonexistent")
	if log != nil {
		t.Error("expected nil for non-existent log")
	}
}

func TestGetLogsStats(t *testing.T) {
	m := NewManager(100)
	stats := m.GetLogsStats()
	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestClearLogs(t *testing.T) {
	m := NewManager(100)
	// Clear logs should not panic
	m.ClearLogs()
}

func TestReplaceProvider(t *testing.T) {
	t.Run("replace existing provider", func(t *testing.T) {
		m := NewManager(100)
		p1 := &mockProvider{name: "test", pType: "type1", status: &core.ProviderStatus{Name: "test", Type: "type1"}}
		_ = m.RegisterProvider("test", p1)

		p2 := &mockProvider{name: "test", pType: "type2", status: &core.ProviderStatus{Name: "test", Type: "type2"}}

		err := m.ReplaceProvider("test", p2)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		// Verify the provider was replaced
		retrieved, _ := m.GetProvider("test")
		if retrieved != p2 {
			t.Error("expected provider to be replaced")
		}
	})

	t.Run("replace non-existent provider", func(t *testing.T) {
		m := NewManager(100)
		p := &mockProvider{name: "test", status: &core.ProviderStatus{Name: "test"}}

		err := m.ReplaceProvider("nonexistent", p)
		if err == nil {
			t.Error("expected error for non-existent provider")
		}
	})
}
