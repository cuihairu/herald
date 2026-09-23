package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
)

func TestRetryableErrorErrorMessage(t *testing.T) {
	inner := errors.New("boom")
	err := NewRetryableError(inner)
	if got := err.Error(); got != "boom" {
		t.Errorf("Error() = %q, want %q", got, "boom")
	}
}

// alwaysRetryPolicy retries everything for a fixed number of rounds, so the
// Execute loop can exhaust its iterations and exit via the loop condition.
type alwaysRetryPolicy struct {
	maxRetries int
	delay      time.Duration
}

func (p *alwaysRetryPolicy) ShouldRetry(err error, retryCount int) bool { return true }
func (p *alwaysRetryPolicy) MaxRetries() int                            { return p.maxRetries }
func (p *alwaysRetryPolicy) NextDelay(retryCount int) time.Duration     { return p.delay }

func TestRetryerExecuteLoopExhaustion(t *testing.T) {
	r := &Retryer{policy: &alwaysRetryPolicy{maxRetries: 2, delay: time.Millisecond}}
	task := &core.DeliveryTask{ID: "t1", Provider: "test"}
	inner := errors.New("persistent failure")
	calls := 0
	fn := func() error { calls++; return inner }

	err := r.Execute(context.Background(), task, fn)
	if err == nil {
		t.Fatal("Execute() = nil, want max retries exceeded error")
	}
	if !errors.Is(err, inner) {
		t.Errorf("Execute() error = %v, want wrapped persistent failure", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (i = 0, 1, 2)", calls)
	}
}

func TestExponentialPolicyShouldRetryMaxReached(t *testing.T) {
	p := &ExponentialPolicy{Max: 3}
	err := NewRetryableError(errors.New("transient"))

	if !p.ShouldRetry(err, 2) {
		t.Error("ShouldRetry(retryCount=2) = false, want true below max")
	}
	if p.ShouldRetry(err, 3) {
		t.Error("ShouldRetry(retryCount=3) = true, want false at max")
	}
	if p.ShouldRetry(err, 10) {
		t.Error("ShouldRetry(retryCount=10) = true, want false beyond max")
	}
}
