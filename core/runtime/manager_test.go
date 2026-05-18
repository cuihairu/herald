package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cuihaitao/herald/core"
)

// mockProvider is a mock provider for testing
type mockProvider struct {
	name   string
	pType  string
	status *core.ProviderStatus
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Type() string {
	return m.pType
}

func (m *mockProvider) Status() *core.ProviderStatus {
	return m.status
}

func (m *mockProvider) Deliver(ctx context.Context, task *core.Task) error {
	return nil
}

func (m *mockProvider) Close() error {
	return nil
}

// mockFactory is a mock factory for testing
type mockFactory struct {
	name  string
	pType string
}

func (f *mockFactory) Name() string {
	return f.name
}

func (f *mockFactory) Type() string {
	return f.pType
}

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

// errorFactory is a factory that always returns an error
type errorFactory struct{}

func (f *errorFactory) Name() string {
	return "error"
}

func (f *errorFactory) Type() string {
	return "test"
}

func (f *errorFactory) Create(config map[string]interface{}) (core.Provider, error) {
	return nil, errors.New("factory error")
}

func TestNewManager(t *testing.T) {
	m := NewManager()
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.providers == nil {
		t.Error("expected providers map to be initialized")
	}
	if m.factories == nil {
		t.Error("expected factories map to be initialized")
	}
}

func TestRegisterFactory(t *testing.T) {
	m := NewManager()
	factory := &mockFactory{name: "test", pType: "builtin"}

	m.RegisterFactory(factory)

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.factories["test"]; !ok {
		t.Error("expected factory to be registered")
	}
}

func TestRegisterProvider(t *testing.T) {
	m := NewManager()
	provider := &mockProvider{
		name: "test-provider",
		status: &core.ProviderStatus{
			Name:   "test-provider",
			Status: "available",
		},
	}

	err := m.RegisterProvider(provider)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Try to register again
	err = m.RegisterProvider(provider)
	if err == nil {
		t.Error("expected error when registering duplicate provider")
	}
}

func TestCreateProvider(t *testing.T) {
	m := NewManager()
	factory := &mockFactory{name: "mock", pType: "builtin"}

	m.RegisterFactory(factory)

	provider, err := m.CreateProvider("mock", nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if provider == nil {
		t.Error("expected non-nil provider")
	}
	if provider.Name() != "mock" {
		t.Errorf("expected provider name 'mock', got %s", provider.Name())
	}
}

func TestCreateProviderNotFound(t *testing.T) {
	m := NewManager()

	_, err := m.CreateProvider("nonexistent", nil)
	if err == nil {
		t.Error("expected error for non-existent factory")
	}
}

func TestGetProvider(t *testing.T) {
	m := NewManager()
	provider := &mockProvider{
		name: "test",
		status: &core.ProviderStatus{
			Name:   "test",
			Status: "available",
		},
	}

	m.RegisterProvider(provider)

	retrieved, err := m.GetProvider("test")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrieved != provider {
		t.Error("expected same provider instance")
	}
}

func TestGetProviderNotFound(t *testing.T) {
	m := NewManager()

	_, err := m.GetProvider("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent provider")
	}
}

func TestGetProviders(t *testing.T) {
	m := NewManager()

	p1 := &mockProvider{name: "p1", status: &core.ProviderStatus{Name: "p1"}}
	p2 := &mockProvider{name: "p2", status: &core.ProviderStatus{Name: "p2"}}

	m.RegisterProvider(p1)
	m.RegisterProvider(p2)

	providers := m.GetProviders()
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}
}

func TestGetProviderStatus(t *testing.T) {
	m := NewManager()

	p1 := &mockProvider{
		name: "p1",
		status: &core.ProviderStatus{
			Name:   "p1",
			Type:   "builtin",
			Status: "available",
		},
	}

	m.RegisterProvider(p1)

	statuses := m.GetProviderStatus()
	if len(statuses) != 1 {
		t.Errorf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Name != "p1" {
		t.Errorf("expected name 'p1', got %s", statuses[0].Name)
	}
}

func TestDeliver(t *testing.T) {
	m := NewManager()

	provider := &mockProvider{
		name: "test",
		status: &core.ProviderStatus{
			Name:   "test",
			Status: "available",
		},
	}

	m.RegisterProvider(provider)

	task := &core.Task{
		ID:       "task-1",
		Provider: "test",
		Title:    "Test",
	}

	ctx := context.Background()
	err := m.Deliver(ctx, task)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestDeliverProviderNotFound(t *testing.T) {
	m := NewManager()

	task := &core.Task{
		ID:       "task-1",
		Provider: "nonexistent",
	}

	ctx := context.Background()
	err := m.Deliver(ctx, task)
	if err == nil {
		t.Error("expected error for non-existent provider")
	}
}

func TestDeliverNilTask(t *testing.T) {
	m := NewManager()

	provider := &mockProvider{
		name: "test",
		status: &core.ProviderStatus{
			Name:   "test",
			Status: "available",
		},
	}

	m.RegisterProvider(provider)

	ctx := context.Background()
	err := m.Deliver(ctx, nil)
	if err == nil {
		t.Error("expected error for nil task")
	}
}

func TestClose(t *testing.T) {
	m := NewManager()

	p1 := &mockProvider{name: "p1", status: &core.ProviderStatus{Name: "p1"}}
	p2 := &mockProvider{name: "p2", status: &core.ProviderStatus{Name: "p2"}}

	m.RegisterProvider(p1)
	m.RegisterProvider(p2)

	ctx := context.Background()
	err := m.Close(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewManager()
	factory := &mockFactory{name: "test", pType: "builtin"}
	m.RegisterFactory(factory)

	done := make(chan bool)

	// Concurrent creates
	for i := 0; i < 10; i++ {
		go func() {
			_, _ = m.CreateProvider("test", nil)
			done <- true
		}()
	}

	// Concurrent registrations
	for i := 0; i < 10; i++ {
		go func(n int) {
			p := &mockProvider{
				name:   "provider",
				status: &core.ProviderStatus{Name: "provider"},
			}
			_ = m.RegisterProvider(p)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Verify state is consistent
	providers := m.GetProviders()
	_ = providers // Just ensure no race occurred
}

func TestManagerEmptyState(t *testing.T) {
	m := NewManager()

	// Get from empty manager
	_, err := m.GetProvider("test")
	if err == nil {
		t.Error("expected error when getting from empty manager")
	}

	providers := m.GetProviders()
	if len(providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(providers))
	}

	statuses := m.GetProviderStatus()
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}
}
