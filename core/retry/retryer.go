package retry

import (
	"context"
	"fmt"
	"time"

	"github.com/cuihairu/herald/core"
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

// Policy is the retry policy
type Policy interface {
	// ShouldRetry returns true if the task should be retried
	ShouldRetry(err error, retryCount int) bool

	// NextDelay returns the delay before next retry
	NextDelay(retryCount int) time.Duration

	// MaxRetries returns the maximum retry count
	MaxRetries() int
}

// Config is the retry configuration
type Config struct {
	Max          int           `yaml:"max"`           // max retry count
	Backoff      string        `yaml:"backoff"`       // backoff type: fixed, exponential
	InitialDelay time.Duration `yaml:"initial_delay"` // initial delay
	MaxDelay     time.Duration `yaml:"max_delay"`     // max delay
}

// Retryer handles retry logic
type Retryer struct {
	policy Policy
}

// NewRetryer creates a new retryer
func NewRetryer(config *Config) *Retryer {
	var policy Policy

	if config == nil {
		config = &Config{
			Max:          3,
			Backoff:      "exponential",
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		}
	}

	switch config.Backoff {
	case "fixed":
		policy = &FixedPolicy{
			Max:   config.Max,
			Delay: config.InitialDelay,
		}
	case "exponential", "":
		policy = &ExponentialPolicy{
			Max:          config.Max,
			InitialDelay: config.InitialDelay,
			MaxDelay:     config.MaxDelay,
		}
	default:
		policy = &ExponentialPolicy{
			Max:          config.Max,
			InitialDelay: config.InitialDelay,
			MaxDelay:     config.MaxDelay,
		}
	}

	return &Retryer{policy: policy}
}

// Execute executes a function with retry
func (r *Retryer) Execute(ctx context.Context, task *core.Task, fn func() error) error {
	var lastErr error

	for i := 0; i <= r.policy.MaxRetries(); i++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !r.policy.ShouldRetry(err, i) {
			return err
		}

		// Wait before next retry
		delay := r.policy.NextDelay(i)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
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

	// Check if error is retryable
	_, isRetryable := err.(*RetryableError)
	return isRetryable
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

	_, isRetryable := err.(*RetryableError)
	return isRetryable
}

func (p *FixedPolicy) NextDelay(retryCount int) time.Duration {
	return p.Delay
}

func (p *FixedPolicy) MaxRetries() int {
	return p.Max
}
