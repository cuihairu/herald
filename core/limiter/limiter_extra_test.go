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

// TestNewOnceGrantsSingleToken: a one-shot bucket grants exactly one token
// and never refills, regardless of elapsed time — the primitive behind the
// login trial table's single-attempt guarantee.
func TestNewOnceGrantsSingleToken(t *testing.T) {
	once := NewOnce()
	if !once.Allow() {
		t.Fatal("first allow on a once bucket must be granted")
	}
	if once.Allow() {
		t.Fatal("second allow on a once bucket must be denied")
	}
	// Even after a long pause the bucket must not refill (rate is 0).
	time.Sleep(5 * time.Millisecond)
	if once.Allow() {
		t.Fatal("once bucket must not refill after time passes")
	}
}

// TestNewOnceReservationNoDivideByZero: Reservation on an exhausted
// one-shot bucket must not divide by the zero rate (which yields an
// implementation-defined duration). It must instead return an unusable
// reservation (ok=false): the bucket never refills, so no finite or
// far-future delay can ever make a token available.
func TestNewOnceReservationNoDivideByZero(t *testing.T) {
	once := NewOnce()
	if !once.Allow() {
		t.Fatal("first allow on a once bucket must be granted")
	}
	r := once.Reservation()
	if r == nil {
		t.Fatal("Reservation() on a once bucket returned nil")
	}
	if r.OK() {
		t.Fatal("Reservation().OK() = true on a never-refilling bucket, want false")
	}
	if d := r.Delay(); d != 0 {
		t.Errorf("Reservation().Delay() = %v on not-ok reservation, want 0", d)
	}
}

// TestNewOnceReservationCancelDoesNotRegrant: canceling an unusable
// reservation on an exhausted one-shot bucket must not mint a phantom token.
// The bug was that Reservation returned ok=true for a zero-rate bucket, so
// Cancel() incremented tokens and re-armed the one-shot gate, letting a
// second Allow() through — bypassing the "exactly once" guarantee.
func TestNewOnceReservationCancelDoesNotRegrant(t *testing.T) {
	once := NewOnce()
	if !once.Allow() {
		t.Fatal("first allow on a once bucket must be granted")
	}
	if once.Allow() {
		t.Fatal("second allow on a once bucket must be denied")
	}

	r := once.Reservation()
	if r == nil {
		t.Fatal("Reservation() on a once bucket returned nil")
	}
	r.Cancel() // must be a no-op: the reservation was never ok

	if once.Allow() {
		t.Fatal("Allow() after Reservation().Cancel() on exhausted once bucket must still be denied")
	}
}
