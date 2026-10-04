package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

// TestExecuteTracksAttemptsOnTask asserts the retryer drives the task's
// delivery state machine: every attempt after the first runs under
// StatusRetrying and RetryCount counts the retries already performed.
func TestExecuteTracksAttemptsOnTask(t *testing.T) {
	r := NewRetryer(&Config{Max: 3, Backoff: "fixed", InitialDelay: time.Millisecond})
	task := &core.DeliveryTask{ID: "t1", Provider: "test"}

	var saw []core.DeliveryStatus
	attempts := 0
	fn := func() error {
		saw = append(saw, task.Status)
		attempts++
		if attempts < 3 {
			return NewRetryableError(errors.New("transient"))
		}
		return nil
	}

	if err := r.Execute(context.Background(), task, fn); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	// Between attempts the task sits in "retrying" (the manager marks each
	// attempt "delivering" right before calling the provider).
	if len(saw) != 3 || saw[1] != core.StatusRetrying || saw[2] != core.StatusRetrying {
		t.Errorf("attempts after the first must run under retrying, saw %v", saw)
	}
	if task.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2 retries before the success", task.RetryCount)
	}
}

// TestExecuteDeadOnExhaustedRetryable asserts retryable failures that use
// up every attempt surface as ErrMaxRetries — the runtime maps that to
// the dead state — with the historical message text preserved.
func TestExecuteDeadOnExhaustedRetryable(t *testing.T) {
	r := NewRetryer(&Config{Max: 2, Backoff: "fixed", InitialDelay: time.Millisecond})
	task := &core.DeliveryTask{ID: "t1"}

	calls := 0
	err := r.Execute(context.Background(), task, func() error {
		calls++
		return NewRetryableError(errors.New("flaky"))
	})
	if !errors.Is(err, ErrMaxRetries) {
		t.Fatalf("Execute() = %v, want ErrMaxRetries", err)
	}
	if got := err.Error(); got != "max retries exceeded: flaky" {
		t.Errorf("error message = %q, want %q", got, "max retries exceeded: flaky")
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (1 attempt + Max 2 retries)", calls)
	}
	if task.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", task.RetryCount)
	}
	if task.Status != core.StatusRetrying {
		t.Errorf("Status = %q, want retrying (last owed retry never ran)", task.Status)
	}
}

// decliningPolicy declines every retry while advertising headroom, so the
// Execute loop returns the bare failure instead of ErrMaxRetries.
type decliningPolicy struct{}

func (decliningPolicy) ShouldRetry(err error, retryCount int) bool { return false }
func (decliningPolicy) MaxRetries() int                            { return 5 }
func (decliningPolicy) NextDelay(retryCount int) time.Duration     { return 0 }

func TestExecutePolicyDeclineIsBareFailure(t *testing.T) {
	r := &Retryer{policy: decliningPolicy{}}
	task := &core.DeliveryTask{}
	inner := errors.New("declined")

	err := r.Execute(context.Background(), task, func() error { return inner })
	if !errors.Is(err, inner) {
		t.Errorf("Execute() = %v, want the bare inner error", err)
	}
	if errors.Is(err, ErrMaxRetries) {
		t.Error("a policy decline is a plain failure, not dead")
	}
	if task.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0 (no retry was taken)", task.RetryCount)
	}
}