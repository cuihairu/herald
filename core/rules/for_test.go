package rules

import (
	"context"
	"testing"
	"time"
)

func TestParseFor(t *testing.T) {
	valid := map[string]time.Duration{
		"":      0,
		"3m":    3 * time.Minute,
		"90s":   90 * time.Second,
		"1h30m": 90 * time.Minute,
		"1ms":   time.Millisecond,
	}
	for in, want := range valid {
		got, err := ParseFor(in)
		if err != nil {
			t.Errorf("ParseFor(%q) error = %v, want %v", in, err, want)
			continue
		}
		if got != want {
			t.Errorf("ParseFor(%q) = %v, want %v", in, got, want)
		}
	}

	invalid := []string{"abc", "3", "0s", "-5m", "25h", "9999h"}
	for _, in := range invalid {
		if _, err := ParseFor(in); err == nil {
			t.Errorf("ParseFor(%q) = nil error, want rejection", in)
		}
	}
}

func TestForGroupKeyStable(t *testing.T) {
	base := NewEnv("deploy", "error", "t", "b", map[string]any{"env": "prod", "service": "api"})

	if ForGroupKey(base) == "" {
		t.Fatal("expected non-empty group key")
	}
	// Same environment → same key.
	if again := ForGroupKey(NewEnv("deploy", "error", "t", "b", map[string]any{"env": "prod", "service": "api"})); again != ForGroupKey(base) {
		t.Errorf("expected stable key, got %s vs %s", again, ForGroupKey(base))
	}

	// Param order must not matter.
	reordered := NewEnv("deploy", "error", "t", "b", map[string]any{"service": "api", "env": "prod"})
	if ForGroupKey(reordered) != ForGroupKey(base) {
		t.Error("expected param order to not affect the group key")
	}

	// Any content change must change the key.
	for name, mutate := range map[string]func(*Env){
		"type":   func(e *Env) { e.Type = "other" },
		"level":  func(e *Env) { e.Level = "warning" },
		"title":  func(e *Env) { e.Title = "other" },
		"body":   func(e *Env) { e.Body = "other" },
		"params": func(e *Env) { e.Params = map[string]any{"env": "staging", "service": "api"} },
		"extra":  func(e *Env) { e.Params = map[string]any{"env": "prod", "service": "api", "x": "1"} },
	} {
		env := base
		mutate(&env)
		if ForGroupKey(env) == ForGroupKey(base) {
			t.Errorf("%s: expected a different group key", name)
		}
	}
}

// fixedClock returns a tracker whose clock the test controls.
func fixedClock(start time.Time) (*ForTracker, *MemoryStateStore, *time.Time) {
	store := NewMemoryStateStore()
	tracker := NewForTracker(store)
	now := start
	tracker.now = func() time.Time { return now }
	return tracker, store, &now
}

func TestForTrackerFirstHitPending(t *testing.T) {
	tracker, _, _ := fixedClock(time.Unix(1000, 0))
	ctx := context.Background()

	outcome, err := tracker.Observe(ctx, "r1", "g1", 3*time.Minute)
	if err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	if outcome != ForPending {
		t.Errorf("first hit outcome = %v, want ForPending", outcome)
	}

	state, err := tracker.store.Get(ctx, stateKey("r1", "g1"))
	if err != nil || state == nil {
		t.Fatalf("expected recorded state, got %+v err %v", state, err)
	}
	if state.Count != 1 || state.Fired {
		t.Errorf("unexpected first state: %+v", state)
	}
}

func TestForTrackerFiresAfterWindow(t *testing.T) {
	tracker, _, now := fixedClock(time.Unix(1000, 0))
	ctx := context.Background()

	if out, _ := tracker.Observe(ctx, "r1", "g1", 3*time.Minute); out != ForPending {
		t.Fatalf("first hit outcome = %v, want ForPending", out)
	}
	*now = (*now).Add(2 * time.Minute)
	if out, _ := tracker.Observe(ctx, "r1", "g1", 3*time.Minute); out != ForPending {
		t.Fatalf("hit at 2m outcome = %v, want ForPending", out)
	}
	*now = (*now).Add(time.Minute)
	outcome, err := tracker.Observe(ctx, "r1", "g1", 3*time.Minute)
	if err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	if outcome != ForFire {
		t.Fatalf("hit at 3m outcome = %v, want ForFire", outcome)
	}

	state, _ := tracker.store.Get(ctx, stateKey("r1", "g1"))
	if state == nil || !state.Fired || state.Count != 3 {
		t.Errorf("unexpected fired state: %+v", state)
	}
}

