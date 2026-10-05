package retry

import (
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/herald/core"
	"github.com/cuihairu/herald/core/errclass"
)

// pinNow freezes the wall clock at T0 and advances it by every wait the
// backoff seam observes, so NextRetryAt stamps are exact and nothing
// actually sleeps.
func pinNow(t *testing.T) time.Time {
	t.Helper()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	origNow, origAfter := now, after
	clock := t0
	now = func() time.Time { return clock }
	after = func(d time.Duration) <-chan time.Time {
		clock = clock.Add(d)
		ch := make(chan time.Time, 1)
		ch <- clock
		return ch
	}
	t.Cleanup(func() { now, after = origNow, origAfter })
	return t0
}

// TestDeferSchedulesRetry: a retryable failure with budget left comes back
// as a Deferred — the retry count advances, the task leaves as retrying,
// and NextRetryAt is stamped with the policy delay.
func TestDeferSchedulesRetry(t *testing.T) {
	t0 := pinNow(t)

	r := NewRetryer(&Config{Max: 2, Backoff: "fixed", InitialDelay: 5 * time.Second})
	task := &core.DeliveryTask{ID: "t", Provider: "p"}
	attemptErr := errclass.New(errclass.RateLimited, errors.New("429"))

	got := r.Defer(task, attemptErr)
	var deferred *Deferred
	if !errors.As(got, &deferred) {
		t.Fatalf("Defer = %v, want a *Deferred", got)
	}
	if deferred.Delay != 5*time.Second {
		t.Errorf("Deferred.Delay = %v, want the policy delay 5s", deferred.Delay)
	}
	if deferred.Unwrap() != attemptErr {
		t.Errorf("Deferred.Unwrap() = %v, want the attempt error", deferred.Unwrap())
	}
	if task.RetryCount != 1 {
		t.Errorf("task retry=%d, want 1 (the attempt that just failed)", task.RetryCount)
	}
	if task.Status != core.StatusRetrying {
		t.Errorf("task status = %q, want retrying", task.Status)
	}
	if task.NextRetryAt == nil || !task.NextRetryAt.Equal(t0.Add(5*time.Second)) {
		t.Errorf("NextRetryAt = %v, want T0+5s", task.NextRetryAt)
	}
}

// TestDeferStampsHintAndCaps: a server-advised wait beats the curve and is
// capped by MaxDelay, same rules the sync path follows.
func TestDeferStampsHintAndCaps(t *testing.T) {
	t0 := pinNow(t)

	r := NewRetryer(&Config{Max: 2, Backoff: "fixed", InitialDelay: time.Second, MaxDelay: time.Minute})
	task := &core.DeliveryTask{ID: "t"}
	got := r.Defer(task, errclass.NewWithRetryAfter(errclass.RateLimited, errors.New("429"), 2*time.Hour))
	if deferred, ok := got.(*Deferred); !ok || deferred.Delay != time.Minute {
		t.Fatalf("Defer = %v, want a Deferred capped to 1m", got)
	}
	if task.NextRetryAt == nil || !task.NextRetryAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("NextRetryAt = %v, want T0+1m", task.NextRetryAt)
	}
}

// TestDeferHonorsUncappedHint: MaxDelay unset means the advice is taken as
// stated.
func TestDeferHonorsUncappedHint(t *testing.T) {
	pinNow(t)

	r := NewRetryer(&Config{Max: 2, Backoff: "fixed", InitialDelay: time.Second, MaxDelay: 0})
	task := &core.DeliveryTask{ID: "t"}
	got := r.Defer(task, errclass.NewWithRetryAfter(errclass.RateLimited, errors.New("429"), 40*time.Millisecond))
	if deferred, ok := got.(*Deferred); !ok || deferred.Delay != 40*time.Millisecond {
		t.Fatalf("Defer = %v, want a Deferred with the raw 40ms advice", got)
	}
}

// TestDeferTerminal: a non-retryable failure and a spent budget both settle
// instead of deferring.
func TestDeferTerminal(t *testing.T) {
	pinNow(t)

	r := NewRetryer(&Config{Max: 1, Backoff: "fixed", InitialDelay: time.Second})

	// Permanent errors never come back.
	permanent := errclass.New(errclass.Permanent, errors.New("template not approved"))
	task := &core.DeliveryTask{ID: "t"}
	if got := r.Defer(task, permanent); got != permanent {
		t.Errorf("Defer(permanent) = %v, want the error as-is", got)
	}
	if task.RetryCount != 0 || task.Status != "" {
		t.Errorf("permanent deferred state: retry=%d status=%q, want untouched", task.RetryCount, task.Status)
	}

	// A retryable failure with the budget already spent is dead.
	spent := &core.DeliveryTask{ID: "t", RetryCount: 1}
	got := r.Defer(spent, errclass.New(errclass.RateLimited, errors.New("429")))
	if !errors.Is(got, ErrMaxRetries) {
		t.Errorf("Defer(spent) = %v, want ErrMaxRetries wrap", got)
	}
	if spent.NextRetryAt != nil {
		t.Errorf("spent budget stamped NextRetryAt = %v, want nil", spent.NextRetryAt)
	}
}

// TestDeferSpendsBudgetAcrossCalls: the budget lives on the task, so two
// Defer rounds then a third failure exhaust it — the async shape of what
// sync Execute does in its loop.
func TestDeferSpendsBudgetAcrossCalls(t *testing.T) {
	pinNow(t)

	r := NewRetryer(&Config{Max: 2, Backoff: "fixed", InitialDelay: time.Millisecond})
	task := &core.DeliveryTask{ID: "t"}
	fail := func() error { return errclass.New(errclass.Temporary, errors.New("503")) }

	if _, ok := r.Defer(task, fail()).(*Deferred); !ok {
		t.Fatal("first failure should defer")
	}
	if _, ok := r.Defer(task, fail()).(*Deferred); !ok {
		t.Fatal("second failure should defer")
	}
	if got := r.Defer(task, fail()); !errors.Is(got, ErrMaxRetries) {
		t.Fatalf("third failure = %v, want ErrMaxRetries (budget spent)", got)
	}
	if task.RetryCount != 2 {
		t.Errorf("task retry = %d, want 2 (Max retries performed)", task.RetryCount)
	}
}

// TestDeferredText: the message names the wait and carries the cause — it
// surfaces in logs only when a re-enqueue fails, but must not be blank.
func TestDeferredText(t *testing.T) {
	cause := errors.New("503")
	d := &Deferred{Delay: 3 * time.Second, Err: cause}
	want := "retry deferred for 3s: 503"
	if d.Error() != want {
		t.Errorf("Error() = %q, want %q", d.Error(), want)
	}
	if !errors.Is(d, cause) {
		t.Error("expected errors.Is to see the cause through the deferral")
	}
}
