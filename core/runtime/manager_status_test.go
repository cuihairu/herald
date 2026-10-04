package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/limiter"
	"github.com/cuihairu/herald/core/retry"
)

// probeProvider fails the first n attempts with failErr and succeeds
// afterwards; calls counts the attempts and statuses records what the
// delivery state machine looked like at every attempt.
type probeProvider struct {
	name     string
	failures int
	failErr  error
	calls    int
	statuses []core.DeliveryStatus
}

func (p *probeProvider) Deliver(ctx context.Context, task *core.DeliveryTask) error {
	p.statuses = append(p.statuses, task.Status)
	p.calls++
	if p.calls <= p.failures {
		return p.failErr
	}
	return nil
}

func (p *probeProvider) Name() string { return p.name }

func (p *probeProvider) Type() string { return "probe" }

func (p *probeProvider) Status() *core.ProviderStatus {
	return &core.ProviderStatus{Name: p.name, Type: "probe", Status: "available"}
}

func TestDeliverMarksStatusDelivered(t *testing.T) {
	m := newRetryTestManager()
	p := &mockProvider{name: "ok", status: &core.ProviderStatus{Name: "ok"}}
	if err := m.RegisterProvider("ok", p); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{
		ID:       "st-ok",
		Provider: "ok",
		Status:   core.StatusQueued,
		Payload:  core.DeliveryPayload{Kind: core.PayloadContent},
	}
	if err := m.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if task.Status != core.StatusDelivered {
		t.Errorf("Status = %q, want %q", task.Status, core.StatusDelivered)
	}
	if task.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0 on a first-attempt success", task.RetryCount)
	}
}

// TestDeliverMarksEveryAttemptDelivering asserts the attempt wrapper flips
// the task back to "delivering" before each provider call, including
// retries (the retryer parks it in "retrying" between attempts).
func TestDeliverMarksEveryAttemptDelivering(t *testing.T) {
	m := newRetryTestManager()
	probe := &probeProvider{
		name:     "flaky-ok",
		failures: 2,
		failErr:  retry.NewRetryableError(errors.New("transient")),
	}
	if err := m.RegisterProvider("flaky-ok", probe); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{
		ID:       "st-attempts",
		Provider: "flaky-ok",
		Status:   core.StatusQueued,
		Payload:  core.DeliveryPayload{Kind: core.PayloadContent},
	}
	if err := m.Deliver(context.Background(), task); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(probe.statuses) != 3 {
		t.Fatalf("expected 3 delivery attempts, got %v", probe.statuses)
	}
	for _, st := range probe.statuses {
		if st != core.StatusDelivering {
			t.Fatalf("every attempt must run under delivering, saw %v", probe.statuses)
		}
	}
	if task.Status != core.StatusDelivered {
		t.Errorf("Status = %q, want %q", task.Status, core.StatusDelivered)
	}
	if task.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2 retries before the success", task.RetryCount)
	}

	log := m.GetLogByID("st-attempts")
	if log == nil || log.Status != "success" {
		t.Fatalf("retried success must log success, got %+v", log)
	}
}

func TestDeliverMarksStatusFailed(t *testing.T) {
	m := newRetryTestManager()
	if err := m.RegisterProvider("dead-on-arrival", &failingProvider{name: "dead-on-arrival", err: errors.New("permanent")}); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{ID: "st-fail", Provider: "dead-on-arrival", Status: core.StatusQueued}
	err := m.Deliver(context.Background(), task)
	if err == nil {
		t.Fatal("expected the delivery error to propagate")
	}
	if task.Status != core.StatusFailed {
		t.Errorf("Status = %q, want %q", task.Status, core.StatusFailed)
	}
	if errors.Is(err, retry.ErrMaxRetries) {
		t.Error("a permanent failure is not dead")
	}
	if task.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0 (no retry was taken)", task.RetryCount)
	}
}

func TestDeliverMarksStatusDead(t *testing.T) {
	m := newRetryTestManager() // Max 2 -> three attempts, then spent
	if err := m.RegisterProvider("flaky", &failingProvider{name: "flaky", err: retry.NewRetryableError(errors.New("flaky"))}); err != nil {
		t.Fatal(err)
	}

	task := &core.DeliveryTask{ID: "st-dead", Provider: "flaky", Status: core.StatusQueued}
	err := m.Deliver(context.Background(), task)
	if !errors.Is(err, retry.ErrMaxRetries) {
		t.Fatalf("Deliver() = %v, want ErrMaxRetries", err)
	}
	if task.Status != core.StatusDead {
		t.Errorf("Status = %q, want %q", task.Status, core.StatusDead)
	}
	if task.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2 retries performed", task.RetryCount)
	}

	// The task log keeps its wire vocabulary: dead maps to "failed".
	log := m.GetLogByID("st-dead")
	if log == nil || log.Status != "failed" {
		t.Fatalf("dead delivery must log as failed, got %+v", log)
	}
	if !strings.Contains(log.Error, "max retries exceeded") {
		t.Errorf("log error %q must say max retries exceeded", log.Error)
	}
}

func TestDeliverRateLimitAbortMarksStatusFailed(t *testing.T) {
	m := newRetryTestManager()
	p := &mockProvider{name: "throttled", status: &core.ProviderStatus{Name: "throttled"}}
	if err := m.RegisterProvider("throttled", p); err != nil {
		t.Fatal(err)
	}
	if err := m.SetProviderLimiter("throttled", &limiter.Config{Type: "token_bucket", Rate: 0.001, Burst: 1}); err != nil {
		t.Fatalf("SetProviderLimiter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	first := &core.DeliveryTask{ID: "st-abort-1", Provider: "throttled", Status: core.StatusQueued, Payload: core.DeliveryPayload{Kind: core.PayloadContent}}
	if err := m.Deliver(ctx, first); err != nil {
		t.Fatalf("first delivery should pass the limiter, got %v", err)
	}

	task := &core.DeliveryTask{ID: "st-abort-2", Provider: "throttled", Status: core.StatusQueued, Payload: core.DeliveryPayload{Kind: core.PayloadContent}}
	if err := m.Deliver(ctx, task); err == nil {
		t.Fatal("second delivery should be throttled past the test deadline")
	}
	if task.Status != core.StatusFailed {
		t.Errorf("Status = %q, want %q", task.Status, core.StatusFailed)
	}
}
