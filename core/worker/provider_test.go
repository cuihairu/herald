package worker

import (
	"context"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNewProviderDefaults(t *testing.T) {
	got, err := NewProvider(map[string]interface{}{})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	p, ok := got.(*Provider)
	if !ok {
		t.Fatalf("NewProvider() returned %T, want *Provider", got)
	}
	if p.Name() != "worker" {
		t.Errorf("Name() = %q, want default %q", p.Name(), "worker")
	}
	if p.Type() != "worker" {
		t.Errorf("Type() = %q, want %q", p.Type(), "worker")
	}
	if p.Target() != "worker" {
		t.Errorf("Target() = %q, want target to default to name", p.Target())
	}
	st := p.Status()
	if st == nil {
		t.Fatal("Status() = nil")
	}
	if st.Name != "worker" || st.Type != "worker" || st.Status != "available" {
		t.Errorf("Status() = %+v, want name/type worker and available", st)
	}
	if st.Since.IsZero() {
		t.Error("Status().Since should be initialized")
	}
}

func TestNewProviderWithConfig(t *testing.T) {
	got, err := NewProvider(map[string]interface{}{
		"name":   "alerts",
		"target": "worker-42",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	p := got.(*Provider)
	if p.Name() != "alerts" {
		t.Errorf("Name() = %q, want %q", p.Name(), "alerts")
	}
	if p.Target() != "worker-42" {
		t.Errorf("Target() = %q, want %q", p.Target(), "worker-42")
	}
	if p.Status().Name != "alerts" {
		t.Errorf("Status().Name = %q, want %q", p.Status().Name, "alerts")
	}
}

func TestNewProviderTargetDefaultsToName(t *testing.T) {
	got, err := NewProvider(map[string]interface{}{"name": "paged"})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if p := got.(*Provider); p.Target() != "paged" {
		t.Errorf("Target() = %q, want %q", p.Target(), "paged")
	}
}

func TestProviderDeliverNoop(t *testing.T) {
	got, _ := NewProvider(nil)
	p := got.(*Provider)
	task := &core.DeliveryTask{
		ID:        "task-1",
		Provider:  "worker",
		Targets:   []string{"worker-1"},
		CreatedAt: time.Now(),
	}
	if err := p.Deliver(context.Background(), task); err != nil {
		t.Errorf("Deliver() error = %v, want nil (worker provider defers to remote worker)", err)
	}
}

func TestProviderClose(t *testing.T) {
	got, _ := NewProvider(nil)
	if err := got.(*Provider).Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestFactory(t *testing.T) {
	f := &Factory{}
	if f.Name() != "worker" {
		t.Errorf("Factory.Name() = %q, want %q", f.Name(), "worker")
	}
	if f.Type() != "worker" {
		t.Errorf("Factory.Type() = %q, want %q", f.Type(), "worker")
	}

	got, err := f.Create(map[string]interface{}{"name": "via-factory"})
	if err != nil {
		t.Fatalf("Factory.Create() error = %v", err)
	}
	if got.Name() != "via-factory" {
		t.Errorf("Factory.Create().Name() = %q, want %q", got.Name(), "via-factory")
	}

	wp, ok := got.(*Provider)
	if !ok {
		t.Fatalf("Factory.Create() returned %T, want *Provider", got)
	}
	if wp.Target() != "via-factory" {
		t.Errorf("Factory.Create().Target() = %q, want %q", wp.Target(), "via-factory")
	}
}
