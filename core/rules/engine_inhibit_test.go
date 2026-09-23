package rules

import (
	"context"
	"strings"
	"testing"
	"time"
)

// inhibitEngine seeds a root-cause rule and a suppressed (leaf) rule:
// critical alerts go to "page", everything else is suppressed for the same
// env while the root cause delivers.
func inhibitEngine(t *testing.T, ttl *string) (*Engine, Env) {
	t.Helper()
	engine := NewEngine(NewMemoryStore())
	ctx := context.Background()

	root := Rule{
		ID:    "root-down",
		Match: `level == "critical"`,
		Mode:  ModeActive,
		Route: []RouteStep{{Channels: []string{"page"}}},
	}
	leaf := Rule{
		ID:    "leaf-error",
		Match: `level == "error"`,
		Mode:  ModeActive,
		Route: []RouteStep{{Channels: []string{"oncall"}}},
		Inhibit: &InhibitSpec{
			Source: "root-down",
			Equal:  []string{"env"},
			TTL:    ttl,
		},
	}
	if err := engine.Put(ctx, &root); err != nil {
		t.Fatalf("Put root: %v", err)
	}
	if err := engine.Put(ctx, &leaf); err != nil {
		t.Fatalf("Put leaf: %v", err)
	}
	return engine, Env{}
}

func TestEngineInhibitSuppressesWhileSourcePresent(t *testing.T) {
	engine, _ := inhibitEngine(t, nil)
	ctx := context.Background()
	prodErr := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	stagingErr := NewEnv("alert", "error", "t", "b", map[string]any{"env": "staging"})

	// Without the root cause, the leaf delivers normally.
	d, err := engine.Evaluate(ctx, prodErr)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Inhibited || len(d.Channels) != 1 {
		t.Fatalf("expected normal delivery without root cause, got %+v", d)
	}

	// Root cause fires for env=prod: presence is recorded.
	rootEnv := NewEnv("alert", "critical", "t", "b", map[string]any{"env": "prod"})
	d, err = engine.Evaluate(ctx, rootEnv)
	if err != nil {
		t.Fatalf("Evaluate root: %v", err)
	}
	if d == nil || d.RuleID != "root-down" || len(d.Channels) != 1 {
		t.Fatalf("expected root delivery, got %+v", d)
	}

	// The leaf for the same env is now inhibited.
	d, err = engine.Evaluate(ctx, prodErr)
	if err != nil {
		t.Fatalf("Evaluate inhibited: %v", err)
	}
	if d == nil || !d.Inhibited || d.RuleID != "leaf-error" || len(d.Channels) != 0 {
		t.Fatalf("expected inhibited decision from leaf-error, got %+v", d)
	}

	// A different env is not suppressed (equal fields scope the suppression).
	d, err = engine.Evaluate(ctx, stagingErr)
	if err != nil {
		t.Fatalf("Evaluate staging: %v", err)
	}
	if d == nil || d.Inhibited || len(d.Channels) != 1 {
		t.Fatalf("staging must deliver while only prod is present, got %+v", d)
	}
}

func TestEngineInhibitPresenceExpires(t *testing.T) {
	ttl := "5ms"
	engine, _ := inhibitEngine(t, &ttl)
	ctx := context.Background()
	prodErr := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	rootEnv := NewEnv("alert", "critical", "t", "b", map[string]any{"env": "prod"})

	if _, err := engine.Evaluate(ctx, rootEnv); err != nil {
		t.Fatalf("Evaluate root: %v", err)
	}
	if d, _ := engine.Evaluate(ctx, prodErr); d == nil || !d.Inhibited {
		t.Fatalf("expected inhibition right after root delivery, got %+v", d)
	}
	// After the TTL the presence entry is gone and the leaf delivers again.
	time.Sleep(20 * time.Millisecond)
	d, err := engine.Evaluate(ctx, prodErr)
	if err != nil {
		t.Fatalf("Evaluate after ttl: %v", err)
	}
	if d == nil || d.Inhibited || len(d.Channels) != 1 {
		t.Fatalf("expected delivery after presence expiry, got %+v", d)
	}
}

func TestEngineInhibitSuppressesBeforeGroupOpens(t *testing.T) {
	engine, _ := inhibitEngine(t, nil)
	ctx := context.Background()

	// The leaf also aggregates by env: an inhibited event must not open a
	// group round (suppression is checked before any group state).
	leaf, _ := engine.Get(ctx, "leaf-error")
	leaf.GroupBy = []string{"env"}
	if err := engine.Put(ctx, &leaf); err != nil {
		t.Fatalf("Put leaf+group: %v", err)
	}

	rootEnv := NewEnv("alert", "critical", "t", "b", map[string]any{"env": "prod"})
	if _, err := engine.Evaluate(ctx, rootEnv); err != nil {
		t.Fatalf("Evaluate root: %v", err)
	}
	prodErr := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	d, err := engine.Evaluate(ctx, prodErr)
	if err != nil {
		t.Fatalf("Evaluate leaf: %v", err)
	}
	if d == nil || !d.Inhibited {
		t.Fatalf("expected inhibited decision, got %+v", d)
	}
	key, _ := RuleGroupKey([]string{"env"}, prodErr)
	if state, _ := engine.groupState.store.Get(ctx, stateGroupKey("leaf-error", key)); state != nil {
		t.Errorf("inhibited event must not open a group round, got %+v", state)
	}
}

