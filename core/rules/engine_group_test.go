package rules

import (
	"context"
	"testing"
	"time"
)

// groupTestEngine builds an engine with a rule seeded and a controllable
// clock shared by the for-window and group trackers.
func groupTestEngine(t *testing.T, r Rule) (*Engine, *time.Time) {
	t.Helper()
	engine := NewEngine(NewMemoryStore())
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	now := time.Unix(1000, 0)
	engine.forState.now = func() time.Time { return now }
	engine.groupState.now = func() time.Time { return now }
	return engine, &now
}

func groupTestRule(id string) Rule {
	r := validRule()
	r.ID = id
	r.Match = `level == "error"`
	r.Mode = ModeActive
	r.Route = []RouteStep{{Channels: []string{"oncall"}}}
	r.GroupBy = []string{"env"}
	return r
}

func groupTestEnv(env string) Env {
	return NewEnv("alert", "error", "disk full", "usage high", map[string]any{"env": env})
}

func strPtr(s string) *string { return &s }

func TestEngineGroupFirstEventDeliveredThenFolded(t *testing.T) {
	engine, _ := groupTestEngine(t, groupTestRule("r-group"))
	ctx := context.Background()

	// First event of the group opens the round and routes normally.
	d, err := engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Folded || d.RuleID != "r-group" || len(d.Channels) != 1 || d.Summary != nil {
		t.Fatalf("expected delivered decision for first event, got %+v", d)
	}

	// A second event of the same group is folded: counted, not routed, and
	// a later rule must not take over.
	late := forTestRule("r-late", "")
	if err := engine.Put(ctx, &late); err != nil {
		t.Fatalf("Put late: %v", err)
	}
	d, err = engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate folded: %v", err)
	}
	if d == nil || !d.Folded || d.RuleID != "r-group" || len(d.Channels) != 0 {
		t.Fatalf("expected folded decision from r-group, got %+v", d)
	}
}

func TestEngineGroupSummaryAfterQuiet(t *testing.T) {
	engine, now := groupTestEngine(t, groupTestRule("r-group"))
	ctx := context.Background()

	// Round one: one delivered + one folded.
	if _, err := engine.Evaluate(ctx, groupTestEnv("prod")); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if _, err := engine.Evaluate(ctx, groupTestEnv("prod")); err != nil {
		t.Fatalf("Evaluate fold: %v", err)
	}
	// Quiet past the default interval; the next event opens round two and
	// carries the finished round's summary.
	*now = (*now).Add(6 * time.Minute)
	d, err := engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate after quiet: %v", err)
	}
	if d == nil || d.Folded || len(d.Channels) != 1 {
		t.Fatalf("expected delivered decision after quiet, got %+v", d)
	}
	if d.Summary == nil {
		t.Fatal("expected summary of the finished round")
	}
	if d.Summary.Count != 2 || d.Summary.RuleID != "r-group" || d.Summary.Group != "env=prod" {
		t.Errorf("unexpected summary: %+v", d.Summary)
	}
}

func TestEngineGroupByFieldsDistinguishGroups(t *testing.T) {
	engine, _ := groupTestEngine(t, groupTestRule("r-group"))
	ctx := context.Background()

	// Different group_by values are different groups: both first events
	// are delivered, neither folds the other.
	d1, err := engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate prod: %v", err)
	}
	d2, err := engine.Evaluate(ctx, groupTestEnv("staging"))
	if err != nil {
		t.Fatalf("Evaluate staging: %v", err)
	}
	if d1 == nil || d1.Folded || d1.Summary != nil {
		t.Errorf("prod first event: %+v", d1)
	}
	if d2 == nil || d2.Folded || d2.Summary != nil {
		t.Errorf("staging first event: %+v", d2)
	}
	// But the same value folds.
	if d, _ := engine.Evaluate(ctx, groupTestEnv("prod")); d == nil || !d.Folded {
		t.Errorf("second prod event should fold, got %+v", d)
	}
}

