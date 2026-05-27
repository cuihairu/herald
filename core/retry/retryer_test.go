package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestNewRetryableError(t *testing.T) {
	baseErr := errors.New("base error")
	retryableErr := NewRetryableError(baseErr)
	if !errors.Is(retryableErr, baseErr) {
		t.Error("expected errors.Is to find base error")
	}
}

func TestNewRetryer(t *testing.T) {
	r := NewRetryer(nil)
	if r == nil {
		t.Fatal("expected non-nil retryer")
	}
	policy, ok := r.policy.(*ExponentialPolicy)
	if !ok {
		t.Fatal("expected exponential policy by default")
	}
	if policy.Max != 3 {
		t.Errorf("expected max 3, got %d", policy.Max)
	}
}

func TestExponentialPolicyNextDelay(t *testing.T) {
	policy := &ExponentialPolicy{InitialDelay: 100 * time.Millisecond, MaxDelay: time.Second}

	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, time.Second},
	}

	for _, tt := range tests {
		delay := policy.NextDelay(tt.retryCount)
		if delay != tt.expected {
			t.Errorf("retryCount %d: expected %v, got %v", tt.retryCount, tt.expected, delay)
		}
	}
}

func TestRetryerExecuteSuccess(t *testing.T) {
	r := NewRetryer(nil)
	task := &core.DeliveryTask{ID: "t1", Provider: "test"}
	calls := 0
	fn := func() error { calls++; return nil }

	if err := r.Execute(context.Background(), task, fn); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryerExecuteRetrySuccess(t *testing.T) {
	r := NewRetryer(&Config{Max: 3, Backoff: "fixed", InitialDelay: 10 * time.Millisecond})
	task := &core.DeliveryTask{ID: "t1", Provider: "test"}
	calls := 0
	fn := func() error {
		calls++
		if calls < 3 {
			return NewRetryableError(errors.New("transient"))
		}
		return nil
	}

	if err := r.Execute(context.Background(), task, fn); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryerExecuteMaxRetriesExceeded(t *testing.T) {
	r := NewRetryer(&Config{Max: 2, Backoff: "fixed", InitialDelay: 10 * time.Millisecond})
	task := &core.DeliveryTask{ID: "t1", Provider: "test"}
	calls := 0
	fn := func() error {
		calls++
		return NewRetryableError(errors.New("always fails"))
	}

	if err := r.Execute(context.Background(), task, fn); err == nil {
		t.Error("expected error after max retries")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryerExecuteNonRetryableError(t *testing.T) {
	r := NewRetryer(nil)
	task := &core.DeliveryTask{ID: "t1", Provider: "test"}
	calls := 0
	fn := func() error { calls++; return errors.New("permanent") }

	r.Execute(context.Background(), task, fn)
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryerExecuteContextCanceled(t *testing.T) {
	r := NewRetryer(&Config{Max: 10, Backoff: "exponential", InitialDelay: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	task := &core.DeliveryTask{ID: "t1", Provider: "test"}
	calls := 0
	fn := func() error {
		calls++
		if calls == 1 {
			cancel()
		}
		return NewRetryableError(errors.New("transient"))
	}

	if err := r.Execute(ctx, task, fn); err != context.Canceled {
		t.Errorf("expected context canceled, got %v", err)
	}
}