func TestEngineInhibitShadowTargetNotSuppressed(t *testing.T) {
	engine, _ := inhibitEngine(t, nil)
	ctx := context.Background()

	leaf, _ := engine.Get(ctx, "leaf-error")
	leaf.Mode = ModeShadow
	if err := engine.Put(ctx, &leaf); err != nil {
		t.Fatalf("Put leaf shadow: %v", err)
	}

	rootEnv := NewEnv("alert", "critical", "t", "b", map[string]any{"env": "prod"})
	if _, err := engine.Evaluate(ctx, rootEnv); err != nil {
		t.Fatalf("Evaluate root: %v", err)
	}
	// Shadow observation records condition hits regardless of suppression —
	// the dry-run preview shows what the rule WOULD have matched.
	d, err := engine.Evaluate(ctx, NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"}))
	if err != nil {
		t.Fatalf("Evaluate leaf shadow: %v", err)
	}
	if d == nil || len(d.Shadow) != 1 || d.Shadow[0].RuleID != "leaf-error" {
		t.Fatalf("expected unsuppressed shadow record, got %+v", d)
	}
}

func TestEngineInhibitInertWithoutSourceDelivery(t *testing.T) {
	engine, _ := inhibitEngine(t, nil)
	ctx := context.Background()

	// A critical that no rule routes (no match) never marks presence.
	// Here the root rule exists but only matches level==critical with the
	// same env; an error alone leaves no presence behind.
	prodErr := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	d, err := engine.Evaluate(ctx, prodErr)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if d == nil || d.Inhibited {
		t.Fatalf("expected delivery, got %+v", d)
	}
}

func TestEngineInhibitReadFailureFailsOpen(t *testing.T) {
	engine, _ := inhibitEngine(t, nil)
	failing := &failingStateStore{inner: engine.inhibitState.store, failOb: true}
	engine.SetStateStore(failing)
	ctx := context.Background()
	prodErr := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})

	// A presence-read outage must degrade to "rule skipped", not "event
	// dropped silently" and not a routing failure.
	d, err := engine.Evaluate(ctx, prodErr)
	if d != nil {
		t.Fatalf("expected nil decision on state failure, got %+v", d)
	}
	if err == nil || !strings.Contains(err.Error(), "state store down") {
		t.Fatalf("expected joined eval error naming the outage, got %v", err)
	}
}

func TestEngineInhibitPresenceWriteFailureKeepsDelivery(t *testing.T) {
	engine, _ := inhibitEngine(t, nil)
	failing := &failingStateStore{inner: engine.inhibitState.store, failPut: true}
	engine.SetStateStore(failing)
	ctx := context.Background()
	rootEnv := NewEnv("alert", "critical", "t", "b", map[string]any{"env": "prod"})

	// A presence-write failure must not block the root cause's own
	// delivery (fail open toward delivery); it surfaces as an eval error.
	d, err := engine.Evaluate(ctx, rootEnv)
	if d == nil || d.RuleID != "root-down" || len(d.Channels) != 1 {
		t.Fatalf("expected root delivery despite write failure, got %+v", d)
	}
	if err == nil || !strings.Contains(err.Error(), "state store down") {
		t.Fatalf("expected an eval error for the failed presence write, got %v", err)
	}
	if len(d.EvalErrors) != 1 || d.EvalErrors[0].RuleID != "leaf-error" {
		t.Fatalf("expected the eval error attributed to the target rule, got %+v", d.EvalErrors)
	}
}

func TestEngineInhibitForwardReferenceWiresOnLaterSource(t *testing.T) {
	ctx := context.Background()
	engine := NewEngine(NewMemoryStore())

	// The leaf is created first and names a source that does not exist yet.
	leaf := Rule{
		ID:    "leaf-error",
		Match: `level == "error"`,
		Mode:  ModeActive,
		Route: []RouteStep{{Channels: []string{"oncall"}}},
		Inhibit: &InhibitSpec{
			Source: "root-down",
			Equal:  []string{"env"},
		},
	}
	if err := engine.Put(ctx, &leaf); err != nil {
		t.Fatalf("Put leaf: %v", err)
	}
	prodErr := NewEnv("alert", "error", "t", "b", map[string]any{"env": "prod"})
	if d, _ := engine.Evaluate(ctx, prodErr); d == nil || d.Inhibited {
		t.Fatalf("expected delivery while source is absent, got %+v", d)
	}

	// Adding the source wires the suppression in.
	root := Rule{
		ID:    "root-down",
		Match: `level == "critical"`,
		Mode:  ModeActive,
		Route: []RouteStep{{Channels: []string{"page"}}},
	}
	if err := engine.Put(ctx, &root); err != nil {
		t.Fatalf("Put root: %v", err)
	}
	if _, err := engine.Evaluate(ctx, rootEnvWithEnv("prod")); err != nil {
		t.Fatalf("Evaluate root: %v", err)
	}
	if d, _ := engine.Evaluate(ctx, prodErr); d == nil || !d.Inhibited {
		t.Fatalf("expected inhibition after source was added, got %+v", d)
	}
}

func rootEnvWithEnv(env string) Env {
	return NewEnv("alert", "critical", "t", "b", map[string]any{"env": env})
}
