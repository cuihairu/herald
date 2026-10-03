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

// TestEngineSilenceUsesConfiguredTZ drives evaluation end to end with a
// zoned window: the injected clock carries a foreign zone (UTC), and the
// decision must follow the window's tz, not the clock's.
func TestEngineSilenceUsesConfiguredTZ(t *testing.T) {
	engine := NewEngine(NewMemoryStore())
	r := validRule()
	r.ID = "r-quiet"
	r.Match = `level != ""`
	r.Silence = &SilenceSpec{Start: "22:00", End: "06:00", TZ: "Asia/Shanghai"}
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	ctx := context.Background()
	env := NewEnv("alert", "error", "t", "b", nil)

	// 14:30 UTC is 22:30 in Shanghai: silenced inside the window even
	// though the clock's own wall time (14:30) is not.
	now := time.Date(2026, 5, 10, 14, 30, 0, 0, time.UTC)
	engine.now = func() time.Time { return now }
	d, err := engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate inside (Shanghai): %v", err)
	}
	if d == nil || !d.Silenced {
		t.Fatalf("expected silenced at 22:30 Asia/Shanghai, got %+v", d)
	}

	// 23:00 UTC is 07:00 the next day in Shanghai: outside the window,
	// even though the clock's own wall time (23:00) would be inside.
	now = time.Date(2026, 5, 10, 23, 0, 0, 0, time.UTC)
	d, err = engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate outside (Shanghai): %v", err)
	}
	if d == nil || d.Silenced || len(d.Channels) != 1 {
		t.Fatalf("expected delivery at 07:00 Asia/Shanghai, got %+v", d)
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

// stubRosterSource answers Covers from a fixed table (id -> covered), the
// shape a pushed schedule presents to the gate.
type stubRosterSource struct {
	covered map[string]bool
}

func (s stubRosterSource) Covers(id string, _ time.Time) bool { return s.covered[id] }

// rosterEngine seeds the same active rule as silenceEngine, but its
// silence names the "ops-oncall" duty roster instead of a window.
func rosterEngine(t *testing.T, src RosterSource) (*Engine, *time.Time) {
	t.Helper()
	engine := NewEngine(NewMemoryStore())
	r := validRule()
	r.ID = "r-quiet"
	r.Match = `level != ""`
	r.Silence = &SilenceSpec{Roster: "ops-oncall"}
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	engine.SetRosterSource(src)
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.Local)
	engine.now = func() time.Time { return now }
	return engine, &now
}

func TestEngineSilenceRosterGate(t *testing.T) {
	src := stubRosterSource{covered: map[string]bool{"ops-oncall": true}}
	engine, _ := rosterEngine(t, src)
	ctx := context.Background()
	env := NewEnv("alert", "error", "t", "b", nil)

	// The pushed schedule covers the moment: the event is silenced.
	d, err := engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || !d.Silenced || d.RuleID != "r-quiet" {
		t.Fatalf("expected silenced decision while the roster covers, got %+v", d)
	}

	// The schedule rotates (a later push no longer covers): the event
	// delivers — the gate follows the live table, no state involved.
	src.covered = map[string]bool{"ops-oncall": false}
	engine.SetRosterSource(src)
	d, err = engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate after rotation: %v", err)
	}
	if d == nil || d.Silenced || len(d.Channels) != 1 {
		t.Fatalf("expected delivery once the roster stops covering, got %+v", d)
	}
}

func TestEngineSilenceRosterFailsOpen(t *testing.T) {
	// No schedule data at all: no source attached (nil), or a roster the
	// scheduler never pushed / has since deleted (covered=false) — the
	// gate stays open and the alert goes out.
	for _, tc := range []struct {
		name string
		src  RosterSource
	}{
		{"nil source", nil},
		{"unknown roster", stubRosterSource{covered: map[string]bool{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := rosterEngine(t, tc.src)
			d, err := engine.Evaluate(context.Background(), NewEnv("alert", "error", "t", "b", nil))
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if d == nil || d.Silenced || len(d.Channels) != 1 {
				t.Fatalf("missing schedule data must never silence, got %+v", d)
			}
		})
	}
}

func TestEngineSilenceRosterMatchLimitsScope(t *testing.T) {
	match := `level != "critical"`
	engine, _ := rosterEngine(t, stubRosterSource{covered: map[string]bool{"ops-oncall": true}})
	r := validRule()
	r.ID = "r-quiet"
	r.Match = `level != ""`
	r.Silence = &SilenceSpec{Roster: "ops-oncall", Match: &match}
	if err := engine.Put(context.Background(), &r); err != nil {
		t.Fatalf("Put: %v", err)
	}
	ctx := context.Background()

	d, err := engine.Evaluate(ctx, NewEnv("alert", "error", "t", "b", nil))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || !d.Silenced {
		t.Fatalf("expected non-critical event silenced by the roster, got %+v", d)
	}

	d, err = engine.Evaluate(ctx, NewEnv("alert", "critical", "t", "b", nil))
	if err != nil {
		t.Fatalf("Evaluate critical: %v", err)
	}
	if d == nil || d.Silenced || len(d.Channels) != 1 {
		t.Fatalf("expected critical to bypass the roster silence, got %+v", d)
	}
}

func TestEngineSilenceWindowIgnoresRosterSource(t *testing.T) {
	// A window gate reads the window alone: an attached roster source
	// (covering everything) must not extend or shorten it.
	engine, now := silenceEngine(t, "10:00", "14:00", nil)
	engine.SetRosterSource(stubRosterSource{covered: map[string]bool{"ops-oncall": true}})
	ctx := context.Background()
	env := NewEnv("alert", "error", "t", "b", nil)

	*now = time.Date(2026, 5, 10, 12, 0, 0, 0, time.Local)
	d, err := engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate inside: %v", err)
	}
	if d == nil || !d.Silenced {
		t.Fatalf("expected silenced inside the window, got %+v", d)
	}

	*now = time.Date(2026, 5, 10, 15, 0, 0, 0, time.Local)
	d, err = engine.Evaluate(ctx, env)
	if err != nil {
		t.Fatalf("Evaluate outside: %v", err)
	}
	if d == nil || d.Silenced || len(d.Channels) != 1 {
		t.Fatalf("expected delivery outside the window, got %+v", d)
	}
}
