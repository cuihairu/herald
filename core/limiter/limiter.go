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

	// Create never fails: unknown kinds fall back to token_bucket, so
	// there is no error path to guard (it returns one for API evolution).
	limiter, _ := m.factory.Create(config)

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
		Type:   "token_bucket",
		Rate:   10,  // 10 requests per second
		Burst:  100, // Allow bursts of 100
		Window: time.Minute,
	}
}

// TokenBucket implements token bucket rate limiting
type TokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	rate     float64
	burst    float64
	lastTime time.Time
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

// NewOnce returns a token bucket that grants exactly one token and never
// refills — a one-shot gate. It is used where a key may make a single
// attempt and must not be topped up by a rate (e.g. the login limiter's
// trial table, where a sustained flood must not mint fresh tokens at the
// full login rate).
func NewOnce() *TokenBucket {
	return &TokenBucket{
		tokens:   1,
		rate:     0,
		burst:    1,
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
	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		if tb.Allow() {
			return nil
		}

		// Calculate wait time
		tb.mu.Lock()
		waitTime := tb.timeUntilToken()
		tb.mu.Unlock()

		if timer == nil {
			timer = time.NewTimer(waitTime)
			timerC = timer.C
		} else {
			// Stop and drain a possibly already-fired timer before
			// resetting it; this avoids the leak that time.After caused
			// on every cancelled iteration.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(waitTime)
		}

		select {
		case <-timerC:
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
	if tb.rate <= 0 {
		// A one-shot bucket never refills: no finite delay makes a token
		// available. Return an unusable reservation (ok=false) rather than
		// an ok=true far-future one: the latter would let a caller Cancel()
		// and mint a token that was never reserved, re-arming a one-shot
		// gate. Dividing by zero would also yield an implementation-defined
		// duration.
		return &Reservation{
			ok:      false,
			limiter: tb,
		}
	}
	waitDuration := time.Duration(float64(time.Second) * tokensNeeded / tb.rate)

	return &Reservation{
		ok:        true,
		timeToAct: time.Now().Add(waitDuration),
		limiter:   tb,
	}
}

// refill adds tokens based on elapsed time
func (tb *TokenBucket) refill() {
	if tb.rate <= 0 {
		return
	}
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
	if tb.rate <= 0 {
		// A one-shot bucket never refills: no finite wait makes a token
		// available. Report the largest representable duration rather than
		// dividing by zero.
		return time.Duration(1<<63 - 1)
	}

	tokensNeeded := 1 - tb.tokens
	return time.Duration(float64(time.Second) * tokensNeeded / tb.rate)
}
