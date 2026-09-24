package rules

import (
	"context"
	"testing"
	"time"
)

// silenceEngine seeds an active rule with the given silence window whose
// channel would be "oncall".
func silenceEngine(t *testing.T, start, end string, match *string) (*Engine, *time.Time) {
	t.Helper()
	engine := NewEngine(NewMemoryStore())
	r := validRule()
	r.ID = "r-quiet"
	r.Match = `level != ""`
	r.Silence = &SilenceSpec{Start: start, End: end, Match: match}
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.Local)
	engine.now = func() time.Time { return now }
	return engine, &now
}

func TestEngineSilenceWithholdsInsideWindow(t *testing.T) {
	engine, now := silenceEngine(t, "10:00", "14:00", nil)
	ctx := context.Background()
	env := NewEnv("alert", "error", "t", "b", nil)

	// Noon is inside 10:00-14:00: the event is silenced.
	d, err := engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || !d.Silenced || d.RuleID != "r-quiet" || len(d.Channels) != 0 {
		t.Fatalf("expected silenced decision inside the window, got %+v", d)
	}

	// Outside the window the rule delivers normally.
	*now = time.Date(2026, 5, 10, 15, 0, 0, 0, time.Local)
	d, err = engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate outside: %v", err)
	}
	if d == nil || d.Silenced || len(d.Channels) != 1 {
		t.Fatalf("expected delivery outside the window, got %+v", d)
	}

	// The end bound is exclusive: exactly at 14:00 the rule delivers.
	*now = time.Date(2026, 5, 10, 14, 0, 0, 0, time.Local)
	if d, _ = engine.Evaluate(ctx, env); d == nil || d.Silenced {
		t.Fatalf("expected delivery at the exclusive end bound, got %+v", d)
	}
}

func TestEngineSilenceWindowCrossesMidnight(t *testing.T) {
	engine, now := silenceEngine(t, "22:00", "06:00", nil)
	ctx := context.Background()
	env := NewEnv("alert", "error", "t", "b", nil)

	for _, tc := range []struct {
		hour   int
		silent bool
	}{
		{23, true}, {3, true}, {5, true}, {6, false}, {12, false}, {21, false}, {22, true},
	} {
		*now = time.Date(2026, 5, 10, tc.hour, 0, 0, 0, time.Local)
		d, err := engine.Evaluate(ctx, env)
		if err != nil {
			t.Fatalf("Evaluate at %02d:00: %v", tc.hour, err)
		}
		if got := d != nil && d.Silenced; got != tc.silent {
			t.Errorf("at %02d:00 silenced = %v, want %v", tc.hour, got, tc.silent)
		}
	}
}

func TestEngineSilenceMatchLimitsScope(t *testing.T) {
	match := `level != "critical"`
	engine, now := silenceEngine(t, "10:00", "14:00", &match)
	ctx := context.Background()

	// Noon, non-critical: silenced.
	*now = time.Date(2026, 5, 10, 12, 0, 0, 0, time.Local)
	d, err := engine.Evaluate(ctx, NewEnv("alert", "error", "t", "b", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || !d.Silenced {
		t.Fatalf("expected non-critical event silenced, got %+v", d)
	}

	// Noon, critical: the silence match exempts it, the rule delivers.
	d, err = engine.Evaluate(ctx, NewEnv("alert", "critical", "t", "b", nil))
	if err != nil {
		t.Fatalf("Evaluate critical: %v", err)
	}
	if d == nil || d.Silenced || len(d.Channels) != 1 {
		t.Fatalf("expected critical to bypass silence, got %+v", d)
	}
}

func TestEngineSilenceFreezesForAndGroup(t *testing.T) {
	// A silenced event must not advance for-window or group-round state:
	// the rule is frozen for the whole window.
	engine, now := silenceEngine(t, "10:00", "14:00", nil)
	ctx := context.Background()
	r := forTestRule("r-quiet", "1m")
	r.Silence = &SilenceSpec{Start: "10:00", End: "14:00"}
	r.GroupBy = []string{"env"}
	if err := engine.Put(ctx, &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	env := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})

	*now = time.Date(2026, 5, 10, 12, 0, 0, 0, time.Local)
	d, err := engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || !d.Silenced {
		t.Fatalf("expected silenced decision, got %+v", d)
	}
	groupKey, _ := RuleGroupKey([]string{"env"}, env)
	forStateKey := stateKey("r-quiet", groupKey)
	if state, _ := engine.forState.store.Get(ctx, forStateKey); state != nil {
		t.Errorf("silenced event must not open a for window, got %+v", state)
	}
	if state, _ := engine.groupState.store.Get(ctx, stateGroupKey("r-quiet", groupKey)); state != nil {
		t.Errorf("silenced event must not open a group round, got %+v", state)
	}
}

func TestEngineSilenceShadowNotSilenced(t *testing.T) {
	// Shadow observation records condition hits regardless of the window —
	// the dry-run preview shows what the rule WOULD have matched.
	engine := NewEngine(NewMemoryStore())
	r := validRule()
	r.ID = "r-quiet"
	r.Match = `level == "error"`
	r.Mode = ModeShadow
	r.Silence = &SilenceSpec{Start: "00:00", End: "23:59"}
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	engine.now = func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.Local) }

	d, err := engine.Evaluate(context.Background(), NewEnv("alert", "error", "t", "b", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || len(d.Shadow) != 1 || d.Shadow[0].RuleID != "r-quiet" {
		t.Fatalf("expected unsuppressed shadow record inside the window, got %+v", d)
	}
}

func TestEngineValidateSilenceMatchMustCompile(t *testing.T) {
	engine := NewEngine(NewMemoryStore())
	r := validRule()
	r.ID = "r-quiet"
	r.Match = `level == "error"`
	bad := `level ==`
	r.Silence = &SilenceSpec{Start: "00:00", End: "06:00", Match: &bad}
	if err := engine.Validate(&r); err == nil {
		t.Error("expected Engine.Validate to reject a non-compiling silence match")
	}
}
