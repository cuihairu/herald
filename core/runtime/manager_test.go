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

	if err := m.RegisterProvider(p); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if err := m.RegisterProvider(p); err == nil {
		t.Error("expected error when registering duplicate")
	}
}

func TestGetProvider(t *testing.T) {
	m := NewManager(100)
	p := &mockProvider{name: "test", status: &core.ProviderStatus{Name: "test"}}
	_ = m.RegisterProvider(p)

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
	_ = m.RegisterProvider(p)

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
	_ = m.RegisterProvider(p)

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
	_ = m.RegisterProvider(&mockProvider{name: "p1", status: &core.ProviderStatus{Name: "p1"}})
	_ = m.RegisterProvider(&mockProvider{name: "p2", status: &core.ProviderStatus{Name: "p2"}})

	if err := m.Close(context.Background()); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
