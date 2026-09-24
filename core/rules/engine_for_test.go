package rules

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// forTestEngine builds an engine with a rule seeded and a controllable
// clock for the for-window tracker.
func forTestEngine(t *testing.T, r Rule) (*Engine, *time.Time) {
	t.Helper()
	engine := NewEngine(NewMemoryStore())
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	now := time.Unix(1000, 0)
	engine.forState.now = func() time.Time { return now }
	return engine, &now
}

func forTestRule(id string, forDur string) Rule {
	r := validRule()
	r.ID = id
	r.Match = `level == "error"`
	r.Mode = ModeActive
	r.Route = []RouteStep{{Channels: []string{"oncall"}}}
	if forDur != "" {
		r.For = &forDur
	}
	return r
}

func TestEngineForPendingSuppressesActive(t *testing.T) {
	engine, now := forTestEngine(t, forTestRule("r-for", "3m"))
	ctx := context.Background()
	env := NewEnv("deploy", "error", "t", "b", nil)

	// First hit: pending, and a later rule must not take over routing.
	late := forTestRule("r-late", "")
	if err := engine.Put(ctx, &late); err != nil {
		t.Fatalf("Put: %v", err)
	}
	d, err := engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || !d.ForPending || d.RuleID != "r-for" {
		t.Fatalf("expected pending decision from r-for, got %+v", d)
	}

	// Window still running.
	*now = (*now).Add(2 * time.Minute)
	if d, _ = engine.Evaluate(ctx, env); d == nil || !d.ForPending {
		t.Fatalf("expected pending at 2m, got %+v", d)
	}

	// Window elapsed: fires with channels, pending cleared.
	*now = (*now).Add(time.Minute)
	d, err = engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.ForPending || d.RuleID != "r-for" || len(d.Channels) != 1 {
		t.Fatalf("expected fired decision at 3m, got %+v", d)
	}

	// Already notified: further hits stay pending (silent).
	*now = (*now).Add(time.Minute)
	if d, _ = engine.Evaluate(ctx, env); d == nil || !d.ForPending {
		t.Fatalf("expected post-fire silence, got %+v", d)
	}
}

func TestEngineForWindowKeepsRunningAcrossOtherGroups(t *testing.T) {
	engine, now := forTestEngine(t, forTestRule("r-for", "3m"))
	ctx := context.Background()

	// The group key is the notification's content hash: a notification
	// that does not match belongs to a DIFFERENT group and must not
	// disturb the running window of the matching group. (The duration is
	// event-driven per the design doc: judged on every hit of the same
	// group, from its first sighting.)
	hitEnv := NewEnv("deploy", "error", "t", "b", nil)
	missEnv := NewEnv("deploy", "info", "t", "b", nil)

	if _, err := engine.Evaluate(ctx, hitEnv); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	*now = (*now).Add(2 * time.Minute)
	if _, err := engine.Evaluate(ctx, missEnv); err != nil {
		t.Fatalf("Evaluate miss: %v", err)
	}
	*now = (*now).Add(time.Minute)
	d, _ := engine.Evaluate(ctx, hitEnv)
	if d == nil || d.ForPending || d.RuleID != "r-for" {
		t.Fatalf("expected the untouched window to fire at 3m, got %+v", d)
	}
}

func TestEngineForShadowOnlyRecordsFired(t *testing.T) {
	r := forTestRule("r-shadow", "3m")
	r.Mode = ModeShadow
	engine, now := forTestEngine(t, r)
	ctx := context.Background()
	env := NewEnv("deploy", "error", "t", "b", nil)

	d, _ := engine.Evaluate(ctx, env)
	if d != nil {
		t.Fatalf("expected no decision (no shadow record) while pending, got %+v", d)
	}
	*now = (*now).Add(3 * time.Minute)
	d, _ = engine.Evaluate(ctx, env)
	if d == nil || len(d.Shadow) != 1 || d.Shadow[0].RuleID != "r-shadow" {
		t.Fatalf("expected shadow record after window, got %+v", d)
	}
}

func TestEngineForPutAndDropState(t *testing.T) {
	engine, _ := forTestEngine(t, forTestRule("r-for", "3m"))
	ctx := context.Background()
	env := NewEnv("deploy", "error", "t", "b", nil)

	if _, err := engine.Evaluate(ctx, env); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if state, _ := engine.forState.store.Get(ctx, stateKey("r-for", ForGroupKey(env))); state == nil {
		t.Fatal("expected in-flight state after first hit")
	}

	// Replacing the rule drops its windows.
	updated := forTestRule("r-for", "5m")
	if err := engine.Put(ctx, &updated); err != nil {
		t.Fatalf("Put updated: %v", err)
	}
	if state, _ := engine.forState.store.Get(ctx, stateKey("r-for", ForGroupKey(env))); state != nil {
		t.Error("expected state dropped after rule replacement")
	}

	if _, err := engine.Evaluate(ctx, env); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if err := engine.Delete(ctx, "r-for"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if state, _ := engine.forState.store.Get(ctx, stateKey("r-for", ForGroupKey(env))); state != nil {
		t.Error("expected state dropped after rule delete")
	}
}

// failingStateStore wraps a store and fails selected operations, so tests
// can pin the fail-open behavior of stateful evaluation.
type failingStateStore struct {
	inner   StateStore
	failOb  bool // fail reads and writes (full outage)
	failPut bool // fail writes only
}

func (f *failingStateStore) Get(ctx context.Context, key string) (*RuleState, error) {
	if f.failOb {
		return nil, errors.New("state store down")
	}
	return f.inner.Get(ctx, key)
}

func (f *failingStateStore) Put(ctx context.Context, key string, s *RuleState, ttl time.Duration) error {
	if f.failOb || f.failPut {
		return errors.New("state store down")
	}
	return f.inner.Put(ctx, key, s, ttl)
}

func (f *failingStateStore) Delete(ctx context.Context, key string) error {
	return f.inner.Delete(ctx, key)
}

func (f *failingStateStore) DeleteRule(ctx context.Context, ruleID string) error {
	return f.inner.DeleteRule(ctx, ruleID)
}

func (f *failingStateStore) Close() error { return f.inner.Close() }

func TestEngineForStateFailureFailsOpen(t *testing.T) {
	engine, _ := forTestEngine(t, forTestRule("r-for", "3m"))
	failing := &failingStateStore{inner: engine.forState.store, failOb: true}
	engine.SetStateStore(failing)
	engine.forState.now = func() time.Time { return time.Unix(1000, 0) }
	ctx := context.Background()
	env := NewEnv("deploy", "error", "t", "b", nil)

	// A state store outage must degrade to "rule skipped", not "event
	// suppressed" and not a routing failure.
	d, err := engine.Evaluate(ctx, env)
	if d != nil {
		t.Fatalf("expected nil decision on state failure, got %+v", d)
	}
	if err == nil || !strings.Contains(err.Error(), "state store down") {
		t.Fatalf("expected joined eval error naming the outage, got %v", err)
	}
}
