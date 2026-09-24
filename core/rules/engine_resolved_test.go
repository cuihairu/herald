package rules

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingResolved captures recovery reports from the engine.
type recordingResolved struct {
	mu     sync.Mutex
	events []ResolvedEvent
}

func (r *recordingResolved) onResolved(ev ResolvedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recordingResolved) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func TestEngineResolvedFiresWhenMatchStopsHolding(t *testing.T) {
	r := forTestRule("r-for", "30m")
	r.GroupBy = []string{"env"}
	engine, now := forTestEngine(t, r)
	rec := &recordingResolved{}
	engine.SetResolvedFunc(rec.onResolved)
	ctx := context.Background()

	// The group identity is the "env" field value: as long as prod keeps
	// reporting — at any level — the group is the same, so the recovery
	// (the level drops back below the threshold) is event-driven.
	errorEnv := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	if _, err := engine.Evaluate(ctx, errorEnv); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	*now = now.Add(31 * time.Minute)
	d, err := engine.Evaluate(ctx, errorEnv)
	if err != nil {
		t.Fatalf("Evaluate fired: %v", err)
	}
	if d == nil || len(d.Channels) != 1 {
		t.Fatalf("expected the window to fire, got %+v", d)
	}
	if rec.count() != 0 {
		t.Fatalf("recovery must not fire while the match still holds")
	}

	// prod still reports, but at info: the alert has recovered.
	okEnv := NewEnv("alert", "info", "t", "b", map[string]any{"env": "prod"})
	if _, err := engine.Evaluate(ctx, okEnv); err != nil {
		t.Fatalf("Evaluate miss: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("expected one recovery event, got %d", rec.count())
	}
	ev := rec.events[0]
	if ev.RuleID != "r-for" || ev.State == nil || !ev.State.Fired || ev.State.Count != 2 {
		t.Fatalf("unexpected recovery event: %+v", ev)
	}

	// A second miss reports nothing: the window is gone.
	if _, err := engine.Evaluate(ctx, okEnv); err != nil {
		t.Fatalf("Evaluate second miss: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("recovery must fire once per episode, got %d", rec.count())
	}
}

func TestEngineResolvedOtherGroupNotAffected(t *testing.T) {
	// A different group's miss must never resolve another group's alert —
	// prod recovering says nothing about staging.
	r := forTestRule("r-for", "30m")
	r.GroupBy = []string{"env"}
	engine, now := forTestEngine(t, r)
	rec := &recordingResolved{}
	engine.SetResolvedFunc(rec.onResolved)
	ctx := context.Background()

	prod := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	if _, err := engine.Evaluate(ctx, prod); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	*now = now.Add(31 * time.Minute)
	if _, err := engine.Evaluate(ctx, prod); err != nil {
		t.Fatalf("Evaluate fired: %v", err)
	}

	staging := NewEnv("alert", "info", "t", "b", map[string]any{"env": "staging"})
	if _, err := engine.Evaluate(ctx, staging); err != nil {
		t.Fatalf("Evaluate miss: %v", err)
	}
	if rec.count() != 0 {
		t.Fatalf("another group's miss must not resolve prod, got %+v", rec.events)
	}
}

func TestEngineNoResolvedForUnfiredWindow(t *testing.T) {
	// A window still in progress that resets is NOT a recovery: nothing
	// was ever delivered for the group.
	engine, _ := forTestEngine(t, forTestRule("r-for", "3m"))
	rec := &recordingResolved{}
	engine.SetResolvedFunc(rec.onResolved)
	ctx := context.Background()

	if _, err := engine.Evaluate(ctx, NewEnv("alert", "error", "t", "b", nil)); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if _, err := engine.Evaluate(ctx, NewEnv("deploy", "info", "t", "b", nil)); err != nil {
		t.Fatalf("Evaluate miss: %v", err)
	}
	if rec.count() != 0 {
		t.Fatalf("an unfired window must not report recovery, got %+v", rec.events)
	}
}

func TestEngineNilResolvedFuncIsSafe(t *testing.T) {
	engine, now := forTestEngine(t, forTestRule("r-for", "3m"))
	ctx := context.Background()
	if _, err := engine.Evaluate(ctx, NewEnv("alert", "error", "t", "b", nil)); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	*now = now.Add(4 * time.Minute)
	if _, err := engine.Evaluate(ctx, NewEnv("alert", "error", "t", "b", nil)); err != nil {
		t.Fatalf("Evaluate fired: %v", err)
	}
	if _, err := engine.Evaluate(ctx, NewEnv("deploy", "info", "t", "b", nil)); err != nil {
		t.Fatalf("Evaluate miss: %v", err)
	}
}