func TestForTrackerSilentAfterFired(t *testing.T) {
	tracker, _, now := fixedClock(time.Unix(1000, 0))
	ctx := context.Background()

	if _, err := tracker.Observe(ctx, "r1", "g1", time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	*now = (*now).Add(time.Minute)
	if out, _ := tracker.Observe(ctx, "r1", "g1", time.Minute); out != ForFire {
		t.Fatalf("outcome = %v, want ForFire at window end", out)
	}
	// Continued hits stay silent (already notified for this group).
	*now = (*now).Add(time.Minute)
	outcome, err := tracker.Observe(ctx, "r1", "g1", time.Minute)
	if err != nil || outcome != ForSilent {
		t.Errorf("post-fire hit: outcome=%v err=%v, want ForSilent", outcome, err)
	}
}

func TestForTrackerResetRestartsWindow(t *testing.T) {
	tracker, _, now := fixedClock(time.Unix(1000, 0))
	ctx := context.Background()

	if _, err := tracker.Observe(ctx, "r1", "g1", time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	*now = (*now).Add(30 * time.Second)
	if _, err := tracker.Observe(ctx, "r1", "g1", time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	// Condition stops holding: the window must restart on the next hit.
	if err := tracker.Reset(ctx, "r1", "g1"); err != nil {
		t.Fatalf("Reset error = %v", err)
	}
	if state, _ := tracker.store.Get(ctx, stateKey("r1", "g1")); state != nil {
		t.Fatalf("expected state cleared after reset, got %+v", state)
	}
	*now = (*now).Add(30 * time.Second)
	if out, _ := tracker.Observe(ctx, "r1", "g1", time.Minute); out != ForPending {
		t.Fatalf("first hit after reset outcome = %v, want ForPending despite old first_seen", out)
	}
}

func TestForTrackerGroupsAreIndependent(t *testing.T) {
	tracker, _, _ := fixedClock(time.Unix(1000, 0))
	ctx := context.Background()

	if _, err := tracker.Observe(ctx, "r1", "gA", time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	// A different group starts its own window; gA's state must not leak.
	if out, _ := tracker.Observe(ctx, "r1", "gB", time.Minute); out != ForPending {
		t.Errorf("group B first hit outcome = %v, want ForPending despite group A", out)
	}
	// Rules are independent too.
	if out, _ := tracker.Observe(ctx, "r2", "gA", time.Minute); out != ForPending {
		t.Errorf("rule 2 first hit outcome = %v, want ForPending despite rule 1", out)
	}
}

func TestForTrackerResetRuleDropsAllGroups(t *testing.T) {
	tracker, _, _ := fixedClock(time.Unix(1000, 0))
	ctx := context.Background()

	for _, g := range []string{"gA", "gB", "gC"} {
		if _, err := tracker.Observe(ctx, "r1", g, time.Minute); err != nil {
			t.Fatalf("Observe error = %v", err)
		}
	}
	if _, err := tracker.Observe(ctx, "r2", "gA", time.Minute); err != nil {
		t.Fatalf("Observe error = %v", err)
	}
	if err := tracker.ResetRule(ctx, "r1"); err != nil {
		t.Fatalf("ResetRule error = %v", err)
	}
	for _, g := range []string{"gA", "gB", "gC"} {
		if state, _ := tracker.store.Get(ctx, stateKey("r1", g)); state != nil {
			t.Errorf("group %s state survived ResetRule", g)
		}
	}
	if state, _ := tracker.store.Get(ctx, stateKey("r2", "gA")); state == nil {
		t.Error("ResetRule must not touch other rules")
	}
}
