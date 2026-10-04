package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/errclass"
)

// withAfter overrides the backoff wait seam: every delay is recorded and
// released immediately, so hint paths assert exactly what would have been
// slept without waiting for it.
func withAfter(t *testing.T) *[]time.Duration {
	t.Helper()
	recorded := &[]time.Duration{}
	orig := after
	after = func(d time.Duration) <-chan time.Time {
		*recorded = append(*recorded, d)
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	t.Cleanup(func() { after = orig })
	return recorded
}

// TestExecuteHonorsRetryAfterHint: a rate-limited failure that states a
// wait replaces the backoff curve — even when the curve would be far
// larger (a 1h exponential step cut to the server's 25ms).
func TestExecuteHonorsRetryAfterHint(t *testing.T) {
	recorded := withAfter(t)

	r := NewRetryer(&Config{Max: 2, Backoff: "exponential", InitialDelay: time.Hour, MaxDelay: time.Hour})
	task := &core.DeliveryTask{}
	calls := 0
	err := r.Execute(context.Background(), task, func() error {
		calls++
		if calls == 1 {
			return errclass.NewWithRetryAfter(errclass.RateLimited, errors.New("429"), 25*time.Millisecond)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Execute = %v, want nil", err)
	}
	if len(*recorded) != 1 || (*recorded)[0] != 25*time.Millisecond {
		t.Fatalf("waits = %v, want exactly the 25ms advice", *recorded)
	}
	// Status stays "retrying" here: flipping to delivered is the runtime
	// manager's job after Execute returns nil (core/runtime/manager.go).
	if task.RetryCount != 1 {
		t.Errorf("task retry=%d, want 1", task.RetryCount)
	}
}

// TestExecuteCapsRetryAfterByMaxDelay: an upstream asking for hours can't
// park the task — the advice is clamped to MaxDelay.
func TestExecuteCapsRetryAfterByMaxDelay(t *testing.T) {
	recorded := withAfter(t)

	r := NewRetryer(&Config{Max: 1, Backoff: "exponential", InitialDelay: time.Second, MaxDelay: time.Minute})
	task := &core.DeliveryTask{}
	err := r.Execute(context.Background(), task, func() error {
		return errclass.NewWithRetryAfter(errclass.RateLimited, errors.New("429"), 2*time.Hour)
	})
	if !errors.Is(err, ErrMaxRetries) {
		t.Fatalf("Execute = %v, want ErrMaxRetries (budget spent waiting)", err)
	}
	if len(*recorded) != 1 || (*recorded)[0] != time.Minute {
		t.Fatalf("waits = %v, want the advice capped to MaxDelay (1m)", *recorded)
	}
}

// TestExecuteRetryAfterUncappedWhenMaxDelayZero: MaxDelay unset means the
// operator didn't ask for a cap, so the advice is taken as stated.
func TestExecuteRetryAfterUncappedWhenMaxDelayZero(t *testing.T) {
	recorded := withAfter(t)

	r := NewRetryer(&Config{Max: 1, Backoff: "fixed", InitialDelay: time.Second, MaxDelay: 0})
	task := &core.DeliveryTask{}
	_ = r.Execute(context.Background(), task, func() error {
		return errclass.NewWithRetryAfter(errclass.RateLimited, errors.New("429"), 40*time.Millisecond)
	})
	if len(*recorded) != 1 || (*recorded)[0] != 40*time.Millisecond {
		t.Fatalf("waits = %v, want the advice uncapped (40ms)", *recorded)
	}
}

// TestExecutePlainBackoffWithoutHint: a rate-limited failure without
// advice keeps the policy's own delay.
func TestExecutePlainBackoffWithoutHint(t *testing.T) {
	recorded := withAfter(t)

	r := NewRetryer(&Config{Max: 1, Backoff: "fixed", InitialDelay: 300 * time.Millisecond})
	task := &core.DeliveryTask{}
	_ = r.Execute(context.Background(), task, func() error {
		return errclass.New(errclass.RateLimited, errors.New("429"))
	})
	if len(*recorded) != 1 || (*recorded)[0] != 300*time.Millisecond {
		t.Fatalf("waits = %v, want the fixed policy delay (300ms)", *recorded)
	}
}
