package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/retry"
)

// failingProvider is a provider whose Deliver always fails with a fixed error.
type failingProvider struct {
	name string
	err  error
}

func (p *failingProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	return p.err
}

func (p *failingProvider) Name() string { return p.name }

func (p *failingProvider) Type() string { return "failing" }

func (p *failingProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: p.name, Type: "failing", Status: "available"}
}

// newRetryTestManager builds a manager with a fast retry config so the
// retryer-backed Deliver path can be exercised without slowing the suite.
func newRetryTestManager() *Manager {
	return NewManager(100, &retry.Config{
		Max:          2,
		Backoff:      "exponential",
		InitialDelay: time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
}

func TestNewManagerWithRetryConfig(t *testing.T) {
	m := NewManager(100, &retry.Config{Max: 1, Backoff: "fixed", InitialDelay: time.Millisecond})
	if m.retryer == nil {
		t.Fatal("expected retryer to be created when a retry config is supplied")
	}

	// An explicit nil config must leave the manager without a retryer.
	m = NewManager(100, nil)
	if m.retryer != nil {
		t.Error("expected no retryer when retry config is nil")
	}
}

func TestRegisterProviderWithEnabledFlag(t *testing.T) {
	m := NewManager(100)
	p := &mockProvider{name: "flagged", status: &core.ProviderStatus{Name: "flagged"}}

	if err := m.RegisterProvider("flagged", p, false); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if m.IsEnabled("flagged") {
		t.Error("expected provider registered with enabled=false to be disabled")
	}

	if err := m.RegisterProvider("flagged-on", p, true); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !m.IsEnabled("flagged-on") {
		t.Error("expected provider registered with enabled=true to be enabled")
	}
}

func TestGetProviderSchemaBuiltinReturnsCopy(t *testing.T) {
	m := NewManager(100)

	schema := m.GetProviderSchema("telegram")
	if len(schema) != 4 {
		t.Fatalf("expected 4 schema fields for telegram, got %d", len(schema))
	}
	if schema["token"] != "string" || schema["chat_id"] != "string" ||
		schema["parse_mode"] != "string" || schema["api_url"] != "string" {
		t.Errorf("unexpected telegram schema contents: %v", schema)
	}

	// The result must be a copy: mutating it must not leak into later calls.
	schema["token"] = "mutated"
	again := m.GetProviderSchema("telegram")
	if again["token"] != "string" {
		t.Errorf("expected fresh copy on each call, got %v", again)
	}
}

func TestDeliverDisabledProvider(t *testing.T) {
	m := NewManager(100)
	p := &mockProvider{name: "off", status: &core.ProviderStatus{Name: "off"}}
	if err := m.RegisterProvider("off", p, false); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{ID: "task-off", Provider: "off"}
	err := m.Deliver(context.Background(), task)
	if err == nil || err.Error() != "provider is disabled: off" {
		t.Errorf("expected disabled-provider error, got %v", err)
	}
}

func TestDeliverWithRetryer(t *testing.T) {
	m := newRetryTestManager()
	p := &mockProvider{name: "ok", status: &core.ProviderStatus{Name: "ok"}}
	if err := m.RegisterProvider("ok", p); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{
		ID:       "task-retry-ok",
		Provider: "ok",
		Payload:  core.DeliveryPayload{Kind: core.PayloadContent},
	}
	if err := m.Deliver(context.Background(), task); err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	log := m.GetLogByID("task-retry-ok")
	if log == nil {
		t.Fatal("expected a task log for the delivery")
	}
	if log.Status != "success" {
		t.Errorf("expected success status, got %q (error: %q)", log.Status, log.Error)
	}
}

func TestDeliverProviderFailureMarksLogFailed(t *testing.T) {
	m := newRetryTestManager()
	deliverErr := errors.New("smtp connection refused")
	if err := m.RegisterProvider("bad", &failingProvider{name: "bad", err: deliverErr}); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{ID: "task-fail", Provider: "bad"}
	err := m.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("expected the delivery error to propagate")
	}

	log := m.GetLogByID("task-fail")
	if log == nil {
		t.Fatal("expected a task log for the failed delivery")
	}
	if log.Status != "failed" {
		t.Errorf("expected failed status, got %q", log.Status)
	}
	if log.Error != "smtp connection refused" {
		t.Errorf("expected failure reason in log, got %q", log.Error)
	}
}

func TestEnableDisableNotFound(t *testing.T) {
	m := NewManager(100)

	if err := m.Enable("missing"); err == nil {
		t.Error("expected error when enabling a non-existent provider")
	}
	if err := m.Disable("missing"); err == nil {
		t.Error("expected error when disabling a non-existent provider")
	}
}

// Note on the remaining uncovered block in Deliver (manager.go:179.16): the
// `if err != nil` branch after GetProvider is unreachable. IsEnabled(name)
// only returns true when the provider is registered, and Manager offers no
// API that removes a registration, so GetProvider cannot fail at that point.
