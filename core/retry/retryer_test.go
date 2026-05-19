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

	if retryableErr == nil {
		t.Fatal("expected non-nil error")
	}

	retryable, ok := retryableErr.(*RetryableError)
	if !ok {
		t.Fatal("expected RetryableError type")
	}

	if retryable.Err != baseErr {
		t.Error("expected base error to be wrapped")
	}
}

func TestRetryableErrorUnwrap(t *testing.T) {
	baseErr := errors.New("base")
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

func TestNewRetryerFixedPolicy(t *testing.T) {
	config := &Config{
		Max:          5,
		Backoff:      "fixed",
		InitialDelay: 100 * time.Millisecond,
	}

	r := NewRetryer(config)
	policy, ok := r.policy.(*FixedPolicy)
	if !ok {
		t.Fatal("expected fixed policy")
	}

	if policy.Max != 5 {
		t.Errorf("expected max 5, got %d", policy.Max)
	}
	if policy.Delay != 100*time.Millisecond {
		t.Errorf("expected delay 100ms, got %v", policy.Delay)
	}
}

func TestNewRetryerExponentialPolicy(t *testing.T) {
	config := &Config{
		Max:          10,
		Backoff:      "exponential",
		InitialDelay: 50 * time.Millisecond,
		MaxDelay:     time.Second,
	}

	r := NewRetryer(config)
	policy, ok := r.policy.(*ExponentialPolicy)
	if !ok {
		t.Fatal("expected exponential policy")
	}

	if policy.Max != 10 {
		t.Errorf("expected max 10, got %d", policy.Max)
	}
	if policy.InitialDelay != 50*time.Millisecond {
		t.Errorf("expected initial delay 50ms, got %v", policy.InitialDelay)
	}
}

func TestExponentialPolicyNextDelay(t *testing.T) {
	policy := &ExponentialPolicy{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     time.Second,
	}

	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{0, 100 * time.Millisecond},
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, time.Second}, // capped at MaxDelay
		{5, time.Second}, // capped at MaxDelay
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			delay := policy.NextDelay(tt.retryCount)
			if delay != tt.expected {
				t.Errorf("retryCount %d: expected %v, got %v", tt.retryCount, tt.expected, delay)
			}
		})
	}
}

func TestExponentialPolicyShouldRetry(t *testing.T) {
	policy := &ExponentialPolicy{Max: 3}

	retryableErr := NewRetryableError(errors.New("transient"))
	nonRetryableErr := errors.New("permanent")

	// Should retry retryable errors within limit
	if !policy.ShouldRetry(retryableErr, 0) {
		t.Error("expected retryable error to be retryable at count 0")
	}
	if !policy.ShouldRetry(retryableErr, 2) {
		t.Error("expected retryable error to be retryable at count 2")
	}
	if policy.ShouldRetry(retryableErr, 3) {
		t.Error("expected retryable error to not be retryable at max count")
	}

	// Should not retry non-retryable errors
	if policy.ShouldRetry(nonRetryableErr, 0) {
		t.Error("expected non-retryable error to not be retryable")
	}
}

func TestFixedPolicyNextDelay(t *testing.T) {
	policy := &FixedPolicy{
		Delay: 200 * time.Millisecond,
	}

	for i := 0; i < 5; i++ {
		delay := policy.NextDelay(i)
		if delay != 200*time.Millisecond {
			t.Errorf("retryCount %d: expected fixed delay 200ms, got %v", i, delay)
		}
	}
}

func TestRetryerExecuteSuccess(t *testing.T) {
	r := NewRetryer(nil)
	ctx := context.Background()
	task := &core.Task{}

	calls := 0
	fn := func() error {
		calls++
		return nil
	}

	err := r.Execute(ctx, task, fn)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryerExecuteRetrySuccess(t *testing.T) {
	r := NewRetryer(&Config{
		Max:          3,
		Backoff:      "fixed",
		InitialDelay: 10 * time.Millisecond,
	})
	ctx := context.Background()
	task := &core.Task{}

	calls := 0
	fn := func() error {
		calls++
		if calls < 3 {
			return NewRetryableError(errors.New("transient"))
		}
		return nil
	}

	start := time.Now()
	err := r.Execute(ctx, task, fn)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
	// Should have waited at least 2 times (between first 3 calls)
	if elapsed < 20*time.Millisecond {
		t.Errorf("expected delay >= 20ms, got %v", elapsed)
	}
}

func TestRetryerExecuteMaxRetriesExceeded(t *testing.T) {
	r := NewRetryer(&Config{
		Max:          2,
		Backoff:      "fixed",
		InitialDelay: 10 * time.Millisecond,
	})
	ctx := context.Background()
	task := &core.Task{}

	calls := 0
	fn := func() error {
		calls++
		return NewRetryableError(errors.New("always fails"))
	}

	err := r.Execute(ctx, task, fn)
	if err == nil {
		t.Error("expected error after max retries")
	}
	if calls != 3 { // initial + 2 retries
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryerExecuteNonRetryableError(t *testing.T) {
	r := NewRetryer(nil)
	ctx := context.Background()
	task := &core.Task{}

	calls := 0
	fn := func() error {
		calls++
		return errors.New("permanent error")
	}

	err := r.Execute(ctx, task, fn)
	if err == nil {
		t.Error("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry for non-retryable), got %d", calls)
	}
}

func TestRetryerExecuteContextCanceled(t *testing.T) {
	r := NewRetryer(&Config{
		Max:          10,
		Backoff:      "exponential",
		InitialDelay: time.Second,
	})
	ctx, cancel := context.WithCancel(context.Background())
	task := &core.Task{}

	calls := 0
	fn := func() error {
		calls++
		if calls == 1 {
			// Cancel after first call
			cancel()
		}
		return NewRetryableError(errors.New("transient"))
	}

	err := r.Execute(ctx, task, fn)
	if err != context.Canceled {
		t.Errorf("expected context canceled, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestExponentialDelayGrowth(t *testing.T) {
	policy := &ExponentialPolicy{
		InitialDelay: 10 * time.Millisecond,
		MaxDelay:     5 * time.Second,
	}

	// Test exponential growth: 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1280ms, 2560ms, 5000ms
	expected := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		40 * time.Millisecond,
		80 * time.Millisecond,
		160 * time.Millisecond,
		320 * time.Millisecond,
		640 * time.Millisecond,
		1280 * time.Millisecond,
		2560 * time.Millisecond,
		5 * time.Second, // capped
		5 * time.Second, // capped
	}

	for i, exp := range expected {
		delay := policy.NextDelay(i)
		if delay != exp {
			t.Errorf("iteration %d: expected %v, got %v", i, exp, delay)
		}
	}
}

func TestRetryerDefaultConfig(t *testing.T) {
	r := NewRetryer(nil)
	task := &core.Task{}
	ctx := context.Background()

	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 4 {
			return NewRetryableError(errors.New("fail"))
		}
		return nil
	}

	// Default is max 3 retries (4 total attempts)
	err := r.Execute(ctx, task, fn)
	if err == nil {
		// Default max is 3, so 4 attempts total
		if attempts != 4 {
			t.Errorf("expected 4 attempts with default config, got %d", attempts)
		}
	} else {
		t.Errorf("expected success with default config, got %v", err)
	}
}
