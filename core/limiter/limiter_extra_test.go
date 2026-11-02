package limiter

import (
	"context"
	"testing"
	"time"
)

func TestFactoryCreateUnknownType(t *testing.T) {
	f := &Factory{}
	l, err := f.Create(&Config{Type: "nonexistent-type", Rate: 5, Burst: 5})
	if err != nil {
		t.Fatalf("Create(unknown type) error = %v, want nil (falls back to token bucket)", err)
	}
	if _, ok := l.(*TokenBucket); !ok {
		t.Errorf("Create(unknown type) = %T, want *TokenBucket fallback", l)
	}
}

func TestNewTokenBucketClampsInvalidValues(t *testing.T) {
	tb := NewTokenBucket(0, 0)
	if tb.rate != 1 {
		t.Errorf("rate = %v, want clamped to 1", tb.rate)
	}
	if tb.burst != 10 {
		t.Errorf("burst = %v, want clamped to 10", tb.burst)
	}

	tb = NewTokenBucket(-5, -3)
	if tb.rate != 1 || tb.burst != 10 {
		t.Errorf("rate/burst = %v/%v, want clamped 1/10", tb.rate, tb.burst)
	}
}

func TestReservationDelayNotOK(t *testing.T) {
	r := &Reservation{ok: false}
	if r.Delay() != 0 {
		t.Errorf("Delay() on not-ok reservation = %v, want 0", r.Delay())
	}
}

func TestReservationDelayPast(t *testing.T) {
	r := &Reservation{ok: true, timeToAct: time.Now().Add(-time.Hour)}
	if got := r.Delay(); got != 0 {
		t.Errorf("Delay() with past timeToAct = %v, want 0", got)
	}
}

func TestReservationDelayFuture(t *testing.T) {
	r := &Reservation{ok: true, timeToAct: time.Now().Add(2 * time.Second)}
	got := r.Delay()
	if got <= 0 || got > 2*time.Second {
		t.Errorf("Delay() with future timeToAct = %v, want in (0, 2s]", got)
	}
}

func TestReservationCancelNotOK(t *testing.T) {
	tb := NewTokenBucket(10, 5)
	before := tb.tokens

	r := &Reservation{ok: false, limiter: tb}
	r.Cancel() // must be a no-op, not panic

	tb.mu.Lock()
	after := tb.tokens
	tb.mu.Unlock()
	if after != before {
		t.Errorf("tokens after Cancel(not-ok) = %v, want unchanged %v", after, before)
	}
}

func TestReservationOnExhaustedBucket(t *testing.T) {
	tb := NewTokenBucket(1, 1)
	if !tb.Allow() {
		t.Fatal("Allow() = false on fresh bucket, want true")
	}

	r := tb.Reservation()
	if r == nil {
		t.Fatal("Reservation() = nil")
	}
	if !r.OK() {
		t.Error("Reservation().OK() = false, want true (delayed reservation)")
	}
	if r.Delay() <= 0 {
		t.Errorf("Delay() on exhausted bucket = %v, want positive", r.Delay())
	}

	r.Cancel()
}

func TestTimeUntilTokenAvailable(t *testing.T) {
	tb := &TokenBucket{tokens: 5, rate: 1, burst: 10, lastTime: time.Now()}
	if got := tb.timeUntilToken(); got != 0 {
		t.Errorf("timeUntilToken() with tokens available = %v, want 0", got)
	}
}

func TestWaitWithImmediateRefill(t *testing.T) {
	// A high-rate bucket refills while waiting, so Wait returns quickly.
	tb := NewTokenBucket(100000, 1)
	tb.Allow() // exhaust

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := tb.Wait(ctx); err != nil {
		t.Errorf("Wait() error = %v, want nil", err)
	}
}

func TestWaitReusesTimerAcrossSpins(t *testing.T) {
	// A bucket whose refill never grants (lastTime in the future keeps
	// elapsed negative) forces Wait to spin: every timer fire is followed by
	// the reuse branch (stop the fired timer, drain it, reset), and only ctx
	// cancel ends the loop.
	tb := &TokenBucket{tokens: 0, rate: 100, burst: 1, lastTime: time.Now().Add(time.Hour)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tb.Wait(ctx) }()

	time.Sleep(30 * time.Millisecond) // several 10ms spins through the reuse branch
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Wait() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after ctx cancel")
	}
}

func TestReservationOnAvailableBucket(t *testing.T) {
	tb := NewTokenBucket(10, 5)

	r := tb.Reservation()
	if r == nil || !r.OK() {
		t.Fatalf("Reservation() on fresh bucket = %v, want ok reservation", r)
	}
	if r.Delay() != 0 {
		t.Errorf("Delay() on immediately-actable reservation = %v, want 0", r.Delay())
	}

	tb.mu.Lock()
	tokensAfter := tb.tokens
	tb.mu.Unlock()
	if tokensAfter != 4 {
		t.Errorf("tokens after reservation = %v, want 4", tokensAfter)
	}
}