func TestEngineGroupShadowDoesNotFold(t *testing.T) {
	r := groupTestRule("r-shadow")
	r.Mode = ModeShadow
	engine, _ := groupTestEngine(t, r)
	ctx := context.Background()

	// Shadow observation records condition hits; the aggregation is a
	// delivery behavior and is not simulated in dry-run.
	for i := 0; i < 3; i++ {
		d, err := engine.Evaluate(ctx, groupTestEnv("prod"))
		if err != nil {
			t.Fatalf("Evaluate %d: %v", i, err)
		}
		if d == nil || len(d.Shadow) != 1 || d.Shadow[0].RuleID != "r-shadow" {
			t.Fatalf("hit %d: expected unsuppressed shadow record, got %+v", i, d)
		}
	}
}

func TestEngineForSilentFallsThroughToGroup(t *testing.T) {
	// for + group_by combined: pending until the window elapsed, fired
	// once, then further hits stay silent for ALERTING but still fold into
	// the group so the summary counts them.
	r := groupTestRule("r-both")
	r.For = strPtr("1m")
	engine, now := groupTestEngine(t, r)
	ctx := context.Background()

	// Pending: suppressed, and the group round must NOT open (the gate is
	// in front of aggregation).
	d, err := engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || !d.ForPending {
		t.Fatalf("expected pending decision, got %+v", d)
	}
	if state, _ := engine.groupState.store.Get(ctx, stateGroupKey("r-both", mustGroupKey(t, []string{"env"}, groupTestEnv("prod")))); state != nil {
		t.Fatalf("group round must not open while for is pending, got %+v", state)
	}

	// Window elapsed: fires (delivered, and the group round opens).
	*now = (*now).Add(time.Minute)
	d, err = engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate fire: %v", err)
	}
	if d == nil || d.ForPending || d.Folded || len(d.Channels) != 1 {
		t.Fatalf("expected fired delivery at 1m, got %+v", d)
	}

	// Post-fire silence falls through to folding.
	*now = (*now).Add(30 * time.Second)
	d, err = engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate silent fold: %v", err)
	}
	if d == nil || !d.Folded {
		t.Fatalf("expected post-fire hit to fold into the group, got %+v", d)
	}

	// When the round closes, the summary counts fire + fold = 2 events.
	*now = (*now).Add(6 * time.Minute)
	d, err = engine.Evaluate(ctx, groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate summary: %v", err)
	}
	if d == nil || d.Summary == nil || d.Summary.Count != 2 {
		t.Fatalf("expected summary counting 2 events, got %+v", d)
	}
}

func TestEngineGroupStateFailureFailsOpen(t *testing.T) {
	engine, _ := groupTestEngine(t, groupTestRule("r-group"))
	failing := &failingStateStore{inner: engine.groupState.store, failOb: true}
	engine.SetStateStore(failing)
	ctx := context.Background()

	// A state store outage must degrade to "rule skipped", not "event
	// dropped silently" and not a routing failure.
	d, err := engine.Evaluate(ctx, groupTestEnv("prod"))
	if d != nil {
		t.Fatalf("expected nil decision on state failure, got %+v", d)
	}
	if err == nil {
		t.Fatal("expected an eval error for the state outage")
	}
}

func TestEngineSetStateStoreNilFallsBackToMemory(t *testing.T) {
	engine, _ := groupTestEngine(t, groupTestRule("r-group"))
	engine.SetStateStore(nil)

	// After the nil fallback the engine must keep a working store: grouping
	// still opens rounds instead of failing.
	d, err := engine.Evaluate(context.Background(), groupTestEnv("prod"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Folded || d.Summary != nil {
		t.Fatalf("expected a clean first delivery after nil SetStateStore, got %+v", d)
	}
}

func mustGroupKey(t *testing.T, groupBy []string, env Env) string {
	t.Helper()
	key, _ := RuleGroupKey(groupBy, env)
	return key
}
