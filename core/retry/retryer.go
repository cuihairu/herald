package retry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/errclass"
	"github.com/cuihairu/herald/core/httpclient"
)

// RetryableError is an error that can be retried
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// NewRetryableError creates a new retryable error
func NewRetryableError(err error) error {
	return &RetryableError{Err: err}
}

// isRetryable reports whether err is worth a later attempt. A classified
// error (core/errclass, §15) speaks for itself: the class decides.
// Otherwise providers mark transient failures (network errors, HTTP
// 408/429/5xx) with httpclient.WithRetry; errors.As sees through any
// additional wrapping.
func isRetryable(err error) bool {
	if class, ok := errclass.Of(err); ok {
		return errclass.Retryable(class)
	}
	var local *RetryableError
	if errors.As(err, &local) {
		return true
	}
	var httpErr *httpclient.RetryableError
	return errors.As(err, &httpErr)
}

// Policy is the retry policy
type Policy interface {
	ShouldRetry(err error, retryCount int) bool
	NextDelay(retryCount int) time.Duration
	MaxRetries() int
}

// Config is the retry configuration
type Config struct {
	Max          int           `yaml:"max"`
	Backoff      string        `yaml:"backoff"`
	InitialDelay time.Duration `yaml:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay"`
}

// ErrMaxRetries marks a delivery that retryable errors exhausted: every
// attempt failed and the last error was retryable. errors.Is lets the
// runtime distinguish "dead" (retries owed but spent) from "failed"
// (nothing to retry).
var ErrMaxRetries = errors.New("max retries exceeded")

// Retryer handles retry logic
type Retryer struct {
	policy Policy
	// maxDelay caps a server-advised Retry-After wait so an upstream
	// asking for days can't park a delivery task indefinitely. Zero means
	// no cap (honour the advice as stated).
	maxDelay time.Duration
}

// after is the backoff wait, a package variable so tests can assert the
// delay without actually sleeping it (same seam pattern as the SDK's
// writeControl).
var after = time.After

// NewRetryer creates a new retryer
func NewRetryer(config *Config) *Retryer {
	if config == nil {
		config = &Config{
			Max:          3,
			Backoff:      "exponential",
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		}
	}

	var policy Policy

	switch config.Backoff {
	case "fixed":
		policy = &FixedPolicy{
			Max:   config.Max,
			Delay: config.InitialDelay,
		}
	default:
		policy = &ExponentialPolicy{
			Max:          config.Max,
			InitialDelay: config.InitialDelay,
			MaxDelay:     config.MaxDelay,
		}
	}

	return &Retryer{policy: policy, maxDelay: config.MaxDelay}
}

// MaxAttempts is the total number of delivery attempts the policy allows:
// the initial try plus every retry in the budget. The runtime snapshots it
// onto the task at the start of delivery (DeliveryTask.MaxAttempts), so
// observers see the budget without reaching into the policy.
func (r *Retryer) MaxAttempts() int {
	return r.policy.MaxRetries() + 1
}

// Execute executes a function with retry
func (r *Retryer) Execute(ctx context.Context, task *core.DeliveryTask, fn func() error) error {
	var lastErr error

	for i := 0; i <= r.policy.MaxRetries(); i++ {
		err := fn()
		if err == nil {
			task.RetryCount = i
			return nil
		}

		// The attempt that just finished: 0 on the initial try, otherwise
		// how many retries it took to get here.
		task.RetryCount = i
		lastErr = err

		if !r.policy.ShouldRetry(err, i) {
			// Retryable errors that spent every attempt are dead, not
			// failed: retries were owed but exhausted. errors.Is tells the
			// runtime apart; the message text keeps its historical shape.
			if i >= r.policy.MaxRetries() && isRetryable(err) {
				return fmt.Errorf("%w: %w", ErrMaxRetries, err)
			}
			return err
		}

		// A retry is owed: the task leaves "delivering" and waits on its
		// backoff under StatusRetrying; the next attempt flips it back.
		task.Status = core.StatusRetrying

		delay := r.policy.NextDelay(i)
		// A server-stated wait (Retry-After on a rate-limited failure)
		// beats the backoff curve — the upstream knows when to come back.
		// Capped by MaxDelay so the advice can't park the task forever.
		if hint, ok := errclass.RetryAfterOf(err); ok {
			if r.maxDelay > 0 && hint > r.maxDelay {
				hint = r.maxDelay
			}
			delay = hint
		}
		select {
		case <-after(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("%w: %w", ErrMaxRetries, lastErr)
}

// ExponentialPolicy implements exponential backoff
type ExponentialPolicy struct {
	Max          int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func (p *ExponentialPolicy) ShouldRetry(err error, retryCount int) bool {
	if retryCount >= p.Max {
		return false
	}
	return isRetryable(err)
}

func (p *ExponentialPolicy) NextDelay(retryCount int) time.Duration {
	delay := p.InitialDelay * time.Duration(1<<uint(retryCount))
	if p.MaxDelay > 0 && delay > p.MaxDelay {
		delay = p.MaxDelay
	}
	return delay
}

func (p *ExponentialPolicy) MaxRetries() int {
	return p.Max
}

// FixedPolicy implements fixed delay
type FixedPolicy struct {
	Max   int
	Delay time.Duration
}

func (p *FixedPolicy) ShouldRetry(err error, retryCount int) bool {
	if retryCount >= p.Max {
		return false
	}
	return isRetryable(err)
}

func (p *FixedPolicy) NextDelay(retryCount int) time.Duration {
	return p.Delay
}

func (p *FixedPolicy) MaxRetries() int {
	return p.Max
}
