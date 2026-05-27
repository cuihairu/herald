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

// Retryer handles retry logic
type Retryer struct {
	policy Policy
}

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

	return &Retryer{policy: policy}
}

// Execute executes a function with retry
func (r *Retryer) Execute(ctx context.Context, task *core.DeliveryTask, fn func() error) error {
	var lastErr error

	for i := 0; i <= r.policy.MaxRetries(); i++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if !r.policy.ShouldRetry(err, i) {
			return err
		}

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
