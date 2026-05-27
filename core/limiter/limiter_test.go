package limiter

import (
	"context"
	"testing"
	"time"
)

func TestNewTokenBucket(t *testing.T) {
	tb := NewTokenBucket(10, 5)

	if tb == nil {
		t.Fatal("expected non-nil token bucket")
	}
	if tb.rate != 10 {
		t.Errorf("expected rate 10, got %f", tb.rate)
	}
	if tb.burst != 5 {
		t.Errorf("expected burst 5, got %f", tb.burst)
	}
}

func TestTokenBucketAllow(t *testing.T) {
	tb := NewTokenBucket(10, 5)

	// Should allow initial requests up to burst
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("expected request %d to be allowed", i)
		}
	}

	// Next request should be denied
	if tb.Allow() {
		t.Error("expected request to be denied after burst exhausted")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	tb := NewTokenBucket(100, 2)

	// Exhaust initial tokens
	if !tb.Allow() {
		t.Error("expected first request to be allowed")
	}
	if !tb.Allow() {
		t.Error("expected second request to be allowed")
	}

	// Should be denied
	if tb.Allow() {
		t.Error("expected request to be denied")
	}

	// Wait for refill
	time.Sleep(20 * time.Millisecond)

	// Should have new tokens
	if !tb.Allow() {
		t.Error("expected request to be allowed after refill")
	}
}

func TestTokenBucketWait(t *testing.T) {
	tb := NewTokenBucket(1000, 1)

	// Exhaust tokens
	if !tb.Allow() {
		t.Error("expected first request to be allowed")
	}

	// Should need to wait for refill
	start := time.Now()
	ctx := context.Background()
	err := tb.Wait(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Wait should have taken some time
	elapsed := time.Since(start)
	if elapsed < 1*time.Millisecond {
		t.Errorf("expected wait time >= 1ms, got %v", elapsed)
	}
}

func TestTokenBucketWaitCancel(t *testing.T) {
	tb := NewTokenBucket(1, 1)

	// Exhaust tokens
	if !tb.Allow() {
		t.Error("expected first request to be allowed")
	}

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Should timeout waiting
	err := tb.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("expected deadline exceeded error, got %v", err)
	}
}

func TestTokenBucketReservation(t *testing.T) {
	tb := NewTokenBucket(10, 5)

	// Exhaust all tokens
	for i := 0; i < 5; i++ {
		if !tb.Allow() {
			t.Errorf("expected request %d to be allowed", i)
		}
	}

	// Make reservation
	reservation := tb.Reservation()

	if !reservation.OK() {
		t.Error("expected reservation to be OK")
	}

	// Reservation should have a delay
	delay := reservation.Delay()
	if delay <= 0 {
		t.Error("expected positive delay")
	}

	// Cancel reservation
	reservation.Cancel()

	// Tokens should be returned
	if !tb.Allow() {
		t.Error("expected request to be allowed after canceling reservation")
	}
}

func TestLimiterManager(t *testing.T) {
	manager := NewManager()

	config := &Config{
		Type:  "token_bucket",
		Rate:  10,
		Burst: 5,
	}

	// Create limiter
	limiter, err := manager.GetOrCreate("test-provider", config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if limiter == nil {
		t.Error("expected non-nil limiter")
	}

	// Get existing limiter
	sameLimiter, err := manager.GetOrCreate("test-provider", config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if sameLimiter != limiter {
		t.Error("expected same limiter instance")
	}

	// Check if exists
	existsLimiter, ok := manager.Get("test-provider")
	if !ok {
		t.Error("expected limiter to exist")
	}
	if existsLimiter != limiter {
		t.Error("expected same limiter instance")
	}

	// Remove limiter
	manager.Remove("test-provider")

	// Should not exist
	_, ok = manager.Get("test-provider")
	if ok {
		t.Error("expected limiter to not exist")
	}
}

func TestFactoryCreate(t *testing.T) {
	factory := NewFactory()

	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "default config",
			config:      nil,
			expectError: false,
		},
		{
			name: "token bucket",
			config: &Config{
				Type:  "token_bucket",
				Rate:  10,
				Burst: 5,
			},
			expectError: false,
		},
		{
			name: "custom rate",
			config: &Config{
				Type:  "token_bucket",
				Rate:  100,
				Burst: 50,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter, err := factory.Create(tt.config)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if !tt.expectError && limiter == nil {
				t.Error("expected non-nil limiter")
			}
		})
	}
}
