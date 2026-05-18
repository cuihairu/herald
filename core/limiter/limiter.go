package limiter

import (
	"context"
	"sync"
	"time"
)

// Limiter defines the rate limiting interface
type Limiter interface {
	// Allow checks if a request is allowed
	Allow() bool

	// Wait waits until a request is allowed
	Wait(ctx context.Context) error

	// Reserve reserves a slot for a request
	Reservation() *Reservation
}

// LimiterManager manages multiple limiters
type Manager struct {
	mu       sync.RWMutex
	limiters map[string]Limiter
	factory  *Factory
}

// NewManager creates a new limiter manager
func NewManager() *Manager {
	return &Manager{
		limiters: make(map[string]Limiter),
		factory:  NewFactory(),
	}
}

// GetOrCreate gets or creates a limiter for a provider
func (m *Manager) GetOrCreate(provider string, config *Config) (Limiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if limiter, ok := m.limiters[provider]; ok {
		return limiter, nil
	}

	limiter, err := m.factory.Create(config)
	if err != nil {
		return nil, err
	}

	m.limiters[provider] = limiter
	return limiter, nil
}

// Get gets a limiter for a provider
func (m *Manager) Get(provider string) (Limiter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	limiter, ok := m.limiters[provider]
	return limiter, ok
}

// Remove removes a limiter
func (m *Manager) Remove(provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.limiters, provider)
}

// Config is the limiter configuration
type Config struct {
	// Rate limit type: token_bucket, leaky_bucket
	Type string `yaml:"type"`

	// Requests per second
	Rate float64 `yaml:"rate"`

	// Burst size (max tokens)
	Burst int `yaml:"burst"`

	// Window size (for sliding window)
	Window time.Duration `yaml:"window"`
}

// Factory creates limiters
type Factory struct{}

// NewFactory creates a new factory
func NewFactory() *Factory {
	return &Factory{}
}

// Create creates a limiter from config
func (f *Factory) Create(config *Config) (Limiter, error) {
	if config == nil {
		config = DefaultConfig()
	}

	switch config.Type {
	case "token_bucket", "", "default":
		return NewTokenBucket(config.Rate, config.Burst), nil
	default:
		return NewTokenBucket(config.Rate, config.Burst), nil
	}
}

// DefaultConfig returns default limiter config
func DefaultConfig() *Config {
	return &Config{
		Type:  "token_bucket",
		Rate:  10,    // 10 requests per second
		Burst: 100,   // Allow bursts of 100
		Window: time.Minute,
	}
}

// TokenBucket implements token bucket rate limiting
type TokenBucket struct {
	mu        sync.Mutex
	tokens    float64
	rate      float64
	burst     float64
	lastTime  time.Time
}

// NewTokenBucket creates a new token bucket limiter
func NewTokenBucket(rate float64, burst int) *TokenBucket {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = 10
	}

	return &TokenBucket{
		tokens:   float64(burst),
		rate:     rate,
		burst:    float64(burst),
		lastTime: time.Now(),
	}
}

// Allow checks if a request is allowed
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}

	return false
}

// Wait waits until a request is allowed
func (tb *TokenBucket) Wait(ctx context.Context) error {
	for {
		if tb.Allow() {
			return nil
		}

		// Calculate wait time
		tb.mu.Lock()
		waitTime := tb.timeUntilToken()
		tb.mu.Unlock()

		select {
		case <-time.After(waitTime):
			// Try again
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Reservation represents a rate limit reservation
type Reservation struct {
	ok        bool
	timeToAct time.Time
	limiter   *TokenBucket
}

// OK returns true if the reservation is valid
func (r *Reservation) OK() bool {
	return r.ok
}

// Delay returns the delay until the reservation is valid
func (r *Reservation) Delay() time.Duration {
	if !r.ok {
		return 0
	}
	delay := time.Until(r.timeToAct)
	if delay < 0 {
		return 0
	}
	return delay
}

// Cancel cancels the reservation
func (r *Reservation) Cancel() {
	if !r.ok {
		return
	}

	r.limiter.mu.Lock()
	defer r.limiter.mu.Unlock()

	r.limiter.tokens++
	r.ok = false
}

// Reservation creates a reservation
func (tb *TokenBucket) Reservation() *Reservation {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()

	if tb.tokens >= 1 {
		tb.tokens--
		return &Reservation{
			ok:        true,
			timeToAct: time.Now(),
			limiter:   tb,
		}
	}

	// Calculate when token will be available
	tokensNeeded := 1 - tb.tokens
	waitDuration := time.Duration(float64(time.Second) * tokensNeeded / tb.rate)

	return &Reservation{
		ok:        true,
		timeToAct: time.Now().Add(waitDuration),
		limiter:   tb,
	}
}

// refill adds tokens based on elapsed time
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastTime)

	if elapsed > 0 {
		tokens := elapsed.Seconds() * tb.rate
		tb.tokens += tokens
		if tb.tokens > tb.burst {
			tb.tokens = tb.burst
		}
		tb.lastTime = now
	}
}

// timeUntilToken calculates time until next token is available
func (tb *TokenBucket) timeUntilToken() time.Duration {
	tb.refill()

	if tb.tokens >= 1 {
		return 0
	}

	tokensNeeded := 1 - tb.tokens
	return time.Duration(float64(time.Second) * tokensNeeded / tb.rate)
}
